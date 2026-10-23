package tui

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The project popup of a paired server's project (docs/SPEC.md §4,
// Remote dashboard; §16.9): A, T and I on its dashboard, or prefix+A, T
// and I over its pane, open the popup's layout with the other server's
// data, read-only. Its board, settings and links with this server come
// from peer.popup, read when it opens and at each tab change; the
// inbox, the coordinator and the threads from the mirror (peer.watch),
// live. Its memory has no tab: nothing of it crosses. Nothing here acts
// on the other server:
// every key that would (delegate, accept, a setting, the access) says
// to change it there. A paired server serves it only while its pane
// switch for this one is view or type; a tm without it, or one whose
// switch is off, gets the remote dashboard's message instead.
//
//	╭─ mini's acme-api ───────────────────────────────────╮
//	│  1 overview   2 inbox 2   3 tasks   4 settings  …    │
//	│ read-only · read 3s ago · change it on mini          │
//	│ …                                                    │

// The remote popup's tabs, in order.
const (
	rtabOverview = iota
	rtabInbox
	rtabTasks
	rtabSettings
	rtabMessages
	rtabCount
)

var rtabNames = [rtabCount]string{"overview", "inbox", "tasks", "settings", "messages"}

// remoteTab is the remote popup's tab the local popup's tab shows (A
// opens the overview, T the tasks, I the inbox).
func remoteTab(tab int) int {
	switch tab {
	case tabInbox:
		return rtabInbox
	case tabTasks:
		return rtabTasks
	case tabSettings:
		return rtabSettings
	case tabMessages:
		return rtabMessages
	}
	return rtabOverview
}

type remotePopupView struct {
	alias, slug string
	tab         int
	// top is each tab's first line shown.
	top [rtabCount]int
	// pp is the last popup read, nil before the first; err why the last
	// read failed; reading while one is out; read when the last came.
	pp      *proto.PeerPopup
	err     error
	reading bool
	read    time.Time
	// hint says why the last key did nothing: it is the other server's.
	hint string
	// msgSel is the Messages tab's selected message.
	msgSel int
}

// remotePopupMsg is a peer.popup read.
type remotePopupMsg struct {
	alias, slug string
	pp          proto.PeerPopup
	err         error
}

// remotePopupOK says whether the paired server alias serves the popup
// of its project to this server now: its tm has it, the link is up and
// its pane switch for this server is view or type. The mirror says.
func remotePopupOK(peers []proto.PeerMirror, alias string) bool {
	pm, _ := findPeer(peers, alias, "")
	return pm != nil && pm.Popup && pm.State == proto.LinkUp && (pm.Panes == proto.PanesView || pm.Panes == proto.PanesType)
}

// openRemotePopup opens the popup of alias's project slug on tab (one
// of the local popup's tabs), or says why not.
func (m *dash) openRemotePopup(alias, slug string, tab int) tea.Cmd {
	if !remotePopupOK(m.data.Peers, alias) {
		m.msg = remoteNoPopup(alias, slug)
		return nil
	}
	v := &remotePopupView{alias: alias, slug: slug, tab: remoteTab(tab)}
	m.push(v)
	return v.load(m)
}

// load reads the popup again, unless a read is out.
func (v *remotePopupView) load(m *dash) tea.Cmd {
	if v.reading {
		return nil
	}
	v.reading = true
	src, alias, slug := m.src, v.alias, v.slug
	return func() tea.Msg {
		pp, err := src.PeerPopup(alias, slug)
		return remotePopupMsg{alias: alias, slug: slug, pp: pp, err: err}
	}
}

// took takes a read.
func (v *remotePopupView) took(msg remotePopupMsg) {
	v.reading = false
	if msg.err != nil {
		v.err = msg.err
		return
	}
	v.pp, v.err, v.read = &msg.pp, nil, time.Now()
}

// remotePopupTook gives a read to the open remote popup it is for.
func (m *dash) remotePopupTook(msg remotePopupMsg) {
	for _, o := range m.stack {
		if v, ok := o.(*remotePopupView); ok && v.alias == msg.alias && v.slug == msg.slug {
			v.took(msg)
		}
	}
}

