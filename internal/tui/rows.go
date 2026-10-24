package tui

import (
	"cmp"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Rows: what the dashboard list shows, as data. Layout and styling
// happen when a row is drawn (text), not when it is built.

// row is one dashboard line: a section header, an unselectable note, or
// an item with columns.
type row struct {
	key      string // selection identity, stable across refreshes
	head     string // a section header: NEEDS YOU, the project, OTHER SESSIONS
	headNote string // a project header's state label: active or inactive
	paused   bool   // a project header: the project is paused
	note     string // an unselectable line: task counts, "no projects; …"
	// counts marks the project's task counts' note; headTasks has them
	// on its header instead, with rows.density = compact (compactRows).
	counts    bool
	headTasks string

	// An item's columns: its marker (" ! ", indent), who (project slug or
	// session id; rows of the project's own section have none), what
	// (coordinator, thread, command), its state word and the rest of the
	// line. lead starts the rest in a colour of its own: a block's
	// reason, or a new report.
	mark, who, what, state, lead, rest string
	leadBad                            bool // lead is a block's reason, else a note in the warning colour
	// pct is the progress bar's percent, -1 for none.
	pct int
	// count is a section header's number of items.
	count int
	// whoW is who's width, 0 for colWho; whatMin is how narrow what may
	// get to leave the lead room, 0 for colWhatMin. The task list's rows
	// set both: an id is short, and the title shows whole when opened.
	whoW, whatMin int

	session string
	project string
	thread  *ThreadRow
	task    *tasks.Task // a needs-you task's row in NEEDS YOU
	// questions is the coordinator's open questions on its row in NEEDS
	// YOU: enter has it open them.
	questions int
	// links is what a link of the project's (other, link.Row.Peer)
	// waits on in NEEDS YOU, linkNeed its kind (needHeld …): enter opens
	// a held message's or a file's dialog, and the Network popup on a
	// request or a paused link.
	links    int
	other    string
	linkNeed int
	// network is NEEDS YOU's header's count of what other projects'
	// links need and the share requests that wait, the Network popup's.
	network int
	// netAt is the Network popup's row a share request's row in the
	// all-projects NEEDS YOU opens it at (shareNeedRows).
	netAt string
	// remote is a paired server's project ("@alias/slug", peerFold) on
	// its row in the all-projects NEEDS YOU: enter shows its read-only
	// dashboard.
	remote string
	// card: the row is a mod's dashboard card's header (modviews.go),
	// its key the card's.
	card bool
	// mods are the mods' cells in their dashboard columns, after the
	// state (rowMods).
	mods []rowCell
}

// rowCell is a mod's cell on a dashboard row: its spans, as wide as
// its column.
type rowCell struct {
	spans []widget.Span
	w     int
}

// rowMods are a row's cells in the mod columns shown in slot, the first
// of targets that has one in each; blank cells keep the columns lined
// up down the rows of the slot.
func rowMods(mc *modCells, slot string, targets ...proto.CellTarget) []rowCell {
	var out []rowCell
	for _, c := range mc.in(slot) {
		out = append(out, rowCell{mc.cell(c.ID, targets...), min(c.Width, view.MaxModColumnWidth)})
	}
	return out
}

func (r row) selectable() bool { return r.key != "" }

// widths are an item's column widths in a list w cells wide: who (0 for
// none), what and the state. what takes a share of the room, so wide
// lists show whole titles and the rest keeps room for progress.
func (r row) widths(w int) (who, what, state int) {
	state = colState
	if r.who != "" {
		who = cmp.Or(r.whoW, colWho)
	}
	room := w - len([]rune(r.mark)) - state - colGap
	for _, c := range r.mods {
		room -= c.w + colGap
	}
	if who > 0 {
		room -= who + colGap
	}
	what = min(max(room*9/20, colWhatMin), colWhatMax)
	if lead := ansi.StringWidth(r.lead); lead > 0 {
		// The lead (a block's reason, waiting on the coordinator) shows
		// whole, and the "…" of a cut rest after it: what gives way,
		// down to its minimum.
		what = max(min(what, room-colGap-lead-2), cmp.Or(r.whatMin, colWhatMin))
	}
	return who, what, state
}

// text is the row laid out in columns for w cells, unstyled.
func (r row) text(w int) string {
	if r.note != "" {
		return r.note
	}
	plain := func(_ int, s string) string { return s }
	return r.withBar(w, func(bar bool) string { return r.line(w, bar, plain) })
}

// withBar is the row drawn by draw with its progress bar when it has a
// percent and the whole row still fits in w cells, else without: the
// row is laid out once, and again only when the bar does not fit.
func (r row) withBar(w int, draw func(bar bool) string) string {
	line := draw(r.pct >= 0)
	if r.pct >= 0 && ansi.StringWidth(line) > w {
		return draw(false)
	}
	return line
}

// line lays the row out for w cells; paint styles each column (0 mark, 1
// who, 2 what, 3 state, 4 lead, 5 bar, 6 rest, 7 a mod cell, given
// plain: styled draws its spans).
//
// The mod cells follow the state, each as wide as its column; the rest
// gives way to them.
func (r row) line(w int, withBar bool, paint func(col int, s string) string) string {
	who, what, state := r.widths(w)
	gap := strings.Repeat(" ", colGap)
	out := paint(0, r.mark)
	if who > 0 {
		out += paint(1, fit(r.who, who)) + gap
	}
	out += paint(2, fit(r.what, what)) + gap + paint(3, fit(stateText(r.state), state)) + gap
	for _, c := range r.mods {
		plain, _ := spansCell(c.spans, c.w)
		out += paint(7, plain) + gap
	}
	var rest []string
	if r.lead != "" {
		rest = append(rest, paint(4, r.lead))
	}
	if withBar {
		rest = append(rest, paint(5, bar(r.pct)))
	}
	if r.rest != "" {
		rest = append(rest, paint(6, r.rest))
	}
	return out + strings.Join(rest, "  ")
}

// styled is text in the theme's colours, w cells wide.
func (r row) styled(w int) string {
	if r.note != "" {
		return th().Faint.Render(fit(r.note, w))
	}
	_, st := stateLook(r.state)
	line := r.withBar(w, func(bar bool) string {
		mod := 0
		return r.line(w, bar, func(col int, s string) string {
			switch col {
			case 7:
				_, styled := spansCell(r.mods[mod].spans, r.mods[mod].w)
				mod++
				return styled
			case 0:
				return markStyle(r.mark).Render(s)
			case 1:
				if strings.HasPrefix(r.key, "p:") {
					return th().Head.Render(s)
				}
			case 3:
				return st.Render(s)
			case 4:
				if r.leadBad {
					return th().Bad.Render(s)
				}
				return th().Warn.Render(s)
			case 5:
				n := strings.Count(s, "▰")
				return th().Good.Render(strings.Repeat("▰", n)) + th().Faint.Render(strings.Repeat("▱", 5-n))
			}
			return s
		})
	})
	return fit(line, w) + reset
}

// reset ends any style a truncated line left open.
const reset = "\x1b[m"

const (
	colWho     = 12 // project slug, or session id
	colWhatMin = 20 // coordinator, thread, command: at least,
	colWhatMax = 40 // and at most
	colState   = 10 // a glyph, a space and the word
	colGap     = 2  // between columns (docs/STYLE.md S2)
)

// Row markers: none but the gutter. A row's section and its state word
// say why it shows (NEEDS YOU, ▲ blocked), so every section's rows share
// one column grid.
const (
	markBlocked = markTop
	markNeeds   = markTop // a task that waits on the user
	markTop     = "  "
)

// threadState is a thread's state as rows and the sidebar show it: its
// live session's (with the block's reason), done once it called tm done
// and isn't working or blocked, else its record's. session is its live
// session, "" without one.
func threadState(t *ThreadRow, byID map[string]proto.SessionInfo) (state, reason, session string) {
	state = t.State
	if s, ok := byID[t.Session]; ok && t.Session != "" {
		session, state = s.ID, stateWord(s)
		if s.State == "blocked" {
			reason = blockReason(s)
		}
	}
	if (t.Done || t.Status != nil && t.Status.Done) && state != "blocked" && state != "working" {
		state = "done"
	}
	return state, reason, session
}

// allProjects is the view's current project while the dashboard shows
// every project's NEEDS YOU (the sidebar's PROJECTS header), which no
// slug can be.
const allProjects = "*"

// listProject is the project the dashboard lists: current when it is
// one, else the first (allProjects too: shownProject).
func listProject(d Data, current string) string {
	for _, p := range d.Projects {
		if p.Slug == current {
			return current
		}
	}
	if len(d.Projects) > 0 {
		return d.Projects[0].Slug
	}
	return ""
}

// shownProject is what the dashboard's list shows: allProjects while
// current is and there are projects, else listProject.
func shownProject(d Data, current string) string {
	if current == allProjects && len(d.Projects) > 0 {
		return allProjects
	}
	return listProject(d, current)
}

// buildRows lays out NEEDS YOU, the project's own section and OTHER SESSIONS.
// The sidebar's tree lists every project; the list shows one, project
// (listProject): what of it needs you, its coordinator, threads,
// sessions and task counts. NEEDS YOU holds only what waits on the
// user: the project's blocked coordinator, held messages, open
// questions and the tasks in its Needs you group (review or blocked),
// then the blocked sessions of the user's own outside the projects,
// which are no project's. With project allProjects (the sidebar's
// PROJECTS header) it is every project's instead, each row labelled
// with its project, the paired servers' shared projects after this
// server's (from their mirror's watch); the list has no project
// section then. The task rows are to see,
// not to act on: enter shows the task in the project popup, and the
// coordinator accepts the work. Everything about threads goes to their
// coordinator, the user's single point of contact (§4).
func buildRows(d Data, project string, sections []string, cards []row) []row {
	all := project == allProjects
	byID := map[string]proto.SessionInfo{}
	for _, s := range d.Sessions {
		byID[s.ID] = s
	}
	now := time.Now()
	blocks := map[string][]row{}
	// One project's NEEDS YOU counts what other projects' links need
	// on its header: the Network popup has them.
	network, links := 0, 0
	if !all {
		for _, p := range d.Projects {
			if p.Slug != project {
				links += len(linkNeedRows(nil, p))
			}
		}
		network = links + proto.ShareWaits(d.Peers)
	}
	if needs := needsRows(d, project); len(needs) > 0 {
		blocks["needs"] = append([]row{{head: "NEEDS YOU", count: len(needs), network: network}}, needs...)
	} else if all {
		blocks["needs"] = []row{{head: "NEEDS YOU", count: 0}, {note: "  nothing waits on you in any project"}}
	} else if network > 0 {
		where := "another project's link needs you"
		if links == 0 {
			where = countedAs(network, "share request waits", "share requests wait")
		}
		blocks["needs"] = []row{{head: "NEEDS YOU", count: 0, network: network}, {note: "  nothing here; N opens Network, where " + where}}
	}
	switch p, ok := findProject(d.Projects, project); {
	case all:
		// Every project's NEEDS YOU, above: no project of its own.
	case ok:
		blocks["project"] = projectListRows(d, p, byID, now)
	default:
		blocks["project"] = []row{{note: "  no projects; n creates one"}}
	}
	// The header's count is every session; this section has those
	// outside the projects, so it says so.
	other := otherRows(d, now)
	blocks["other"] = []row{{head: "OTHER SESSIONS", count: len(other)}}
	if len(other) == 0 {
		note := "  no sessions"
		if n := len(d.Sessions); n > 0 {
			note = fmt.Sprintf("  none outside the projects (%d in them)", n)
		}
		other = []row{{note: note}}
	}
	blocks["other"] = append(blocks["other"], other...)
	if !all {
		blocks["cards"] = cards
	}
	return sectionsInOrder(sections, blocks)
}

// sectionsInOrder lays out the sections' rows, blocks by section id, in
// the layout's order; one it leaves out is hidden (layoutSections has
// put needs and project back).
func sectionsInOrder(sections []string, blocks map[string][]row) []row {
	var rows []row
	for _, id := range sections {
		rows = append(rows, blocks[id]...)
	}
	return rows
}

// projectSlugs says which slugs are this server's projects.
func projectSlugs(ps []ProjectData) map[string]bool {
	bySlug := map[string]bool{}
	for _, p := range ps {
		bySlug[p.Slug] = true
	}
	return bySlug
}

// findProject is the project slug, false when there is none.
func findProject(ps []ProjectData, slug string) (ProjectData, bool) {
	for _, p := range ps {
		if p.Slug == slug {
			return p, true
		}
	}
	return ProjectData{}, false
}

// needsRows are NEEDS YOU's rows (buildRows): the project's, or with
// project allProjects every project's and the paired servers', then the
// user's own blocked sessions outside the projects.
func needsRows(d Data, project string) []row {
	all := project == allProjects
	bySlug := projectSlugs(d.Projects)
	// NEEDS YOU by project, in the projects' order, then the rest.
	needsOf := map[string][]row{}
	var loose []row
	for _, s := range d.Sessions {
		if s.State == "blocked" && s.Role != proto.RoleThread {
			who := s.Project
			if who == "" {
				who = s.ID
			}
			r := row{key: "n:" + s.ID, session: s.ID, project: s.Project,
				mark: markBlocked, who: who, what: sessionName(s), state: "blocked", lead: oneLine(s.Reason), leadBad: true,
				rest: progressOnly(s), pct: -1}
			if bySlug[s.Project] {
				needsOf[s.Project] = append(needsOf[s.Project], r)
			} else {
				loose = append(loose, r)
			}
		}
	}
	var needs []row
	for _, p := range d.Projects {
		if !all && p.Slug != project {
			continue
		}
		needs = append(needs, needsOf[p.Slug]...)
		if p.Questions > 0 {
			needs = append(needs, row{key: "nq:" + p.Slug, project: p.Slug, questions: p.Questions,
				mark: markNeeds, who: p.Slug, what: questionsWaiting(p.Questions), state: "blocked",
				rest: "enter answers them", pct: -1})
		}
		needs = append(needs, linkNeedRows(d.Peers, p)...)
		for _, t := range p.NeedsYou {
			r := row{key: fmt.Sprintf("nt:%s:%d", p.Slug, t.ID), project: p.Slug, task: t,
				mark: markNeeds, who: p.Slug, what: t.Ref() + " " + oneLine(t.Title), state: string(t.Status), rest: t.Thread, pct: -1}
			r.mods = rowMods(d.Cells, proto.SlotDashboardTask, proto.CellTarget{Project: p.Slug, Task: t.Ref()})
			if len(t.Steps) > 0 {
				r.pct = pctOf(t.StepsDone(), len(t.Steps))
				r.rest = joinSp(fmt.Sprintf("%d/%d", t.StepsDone(), len(t.Steps)), t.Thread)
			}
			needs = append(needs, r)
		}
	}
	if all {
		needs = append(needs, shareNeedRows(d.Peers)...)
		needs = append(needs, peerNeeds(d.Peers)...)
	} else {
		// The project's rows need no label: the section is the project's.
		for i := range needs {
			needs[i].who = ""
		}
	}
	// The user's own, outside the projects: in every project's.
	needs = append(needs, loose...)
	return needs
}

// projectListRows are the project section's rows (buildRows): its
// header, its coordinator, threads and other sessions, and its task
// counts.
func projectListRows(d Data, p ProjectData, byID map[string]proto.SessionInfo, now time.Time) []row {
	headNote := "active"
	if p.inactive() {
		headNote = "inactive"
	}
	rows := []row{{head: p.Slug, headNote: headNote, paused: p.Safety != nil && p.Safety.Paused}}
	var coord *proto.SessionInfo
	var members []proto.SessionInfo
	threadOf := map[string]bool{}
	for _, t := range p.Threads {
		threadOf[t.ID] = true
	}
	for i, s := range d.Sessions {
		if s.Project != p.Slug {
			continue
		}
		if s.Role == proto.RoleCoordinator && coord == nil {
			coord = &d.Sessions[i]
			continue
		}
		if s.Role == proto.RoleThread && threadOf[s.Thread] {
			continue // shown as its thread's row
		}
		members = append(members, s)
	}
	r := row{key: "p:" + p.Slug, project: p.Slug, mark: markTop, what: "coordinator", state: "—",
		rest: "not running; enter starts it", pct: -1}
	if coord != nil {
		r.session, r.pct, r.state = coord.ID, sessionPct(*coord), stateWord(*coord)
		if coord.State == "blocked" {
			r.lead, r.leadBad = oneLine(coord.Reason), true
		}
		r.rest = progressOnly(*coord)
	}
	if p.Err != "" {
		r.rest = "error: " + oneLine(p.Err)
	}
	if p.Unread > 0 {
		r.rest = joinSp(r.rest, fmt.Sprintf("%d inbox", p.Unread))
	}
	rows = append(rows, r)
	threads := orderedThreads(p.Threads, byID)
	for i := range threads {
		t := &threads[i]
		tr := row{key: "th:" + p.Slug + ":" + t.ID, project: p.Slug, thread: t, mark: markTop, pct: -1}
		tr.pct = t.Status.Progress().Percent
		var reason string
		tr.state, reason, tr.session = threadState(t, byID)
		switch {
		case reason != "":
			tr.lead, tr.leadBad = oneLine(reason), true
		case t.ReportState() == "new":
			tr.lead = "report new"
		}
		// A thread leads with its task: its progress and step first,
		// so a narrow row cuts its own id (threadName), last.
		rest := threadProgress(t.Status)
		if t.Status != nil && !t.Status.Updated.IsZero() {
			rest = joinSp(rest, age(now.Sub(t.Status.Updated)))
		}
		if t.Report != nil && t.Report.PR != "" {
			rest = joinSp(rest, "PR "+prRef(t.Report.PR))
		}
		if t.Task != "" {
			rest = joinSp(rest, t.ID)
		}
		tr.what, tr.rest = threadName(t.Task, t.ID)+" "+oneLine(t.Title), rest
		tr.mods = rowMods(d.Cells, proto.SlotDashboardThread, threadTargets(p.Slug, t.ID, t.Task, tr.session)...)
		rows = append(rows, tr)
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].Thread < members[j].Thread })
	for _, s := range members {
		sr := row{key: "s:" + s.ID, session: s.ID, project: p.Slug, mark: markTop, what: s.ID + " " + sessionName(s),
			state: stateWord(s), rest: joinSp(progressOnly(s), age(now.Sub(s.Created))), pct: sessionPct(s)}
		if s.State == "blocked" {
			sr.lead, sr.leadBad = oneLine(s.Reason), true
		}
		rows = append(rows, sr)
	}
	c := p.Counts
	rows = append(rows, row{note: "  tasks: " + taskCounts(c), counts: true})
	return rows
}

