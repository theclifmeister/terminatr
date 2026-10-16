package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// The coordinator's info panel (docs/SPEC.md §4, T90): beside a
// coordinator's pane, the info panel shows its project as the /tm mod
// pane does, from the same state (server.ProjectWatchOf, what
// project.watch sends): what needs the user first, then the inbox, the
// threads, the coordinator's context use and the tasks on deck. Its
// width, toggle and keyboard are the thread panel's (infopanel.go). It
// has no buttons: a click on a task opens the task view over the
// session, on a running thread shows that thread's pane, on a PR opens
// it in the browser; the user asks the coordinator for the rest.

// coordReady bounds the tasks on deck the panel lists, as /tm does.
const coordReady = 5

// loadCoordInfo reads the panel's data for coordinator session s, with
// the poll's sessions.
func loadCoordInfo(paths tmclient.Paths, s proto.SessionInfo, sessions []proto.SessionInfo) *infoData {
	d := &infoData{slug: s.Project, session: s}
	p, err := project.Open(s.Project)
	if err != nil {
		d.watch = &proto.ProjectWatch{Project: s.Project}
		return d
	}
	ts := ""
	if paths.Sessions != "" {
		ts = ticker.StatePath(paths.Sessions)
	}
	w := server.ProjectWatchOf(p, sessions, ts)
	d.watch = &w
	return d
}

// whyWords say why a need waits, short, as /tm says it.
var whyWords = map[string]string{
	proto.WhyQueue:    "prompts held",
	proto.WhyReview:   "in review",
	proto.WhyQuestion: "asks you",
	proto.WhyCI:       "checks failed",
	proto.WhyBlocked:  "blocked",
}

// whyState is the state whose glyph a need's words get.
var whyState = map[string]string{
	proto.WhyQueue:    "blocked",
	proto.WhyReview:   "review",
	proto.WhyQuestion: "blocked",
	proto.WhyCI:       "blocked",
	proto.WhyBlocked:  "blocked",
}

// coordPanel is coordLines' panel, with core's blocks named
// (mod.CoordinatorBlocks: needs, inbox, threads, deck) and its head and
// NEEDS YOU must-see, for the mods' sections placed among them.
func coordPanel(pw *proto.ProjectWatch, w int, now time.Time) (*panel, []infoHit) {
	return watchPanel(pw, w, now, false)
}

