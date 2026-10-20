package tui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/modstore"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The project popup (a on the dashboard, prefix+a in a session;
// docs/SPEC.md §4): everything about one project in tabs. Only the
// repositories and the settings change here, on the human's keypress;
// the inbox, the tasks and the memory are read-only, since the
// coordinator keeps them, save that D, A and x ask it to act on a task
// (asks.go). The Tasks tab is the built-in tasks-tab mod's, which binds
// those keys on its rows (modviews.go, binding.go), and the Memory tab
// the built-in memory-tab mod's; neither has another drawing, so with
// its mod off the tab says how to turn it on. Which tab is open
// is this console's own, as every popup.

// The tabs, in order.
const (
	tabOverview = iota
	tabInbox
	tabTasks
	tabSettings
	tabAccess
	tabKeys
	tabMemory
	tabLibrary
	tabMessages
	tabCount
)

var tabNames = [tabCount]string{"overview", "inbox", "tasks", "settings", "access", "keys", "memory", "library", "messages"}

type projectView struct {
	slug string
	tab  int
	// sel is each tab's selection: a repository, an inbox item, a
	// setting, a line of the keys.
	sel [tabCount]int
	// top is each tab's first content line shown; shown is the
	// selection's line (plus one) it last scrolled to, so the content
	// scrolls to a selection that moved, not back to one scrolled away
	// from; nudge is how far the keys and the wheel scrolled it since,
	// applied when it's drawn (after following the selection).
	top, shown, nudge [tabCount]int
	board             *tasks.Board
	reviews           map[int]Review // the tasks in review, by id
	// settings and access are the Settings and Access tabs' rows.
	settings, access settingsList
	// library is every thread's library files, newest first (the
	// Library tab), libOK once loaded; libErr is why it didn't.
	library []thread.LibFile
	libOK   bool
	libErr  error
	// show is the task the popup was opened to show, 0 for none: the
	// Tasks tab's mod hears it in view.opened, and picking puts the
	// cursor on its row once the mod lists it.
	show    int
	picking bool
}

func (m *dash) projectPopup(string) tea.Cmd {
	slug := m.needProject()
	if slug == "" {
		return nil
	}
	m.push(m.newProjectView(slug))
	return m.loadPopup(slug)
}

// newProjectView is slug's popup on its first tab, its settings rows
// made: the Access tab's for the platform the server is on.
func (m *dash) newProjectView(slug string) *projectView {
	return &projectView{slug: slug, settings: settingsList{rows: m.projectRows(slug)},
		access: settingsList{rows: accessSettings(slug, m.serverOS())}}
}

// list is the open tab's settings list, nil for a tab without one.
func (pv *projectView) list() *settingsList {
	switch pv.tab {
	case tabSettings:
		return &pv.settings
	case tabAccess:
		return &pv.access
	}
	return nil
}

// showTask opens slug's project popup on the Tasks tab, task id
// selected when it isn't 0 (pickTask): t, and enter on a NEEDS YOU task.
// It only shows the task; the coordinator acts on it.
func (m *dash) showTask(slug string, id int) tea.Cmd {
	return m.openTab(slug, tabTasks, id)
}

// showTab opens slug's project popup on tab: i opens the Inbox tab.
func (m *dash) showTab(slug string, tab int) tea.Cmd { return m.openTab(slug, tab, 0) }

func (m *dash) openTab(slug string, tab, show int) tea.Cmd {
	pv := m.newProjectView(slug)
	pv.tab, pv.show, pv.picking = tab, show, show != 0
	m.push(pv)
	return m.loadPopup(slug)
}

// setBoard takes a loaded board.
func (pv *projectView) setBoard(b *tasks.Board) { pv.board = b }

// shownTask is the task pv's tab v was opened to show, for view.opened:
// only the Tasks tab's, "" for none.
func (pv *projectView) shownTask(v proto.ModView) string {
	if t, ok := coreTabOf(v); ok && t == tabTasks && pv.show != 0 {
		return fmt.Sprintf("T%d", pv.show)
	}
	return ""
}

// pickTask puts the cursor of host, the Tasks tab's mod view, on the
// task the popup was opened to show, once the mod lists it; the human's
// first key or click there gives up on it.
func (pv *projectView) pickTask(v proto.ModView, host *widget.View) {
	if !pv.picking || host == nil || pv.shownTask(v) == "" {
		return
	}
	if host.Select(modaction.Target{Project: pv.slug, Task: pv.shownTask(v)}) {
		pv.picking = false
	}
}