// otherRows are the sessions outside the projects (buildRows' OTHER
// SESSIONS), each with where it runs.
func otherRows(d Data, now time.Time) []row {
	bySlug := projectSlugs(d.Projects)
	var rows []row
	home, _ := os.UserHomeDir()
	for _, s := range d.Sessions {
		if s.Project != "" && bySlug[s.Project] {
			continue
		}
		where := shortHome(s.Cwd, home)
		sr := row{key: "s:" + s.ID, session: s.ID,
			mark: markTop, who: s.ID, what: sessionName(s), state: stateWord(s), rest: joinSp(progressOnly(s), age(now.Sub(s.Created)), where), pct: sessionPct(s)}
		if s.State == "blocked" {
			sr.lead, sr.leadBad = oneLine(s.Reason), true
		}
		rows = append(rows, sr)
	}
	return rows
}

// peerNeeds are the NEEDS YOU rows of the paired servers' shared
// projects, from the mirror's last watch of each (peer.list, §16.10):
// its open questions, then what its ProjectWatch says waits on its
// user, labelled "mini › acme-api". They are the other server's, to
// see: enter shows that project's read-only dashboard.
func peerNeeds(peers []proto.PeerMirror) []row {
	var out []row
	for _, pm := range peers {
		for _, mp := range pm.Projects {
			w := mp.Watch
			if w == nil {
				continue
			}
			fold, who := peerFold(pm.Alias, mp.Project), remoteCrumb(cmp.Or(pm.Name, pm.Alias), mp.Project)
			if w.Questions > 0 {
				out = append(out, row{key: "nr:" + fold + ":q", remote: fold, mark: markNeeds, who: who,
					what: questionsWaiting(w.Questions), state: "blocked", rest: "on " + cmp.Or(pm.Name, pm.Alias), pct: -1})
			}
			for i, n := range w.NeedsYou {
				ref := cmp.Or(n.Task, n.Thread, n.Session)
				out = append(out, row{key: fmt.Sprintf("nr:%s:%d", fold, i), remote: fold, mark: markNeeds, who: who,
					what: strings.TrimSpace(ref + " " + oneLine(n.Title)), state: cmp.Or(whyState[n.Why], "blocked"),
					rest: joinSp(whyWords[n.Why], n.Thread), pct: -1})
			}
		}
	}
	return out
}

