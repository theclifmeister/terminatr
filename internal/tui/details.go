package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modstore"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The details panel: everything about the selected row, beside the list
// in a wide window or under it in a tall one, as the layout places it
// (docs/SPEC.md §4). Without room for it, or with it off, a thread's
// details show under its row instead.

// detailsLay is the panel for r, w cells wide and h rows tall (0: no
// fitting), with the mods' details sections placed.
func (m *dash) detailsLay(r row, w, h int) panelLay {
	d := &panel{w: w}
	switch {
	case r.task != nil:
		taskPanelWith(d, r.task, nil, m.asked(r.project, r.task), m.taskUsage(r.project, r.task.ID), m.badges(r.project, modstore.Task(r.task.Ref())))
	case r.thread != nil:
		m.threadPanel(d, r)
	case strings.HasPrefix(r.key, "p:"):
		m.projectPanel(d, r)
	case r.session != "":
		if s, ok := m.session(r.session); ok {
			sessionPanel(d, s)
		}
	}
	if t, ok := detailsTarget(r); ok {
		return layPanel(d, nil, m.mv.list, t, m.detailsOpts(h))
	}
	return panelLay{lines: d.lines}
}

// detailsTarget is what the details sections in r's panel show for; ok
// false for a row that has none.
func detailsTarget(r row) (panelTarget, bool) {
	project, task, thr, ok := sectionTarget(r)
	return panelTarget{slot: mod.SlotDetailsSection, project: project, task: task, thr: thr}, ok
}

// detailsOpts are this console's state of the details panel's sections,
// h rows tall.
func (m *dash) detailsOpts(h int) panelOpts {
	return panelOpts{order: m.layout.detSections, folds: m.folds, sel: m.detSelNow(), focused: m.area() == areaDetails, rows: h,
		mods: m.data.Mods, noMods: m.data.NoMods, cache: &m.mv.panel}
}

// detailsNow is the details panel as it shows now, ok false while it
// doesn't.
func (m *dash) detailsNow() (panelLay, bool) {
	g := m.geom()
	dr := g.details()
	r, ok := m.selected()
	if g.panel() == "" || dr.w <= 0 || !ok {
		return panelLay{}, false
	}
	return m.detailsLay(r, dr.w, dr.h), true
}

func (m *dash) session(id string) (proto.SessionInfo, bool) {
	for _, s := range m.data.Sessions {
		if s.ID == id {
			return s, true
		}
	}
	return proto.SessionInfo{}, false
}

// panel collects a details panel's lines: core's, with where its named
// blocks start (block) and which lines are must-see (mustFrom), for the
// mods' sections placed among them (panelsections.go).
type panel struct {
	w      int
	lines  []string
	starts map[string]int
	must   []bool
}

// block says core's block name starts at the next line.
func (d *panel) block(name string) {
	if d.starts == nil {
		d.starts = map[string]int{}
	}
	d.starts[name] = len(d.lines)
}

// mustFrom marks the lines from line i on as must-see: no mod section
// pushes one off the first screen.
func (d *panel) mustFrom(i int) {
	for len(d.must) < len(d.lines) {
		d.must = append(d.must, false)
	}
	for j := i; j < len(d.lines); j++ {
		d.must[j] = true
	}
}

func (d *panel) add(s string) { d.lines = append(d.lines, fit(" "+s, d.w)+reset) }

func (d *panel) gap() {
	if len(d.lines) > 0 && d.lines[len(d.lines)-1] != "" {
		d.lines = append(d.lines, "")
	}
}

// section starts a section: a blank row, then its heading, meta faint
// after it (docs/STYLE.md, Section).
func (d *panel) section(title, meta string) {
	d.gap()
	st := sectionStyle(title)
	w := max(d.w-2, 1)
	if meta == "" {
		d.add(sectionRule(title, st, w))
		return
	}
	t := title + " " + meta + " "
	d.add(st.Render(title) + " " + th().Faint.Render(meta+" "+strings.Repeat("─", max(w-ansi.StringWidth(t), 0))))
}