// watchPanel is coordPanel; readOnly draws it for a paired server's
// project (peerdash.go), with no hint of a key or a command, which
// would act here, not there.
func watchPanel(pw *proto.ProjectWatch, w int, now time.Time, readOnly bool) (*panel, []infoHit) {
	pl := &panel{w: w}
	var hits []infoHit
	// line adds text as one line, h what a click on it does.
	line := func(text string, h infoHit) {
		pl.add(text)
		hits = padHits(hits, len(pl.lines), h)
	}
	// wrapped adds lead then text wrapped under it, all lines doing h.
	wrapped := func(lead, text string, h infoHit) {
		hang(pl, lead, text)
		hits = padHits(hits, len(pl.lines), h)
	}
	// item adds an item's text wrapped, its lines after the first
	// indented as its detail lines are (docs/STYLE.md S3), all doing h.
	item := func(text string, h infoHit) {
		for i, l := range strings.Split(ansi.Wordwrap(text, max(pl.w-4, 8), ""), "\n") {
			if i > 0 {
				l = "  " + l
			}
			pl.add(l)
		}
		hits = padHits(hits, len(pl.lines), h)
	}
	gap := func() {
		pl.gap()
		hits = padHits(hits, len(pl.lines), infoHit{})
	}

	line(th().Title.Render(oneLine(pw.Project)), infoHit{})
	line(th().Faint.Render(coordSummary(pw)), infoHit{})
	// Core's warning, whatever the ticker mod does (on, off, failed or
	// in the sidebar): PR polls fail, and the inbox says why. The ticker
	// mod's section draws the ticker's times.
	if prHostFailing(pw.Ticker) {
		line(th().Bad.Render(strings.TrimSpace(ic().blocked+" PR host failing"))+th().Faint.Render(" · inbox"), infoHit{})
	}
	if c := pw.Context; c != nil {
		text, look, hint := contextWords(c)
		line(look.Render(text), infoHit{})
		if hint != "" {
			wrapped("", look.Render(hint), infoHit{})
		}
	}

	pl.mustFrom(0)

	// What waits for the user.
	// heading starts a section.
	heading := func(title string) {
		pl.section(title, "")
		hits = padHits(hits, len(pl.lines), infoHit{})
	}
	pl.block("needs")
	needs := len(pl.lines)
	if len(pw.NeedsYou) == 0 && pw.Questions == 0 {
		gap()
		line(th().Faint.Render("Nothing waits for you."), infoHit{})
	} else {
		heading("NEEDS YOU")
	}
	// The coordinator's open questions (tm ask): a click, or a, has it
	// open them in its question dialog.
	if pw.Questions > 0 {
		h := infoHit{kind: hitQuestions}
		g, _ := stateLook("blocked")
		text := th().Warn.Render(g + " " + project.QuestionsWaiting(pw.Questions))
		if !readOnly {
			text += th().Faint.Render(" · a or click answers")
		}
		item(text, h)
	}
	for _, n := range pw.NeedsYou {
		h := taskHit(n.Task)
		// What went wrong in red, what waits on the user in yellow
		// (docs/STYLE.md, colour roles), as the /tm pane draws them.
		look := th().Bad
		if n.Why == proto.WhyReview || n.Why == proto.WhyQuestion {
			look = th().Warn
		}
		ref := n.Task
		if ref == "" {
			ref = n.Thread
		}
		if ref == "" {
			ref = n.Session
		}
		g, _ := stateLook(whyState[n.Why])
		item(th().Head.Render(ref)+" "+look.Render(strings.TrimSpace(g+" "+whyWords[n.Why]))+" "+oneLine(n.Title), h)
		switch {
		case n.Why == proto.WhyQueue:
			what := "the coordinator hears of nothing"
			if n.Thread != "" {
				what = "the thread gets nothing"
			}
			why := "; tm agent explain " + n.Session + " says why"
			if readOnly {
				why = ""
			}
			wrapped("  ", th().Bad.Render(what+" until it clears"+why), infoHit{})
		case n.Question != "":
			wrapped("  ", th().Warn.Render("“"+oneLine(n.Question)+"”"), h)
		case n.PR != "":
			look := th().Faint
			if n.Why == proto.WhyCI || prBad(n.PR) {
				look = th().Bad
			}
			ph := h
			if n.PRURL != "" {
				ph = infoHit{kind: hitPR, url: n.PRURL}
			}
			wrapped("  ", look.Render(n.PR), ph)
		}
		if n.Asked != "" {
			line(th().Faint.Render("  asked the coordinator: "+kindWord(n.Asked)), h)
		}
	}

	if len(pw.NeedsYou) > 0 || pw.Questions > 0 {
		pl.mustFrom(needs)
	}

	// The inbox, a row per kind per subject.
	pl.block("inbox")
	if len(pw.Inbox) > 0 {
		heading("INBOX")
	}
	for _, it := range pw.Inbox {
		var parts []string
		if it.Count > 1 {
			parts = append(parts, "×"+strconv.Itoa(it.Count))
		}
		for _, s := range []string{it.Task, oneLine(it.What)} {
			if s != "" {
				parts = append(parts, s)
			}
		}
		text := strings.Join(parts, " ")
		if it.Title != "" {
			text += th().Faint.Render(" " + oneLine(it.Title))
		}
		line(kindStyle(it.Kind).Render(kindWord(it.Kind))+" "+text, taskHit(it.Task))
	}

	// The threads, compact: a click on a running one shows its pane.
	pl.block("threads")
	if len(pw.Threads) > 0 {
		heading("THREADS")
	}
	for _, t := range pw.Threads {
		state := watchThreadState(t)
		g, st := stateLook(state)
		if state == "asks you" {
			g, st = stateLook("blocked")
		}
		text := t.ID
		if t.Task != nil {
			text += " " + t.Task.ID
		}
		text += " " + st.Render(strings.TrimSpace(g+" "+state))
		if t.Task != nil && t.Task.StepsTotal > 0 {
			text += fmt.Sprintf(" %d/%d", t.Task.StepsDone, t.Task.StepsTotal)
		}
		var h infoHit
		if t.Session != "" && !t.Done {
			h = infoHit{kind: hitSession, session: t.Session}
		} else {
			text = th().Faint.Render(text)
		}
		line(text, h)
		switch {
		case t.Task != nil && t.Task.Current != "":
			line(th().Faint.Render("  ▸ ")+oneLine(t.Task.Current), h)
		case t.Task != nil:
			line(th().Faint.Render("  "+oneLine(t.Task.Title)), h)
		default:
			line(th().Faint.Render("  "+oneLine(t.Title)), h)
		}
		if t.PR != "" {
			look := th().Faint
			if t.PRBad {
				look = th().Bad
			}
			ph := h
			if t.PRURL != "" {
				ph = infoHit{kind: hitPR, url: t.PRURL}
			}
			pr := t.PR
			if c := proto.CheckedWords(t.PRChecked, now); c != "" {
				pr += " · " + c
			}
			wrapped("  ", look.Render(pr), ph)
		}
	}

	// The tasks on deck.
	pl.block("deck")
	if len(pw.Ready) > 0 {
		heading("ON DECK")
	}
	for i, t := range pw.Ready {
		if i == coordReady {
			line(th().Faint.Render(fmt.Sprintf("  and %d more", len(pw.Ready)-coordReady)), infoHit{})
			break
		}
		g, st := stateLook(t.Status)
		text := st.Render(strings.TrimSpace(g+" "+t.Status)) + " " + oneLine(t.Title)
		if t.Asked != "" {
			text += th().Faint.Render(" · asked the coordinator: " + kindWord(t.Asked))
		}
		item(th().Head.Render(t.Task)+" "+text, taskHit(t.Task))
	}

	hits = padHits(hits, len(pl.lines), infoHit{})
	return pl, hits
}