// shareNeedRows are the all-projects NEEDS YOU's rows of the share
// requests from paired servers that wait for the user: enter opens the
// Network popup at the request's row.
func shareNeedRows(peers []proto.PeerMirror) []row {
	var out []row
	for _, pm := range peers {
		for _, q := range pm.Shares {
			if q.Out || !q.Pending() {
				continue
			}
			what := ""
			if q.For != "" {
				what = " for its " + oneLine(q.For)
			}
			out = append(out, row{key: "ns:" + pm.Alias + ":" + q.ID, netAt: "s:" + pm.Alias + ":" + q.ID, mark: markNeeds, who: cmp.Or(pm.Name, pm.Alias),
				what: "a share request", state: "blocked", rest: "asks you to share a project" + what + " · enter opens Network", pct: -1})
		}
	}
	return out
}

// compactRows are rows with no extra lines (rows.density = compact):
// the project's task counts move onto its header's line.
func compactRows(rows []row) []row {
	head := -1
	out := rows[:0:0]
	for _, r := range rows {
		switch {
		case r.headNote != "":
			head = len(out)
		case r.counts && head >= 0:
			out[head].headTasks = strings.TrimSpace(r.note)
			continue
		}
		out = append(out, r)
	}
	return out
}

// needsYouCount is what NEEDS YOU counts (buildRows) in ps and sessions:
// blocked sessions other than threads', each project's waiting
// questions (one row) and its tasks that need you.
func needsYouCount(ps []ProjectData, sessions []proto.SessionInfo) int {
	n := 0
	for _, s := range sessions {
		if s.State == "blocked" && s.Role != proto.RoleThread {
			n++
		}
	}
	for _, p := range ps {
		n += len(p.NeedsYou)
		if p.Questions > 0 {
			n++
		}
		n += len(linkNeedRows(nil, p))
	}
	return n
}