// remoteActionKeys are the keys that act in the local popup: here they
// say where to change it.
var remoteActionKeys = map[string]string{
	"enter": "", "space": "", "+": "", "-": "", "X": "", "x": "", "D": "delegating", "A": "accepting",
	"E": "the access", "R": "the access", "c": "", "C": "",
}

func (v *remotePopupView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	tab := v.tab
	switch s {
	case "esc":
		m.pop()
		return nil
	case "right", "tab":
		v.tab = (v.tab + 1) % rtabCount
	case "left", "shift+tab":
		v.tab = (v.tab + rtabCount - 1) % rtabCount
	case "1", "2", "3", "4", "5":
		v.tab = int(s[0] - '1')
	case "home":
		v.top[v.tab] = 0
	default:
		if v.tab == rtabMessages {
			if cmd, ok := v.messagesKey(m, s); ok {
				return cmd
			}
		}
		if d, ok := scrollKeys[s]; ok {
			v.top[v.tab] = max(v.top[v.tab]+d, 0)
			return nil
		}
		if what, ok := remoteActionKeys[s]; ok {
			v.hint = v.refusal(what)
		}
		return nil
	}
	if v.tab != tab {
		v.hint = ""
		return v.load(m)
	}
	return nil
}

// refusal is why an action key does nothing: what it would change is
// the other server's.
func (v *remotePopupView) refusal(what string) string {
	if what == "" {
		what = map[int]string{rtabOverview: "the repositories", rtabInbox: "the inbox", rtabTasks: "the tasks",
			rtabSettings: "the settings", rtabMessages: "the links"}[v.tab]
	}
	return fmt.Sprintf("%s of %s are %s's: change it on %s", what, v.slug, v.alias, v.alias)
}

// messages are the Messages tab's messages, newest first.
func (v *remotePopupView) messages() []msgEntry {
	if v.pp == nil {
		return nil
	}
	return remoteMsgEntries(v.pp.Links)
}

// messagesKey moves the Messages tab's selection, and enter opens the
// message's dialog, read-only; ok is false for a key it leaves.
func (v *remotePopupView) messagesKey(m *dash, s string) (tea.Cmd, bool) {
	es := v.messages()
	n := min(len(es), msgShown)
	if d, ok := scrollKeys[s]; ok {
		v.msgSel = moveSel(v.msgSel, d, n)
		return nil, true
	}
	if (s == "enter" || s == "space") && v.msgSel < n {
		return m.openMessage(v.alias+"/"+v.slug, es[v.msgSel], v.alias), true
	}
	return nil, false
}

func (v *remotePopupView) wheel(m *dash, d int) {
	if v.tab == rtabMessages {
		v.messagesKey(m, arrow(d).String())
		return
	}
	v.top[v.tab] = max(v.top[v.tab]+3*d, 0)
}

// click picks a tab on the tab bar, or opens the clicked message.
func (v *remotePopupView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if item >= 0 && v.tab == rtabMessages {
		v.msgSel = item
		cmd, _ := v.messagesKey(m, "enter")
		return cmd
	}
	if item != tabHit {
		return nil
	}
	if t := v.tabAt(col); t >= 0 && t != v.tab {
		v.tab, v.hint = t, ""
		return v.load(m)
	}
	return nil
}

// tabLabel is tab i's label, with the inbox's count.
func (v *remotePopupView) tabLabel(w *proto.ProjectWatch, i int) string {
	label := fmt.Sprintf("%d %s", i+1, rtabNames[i])
	if i == rtabInbox && w != nil && len(w.Inbox) > 0 {
		label += fmt.Sprintf(" %d", len(w.Inbox))
	}
	return label
}

