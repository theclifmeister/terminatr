package tui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// The info panel (docs/SPEC.md §4): right of a thread's pane, what the
// thread is doing: its task with the steps, its state and progress, its
// PR and the checks, its last report, its branch. Its layout is the
// view's (view.Info), since it takes columns from the pane; what it shows
// is read here, with the sidebar's poll, from the project folder and the
// ticker's state. It is read-only: a click on the task opens the task
// view over the session, a click on the PR opens it in the browser, and
// a click anywhere else gives it the keyboard, whose arrows scroll it.
// This file has what it shows and its layout; the attach view's client
// draws it and handles its keys and clicks (attachinfo.go).

// infoData is what the panel shows about one thread.
type infoData struct {
	slug    string
	session proto.SessionInfo
	rec     *thread.Record
	status  *thread.Status
	report  *thread.Report
	task    *tasks.Task
	pr      ticker.PR
	// attached: the names of the files its reports attached for the
	// user. Names only: the UI shows no file paths (§4).
	attached []string
	// refused are the guard's latest refusals of the thread, oldest
	// first, from the journal.
	refused []refusal
	// watch is a coordinator's project as the /tm pane shows it
	// (project.watch); set only beside a coordinator, which has none of
	// the above but slug and session.
	watch *proto.ProjectWatch
	// sections are the mods' coordinator sections, as mod.views last
	// listed them (modsections.go); beside a coordinator only.
	sections []proto.ModView
	// remote is a paired server's pane's, from the mirror
	// (remotepanel.go); none of the above but slug ("@alias/slug") and
	// session then.
	remote *remoteData
}

// loadInfo reads the panel's data for session s, one of sessions: a
// thread's, or a coordinator's (its project, coordpanel.go); nil for any
// other.
func loadInfo(paths tmclient.Paths, s proto.SessionInfo, sessions []proto.SessionInfo) *infoData {
	if s.Role == proto.RoleCoordinator && s.Project != "" {
		return loadCoordInfo(paths, s, sessions)
	}
	if s.Role != proto.RoleThread || s.Project == "" || !thread.ValidID(s.Thread) {
		return nil
	}
	d := &infoData{slug: s.Project, session: s}
	p, err := project.Open(s.Project)
	if err != nil {
		return d
	}
	d.rec, _ = thread.Load(p, s.Thread)
	d.status, _ = thread.ReadStatus(p, s.Thread)
	d.report, _ = thread.ReadReport(p, s.Thread)
	d.attached = thread.Attachments(p, s.Thread)
	d.refused = guardRefusals(p, s.Thread, maxRefusals)
	if d.rec != nil && d.rec.TaskID() > 0 {
		d.task, _ = p.Tasks().Get(d.rec.TaskID())
	}
	if paths.Sessions != "" {
		d.pr = ticker.PRs(ticker.StatePath(paths.Sessions), s.Project)[s.Thread]
	}
	return d
}

// infoHit is what a click on a panel line does: its kind, and the task,
// session or address it opens.
type infoHit struct {
	kind    hitKind
	task    int    // hitTask: the task's number
	session string // hitSession: the session to show
	url     string // hitPR: the PR's address
	sec     string // hitSection: the mod section's fold id
}

// hitKind is the kind of an infoHit.
type hitKind int

const (
	hitNone      hitKind = iota
	hitTask              // the task view over the session
	hitPR                // the PR in the browser
	hitSession           // that session in the pane (a coordinator's thread row)
	hitQuestions         // the coordinator opens its questions (tm ask open)
	hitSection           // a mod section's header or body (panelsections.go)
)

// prURL is the thread's PR's address: the ticker's, else its report's.
func (d *infoData) prURL() string {
	if d.pr.URL != "" {
		return d.pr.URL
	}
	if d.report != nil && strings.HasPrefix(d.report.PR, "https://") {
		return d.report.PR
	}
	return ""
}

// prLine is the PR's state in the ticker's words ("#70 open, checks
// pending"), with whether its branch is behind its base; "" without one.
func (d *infoData) prLine() string {
	s := d.pr.Summary()
	if s == "" {
		if n := ticker.PRNumber(d.prURL()); n > 0 {
			s = "#" + strconv.Itoa(n)
		}
	}
	if s != "" && d.pr.State == "OPEN" && d.pr.MergeState == "BEHIND" {
		s += ", behind " + cmp.Or(d.pr.Base, "its base")
	}
	return s
}

// infoLines draws the panel's content w cells wide (its border not
// included), with what a click on each line does. now dates the ages.
func infoLines(d *infoData, w int, now time.Time) ([]string, []infoHit) {
	lay := infoLay(d, w, now, panelOpts{})
	return lay.lines, lay.hits
}