// needsHead is NEEDS YOU's header's words: its count, and what other
// projects' links need, "NEEDS YOU 2 · 1 network".
func needsHead(r row) string {
	s := countLabel(r.head, r.count)
	if r.network > 0 {
		s += fmt.Sprintf(" · %d network", r.network)
	}
	return s
}

// needsBadge is "⚑ N" in the warning colour, for n things that need the
// human while the sidebar, and its needs-you column, is hidden; "" for
// none.
func needsBadge(n int) string {
	if n == 0 {
		return ""
	}
	return th().Warn.Render(fmt.Sprintf("%s %d", ic().hint, n))
}

// taskCounts are a project's task counts; tasks that need the user say
// where to see them: the task list (T).
func taskCounts(c map[string]int) string {
	needs := needsYouWords(c["needs_you"])
	if c["needs_you"] > 0 {
		needs += " (" + keyShown(boundKey("T")) + " lists them)"
	}
	return fmt.Sprintf("%s · %d in motion · %d on deck", needs, c["in_motion"], c["on_deck"])
}

// needsYouWords is "1 needs you", "2 need you".
func needsYouWords(n int) string {
	if n == 1 {
		return "1 needs you"
	}
	return fmt.Sprintf("%d need you", n)
}

// sessionPct is a session's todo percent, -1 without todos.
func sessionPct(s proto.SessionInfo) int { return sessionProgress(s).Percent }