// title is the panel's first line, bold in the accent colour, and the
// state under it: the head; core's block, core, starts under it.
func (d *panel) title(text, state string) {
	d.add(th().Title.Render(oneLine(text)))
	if state != "" && state != "—" {
		g, st := stateLook(state)
		d.add(st.Render(strings.TrimSpace(g + " " + state)))
	}
	d.add(th().Faint.Render(strings.Repeat("─", max(d.w-2, 0))))
	d.block("core")
}

// badges shows the mods' annotations under the title: per mod one line
// of its badges. Spans are drawn in their theme role; the
// text was cleaned by the store.
func (d *panel) badges(bs []modstore.Badge) {
	for i := 0; i < len(bs); {
		j := i
		for j < len(bs) && bs[j].Mod == bs[i].Mod {
			j++
		}
		var parts []string
		for _, b := range bs[i:j] {
			for _, sp := range b.Spans {
				parts = append(parts, roleStyle(sp.Role).Render(sp.Text))
			}
		}
		d.add(strings.Join(parts, "  "))
		i = j
	}
}

// roleStyle is the style of theme role name; plain when it is unknown.
func roleStyle(name string) lipgloss.Style {
	t := th()
	switch name {
	case "head":
		return t.Head
	case "sel":
		return t.Sel
	case "faint":
		return t.Faint
	case "accent":
		return t.Accent
	case "title":
		return t.Title
	case "bad":
		return t.Bad
	case "warn":
		return t.Warn
	case "good":
		return t.Good
	case "info":
		return t.Info
	}
	return t.Plain
}

// badges are the mods' annotations of target in project slug.
func (m *dash) badges(slug, target string) []modstore.Badge {
	if p := m.projectData(slug); p != nil {
		return p.Badges[target]
	}
	return nil
}

// field is a "label  value" line.
func (d *panel) field(label, value string) {
	if value != "" {
		d.add(th().Faint.Render(fmt.Sprintf("%-9s", label)) + " " + value)
	}
}

// wrap adds text wrapped to the panel's width.
func (d *panel) wrap(text string) {
	for _, l := range strings.Split(ansi.Wordwrap(text, max(d.w-2, 10), ""), "\n") {
		d.add(l)
	}
}

// progressLine is "▰▰▰▱▱ 60% 3/5".
func progressLine(pr thread.Progress) string {
	pct, done, total := pr.Percent, pr.Done, pr.Total
	if pct < 0 {
		return ""
	}
	n := strings.Count(bar(pct), "▰")
	s := th().Good.Render(strings.Repeat("▰", n)) + th().Faint.Render(strings.Repeat("▱", 5-n)) + fmt.Sprintf(" %d%%", pct)
	if total > 0 {
		s += fmt.Sprintf(" %d/%d", done, total)
	}
	return s
}

func (m *dash) threadPanel(d *panel, r row) {
	t := r.thread
	state := t.State
	var q *proto.Question
	if s, ok := m.session(t.Session); ok && t.Session != "" {
		q = s.Question
		state = sessionState(s)
	}
	d.title(threadName(t.Task, t.ID)+" "+t.Title, state)
	d.badges(m.badges(r.project, modstore.Thread(t.ID)))
	d.field("project", r.project)
	d.field("thread", t.ID)
	d.field("session", t.Session)
	d.field("usage", t.Usage.String())
	if st := t.Status; st != nil {
		d.field("progress", progressLine(st.Progress()))
		d.field("now", nowWords(st))
		if !st.Updated.IsZero() {
			d.field("updated", age(time.Since(st.Updated))+" ago")
		}
		if st.NeedsYou != "" {
			from := len(d.lines)
			d.field("waiting", th().Warn.Render(oneLine(st.NeedsYou)))
			d.mustFrom(from)
		}
	}
	if t.Report != nil && t.Report.PR != "" {
		d.field("PR", oneLine(t.Report.PR))
	}
	if t.Reports > 0 {
		d.field("report", reportState(t, true))
	}
	if q != nil {
		from := len(d.lines)
		d.section("QUESTION OPEN", "")
		for _, l := range questionLines(q) {
			d.wrap(l)
		}
		d.mustFrom(from)
	}
	d.gap()
	for _, l := range threadDetail(t, "", false) {
		d.add(l)
	}
	d.gap()
	if r.session != "" {
		d.add(keysLine("enter attach"))
	}
}