// infoLay is the panel's content w cells wide with the mods' sections
// placed as o says: beside a coordinator its coordinator sections,
// beside a thread the details sections for it, as the dashboard's
// details panel shows them on the thread's row.
func infoLay(d *infoData, w int, now time.Time, o panelOpts) panelLay {
	if d == nil {
		pl := &panel{w: w}
		pl.add(th().Faint.Render("no thread here"))
		return panelLay{lines: pl.lines}
	}
	if d.remote != nil {
		// No mod sections: mods are per server.
		pl, hits := remotePanel(d, w, now)
		return layPanel(pl, hits, nil, infoTarget(d), o)
	}
	if d.watch != nil {
		pl, hits := coordPanel(d.watch, w, now)
		return layPanel(pl, hits, d.sections, infoTarget(d), o)
	}
	pl, hits := threadInfo(d, w, now)
	return layPanel(pl, hits, d.sections, infoTarget(d), o)
}

// threadInfo is a thread's panel w cells wide, with what a click on
// each line does: its head (the task's title and state) then core's
// block, named core; what needs you, its question and a red PR line are
// must-see.
func threadInfo(d *infoData, w int, now time.Time) (*panel, []infoHit) {
	pl := &panel{w: w}
	var hits []infoHit
	// hit makes the lines added since line from clickable.
	hit := func(from int, h infoHit) {
		for len(hits) < len(pl.lines) {
			hits = append(hits, infoHit{})
		}
		for i := from; i < len(hits); i++ {
			hits[i] = h
		}
	}
	s := d.session
	id := s.Thread
	// The task, with its steps; the one under way marked.
	switch t := d.task; {
	case t != nil:
		pl.wrap(th().Title.Render(oneLine(t.Ref() + " " + t.Title)))
		g, st := stateLook(string(t.Status))
		pl.add(st.Render(strings.TrimSpace(g+" "+string(t.Status))) + th().Faint.Render(" · click to open"))
		hit(0, infoHit{kind: hitTask, task: t.ID})
		pl.block("core")
		if len(t.Steps) > 0 {
			pl.section("STEPS", "")
			pl.add(stepsProgress(t))
			cur := true
			for _, step := range t.Steps {
				glyph, text := todoGlyph("pending"), oneLine(step.Text)
				switch {
				case step.Done:
					glyph = todoGlyph("done")
					text = th().Faint.Render(text)
				case cur:
					glyph, text, cur = todoGlyph("in_progress"), th().Accent.Render(text), false
				}
				hang(pl, glyph+" ", text)
			}
		}
	case d.rec != nil:
		pl.wrap(th().Title.Render(oneLine(d.rec.Title)))
		pl.add(th().Faint.Render("no task"))
	}
	if pl.starts == nil {
		pl.block("core")
	}
	// The thread: its state, progress, what it does now, what it waits on.
	pl.section("THREAD", "")
	g, st := stateLook(s.State)
	pl.field("thread", id+" "+st.Render(strings.TrimSpace(g+" "+oneLine(sessionState(s)))))
	if d.rec != nil && d.rec.Model != "" {
		pl.field("model", oneLine(d.rec.Model))
	}
	if d.rec != nil {
		pl.field("usage", d.rec.Usage.String())
	}
	if ts := d.status; ts != nil {
		if d.task == nil || len(d.task.Steps) == 0 {
			// A task with steps shows its progress above, once.
			pl.field("progress", progressLine(ts.Progress()))
		}
		pl.field("now", nowWords(ts))
		if ts.NeedsYou != "" {
			from := len(pl.lines)
			hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "needs you"))+" ", th().Warn.Render(oneLine(ts.NeedsYou)))
			pl.mustFrom(from)
		}
	}
	if q := s.Question; q != nil {
		from := len(pl.lines)
		pl.section("QUESTION OPEN", age(now.Sub(q.Since))+" ago")
		for _, l := range questionLines(q) {
			pl.wrap(l)
		}
		pl.mustFrom(from)
		pl.gap()
	}
	// The PR and its checks.
	if l := d.prLine(); l != "" {
		look, bad := th().Accent, false
		switch {
		case d.pr.Checks == "fail" || d.pr.Mergeable == "CONFLICTING":
			look, bad = th().Bad, true
		case d.pr.State == "MERGED":
			look = th().Good
		}
		from := len(pl.lines)
		hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "PR"))+" ", look.Render(l))
		if d.prURL() != "" {
			hit(from, infoHit{kind: hitPR, url: d.prURL()})
		}
		if bad {
			pl.mustFrom(from)
		}
	}
	// The last report: its first lines. Its Next items are the
	// coordinator's and never show in the TUI.
	if r := d.report; r != nil {
		when := ""
		if d.rec != nil && !d.rec.ReportAt.IsZero() {
			when = age(now.Sub(d.rec.ReportAt)) + " ago"
		}
		pl.section("LAST REPORT", when)
		for _, l := range reportHead(r.Text, 3) {
			pl.wrap(oneLine(l))
		}
	}
	if len(d.attached) > 0 {
		names := make([]string, len(d.attached))
		for i, n := range d.attached {
			names[i] = oneLine(n)
		}
		hang(pl, th().Faint.Render(fmt.Sprintf("%-9s", "attached"))+" ", strings.Join(names, ", "))
	}
	// What the guard refused: journaled, not sent to the inbox.
	if len(d.refused) > 0 {
		pl.gap()
		pl.add(th().Head.Render("Guard refused"))
		for _, r := range d.refused {
			hang(pl, th().Faint.Render(age(now.Sub(r.at))+" ago")+" ", th().Warn.Render(oneLine(r.what)))
		}
	}
	// Where it works.
	pl.gap()
	if rec := d.rec; rec != nil {
		pl.field("branch", oneLine(rec.Branch))
		pl.field("worktree", oneLine(homeShort(rec.Worktree)))
	}
	if at := d.lastActive(); !at.IsZero() {
		pl.field("active", age(now.Sub(at))+" ago")
	}
	for len(hits) < len(pl.lines) {
		hits = append(hits, infoHit{})
	}
	return pl, hits
}