// threadGroup orders a project's threads as §7.4 does: waiting on you,
// ready for review, working, idle, then the rest.
func threadGroup(t ThreadRow, byID map[string]proto.SessionInfo) int {
	s, live := byID[t.Session]
	live = live && t.Session != ""
	switch {
	case live && s.State == "blocked", t.Status != nil && t.Status.NeedsYou != "":
		return 0
	case t.ReportState() == "new":
		return 1
	case live && s.State == "working":
		return 2
	case live:
		return 3
	}
	return 4
}

// orderedThreads are a copy of a project's threads in threadGroup's
// order, the dashboard's and the sidebar's alike.
func orderedThreads(threads []ThreadRow, byID map[string]proto.SessionInfo) []ThreadRow {
	out := append([]ThreadRow(nil), threads...)
	sort.SliceStable(out, func(i, j int) bool {
		return threadGroup(out[i], byID) < threadGroup(out[j], byID)
	})
	return out
}

// threadTargets are where a thread's mod cell is looked up, in order:
// the thread's own, its task's (when it has one), its session's (when
// it runs).
func threadTargets(slug, thread, task, session string) []proto.CellTarget {
	ts := []proto.CellTarget{{Project: slug, Thread: thread}}
	if task != "" {
		ts = append(ts, proto.CellTarget{Project: slug, Task: task})
	}
	if session != "" {
		ts = append(ts, proto.CellTarget{Session: session})
	}
	return ts
}