// prHostFailing says a project's PR polls fail: its ticker says so and
// it has a PR host (watchTicker's rule).
func prHostFailing(tk *proto.WatchTicker) bool {
	return tk != nil && tk.GHFailing && !tk.NoPRHost
}

// coordSummary is the panel's second line: "3 need you · 1 in inbox · 5
// threads", as /tm's title says it.
func coordSummary(pw *proto.ProjectWatch) string {
	n := len(pw.NeedsYou) + pw.Questions
	parts := []string{fmt.Sprintf("%d need%s you", n, plural(n == 1, "s"))}
	if len(pw.Inbox) > 0 {
		parts = append(parts, fmt.Sprintf("%d in inbox", len(pw.Inbox)))
	}
	parts = append(parts, fmt.Sprintf("%d thread%s", len(pw.Threads), plural(len(pw.Threads) != 1, "s")))
	return strings.Join(parts, " · ")
}

func plural(on bool, s string) string {
	if on {
		return s
	}
	return ""
}

// contextWords is the coordinator's context use, "context 84k / 200k ·
// 42%", its look (faint below the threshold, yellow from it, red from
// 80%, as the sidebar's), and the /clear hint past the threshold.
func contextWords(c *proto.WatchContext) (text string, look lipgloss.Style, hint string) {
	text = fmt.Sprintf("context %s / %s · %d%%", kTokens(c.Tokens), kTokens(c.Window), c.Percent)
	switch {
	case c.Percent >= 80:
		look = th().Bad
	case c.Hint:
		look = th().Warn
	default:
		look = th().Faint
	}
	if c.Hint {
		hint = "consider /clear: the context lives in files (tm context)"
	}
	return text, look, hint
}

// kTokens writes a token count short: 840, 84k, 1.2M.
func kTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strconv.FormatInt(n, 10)
}

// watchThreadState is a thread's state in a word, as /tm says it:
// "done" once it called tm done, "stopped" with no session, "asks" for a
// question.
func watchThreadState(t proto.WatchThread) string {
	switch {
	case t.Done:
		return "done"
	case t.Session == "":
		return "stopped"
	case t.State == "blocked" && t.Reason == "question":
		return "asks you"
	case t.State != "":
		return t.State
	}
	return "running"
}

// prBad reports a PR the thread has to act on, in the ticker's words.
func prBad(pr string) bool {
	for _, w := range []string{"failed", "conflicts", "changes requested", "behind"} {
		if strings.Contains(pr, w) {
			return true
		}
	}
	return false
}

// taskHit opens task ref ("T12") in the task view; none for "".
func taskHit(ref string) infoHit {
	if id, ok := tasks.ParseRef(ref); ok {
		return infoHit{kind: hitTask, task: id}
	}
	return infoHit{}
}