// maxRefusals is how many of the guard's refusals the panel lists.
const maxRefusals = 3

// refusal is one guard.deny line of the journal.
type refusal struct {
	at   time.Time
	what string // "<rule> <tool>: <summary>"
}

// guardRefusals is the last n guard.deny lines the journal holds for
// thread id, oldest first.
func guardRefusals(p *project.Project, id string, n int) []refusal {
	lines, _, err := p.JournalTail(500)
	if err != nil {
		return nil
	}
	var out []refusal
	for _, l := range lines {
		// "<time> <who> guard.deny <ref> <detail>"
		f := strings.SplitN(l, " ", 5)
		if len(f) < 5 || f[2] != "guard.deny" || f[3] != id {
			continue
		}
		at, err := time.Parse(time.RFC3339, f[0])
		if err != nil {
			continue
		}
		out = append(out, refusal{at: at, what: f[4]})
	}
	return out[max(len(out)-n, 0):]
}

// lastActive is when the thread last did something it tells tm about:
// its status or its last report.
func (d *infoData) lastActive() time.Time {
	var at time.Time
	if d.status != nil {
		at = d.status.Updated
	}
	if d.rec != nil && d.rec.ReportAt.After(at) {
		at = d.rec.ReportAt
	}
	return at
}

// hang adds lead then text wrapped to the panel, its next lines under
// the text.
func hang(pl *panel, lead, text string) {
	lw := ansi.StringWidth(lead)
	for i, l := range strings.Split(ansi.Wordwrap(text, max(pl.w-2-lw, 8), ""), "\n") {
		if i == 0 {
			pl.add(lead + l)
		} else {
			pl.add(strings.Repeat(" ", lw) + l)
		}
	}
}

// reportHead is the first n non-empty lines of a report's ## Report
// section.
func reportHead(text string, n int) []string {
	var out []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "## "):
			if in {
				return out
			}
			in = strings.EqualFold(t, "## Report")
		case in && t != "" && len(out) < n:
			out = append(out, t)
		}
	}
	return out
}

// homeShort writes a path under the home directory with ~.
func homeShort(p string) string {
	home, _ := os.UserHomeDir()
	return shortHome(p, home)
}

// shortHome writes p with ~ for home when p is home or under it.
func shortHome(p, home string) string {
	rel, ok := fsx.Rel(home, p)
	switch {
	case !ok:
		return p
	case rel == ".":
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

// infoPanel is the attach client's info panel: what it shows and this
// console's own state of it. Guarded by client.mu.
type infoPanel struct {
	data  *infoData
	lines []string  // the content, as last laid out
	hits  []infoHit // what a click on each content line does
	top   int       // the first content line shown
	drag  bool      // the mouse is moving its border
	drawn []string  // the lines on screen; nil repaints them all
	// lay is the content with the mods' sections placed, sel the
	// selected section header's fold id ("" for none), cache their lines;
	// coordOrder and detOrder are the layout's coordinator.sections and
	// details.sections.
	lay                  panelLay
	sel                  string
	cache                sectionCache
	coordOrder, detOrder []string
}