// prRef shortens a PR URL to "#12"; anything else is shown cut short.
func prRef(url string) string {
	if n, _, ok := codehost.ParsePRURL(url); ok {
		return "#" + strconv.Itoa(n)
	}
	return fit(oneLine(url), 30)
}

// threadDetail is what shows under a selected thread row (§4): its full
// todo list and its task's steps, styled and indented by ind, and, with
// report set, its latest report's state. The coordinator alone acts on
// reports, so their ## Next lines never show here; the user only reads.
func threadDetail(t *ThreadRow, ind string, report bool) []string {
	var out []string
	if t.Status != nil && len(t.Status.Todos) > 0 {
		out = append(out, ind+th().Faint.Render("todos"))
		for _, td := range t.Status.Todos {
			out = append(out, ind+"  "+todoGlyph(string(td.Status))+" "+oneLine(td.Text))
		}
	}
	if t.TaskRec != nil && len(t.TaskRec.Steps) > 0 {
		out = append(out, ind+th().Faint.Render(fmt.Sprintf("steps %d/%d", t.TaskRec.StepsDone(), len(t.TaskRec.Steps))))
		for _, st := range t.TaskRec.Steps {
			out = append(out, ind+"  "+stepLine(st))
		}
	}
	if report && t.Reports > 0 {
		out = append(out, ind+th().Faint.Render("report ")+reportState(t, false))
	}
	if len(out) == 0 {
		out = append(out, ind+th().Faint.Render("no todos, steps or report yet"))
	}
	return out
}