// taskUsage is what the threads of task id of project slug used.
func (m *dash) taskUsage(slug string, id int) thread.Usage {
	for _, p := range m.data.Projects {
		if p.Slug == slug {
			return p.TaskUsage[id]
		}
	}
	return thread.Usage{}
}

// taskPanelWith shows a task with what the user needs to act on it:
// what it is blocked on; for a task in review, how to check it and
// whether its change shipped (rv, when known); and what the coordinator
// was asked about it (asked, an item kind, "" for nothing).
func taskPanelWith(d *panel, t *tasks.Task, rv *Review, asked string, usage thread.Usage, badges []modstore.Badge) {
	d.title(t.Ref()+" "+t.Title, string(t.Status))
	d.badges(badges)
	d.field("priority", string(t.Priority))
	d.field("thread", t.Thread)
	d.field("usage", usage.String())
	d.field("design", t.Design)
	d.field("after", taskRefList(t.After))
	if asked != "" {
		d.gap()
		d.wrap(th().Warn.Render(waitingFor(asked)))
	}
	if t.Status == tasks.Blocked {
		d.gap()
		on := blockedOn(t)
		if on == "" {
			on = th().Faint.Render("no note says; c opens the coordinator to ask")
		}
		d.wrap(th().Warn.Render("Blocked on: ") + on)
	}
	if rv != nil && t.Status == tasks.Review {
		reviewLines(d, t, *rv)
	}
	if len(t.Steps) > 0 {
		d.section("STEPS", "")
		d.add(stepsProgress(t))
		for _, s := range t.Steps {
			d.add(stepLine(s))
		}
	}
	if notes := strings.TrimSpace(t.Notes); notes != "" {
		d.section("NOTES", "")
		for _, l := range strings.Split(notes, "\n") {
			d.wrap(oneLine(l))
		}
	}
}

func (m *dash) projectPanel(d *panel, r row) {
	p := m.projectData(r.project)
	if p == nil {
		return
	}
	d.title(p.Slug, r.state)
	if s, ok := m.session(r.session); ok && r.session != "" {
		d.field("session", s.ID)
		d.field("progress", progressLine(sessionProgress(s)))
		if s.Current != "" {
			d.field("now", "▸ "+oneLine(s.Current))
		}
	} else {
		d.field("session", th().Faint.Render("not running; enter starts it"))
	}
	c := p.Counts
	d.field("tasks", taskCounts(c))
	d.field("threads", fmt.Sprint(len(p.Threads)))
	d.field("usage", p.Usage.String())
	if p.Unread > 0 {
		d.field("inbox", th().Warn.Render(fmt.Sprintf("%d unhandled", p.Unread))+th().Faint.Render(" · i shows them"))
	}
	if p.Err != "" {
		d.gap()
		d.wrap(th().Bad.Render("error: " + oneLine(p.Err)))
	}
	d.gap()
	d.add(keysLine("enter open the coordinator · prefix+" + keyShown(boundKey("T")) + " tasks · prefix+" + keyShown(boundKey("I")) + " inbox"))
}

func sessionPanel(d *panel, s proto.SessionInfo) {
	d.title(s.ID+" "+sessionName(s), sessionState(s))
	d.field("project", s.Project)
	d.field("role", string(s.Role))
	if s.Task != "" {
		d.field("thread", s.Thread)
	}
	d.field("agent", s.Agent)
	d.field("progress", progressLine(sessionProgress(s)))
	if s.Question != nil {
		d.section("QUESTION OPEN", "")
		for _, l := range questionLines(s.Question) {
			d.wrap(l)
		}
		d.gap()
	}
	if s.Current != "" {
		d.field("now", "▸ "+oneLine(s.Current))
	}
	if s.Queued > 0 {
		q := fmt.Sprintf("%d prompt%s", s.Queued, map[bool]string{true: "s"}[s.Queued != 1])
		if n := s.QueueNote(time.Now()); n != "" {
			q += ", " + n
		}
		d.field("queued", q)
	}
	d.field("dir", oneLine(homeShort(s.Cwd)))
	if len(s.Argv) > 0 {
		d.field("command", oneLine(strings.Join(s.Argv, " ")))
	}
	if !s.Created.IsZero() {
		d.field("started", age(time.Since(s.Created))+" ago")
	}
	d.gap()
	d.add(keysLine("enter attach"))
}