func (v *remotePopupView) tabBar(w *proto.ProjectWatch) string {
	var parts []string
	for i := range rtabCount {
		label := " " + v.tabLabel(w, i) + " "
		if i == v.tab {
			parts = append(parts, th().Sel.Render(label))
		} else {
			parts = append(parts, th().Faint.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

// tabAt is the tab at column col of the tab bar, -1 for none. The
// labels it measures are the bar's without the inbox count, which is
// at most a few cells: a click a little past a label still picks it.
func (v *remotePopupView) tabAt(col int) int {
	x := 0
	for i := range rtabCount {
		w := len([]rune(v.tabLabel(nil, i))) + 2
		if col >= x && col < x+w+3 {
			return i
		}
		x += w + 1
	}
	return -1
}

// title is the popup's title: whose project it is.
func (v *remotePopupView) title() string { return v.alias + "'s " + v.slug }

func (v *remotePopupView) render(m *dash) string { return m.popup(v.box(m)) }

func (v *remotePopupView) box(m *dash) box {
	w := m.inner(viewWidth)
	pm, p := findPeer(m.data.Peers, v.alias, v.slug)
	var watch *proto.ProjectWatch
	var sessions []proto.PeerSession
	if p != nil {
		watch, sessions = p.Watch, p.Sessions
	}
	var body []string
	var bodyHits []int
	sel := -1
	switch {
	case v.pp == nil && v.err != nil:
		body = remotePopupErr(v.alias, v.err, w)
	case v.pp == nil:
		body = []string{th().Faint.Render("reading from " + v.alias + "…")}
	default:
		switch v.tab {
		case rtabOverview:
			body = v.overview(v.pp, sessions, w)
		case rtabInbox:
			body = remoteInbox(v.alias, watch, w)
		case rtabTasks:
			body = remoteTasks(v.pp.Tasks, w)
		case rtabSettings:
			body = remoteSettings(v.slug, v.pp.Settings, w)
		case rtabMessages:
			body, sel, bodyHits = v.messageLines(w)
		}
	}
	status := th().Faint.Render(v.statusWords(pm))
	if v.hint != "" {
		status = th().Warn.Render(v.hint)
	}
	head := []string{v.tabBar(watch), fit(status, w) + reset, ""}
	hits := make([]int, len(head)+len(body))
	for i := range hits {
		hits[i] = noHit
	}
	hits[0] = tabHit
	copy(hits[len(head):], bodyHits)
	keys := "← → tabs · ↑ ↓ scroll · esc close"
	if v.tab == rtabMessages {
		keys = "enter open · ← → tabs · ↑ ↓ move · esc close"
	}
	b := box{title: v.title(), head: head, body: body, sel: sel, hits: hits, keys: keys}
	rows := m.boxRows(b) - len(head)
	v.top[v.tab] = min(v.top[v.tab], max(len(body)-max(rows, 1), 0))
	b.scroll = v.top[v.tab]
	return b
}

// statusWords say what the popup shows: read-only, how fresh, and where
// it changes.
func (v *remotePopupView) statusWords(pm *proto.PeerMirror) string {
	words := []string{"read-only"}
	switch {
	case pm != nil && pm.State != proto.LinkUp:
		words = append(words, v.alias+" is offline; the last read")
	case v.pp != nil && v.err != nil:
		words = append(words, "the last read failed; shown as read "+age(time.Since(v.read))+" ago")
	case v.reading && v.pp != nil:
		words = append(words, "reading…")
	case v.pp != nil:
		words = append(words, "read "+age(time.Since(v.read))+" ago")
	}
	return strings.Join(append(words, "change it on "+v.alias), " · ")
}

// remotePopupErr is why the popup has nothing to show: the other
// server's tm serves none, its switch is off, or it can't be reached.
func remotePopupErr(alias string, err error, w int) []string {
	var pe *proto.Error
	text := oneLine(err.Error())
	if errors.As(err, &pe) {
		text = oneLine(pe.Message)
	}
	var out []string
	for _, l := range wrapLines(text, max(w, 8)) {
		out = append(out, th().Warn.Render(l))
	}
	return append(out, "", th().Faint.Render("Its dashboard shows what "+alias+" shares; esc closes this."))
}

// remoteField is a label column, as the local popup's overview has it.
func remoteField(label string) string { return th().Faint.Render(fmt.Sprintf("%-14s", label)) }

// overview is the project's slug, goal, repositories (how many), the
// server, its coordinator and threads (from the mirror), and its tasks
// counted.
func (v *remotePopupView) overview(pp *proto.PeerPopup, sessions []proto.PeerSession, w int) []string {
	var out []string
	out = append(out, remoteField("Project")+th().Head.Render(v.slug)+th().Faint.Render(" on "+v.alias))
	if g := strings.TrimSpace(pp.Goal); g == "" {
		out = append(out, remoteField("Goal")+th().Faint.Render("none set"))
	} else {
		for i, l := range wrapLines(oneLine(g), max(w-14, 8)) {
			label := remoteField("")
			if i == 0 {
				label = remoteField("Goal")
			}
			out = append(out, label+l)
		}
	}
	out = append(out, "")
	repos := fmt.Sprintf("%d", pp.Repos)
	out = append(out, remoteField("Repositories")+repos+th().Faint.Render(" on "+v.alias+"; their paths stay there"))
	out = append(out, remoteField("Machines")+v.alias)
	out = append(out, "", remoteField("Coordinator")+remoteCoord(sessions))
	threads := map[string][]string{}
	for _, s := range sessions {
		if s.Role == proto.RoleThread && s.Thread != "" {
			a := cmp.Or(oneLine(s.Agent), config.LegacyAgent)
			threads[a] = append(threads[a], s.Thread)
		}
	}
	if len(threads) == 0 {
		out = append(out, remoteField("Threads")+th().Faint.Render("none running"))
	}
	for i, a := range slices.Sorted(maps.Keys(threads)) {
		label := remoteField("")
		if i == 0 {
			label = remoteField("Threads")
		}
		out = append(out, label+a+th().Faint.Render(" · "+strings.Join(threads[a], " ")))
	}
	counts := map[tasks.Group]int{}
	for _, t := range pp.Tasks {
		counts[tasks.GroupOf(tasks.Status(t.Status))]++
	}
	out = append(out, "", remoteField("Tasks")+fmt.Sprintf("%s · %d in motion · %d on deck · %d done",
		needsYouWords(counts[tasks.NeedsYou]), counts[tasks.InMotion], counts[tasks.OnDeck], counts[tasks.DoneG]))
	return out
}

// remoteInbox is the project's inbox as the mirror has it: each item's
// kind, count, task and title, never its words.
func remoteInbox(alias string, w *proto.ProjectWatch, width int) []string {
	if w == nil {
		return []string{th().Faint.Render("Nothing read from " + alias + " yet.")}
	}
	var out []string
	for _, it := range w.Inbox {
		kind := fit(kindWord(oneLine(it.Kind)), 14)
		rest := oneLine(it.Task)
		if it.Count > 1 {
			rest = fmt.Sprintf("×%d %s", it.Count, rest)
		}
		if it.Title != "" {
			rest += " " + th().Faint.Render(oneLine(it.Title))
		}
		out = append(out, fit(kindStyle(it.Kind).Render(kind)+"  "+rest, width)+reset)
	}
	if len(out) == 0 {
		out = append(out, th().Faint.Render("inbox empty"))
	}
	return append(out, "", th().Faint.Render(alias+"'s coordinator handles these; what each says stays on "+alias+"."))
}

// remoteTasks is the board by group, as tm task list has it: id,
// status, title, then the thread and the steps.
func remoteTasks(ts []proto.PeerTask, w int) []string {
	var out []string
	for _, g := range tasks.Groups {
		var rows []string
		for _, t := range ts {
			if tasks.GroupOf(tasks.Status(t.Status)) != g {
				continue
			}
			line := fmt.Sprintf("%-6s %-8s %s", fmt.Sprintf("T%d", t.ID), oneLine(t.Status), oneLine(t.Title))
			var bits []string
			if t.Thread != "" {
				bits = append(bits, t.Thread)
			}
			if t.StepsTotal > 0 {
				bits = append(bits, fmt.Sprintf("%d/%d steps", t.StepsDone, t.StepsTotal))
			}
			if len(bits) > 0 {
				line += th().Faint.Render(" · " + strings.Join(bits, " · "))
			}
			rows = append(rows, fit(line, w)+reset)
		}
		if len(rows) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, th().Head.Render(fmt.Sprintf("%s %d", g, len(rows))))
		out = append(out, rows...)
	}
	if len(out) == 0 {
		out = append(out, th().Faint.Render("no tasks"))
	}
	return out
}

// peerKeys are the settings keys a paired server's popup carries: its
// fields' JSON names.
var peerKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeFor[proto.PeerSettings]()
	for i := range t.NumField() {
		k, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		keys[k] = true
	}
	return keys
}()

// safetyOfPeer is a paired server's settings as config.Safety, field by
// field; what it doesn't send is zero, and no row shows it.
func safetyOfPeer(p proto.PeerSettings) config.Safety {
	return config.Safety{StartThreads: p.StartThreads, Yolo: p.Yolo, CoordinatorApproves: p.CoordinatorApproves,
		ParallelThreads: p.ParallelThreads, AutoClose: p.AutoClose, AutoCloseDays: p.AutoCloseDays, PRFollowup: p.PRFollowup,
		PRPollSeconds: p.PRPollSeconds, CompleteTasks: p.CompleteTasks, CoordinatorRemoteControl: p.CoordinatorRemoteControl,
		AutoClear: p.AutoClear, CoordinatorMerges: p.CoordinatorMerges, FastForwardCheckout: p.FastForwardCheckout,
		Protection: p.Protection, CoordinatorAccess: p.CoordinatorAccess, ThreadAgent: p.ThreadAgent,
		CoordinatorAgent: p.CoordinatorAgent, Models: p.Models, CoordinatorModel: p.CoordinatorModel, Active: p.Active,
		Paused: p.Paused, Share: p.Share, Merge: p.Merge, MergeMethod: p.MergeMethod, Guard: p.Guard, GuardMode: p.GuardMode,
		ArchiveTasksDays: p.ArchiveTasksDays, ArchiveThreadsDays: p.ArchiveThreadsDays, ArchiveInboxDays: p.ArchiveInboxDays,
		ArchiveJournalDays: p.ArchiveJournalDays, LinkBurst: p.LinkBurst, LinkPerHour: p.LinkPerHour, LinkMaxKB: p.LinkMaxKB,
		LinkFileMB: p.LinkFileMB, LinkFileKbps: p.LinkFileKbps}
}

// remoteSettings are the project's settings as its Settings tab words
// them, every row whose keys the paired server sent, then how its
// agents run and its own state; read-only.
func remoteSettings(slug string, ps proto.PeerSettings, w int) []string {
	s := safetyOfPeer(ps)
	sr := newSafetyRows(slug)
	type kv struct{ label, value string }
	var rows []kv
	for _, r := range append(sr.sharedRows(), sr.historyRow()) {
		if r.word == nil || len(r.keys) == 0 || !allKeys(r.keys) {
			continue
		}
		rows = append(rows, kv{r.label, r.word(s)})
	}
	access := []kv{
		{"Protection", cmp.Or(s.Protection, "the default")},
		{"Yolo mode", onOff(s.Yolo)},
		{"Guard", onOff(s.Guard) + map[bool]string{true: " · " + s.GuardMode, false: ""}[s.Guard && s.GuardMode != ""]},
		{"Coordinator access", onOff(s.CoordinatorAccess)},
		{"Merge", cmp.Or(s.Merge, "the default")},
	}
	state := []kv{{"Active", onOff(s.Active)}, {"Paused", onOff(s.Paused)}, {"Shared", onOff(s.Share)}}
	col := 28
	var out []string
	add := func(rs []kv) {
		for _, r := range rs {
			out = append(out, fit(th().Faint.Render(fmt.Sprintf("%-*s", col, r.label))+oneLine(r.value), w)+reset)
		}
	}
	add(rows)
	out = append(out, "", th().Head.Render("Access"))
	add(access)
	out = append(out, "", th().Head.Render("Project"))
	add(state)
	return out
}

// allKeys says whether a paired server's popup carries every key.
func allKeys(keys []string) bool {
	for _, k := range keys {
		if !peerKeys[k] {
			return false
		}
	}
	return true
}

// messageLines are the Messages tab's lines: the messages of the
// project's links with this server's projects that reached this
// server, as the local tab lists them, headers only; the selected
// line's, and what each picks.
func (v *remotePopupView) messageLines(w int) ([]string, int, []int) {
	es := v.messages()
	v.msgSel = min(v.msgSel, max(min(len(es), msgShown)-1, 0))
	lines, sel, hits := msgLines(nil, es, v.msgSel, w)
	if len(v.pp.Links) == 0 {
		lines, sel, hits = []string{th().Faint.Render("no links with this server's projects")}, -1, []int{noHit}
	}
	return append(lines, "", th().Faint.Render("→ is "+v.alias+"'s side sending; only messages that reached this server show, sent or read. Their texts are in your project's Messages tab.")), sel, append(hits, noHit, noHit)
}