// reportState is a thread's latest report as the user sees it: whether
// the coordinator has read it, "new · for the coordinator" or "read",
// after how many there are with count.
func reportState(t *ThreadRow, count bool) string {
	rs := "read"
	if t.ReportState() == "new" {
		rs = "new"
	}
	if count {
		rs = fmt.Sprintf("%d, %s", t.Reports, rs)
	}
	if t.ReportState() == "new" {
		return th().Warn.Render(rs) + th().Faint.Render(" · for the coordinator")
	}
	return rs
}

func joinSp(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "  ")
}

// linkNeedRows are NEEDS YOU's rows of what project p's links wait on,
// each link's own: its held messages, its files offered, a paired
// server's request, the link paused; a paired server's project by its
// display name in peers.
func linkNeedRows(peers []proto.PeerMirror, p ProjectData) []row {
	var out []row
	for _, r := range p.Links {
		other := peerShownKey(peers, r.Peer)
		add := func(kind, n int, what, rest string) {
			out = append(out, row{key: "nl:" + p.Slug + ">" + r.Peer + ">" + strconv.Itoa(kind), project: p.Slug, links: n, other: r.Peer, linkNeed: kind,
				mark: markNeeds, who: p.Slug, what: what, state: "blocked", rest: rest, pct: -1})
		}
		if r.Held > 0 {
			add(needHeld, r.Held, countedAs(r.Held, "message waits", "messages wait"), "to "+other+" · enter opens it")
		}
		if r.FilesWaiting > 0 {
			add(needFile, r.FilesWaiting, countedAs(r.FilesWaiting, "file waits", "files wait"), "from "+other+" · enter opens it")
		}
		if r.Asks != nil {
			add(needAsk, 1, "a link request", "from "+other+" · enter opens Network")
		}
		if r.Paused {
			add(needPaused, 1, "link paused", "with "+other+" · enter opens Network")
		}
	}
	return out
}

// countLinkWaits sets LinkWaits, LinkAsks and LinkFiles from the links.
func (p *ProjectData) countLinkWaits() {
	p.LinkWaits, p.LinkAsks, p.LinkFiles = 0, 0, 0
	for _, r := range p.Links {
		p.LinkWaits += r.Held + r.FilesWaiting
		p.LinkFiles += r.FilesWaiting
		if r.Paused {
			p.LinkWaits++
		}
		if r.Asks != nil {
			p.LinkWaits++
			p.LinkAsks++
		}
	}
}