// projectPopupView is the open project popup, if any.
func (m *dash) projectPopupView() *projectView {
	for i := len(m.stack) - 1; i >= 0; i-- {
		if p, ok := m.stack[i].(*projectView); ok {
			return p
		}
	}
	return nil
}

func (pv *projectView) data(m *dash) ProjectData {
	if p := m.projectData(pv.slug); p != nil {
		return *p
	}
	return ProjectData{Slug: pv.slug}
}

func (pv *projectView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	v, modded := pv.modTab(m)
	if modded {
		// The mod's view moves, scrolls and filters first; what it
		// leaves (esc without a filter, the tabs) is the popup's.
		if cmd, used := pv.modTabKey(m, v, k.String()); used {
			return cmd
		}
	}
	switch s := k.String(); s {
	case "esc":
		m.pop()
		return nil
	case "right":
		pv.tab = (pv.tab + 1) % pv.tabs(m)
		return nil
	case "left":
		pv.tab = (pv.tab + pv.tabs(m) - 1) % pv.tabs(m)
		return nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
		if t := (int(s[0]-'0') + 9) % 10; t < pv.tabs(m) {
			pv.tab = t // 0 is the tenth
		}
		return nil
	}
	if modded || pv.tab >= tabCount {
		return nil
	}
	s := k.String()
	d := scrollKeys[s]
	if pv.tab == tabAccess && s == "e" {
		return m.showAccess(pv.slug, "")
	}
	if pv.tab == tabAccess && s == "p" {
		return m.openPresets(pv.slug)
	}
	if pv.tab == tabAccess && s == "R" {
		return m.resetAccess(pv.slug)
	}
	if l := pv.list(); l != nil {
		before := l.sel
		cmd, _ := l.key(m, k)
		if l.sel == before {
			pv.nudge[pv.tab] += d
		}
		return cmd
	}
	// The arrows move the selection; past the first or last item (or in
	// a tab without one) they scroll the content.
	before := pv.sel[pv.tab]
	if n := pv.count(m); n > 0 {
		pv.sel[pv.tab] = moveSel(before, d, n)
	}
	if pv.sel[pv.tab] == before {
		pv.nudge[pv.tab] += d
	}
	switch pv.tab {
	case tabOverview:
		return pv.repoKey(m, k)
	case tabLibrary:
		return pv.libraryKey(m, k)
	case tabMessages:
		ls := pv.data(m).Links
		if pv.sel[tabMessages] >= len(ls) {
			break
		}
		switch r := ls[pv.sel[tabMessages]]; s {
		case "enter", "space":
			return m.openConversation(pv.slug, r.Peer)
		case "A", "x":
			return m.answerRequest(pv.slug, r, s == "A")
		}
	}
	return nil
}

// openTask shows task t of the popup's project over it; esc comes back
// to the popup.
func (m *dash) openTask(pv *projectView, t *tasks.Task) tea.Cmd {
	m.push(&taskView{pv: pv, id: t.ID})
	return nil
}

// taskView is one task of a project popup's Tasks tab, in full. It
// follows the popup's board, which reloads while it is open.
type taskView struct {
	pv *projectView
	id int
}

func (tv *taskView) task() *tasks.Task {
	if tv.pv.board != nil {
		for _, t := range tv.pv.board.Tasks {
			if t.ID == tv.id {
				return t
			}
		}
	}
	return nil
}

func (tv *taskView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.pop()
	case "D", "A", "x", "c":
		if t := tv.task(); t != nil {
			return m.taskKey(tv.pv.slug, t, k.String())
		}
	}
	return nil
}

func (tv *taskView) render(m *dash) string {
	title := "Task · " + tv.pv.slug
	t := tv.task()
	if t == nil {
		body := th().Faint.Render("loading…")
		if tv.pv.board != nil {
			body = th().Faint.Render("this task is gone")
		}
		return m.popup(box{title: title, body: []string{body}, sel: -1, keys: "esc back"})
	}
	d := &panel{w: m.inner(viewWidth) + 2} // panel lines start with a space, and keep one at the end
	var rv *Review
	if r, ok := tv.pv.reviews[t.ID]; ok {
		rv = &r
	}
	taskPanelWith(d, t, rv, m.asked(tv.pv.slug, t), m.taskUsage(tv.pv.slug, t.ID), m.badges(tv.pv.slug, modstore.Task(t.Ref())))
	for i, l := range d.lines {
		// The box has its own gutters.
		d.lines[i] = strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(l, " "), reset), " ") + reset
	}
	return m.popup(box{title: title, body: d.lines, sel: -1, keys: joinKeys(taskKeys(t), "esc back")})
}