// reviewLines are a task in review's ship state and how to check it.
func reviewLines(d *panel, t *tasks.Task, rv Review) {
	d.gap()
	if s := shipLine(rv); s != "" {
		d.wrap(s)
	}
	checks := append(append([]string(nil), rv.Check...), noteChecks(t)...)
	d.section("HOW TO CHECK", "")
	if len(checks) == 0 {
		d.wrap(th().Faint.Render("The report doesn't say; c opens the coordinator to ask."))
		return
	}
	for _, c := range checks {
		d.wrap("• " + oneLine(c))
	}
}

// shipLine says where a task's pull request stands, "" when no PR is
// known.
func shipLine(rv Review) string {
	if rv.PR <= 0 {
		return ""
	}
	pr := fmt.Sprintf("PR #%d", rv.PR)
	switch rv.Ship {
	case ShipMerged:
		return pr + " merged"
	case ShipOpen:
		return pr + " open, not merged yet"
	case ShipClosed:
		return th().Bad.Render(pr + " closed without merging")
	}
	return pr
}

// detailsState is where the details panel is scrolled and what it selects.
type detailsState struct {
	// detailTop is the details panel's first line, scrolled with the
	// wheel, for the row detailKey (another row shows from the top).
	detailTop int
	detailKey string
	// bodyLay is the panel listBody laid out for the frame being drawn,
	// which the footer's hint reads instead of laying it out again.
	bodyLay *panelLay
	// detSel is the details panel's selected mod section header (its fold
	// id), for the row detSelKey; "" for none.
	detSel, detSelKey string
}

// detailsKey runs a key while the details panel has the focus, by the
// panel's key table (panelActions): the arrows scroll it, ] and [
// select a mod section's header, ← → and enter fold, unfold or open the
// selected one, esc drops the selection, then goes back to the list. ok
// is false for the other keys, and for ] [ ← → enter where they find no
// section, which go to the list's actions.
func (m *dash) detailsKey(key string) (tea.Cmd, bool) {
	op, ok := panelOp(key)
	if !ok {
		return nil, false
	}
	switch op {
	case panelUp:
		m.scrollDetails(-1)
	case panelDown:
		m.scrollDetails(1)
	case panelPageUp:
		m.scrollDetails(scrollKeys["pgup"])
	case panelPageDown:
		m.scrollDetails(scrollKeys["pgdown"])
	case panelFirst:
		m.scrollDetails(-1 << 20)
	case panelLast:
		m.scrollDetails(1 << 20)
	case panelBack:
		if m.detSelNow() != "" {
			m.detSel = ""
		} else {
			m.focus = areaMain
		}
	default:
		lay, shows := m.detailsNow()
		if !shows {
			return nil, false
		}
		st, next, took := panelStep(lay, m.detSelNow(), op)
		switch {
		case !took:
			return nil, false
		case next != m.detSelNow():
			m.detSel, m.detSelKey = next, m.sel
			m.revealDetails()
			return nil, true
		}
		return m.sectionCmd(st), true
	}
	return nil, true
}

// detSelNow is the details panel's selected header for the row shown.
func (m *dash) detSelNow() string {
	if m.detSelKey != m.sel {
		return ""
	}
	return m.detSel
}

// revealDetails scrolls the details panel so its selected header shows.
func (m *dash) revealDetails() {
	lay, ok := m.detailsNow()
	if !ok {
		return
	}
	i := slices.IndexFunc(lay.secs, func(s *sideSection) bool { return s.id == m.detSelNow() })
	if i < 0 || lay.heads[i] < 0 {
		return
	}
	if m.detailKey != m.sel {
		m.detailKey, m.detailTop = m.sel, 0
	}
	h, rows := lay.heads[i], m.geom().details().h
	switch {
	case h < m.detailTop:
		m.detailTop = h
	case h >= m.detailTop+rows:
		m.detailTop = h - rows + 1
	}
	m.detailTop = min(max(m.detailTop, 0), max(len(lay.lines)-rows, 0))
}