// count is how many things the tab selects among.
func (pv *projectView) count(m *dash) int {
	switch pv.tab {
	case tabOverview:
		return len(pv.data(m).Repos)
	case tabInbox:
		return len(project.Rows(pv.data(m).Items))
	case tabLibrary:
		return len(pv.library)
	case tabMessages:
		return len(pv.data(m).Links)
	}
	return 0
}

// repoKey adds (+) or removes (x) a repository.
func (pv *projectView) repoKey(m *dash, k tea.KeyPressMsg) tea.Cmd {
	slug, repos := pv.slug, pv.data(m).Repos
	switch k.String() {
	case "+":
		m.prompt("Add a repository", "Its directory:", m.cwd, func(path string) tea.Cmd {
			path = absPath(path, m.cwd)
			return m.setRepo(slug, path, true, "added "+path)
		})
	case "x":
		i := pv.sel[tabOverview]
		if i >= len(repos) {
			return nil
		}
		path := repos[i]
		m.confirm("Remove a repository", "Remove "+path+" from "+slug+"'s repositories? The directory itself stays.", func() tea.Cmd {
			return m.setRepo(slug, path, false, "removed "+path)
		})
	}
	return nil
}

func (m *dash) setRepo(slug, path string, add bool, msg string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		if err := src.SetRepo(slug, path, add); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: msg}
	})
}

// absPath reads a typed path: ~ is the home directory, a relative path
// is from cwd, links are resolved.
func absPath(p, cwd string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok && (rest == "" || rest[0] == '/') {
		if h, err := os.UserHomeDir(); err == nil {
			p = h + rest
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func (pv *projectView) render(m *dash) string { return m.popup(pv.box(m)) }

func (pv *projectView) box(m *dash) box {
	w := m.inner(viewWidth)
	p := pv.data(m)
	if v, ok := pv.modTab(m); ok {
		return pv.modTabBox(m, p, v)
	}
	var body []string
	var hits []int
	sel := -1
	keys := "← → tabs · esc close"
	switch pv.tab {
	case tabOverview:
		body, sel, hits = pv.overview(m, p, w)
		keys = "+ add repository · x remove it · " + keys
	case tabInbox:
		body, sel, hits = inboxLines(p.Items, pv.sel[tabInbox], w)
		body = append(body, "", th().Faint.Render("The coordinator handles these (tm inbox done)."))
	case tabTasks, tabMemory:
		// A built-in mod draws this tab (modTab): it isn't running.
		body = pv.modOffLines(m, w)
	case tabSettings, tabAccess:
		l := pv.list()
		body, sel, hits = l.lines(m, w)
		if pv.tab == tabAccess {
			head := append(accessHead(m, w), "")
			body = append(head, body...)
			if sel >= 0 {
				sel += len(head)
			}
			hits = append(make([]int, len(head)), hits...)
			for i := range head {
				hits[i] = noHit
			}
		}
		keys = listKeys(m, l, pv.tab == tabAccess)
	case tabKeys:
		// The same list as the help.
		body = keyLines(w, m.commandHelp())
		keys = "↑ ↓ scroll · " + keys
	case tabLibrary:
		body, sel, hits = pv.libraryLines(w)
		if len(pv.library) > 0 {
			keys = "enter read · d delete · D delete the thread's · " + keys
		}
	case tabMessages:
		body, sel, hits = messagesLines(p, pv.sel[tabMessages], w)
		body = append(body, "", th().Faint.Render("Messages between this project's coordinator and linked projects' (tm link)."))
		if len(p.Links) > 0 {
			keys = "enter open · " + keys
		}
		if pv.sel[tabMessages] < len(p.Links) && p.Links[pv.sel[tabMessages]].Asks != nil {
			keys = "A accept the link · x decline · " + keys
		}
	}
	head := []string{pv.tabBar(m, p), ""}
	all := append([]int{tabHit, noHit}, hits...)
	for len(all) < len(head)+len(body) {
		all = append(all, noHit)
	}
	b := box{title: pv.slug, head: head, body: body, sel: -1, hits: all, keys: keys}
	b.scroll = pv.scroll(m.boxRows(b)-len(head), sel, len(body))
	return b
}

// listKeys are the Settings or Access tab's (access) keys for its
// selected row: + and - on a number, x on a setting the project sets
// itself (it follows all projects again), and on the Access tab p for
// a preset and e for the effective access.
func listKeys(m *dash, l *settingsList, access bool) string {
	keys := []string{"enter change"}
	if l.sel < len(l.rows) {
		r := l.rows[l.sel]
		if r.change == nil {
			keys = nil
		}
		if r.adjust != nil {
			keys = append(keys, "+ - number")
		}
		if r.from != nil && r.from(m) == "" {
			keys = append(keys, "x follow all projects")
		}
	}
	if access {
		keys = append(keys, "p add a preset", "e effective access", "R reset")
	}
	return strings.Join(append(keys, "← → tabs", "esc close"), " · ")
}

// scroll is the open tab's first content line shown, rows at a time of
// n lines: where it was, moved just enough to show the line sel when the
// selection moved there, then by the nudges since.
func (pv *projectView) scroll(rows, sel, n int) int {
	t := pv.top[pv.tab]
	if sel >= 0 && sel+1 != pv.shown[pv.tab] {
		pv.shown[pv.tab] = sel + 1
		t = min(t, sel)
		t = max(t, sel-rows+1)
	}
	t += pv.nudge[pv.tab]
	pv.nudge[pv.tab] = 0
	t = min(max(t, 0), max(n-max(rows, 1), 0))
	pv.top[pv.tab] = t
	return t
}

// tabHit is the tab bar's line: the column picks the tab.
const tabHit = -2

// click picks a tab on the tab bar, else the clicked line's repository,
// inbox item, task or setting (which it changes, as enter does).
func (pv *projectView) click(m *dash, item, col int, double bool) tea.Cmd {
	if item == tabHit {
		if t := pv.tabAt(m, pv.data(m), col); t >= 0 {
			pv.tab = t
		}
		return nil
	}
	if v, ok := pv.modTab(m); ok {
		pv.picking = false
		return m.panelClick(v, pv.slug, col, item)
	}
	if l := pv.list(); l != nil {
		return l.click(m, item, col)
	}
	pv.sel[pv.tab] = item
	if double && pv.tab == tabMessages {
		if ls := pv.data(m).Links; item >= 0 && item < len(ls) {
			return m.openConversation(pv.slug, ls[item].Peer)
		}
	}
	if double && pv.tab == tabLibrary {
		if f, ok := pv.selLib(); ok {
			return m.openLibFile(pv, f)
		}
	}
	return nil
}

// tabAt is the tab at column col of the tab bar, -1 for none.
func (pv *projectView) tabAt(m *dash, p ProjectData, col int) int {
	x, pad := 0, pv.tabPad(m, p)
	for i := range pv.tabs(m) {
		w := len([]rune(pv.tabLabel(m, p, i))) + 2*len(pad)
		if col >= x && col < x+w {
			return i
		}
		x += w + 1
	}
	return -1
}

// tabPad is what pads each tab's label: a space either side, none when
// the tabs wouldn't fit the popup's width so.
func (pv *projectView) tabPad(m *dash, p ProjectData) string {
	n := pv.tabs(m)
	w := 3*n - 1
	for i := range n {
		w += len([]rune(pv.tabLabel(m, p, i)))
	}
	if w > m.inner(viewWidth) {
		return ""
	}
	return " "
}

// wheel moves the selection as the arrows do, or scrolls a tab without
// one by three lines.
func (pv *projectView) wheel(m *dash, d int) {
	if v, ok := pv.modTab(m); ok {
		m.panelWheel(v, pv.slug, d)
		return
	}
	if pv.tab == tabKeys {
		pv.nudge[pv.tab] += 3 * d
		return
	}
	pv.key(m, arrow(d))
}

// tabBar names the tabs, the open one in reverse video, with the inbox's
// count.
func (pv *projectView) tabBar(m *dash, p ProjectData) string {
	var parts []string
	pad := pv.tabPad(m, p)
	for i := range pv.tabs(m) {
		label := pad + pv.tabLabel(m, p, i) + pad
		if i == pv.tab {
			parts = append(parts, th().Sel.Render(label))
		} else {
			parts = append(parts, th().Faint.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

// tabLabel is tab i's label: its number and name, with the inbox's
// count; a mod's tab its title and the count it sets.
func (pv *projectView) tabLabel(m *dash, p ProjectData, i int) string {
	if i >= tabCount {
		if tabs := m.mv.tabsFor(pv.slug); i-tabCount < len(tabs) {
			return modTabLabel(tabs[i-tabCount], pv.slug, i)
		}
		return ""
	}
	if v, ok := m.mv.coreTab(pv.slug, i); ok {
		return modTabLabel(v, pv.slug, i)
	}
	label := fmt.Sprintf("%d %s", i+1, tabNames[i])
	if i == tabInbox && len(p.Items) > 0 {
		label += fmt.Sprintf(" %d", len(p.Items))
	}
	if i == tabMessages && p.LinkWaits > 0 {
		label += fmt.Sprintf(" %d", p.LinkWaits)
	}
	return label
}

// overview is the project's slug, goal, repositories, machine and
// agents.
func (pv *projectView) overview(m *dash, p ProjectData, w int) ([]string, int, []int) {
	field := func(label string) string { return th().Faint.Render(fmt.Sprintf("%-14s", label)) }
	var out []string
	repoAt := map[int]int{} // a repository's line: its index
	out = append(out, field("Project")+th().Head.Render(p.Slug))
	if g := strings.TrimSpace(p.Goal); g == "" {
		out = append(out, field("Goal")+th().Faint.Render("none set; ask the coordinator to set one"))
	} else {
		for i, l := range wrapLines(oneLine(g), w-14) {
			label := field("")
			if i == 0 {
				label = field("Goal")
			}
			out = append(out, label+l)
		}
	}
	out = append(out, "")
	sel := -1
	if len(p.Repos) == 0 {
		out = append(out, field("Repositories")+th().Faint.Render("none; + adds one"))
	}
	for i, r := range p.Repos {
		label := field("")
		if i == 0 {
			label = field("Repositories")
		}
		repoAt[len(out)] = i
		if i == pv.sel[tabOverview] {
			sel = len(out)
			out = append(out, label+th().Sel.Render(fit(r, w-14)))
		} else {
			out = append(out, label+r)
		}
		if n := p.Checkouts[r]; n != "" {
			out = append(out, field("")+th().Warn.Render(fit(n, w-14)))
		}
	}
	out = append(out, "")
	host, _ := os.Hostname()
	out = append(out, field("Machines")+"this one"+th().Faint.Render("  "+host))
	out = append(out, "")
	out = append(out, field("Coordinator")+pv.coordinatorLine(m, p))
	threads := map[string][]string{}
	for _, t := range p.Threads {
		a := cmp.Or(t.Agent, config.LegacyAgent)
		threads[a] = append(threads[a], t.ID)
	}
	if len(threads) == 0 {
		out = append(out, field("Threads")+th().Faint.Render("none open"))
	}
	agents := make([]string, 0, len(threads))
	for a := range threads {
		agents = append(agents, a)
	}
	slices.Sort(agents)
	for i, a := range agents {
		label := field("")
		if i == 0 {
			label = field("Threads")
		}
		out = append(out, label+a+th().Faint.Render(" · "+strings.Join(threads[a], " ")))
	}
	s := config.Defaults
	if p.Safety != nil {
		s = *p.Safety
	}
	modes := []string{map[bool]string{true: "start after asking you", false: "start automatically"}[s.StartThreads != config.StartAuto]}
	if s.Yolo {
		modes = append(modes, "yolo mode")
	}
	out = append(out, field("")+th().Faint.Render(strings.Join(modes, " · ")+" (Settings tab)"))
	c := p.Counts
	needs := needsYouWords(c["needs_you"])
	if c["needs_you"] > 0 {
		needs += " (Tasks tab)"
	}
	out = append(out, "", field("Tasks")+fmt.Sprintf("%s · %d in motion · %d on deck · %d done", needs, c["in_motion"], c["on_deck"], c["done"]))
	if p.Err != "" {
		out = append(out, "", th().Bad.Render("error: "+oneLine(p.Err)))
	}
	return out, sel, lineHits(len(out), repoAt)
}

// lineHits are n lines' hits: at's, noHit elsewhere.
func lineHits(n int, at map[int]int) []int {
	hits := make([]int, n)
	for i := range hits {
		hits[i] = noHit
		if v, ok := at[i]; ok {
			hits[i] = v
		}
	}
	return hits
}

// coordinatorLine is the coordinator's agent and state, or the agent a
// new one runs.
func (pv *projectView) coordinatorLine(m *dash, p ProjectData) string {
	for _, s := range m.data.Sessions {
		if s.Role == proto.RoleCoordinator && s.Project == p.Slug {
			line := cmp.Or(s.Agent, config.LegacyAgent) + th().Faint.Render(" · "+s.ID+" "+stateWord(s))
			if s.RemoteControl {
				line += th().Faint.Render(" · remote control on")
			}
			return line
		}
	}
	s := config.Defaults
	if p.Safety != nil {
		s = *p.Safety
	}
	name, why := resolvedAgent(s.CoordinatorAgent, m.agentList())
	switch {
	case name == "" && len(m.agentList()) > 1 && s.CoordinatorAgent == "":
		return th().Warn.Render(why) + th().Faint.Render(" · not running; enter on the project asks which agent")
	case name == "":
		return th().Warn.Render(why) + th().Faint.Render(" · nothing can start")
	case s.CoordinatorAgent == "":
		name += " (auto)"
	}
	if s.CoordinatorModel != "" {
		name += th().Faint.Render(" · " + s.CoordinatorModel)
	}
	return name + th().Faint.Render(" · not running; enter on the project starts it")
}

// kindStyle is the style of an inbox item's kind: red for what went
// wrong, green for what finished, yellow for the rest.
// kindWords are the inbox kinds in words, where the kind's own name is a
// code (docs/STYLE.md Y3).
var kindWords = map[string]string{
	"thread-resolved":  "resolved",
	"takeover":         "you typed",
	"server-restart":   "server restart",
	"pr-opened":        "PR opened",
	"pr-checks-failed": "checks failed",
	"pr-review":        "PR review",
	"pr-merged":        "PR merged",
	"pr-closed":        "PR closed",
	"pr-conflict":      "PR conflict",
	"close-held":       "kept open",
	"gh-failing":       "PR host failing",
	"link-message":     "message",
	"link-held":        "message held",
	"link-paused":      "link paused",
	"link-undelivered": "not delivered",
	"link-request":     "link request",
	"link-answer":      "link answer",
	"slot-free":        "slot free",
	"guard":            "guard refused",
	"model-refused":    "model refused",
}

// kindWord is an inbox kind (or an ask, "send-back") in words.
func kindWord(kind string) string {
	if w, ok := kindWords[kind]; ok {
		return w
	}
	return strings.ReplaceAll(kind, "-", " ")
}

func kindStyle(kind string) lipgloss.Style {
	switch kind {
	case "pr-checks-failed", "pr-conflict", "exited", "gh-failing", "blocked", "needs-you", "guard", "model-refused":
		return th().Bad
	case "report", "pr-merged", "pr-opened", "task-done", "thread-resolved":
		return th().Good
	}
	return th().Warn
}

// inboxLines are a project's unhandled inbox items, one row for the
// items of one kind for one subject, sel selected, and the row on each
// line. A row reads kind, age, ×N when it stands for more, task, what
// happened, then the task's title, which is what a narrow row cuts.
func inboxLines(items []project.Item, sel, w int) ([]string, int, []int) {
	var lines []string
	var hits []int
	at := -1
	now := time.Now()
	rows := project.Rows(items)
	for i, r := range rows {
		// Columns two cells apart (docs/STYLE.md S2): kind, age, then
		// what happened.
		kind := fit(kindWord(r.Kind), 14)
		when := fmt.Sprintf("%-4s", age(now.Sub(r.Created)))
		rest := oneLine(strings.TrimSpace(strings.Join([]string{r.Task, r.What}, " ")))
		if r.Count > 1 {
			rest = fmt.Sprintf("×%d %s", r.Count, rest)
		}
		hits = append(hits, i)
		if i == sel {
			at = len(lines)
			if r.Title != "" {
				rest += " " + oneLine(r.Title)
			}
			lines = append(lines, th().Sel.Render(fit(kind+"  "+when+"  "+rest, w)))
			continue
		}
		if r.Title != "" {
			rest += " " + th().Faint.Render(oneLine(r.Title))
		}
		lines = append(lines, fit(kindStyle(r.Kind).Render(kind)+"  "+th().Faint.Render(when)+"  "+rest, w)+reset)
	}
	if len(rows) == 0 {
		lines = append(lines, th().Faint.Render("inbox empty"))
		hits = append(hits, noHit)
	}
	return lines, at, hits
}
