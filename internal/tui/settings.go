package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Settings (docs/SPEC.md §4, §11.2): plain labels, each with a line on
// what it does, changed in place with enter or space. The , popup has the
// settings of every project (the prefix key, the layout, and the project
// settings all projects follow); a project's own are the Settings tab of
// its popup (a). The UI never names the file or its keys.
//
// Where they live: the prefix key and the projects' settings are the human's, in the settings file, shared by every
// console; the details panel and list width are this console's (ui.json);
// the sidebar is the view's. Which popup is open is each console's own.

// setting is one row of a settings list.
type setting struct {
	label, help string
	// section, when set, heads the row and those after it (the Access
	// tab's Protection and prompts, Guard, …).
	section string
	value   func(m *dash) string
	// change runs on enter or space; nil shows the value only.
	change func(m *dash) tea.Cmd
	// adjust changes a number by delta, on + and -.
	adjust func(m *dash, delta int) tea.Cmd
	// note adds lines under the help (e.g. the running coordinator's
	// remote control when it differs).
	note func(m *dash) []string
	// from follows the value, faint: where it comes from ("all
	// projects" on a project's setting it doesn't set itself).
	from func(m *dash) string
	// unset runs on X: a project's own value goes,
	// so it follows all projects again.
	unset func(m *dash) tea.Cmd
	// keys are the settings-file keys a project's row follows all
	// projects in, and word their all-projects value in words: the
	// safety rows' scope fills in from, unset and note from them.
	keys []string
	word func(config.Safety) string
}

// settingsList is a list of settings with a selection.
type settingsList struct {
	rows []setting
	sel  int
	// build, when set, makes the rows anew from what the console has
	// now (the Access tab's, which show only what the protection level
	// uses); sync calls it, keeping the selection on the same row.
	build func(m *dash) []setting
	// lw is the widest label's width, measured on lwRows rows
	// (labelWidth).
	lw     int
	lwRows int
}

// labelWidth is the widest label's width, measured once for the rows (a
// list's rows are set as it is made, and a label never changes), not
// on every frame.
func (l *settingsList) labelWidth() int {
	if l.lwRows != len(l.rows) {
		l.lw, l.lwRows = 0, len(l.rows)
		for _, r := range l.rows {
			l.lw = max(l.lw, len([]rune(r.label)))
		}
	}
	return l.lw
}

// sync makes the rows anew with build, if the list has one: the
// selection stays on the row of the same label, or the nearest.
func (l *settingsList) sync(m *dash) {
	if l.build == nil {
		return
	}
	label := ""
	if l.sel < len(l.rows) {
		label = l.rows[l.sel].label
	}
	l.rows, l.lwRows = l.build(m), -1
	if i := slices.IndexFunc(l.rows, func(r setting) bool { return r.label == label }); i >= 0 {
		l.sel = i
	} else {
		l.sel = max(0, min(l.sel, len(l.rows)-1))
	}
}

func (l *settingsList) key(m *dash, k tea.KeyPressMsg) (tea.Cmd, bool) {
	l.sync(m)
	var run func(m *dash) tea.Cmd
	if l.sel < len(l.rows) {
		r := l.rows[l.sel]
		switch k.String() {
		case "enter", "space", " ":
			if r.change != nil {
				run = r.change
			}
		case "+", "-":
			if r.adjust != nil {
				d := map[bool]int{true: -1, false: 1}[k.String() == "-"]
				run = func(m *dash) tea.Cmd { return r.adjust(m, d) }
			}
		case "X":
			if r.unset != nil {
				run = r.unset
			}
		}
	}
	switch k.String() {
	case "up", "down", "pgup", "pgdown":
		l.sel = moveSel(l.sel, scrollKeys[k.String()], len(l.rows))
	case "enter", "space", " ", "+", "-", "X":
		if run == nil {
			break
		}
		if m.busy {
			// A save is in flight: the key waits its turn, on the row it
			// was pressed on, and runs on top of the saved value.
			m.queued = append(m.queued, run)
			return nil, true
		}
		return run(m), true
	default:
		return nil, false
	}
	return nil, true
}

// runQueued runs the next key that waited for a save, if any: nil when
// none did. The rest stay queued until its save is done.
func (m *dash) runQueued() tea.Cmd {
	for len(m.queued) > 0 {
		run := m.queued[0]
		m.queued = m.queued[1:]
		if cmd := run(m); cmd != nil {
			return cmd
		}
	}
	return nil
}

// lines draws the list w cells wide; sel is the selected row's last
// line (its help or note), so the whole setting scrolls into view, hits what a click on each line picks: a setting's own line is its
// index, which changes it; its help is helpHit more, which selects it.
func (l *settingsList) lines(m *dash, w int) (out []string, sel int, hits []int) {
	l.sync(m)
	sel = -1
	defer func() {
		for len(hits) < len(out) {
			hits = append(hits, noHit)
		}
	}()
	lw := l.labelWidth()
	for i, r := range l.rows {
		if i > 0 {
			out = append(out, "")
			hits = append(hits, noHit)
		}
		if r.section != "" {
			out = append(out, th().Accent.Render(r.section), "")
			hits = append(hits, noHit, noHit)
		}
		hits = append(hits, i)
		value := r.value(m)
		btns := ""
		if r.adjust != nil {
			btns = adjustButtons
		}
		from := ""
		if r.from != nil {
			if f := r.from(m); f != "" {
				from = " · " + f
			}
		}
		text := fit(r.label, lw) + "  " + value + btns + from
		if i == l.sel {
			out = append(out, th().Sel.Render(fit(text, w)))
		} else {
			out = append(out, th().Head.Render(fit(r.label, lw))+"  "+th().Accent.Render(value)+th().Head.Render(btns)+th().Faint.Render(from))
		}
		out = append(out, faintLines(r.help, w)...)
		if r.note != nil {
			out = append(out, r.note(m)...)
		}
		if i == l.sel {
			sel = len(out) - 1
		}
		for len(hits) < len(out) {
			hits = append(hits, i+helpHit)
		}
	}
	return out, sel, hits
}

// helpHit marks a setting's help lines in its list's hits.
const helpHit = 1 << 16

// adjustButtons follow a number's value: a click on − or + is - or +.
const adjustButtons = "  −  +"

// click selects the clicked setting; a click on the selected setting's
// own line changes it, as enter does, and on a number's − or + (col is
// the column in the line) as - or + do (docs/SPEC.md §4).
func (l *settingsList) click(m *dash, item, col int) tea.Cmd {
	l.sync(m)
	if item >= helpHit {
		l.sel = moveSel(item-helpHit, 0, len(l.rows))
		return nil
	}
	// A click selects a setting; a click on the selected one changes it,
	// as enter does (or steps a number with its − +).
	if i := moveSel(item, 0, len(l.rows)); i != l.sel {
		l.sel = i
		return nil
	}
	key := "enter"
	if r := l.rows[l.sel]; r.adjust != nil {
		lw := l.labelWidth()
		// − is 2 cells after the value, + 3 after −; a cell either side
		// counts.
		minus := lw + 2 + ansi.StringWidth(r.value(m)) + 2
		switch {
		case col >= minus-1 && col <= minus+1:
			key = "-"
		case col >= minus+2 && col <= minus+4:
			key = "+"
		}
	}
	cmd, _ := l.key(m, keyMsg(key))
	return cmd
}

// onOff is a bool as the settings show it.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// setSetting changes a setting in the background, then reloads.
func (m *dash) setSetting(table, key string, value any, msg string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		if err := src.SetSetting(table, key, value); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}

var lineRE = regexp.MustCompile(`line (\d+)`)

// settingsErr is a settings error in the user's words: no file, no keys.
func settingsErr(err error) error {
	if err == nil {
		return nil
	}
	if err == config.ErrForm {
		return fmt.Errorf("%w; it stays as it is", err)
	}
	if m := lineRE.FindStringSubmatch(err.Error()); m != nil {
		return fmt.Errorf("the settings can't be read: line %s is broken", m[1])
	}
	return err
}

// settingsView is the , popup: the settings of every project, in six
// tabs: General, All projects (the project settings every project
// follows unless it sets its own), Sandbox (all projects' sandbox
// lists, settings_sandbox.go), Mods (modstab.go), Peers, the
// paired servers (peerstab.go), and Logs, the server host's logs
// (logstab.go).
type settingsView struct {
	tab   int
	tabs  [3]settingsList
	mods  modsTab
	peers peersTab
	logs  logsTab
	// top is each tab's first line shown; shown the selection's line
	// (plus one) it last scrolled to (projectView.scroll).
	top, shown [len(settingsTabs)]int
}

// settingsTabs are the , popup's tabs.
var settingsTabs = [6]string{"general", "all projects", "access", "mods", "peers", "logs"}

// The Access, Mods, Peers and Logs tabs' indexes.
const (
	tabAllAccess = 2
	tabMods      = 3
	tabPeers     = 4
	tabLogs      = 5
)

func (m *dash) openSettings() {
	m.agentsOK = false
	m.catalogs = m.src.Catalogs()
	rows := globalSettings()
	if safeMode() {
		rows = append([]setting{safeModeRow()}, rows...)
	}
	sv := &settingsView{tabs: [3]settingsList{{rows: rows}, {rows: allProjectsSettings()}, accessList("", m.serverOS())}}
	m.push(sv)
}

// safeModeRow shows tm --no-mods in General: the theme, icons and keymap
// are the built-in ones, whatever the Mods tab has on.
func safeModeRow() setting {
	return setting{label: "Safe mode", help: "tm --no-mods: this console uses the built-in theme, icons, layout and keymap, and loads no user mod's data, so no mod can make keys unreachable. The mods stay installed and on; restart tm without --no-mods to use them.",
		value: func(m *dash) string { return "on (tm --no-mods)" }}
}

func globalSettings() []setting {
	return []setting{
		{label: "Prefix key", help: "Starts the session commands, written prefix+<key> in hints. Inside tmux, which takes ctrl+b, pick another. Enter, then press the new one.",
			value:  func(m *dash) string { return m.prefix },
			change: func(m *dash) tea.Cmd { m.push(&captureView{}); return nil }},
		{label: "Details panel", help: "Where the panel about the selected row shows: right of the list (in dashboards 120 columns or wider), under it (with 16 rows or more), or off; where it has no room, a thread's details show under its row. | on the dashboard turns it off and on. A layout mod may set it. This console only.",
			value: func(m *dash) string {
				p := m.layout.place()
				if p != placeOff && m.panel() == "" {
					p += " (no room: under the row)"
				}
				return p
			},
			change: func(m *dash) tea.Cmd {
				l := m.layout
				switch l.place() {
				case placeRight:
					l.Details, l.DetailsPlace = true, placeBottom
				case placeBottom:
					l.Details = false
				default:
					l.Details, l.DetailsPlace = true, placeRight
				}
				m.setLayout(l)
				return nil
			}},
		{label: "List size", help: "The list's share beside the details panel (its width) or above it (its height); < and > on the dashboard fine-tune it, as dragging the divider does. This console only.",
			value: func(m *dash) string {
				l := m.layout
				switch {
				case l.place() == placeBottom && l.rowCells != 0:
					return fmt.Sprintf("the panel %d rows", l.rowCells)
				case l.place() == placeBottom:
					return fmt.Sprintf("%d%% high", int((1-l.rows())*100+0.5))
				case l.detCells != 0:
					return fmt.Sprintf("the panel %d columns", l.detCells)
				}
				return fmt.Sprintf("%d%% wide", int(l.Split*100+0.5))
			},
			change: func(m *dash) tea.Cmd {
				l := m.layout
				if l.place() == placeBottom {
					l.DetailsRows, l.rowCells = nextRows(l.rows()), 0
				} else {
					l.Split, l.detCells = nextSplit(l.Split), 0
				}
				m.setLayout(l)
				return nil
			}},
		{label: "Sidebar", help: "The projects sidebar: full, the slim strip or hidden (⚑ N in the header counts what needs you; b on the dashboard). Every console of this view follows.",
			value:  func(m *dash) string { return m.layout.mode() },
			change: func(m *dash) tea.Cmd { return m.sideKey("B") }},
		{label: "Theme", help: "The colours of every part of tm: follow terminal, the built-in one, which uses your terminal's own theme (Ghostty's, say) in tm and in the panes, Claude's too, or a theme from a mod that is on, which may differ light and dark. Each console follows its terminal's light or dark scheme.",
			value: func(m *dash) string { return themeLabel(themeSetting()) },
			change: func(m *dash) tea.Cmd {
				return withMods(func(m *dash, r *mod.Registry) tea.Cmd {
					choices := modChoicesIn(r, []string{ThemeDefault}, mod.KindTheme)
					next := choices[(slices.Index(choices, themeSetting())+1)%len(choices)]
					return m.pickMod("theme", next, useTheme(r, next))
				})
			}},
		{label: "Icons", help: "The glyphs of the sidebar's tree, states and progress: Nerd Font icons, Unicode shapes, ASCII for any font, or a set from a mod that is on. Auto picks Nerd Font icons in Ghostty, Unicode elsewhere. Each console decides auto for itself.",
			value: func(m *dash) string {
				v := iconsSetting()
				if v == IconsAuto || ic().name != v {
					return v + " (" + ic().name + ")"
				}
				return v
			},
			change: func(m *dash) tea.Cmd {
				return withMods(func(m *dash, r *mod.Registry) tea.Cmd {
					choices := modChoicesIn(r, IconChoices, mod.KindIcons)
					next := choices[(slices.Index(choices, iconsSetting())+1)%len(choices)]
					return m.pickMod("icons", next, setIconsFrom(r, next))
				})
			}},
		{label: "Layout", help: "The built-in layout, or one from a mod that is on: the sidebar's columns (a mod's among them), width and mode, the details panel's place and size, the header, the footer, the status bar and how compact the rows are. A layout sets only what it changes; what you change under it (a drag, { }, b, < >, |) is kept for it, and the rest is yours as before. The state, paused, inactive and needs-you columns always show. The sidebar and the status bar are the view's: every console of this view follows.",
			value: func(m *dash) string { return layoutValue() },
			change: func(m *dash) tea.Cmd {
				return withMods(func(m *dash, r *mod.Registry) tea.Cmd {
					choices := modChoicesIn(r, []string{ThemeDefault}, mod.KindLayout)
					next := choices[(slices.Index(choices, layoutValue())+1)%len(choices)]
					spec, err := modLayout(r, next)
					useLayoutValue(next, spec)
					m.useLayout(next, spec, true)
					cmd := m.pickMod("layout", next, err)
					if m.view == nil {
						return cmd
					}
					return tea.Batch(cmd, m.call(proto.MethodViewColumns, proto.ViewParams{Columns: layoutColumns(spec)}))
				})
			}},
		{label: "Context hint", help: "Offer to save and clear a coordinator once its context window is this full: a toast in its pane, a bell and a yellow percent on its sidebar row (red from 80%). prefix+C, or /saveclear in its pane, has it save what it learned, then clears it (tm clear); a /clear typed by hand skips the save. Off never says it.",
			value: func(m *dash) string {
				if m.data.ContextHint <= 0 {
					return "off"
				}
				return fmt.Sprintf("%d%%", m.data.ContextHint)
			},
			change: func(m *dash) tea.Cmd {
				next := ContextHintChoices[(slices.Index(ContextHintChoices, m.data.ContextHint)+1)%len(ContextHintChoices)]
				m.data.ContextHint = next
				return m.setSetting("ui", "context_hint", next, "context hint "+contextHintWords(next))
			}},
		{label: "Needs you first", help: "The sidebar lists the projects where something needs you (a thread waits on you, a task to review, a question) first, the active ones above the inactive as always. Off keeps them in their usual order. Every console follows.",
			value: func(m *dash) string { return onOff(m.data.NeedsFirst) },
			change: func(m *dash) tea.Cmd {
				on := !m.data.NeedsFirst
				m.data.NeedsFirst = on
				return m.setSetting("ui", "needs_first", on, "needs you first "+onOff(on))
			}},
		{label: "Models", help: "Each agent's models a thread may run, a line on when each fits, and the default. Agents release new ones often: add, change or remove them here. Enter lists them.",
			value:  func(m *dash) string { return catalogWords(m.catalogs) },
			change: func(m *dash) tea.Cmd { m.openCatalog(); return nil }},
	}
}

// pickMod saves a [ui] pick of a built-in or a mod's theme, icons or
// layout (key), which this console has just applied: "default" as
// unset. err says why a mod's didn't load, and so the built-in
// one is in use.
func (m *dash) pickMod(key, value string, err error) tea.Cmd {
	shown := value
	if key == "theme" {
		shown = themeLabel(value)
	}
	msg := key + ": " + shown + "; consoles started earlier pick it up when they next open"
	if key == "layout" {
		msg = key + ": " + value
	}
	if err != nil {
		msg = key + ": " + shown + " doesn't load (" + oneLine(err.Error()) + "); the built-in one is used"
	}
	var v any = value
	if value == ThemeDefault {
		v = nil
	}
	return m.setSetting("ui", key, v, msg)
}

// splitSteps are the list widths enter steps through.
var splitSteps = []float64{0.5, 0.65, 0.8}

func nextSplit(cur float64) float64 {
	for _, s := range splitSteps {
		if s > cur+0.001 {
			return s
		}
	}
	return splitSteps[0]
}

// rowsSteps are the bottom panel's shares enter steps through: the
// list 70%, 60%, 50% high.
var rowsSteps = []float64{0.3, 0.4, 0.5}

func nextRows(cur float64) float64 {
	for _, s := range rowsSteps {
		if s > cur+0.001 {
			return s
		}
	}
	return rowsSteps[0]
}

func (sv *settingsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if sv.tab == tabMods && s != "h" && s != "left" && s != "right" {
		// Its own keys first: l is its log.
		if cmd, ok := sv.mods.key(m, k); ok {
			return cmd
		}
	}
	if sv.tab == tabPeers {
		if cmd, ok := sv.peers.key(m, k); ok {
			return cmd
		}
	}
	if sv.tab == tabLogs && (sv.logs.typing || s != "h" && s != "l" && s != "left" && s != "right" && s != "esc") {
		// Its own keys first: l and h stay the tabs', as in Mods.
		if cmd, ok := sv.logs.key(m, k); ok {
			return cmd
		}
	}
	n := len(settingsTabs)
	switch s {
	case "esc":
		m.pop()
		return nil
	case "right", "l":
		sv.tab = (sv.tab + 1) % n
		return nil
	case "left", "h":
		sv.tab = (sv.tab + n - 1) % n
		return nil
	case "1", "2", "3", "4", "5", "6":
		sv.tab = int(s[0] - '1')
		if sv.tab == tabLogs {
			return sv.logs.poll(m)
		}
		return nil
	}
	if sv.tab == tabMods || sv.tab == tabPeers || sv.tab == tabLogs {
		return nil
	}
	if sv.tab == tabAllAccess && s == "R" {
		return m.resetAccess("")
	}
	cmd, _ := sv.tabs[sv.tab].key(m, k)
	return cmd
}

// tabLines are tab i's lines, its selected line and what a click on
// each line picks.
func (sv *settingsView) tabLines(m *dash, i, w int) ([]string, int, []int) {
	switch i {
	case tabMods:
		return sv.mods.lines(m, w)
	case tabPeers:
		return sv.peers.lines(m, w)
	case tabAllAccess:
		head := append(accessHead(m, "", w), "")
		lines, sel, hits := sv.tabs[i].lines(m, w)
		if sel >= 0 {
			sel += len(head)
		}
		for range head {
			hits = append([]int{noHit}, hits...)
		}
		return append(head, lines...), sel, hits
	}
	return sv.tabs[i].lines(m, w)
}

func (sv *settingsView) render(m *dash) string {
	w := m.inner(viewWidth)
	if sv.tab == tabLogs {
		return sv.renderLogs(m, w)
	}
	lines, sel, hits := sv.tabLines(m, sv.tab, w)
	lines = append(lines, "")
	keys := "enter change · ↑ ↓ move · ← → tabs · esc close"
	switch sv.tab {
	case 0:
		lines = append(lines, faintLines("A project's own settings (starting threads, remote control, …) and access (yolo mode, the guard, …) are in its popup: A on the dashboard, prefix+A in a session.", w)...)
	case tabMods:
		lines = append(lines, faintLines("Mods extend terminatr: themes, keys, columns, panels. They never change the safety settings, and no agent can install or turn one on. I installs one, E lists the examples.", w)...)
		keys = modsKeys
	case tabPeers:
		lines = append(lines, faintLines("Paired servers see the projects you share with them, read-only, in their sidebar; you see theirs in yours. No agent can pair, share or remove a server.", w)...)
		keys = peersKeys
	case tabAllAccess:
		lines = append(lines, faintLines("Every project's threads get these, a new one's too. A project's lists add to these, and it may drop one of them, in its popup's Access tab (A on the dashboard, prefix+A in a session); its own switches replace these, X there makes it follow these again.", w)...)
		keys = "enter change · R reset · ↑ ↓ move · ← → tabs · esc close"
	default:
		lines = append(lines, faintLines("Every project follows these, a new one too, unless it sets its own in its popup (A on the dashboard, prefix+A in a session); X there makes it follow these again.", w)...)
		keys = "enter change · + - number · ↑ ↓ move · ← → tabs · esc close"
	}
	about := aboutLines(m, w)
	head := append([]string{sv.tabBar()}, about...)
	head = append(head, "")
	all := []int{tabHit}
	for range len(head) - 1 {
		all = append(all, noHit)
	}
	all = append(all, hits...)
	for len(all) < len(head)+len(lines) {
		all = append(all, noHit)
	}
	b := box{title: "Settings", head: head, body: lines, sel: -1, hits: all, keys: keys}
	b.scroll = sv.scroll(m.boxRows(b)-len(head), sel, len(lines), lastRow(hits, sel))
	return m.popup(b)
}

// aboutLines is the popup's faint About line: this tm, the server it
// talks to (and when that is another build, which a restart fixes) and
// whether the daily check for a newer release is on.
func aboutLines(m *dash, w int) []string {
	text := "About: tm " + version.Version + " · server "
	switch s := m.data.Server; {
	case !m.loaded || !m.data.ServerOK || s.Version == "":
		text += "unknown"
	case s.Build != version.BuildID():
		text += s.Version + " (another build)"
	default:
		text += s.Version
	}
	text += " · update check " + onOff(m.data.UpdateCheck)
	return faintLines(text, w)
}

// lastRow is whether line sel belongs to the tab's last row, whose
// note or help below it then scrolls into view with it.
func lastRow(hits []int, sel int) bool {
	if sel < 0 || sel >= len(hits) || hits[sel] < 0 {
		return false
	}
	return hits[sel] == slices.Max(hits)
}

// scroll is the open tab's first line shown, as projectView.scroll; on
// the last row it goes on to the end, so the closing note shows.
func (sv *settingsView) scroll(rows, sel, n int, last bool) int {
	t := sv.top[sv.tab]
	if sel >= 0 && sel+1 != sv.shown[sv.tab] {
		sv.shown[sv.tab] = sel + 1
		t = min(t, sel)
		t = max(t, sel-rows+1)
		if last {
			t = n
		}
	}
	t = min(max(t, 0), max(n-max(rows, 1), 0))
	sv.top[sv.tab] = t
	return t
}

func (sv *settingsView) tabBar() string {
	var parts []string
	for i, name := range settingsTabs {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == sv.tab {
			parts = append(parts, th().Sel.Render(label))
		} else {
			parts = append(parts, th().Faint.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

func (sv *settingsView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if item == tabHit {
		x := 0
		for i, name := range settingsTabs {
			w := len([]rune(fmt.Sprintf("%d %s", i+1, name))) + 2
			if col >= x && col < x+w {
				sv.tab = i
			}
			x += w + 1
		}
		return nil
	}
	switch sv.tab {
	case tabMods:
		if item >= 0 {
			sv.mods.sel = item
		}
		return nil
	case tabPeers:
		return sv.peers.click(m, item, col)
	case tabLogs:
		return nil
	}
	return sv.tabs[sv.tab].click(m, item, col)
}

func (sv *settingsView) wheel(m *dash, d int) {
	switch sv.tab {
	case tabMods:
		sv.mods.key(m, arrow(d))
		return
	case tabPeers:
		sv.peers.key(m, arrow(d))
		return
	case tabLogs:
		sv.logs.scroll(m, 3*d)
		return
	}
	sv.tabs[sv.tab].key(m, arrow(d))
}

// captureView takes the next ctrl+<key> as the new prefix key.
type captureView struct{ err string }

func (cv *captureView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "esc" {
		m.pop()
		return nil
	}
	c, err := parseChord(k.String())
	if err != nil {
		cv.err = k.String() + " can't be the prefix: press ctrl and a key"
		return nil
	}
	m.pop()
	key := c.String()
	m.prefix = key
	return m.setSetting("keys", "prefix", key, "the prefix is "+key+" now; consoles started earlier pick it up when they next open")
}

func (cv *captureView) render(m *dash) string {
	lines := []string{"Press the new prefix key: ctrl and a key, e.g. ctrl+a.", th().Faint.Render("Now " + m.prefix + ".")}
	if cv.err != "" {
		lines = append(lines, th().Bad.Render(cv.err))
	}
	return m.popup(box{title: "Prefix key", body: lines, sel: -1, keys: "esc cancel", dialog: true})
}

// confirmView asks a yes/no question: Y runs yes, N or esc says no, and
// no other key does anything (enter and a lowercase y or n included: they
// are no answer, so stray typing can't say yes).
type confirmView struct {
	title, question string
	yes             func() tea.Cmd
	// no is the footer message on N or esc.
	no string
}

// confirmKeys are a yes/no question's action row, the same everywhere.
const confirmKeys = "Y yes · N no · esc cancel"

func (m *dash) confirm(title, question string, yes func() tea.Cmd) {
	m.confirmNo(title, question, "unchanged", yes)
}

// confirmNo is confirm, saying no in the footer on N or esc.
func (m *dash) confirmNo(title, question, no string, yes func() tea.Cmd) {
	m.push(&confirmView{title: title, question: question, yes: yes, no: no})
}

func (cv *confirmView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "Y":
		m.pop()
		return cv.yes()
	case "N", "esc":
		m.pop()
		m.msg = cv.no
	}
	return nil
}

func (cv *confirmView) render(m *dash) string {
	return m.popup(box{title: cv.title, body: wrapLines(cv.question, m.inner(dialogWidth)), sel: -1, keys: confirmKeys, dialog: true})
}

// projectSettings are a project's own settings (§11.2), for its popup.
func projectSettings(slug string) []setting {
	return append(safetySettings(slug), linkRow(slug), shareRow(slug))
}

// allProjectsSettings are the settings every project follows unless it
// sets its own (§11.2 All projects), for the , popup.
func allProjectsSettings() []setting { return safetySettings("") }

// ownsModel reports whether project slug sets its own coordinator_model.
func ownsModel(m *dash, slug string) bool {
	p := m.projectData(slug)
	return p != nil && slices.Contains(p.Own, "coordinator_model")
}

// ownsAny reports whether own (a project's own settings) has one of keys.
func ownsAny(own, keys []string) bool {
	for _, k := range keys {
		if slices.Contains(own, k) {
			return true
		}
	}
	return false
}

// ownNote names, under an all-projects setting, the projects that set
// their own value.
func ownNote(m *dash, keys []string) []string {
	return ownersNote(m, func(p ProjectData) bool { return ownsAny(p.Own, keys) })
}

// ownersNote names the projects that set their own (own), under an
// all-projects row.
func ownersNote(m *dash, own func(ProjectData) bool) []string {
	var slugs []string
	for _, p := range m.data.Projects {
		if own(p) {
			slugs = append(slugs, p.Slug)
		}
	}
	switch len(slugs) {
	case 0:
		return nil
	case 1:
		return []string{th().Faint.Render(slugs[0] + " sets its own")}
	}
	return []string{th().Faint.Render(joinAnd(slugs) + " set their own")}
}

// followers names the projects that follow all projects in keys, for
// the yolo question.
func followers(m *dash, keys []string) string {
	var slugs []string
	for _, p := range m.data.Projects {
		if !ownsAny(p.Own, keys) {
			slugs = append(slugs, p.Slug)
		}
	}
	if len(slugs) == 0 {
		return "every project that doesn't set it itself"
	}
	return joinAnd(slugs) + " and every new project"
}

// joinAnd joins words as "a, b and c".
func joinAnd(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// joinNotes is a note of a's lines, then b's.
func joinNotes(a, b func(m *dash) []string) func(m *dash) []string {
	if a == nil {
		return b
	}
	return func(m *dash) []string { return append(a(m), b(m)...) }
}

// toggleActive activates or deactivates a project (the sidebar's space,
// the Active row): deactivating asks first while its coordinator or
// threads run, which it stops, to resume at its activation (§5.1).
func (m *dash) toggleActive(slug string, on bool) tea.Cmd { return m.toggleActiveVia(slug, on, nil) }

// toggleActiveVia is toggleActive for binding b (nil for the core's own
// key); a mod's bound project.activate always asks first, naming which.
func (m *dash) toggleActiveVia(slug string, on bool, b *binding) tea.Cmd {
	set := func() tea.Cmd {
		if p := m.projectData(slug); p != nil && p.Safety != nil {
			s := *p.Safety
			s.Active = on
			p.Safety = &s
			m.rebuild()
		}
		return m.lifecycleVia(slug, map[bool]string{true: "activate", false: "deactivate"}[on], b)
	}
	busy := runningIn(m.data.Sessions, slug)
	switch {
	case !on && len(busy) > 0:
		m.confirmNo("Deactivate "+slug, DeactivateQuestion(slug, busy), slug+" stays active", set)
		return nil
	case b != nil && on:
		m.confirmNo("Activate "+slug, "Activate "+slug+"? Its coordinator starts, and its threads resume.", slug+" stays inactive", set)
		return nil
	case b != nil:
		m.confirmNo("Deactivate "+slug, "Deactivate "+slug+"? Nothing of it runs now; it stays inactive until you activate it.", slug+" stays active", set)
		return nil
	}
	return set()
}

// lifecycle pauses, resumes, archives or deletes a project in the
// background, then reloads.
func (m *dash) lifecycle(slug, verb string) tea.Cmd { return m.lifecycleVia(slug, verb, nil) }

func (m *dash) lifecycleVia(slug, verb string, b *binding) tea.Cmd {
	src := m.src
	return m.actVia(b, func() (actionMsg, bool) {
		msg, err := src.Lifecycle(slug, verb)
		return actionMsg{msg: msg, err: err}, err == nil
	})
}

// capSteps are the caps enter steps through; + and - fine-tune.
var capSteps = []int{1, 2, 3, 5, 10, 15, 20}

// globalCapSteps are the caps across projects enter steps through; 0 is
// off.
var globalCapSteps = []int{0, 5, 10, 15, 20, 30}

// globalCapWords is global_parallel_threads as the popup shows it.
func globalCapWords(n int) string {
	if n <= 0 {
		return "off"
	}
	return fmt.Sprint(n)
}

// nextStep is the first step above cur, or the first one.
func nextStep(steps []int, cur int) int {
	for _, s := range steps {
		if s > cur {
			return s
		}
	}
	return steps[0]
}

// agentWords is an agent setting's value: the agent it names, or, unset,
// the one installed agent (auto), or that none or several are installed.
func agentWords(setting string, installed []string) string {
	if setting != "" {
		return setting
	}
	name, why := resolvedAgent(setting, installed)
	if name != "" {
		return name + " (auto)"
	}
	return why
}

// resolvedAgent is the agent an unset or set agent setting runs, or why
// there is none (models.Resolve, in words).
func resolvedAgent(setting string, installed []string) (string, string) {
	switch {
	case setting != "" && slices.Contains(installed, setting):
		return setting, ""
	case setting != "":
		return "", setting + " isn't installed"
	case len(installed) == 1:
		return installed[0], ""
	case len(installed) == 0:
		return "", "no agent installed"
	}
	return "", "not chosen (" + strings.Join(installed, ", ") + ")"
}

// modelWords is the models setting as the popup shows it.
func modelWords(allow []string) string {
	if len(allow) == 0 {
		return "any"
	}
	return strings.Join(allow, ", ")
}

// modelsView lists the models a thread may use, one to allow or leave
// out with enter or space; each change is saved at once. At least one
// stays allowed. An allowed name no agent's catalog lists any more is
// shown stale, never dropped unasked: enter leaves it out.
type modelsView struct {
	all   []string // every model the agents' catalogs offer
	allow []string // the allowed ones; empty is every model
	sel   int
	err   string
	// save writes the new list (never empty).
	save func(m *dash, list []string) tea.Cmd
}

func (v *modelsView) allowed(name string) bool {
	return len(v.allow) == 0 || slices.Contains(v.allow, name)
}

// rows are the catalogs' models, then the stale allowed names.
func (v *modelsView) rows() []string {
	out := slices.Clone(v.all)
	for _, n := range v.allow {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func (v *modelsView) stale(name string) bool { return !slices.Contains(v.all, name) }

func (v *modelsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	rows := v.rows()
	switch k.String() {
	case "esc", "q":
		m.pop()
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(rows))
	case "enter", "space", " ":
		if v.sel >= len(rows) {
			break
		}
		name := rows[v.sel]
		var next []string
		for _, n := range rows {
			if (n == name) != v.allowed(n) { // flip name, keep the others
				next = append(next, n)
			}
		}
		if !slices.ContainsFunc(next, func(n string) bool { return !v.stale(n) }) {
			v.err = "at least one model stays allowed"
			break
		}
		v.err = ""
		v.allow = next
		if len(next) == len(v.all) && !slices.ContainsFunc(next, v.stale) {
			v.allow = nil
		}
		v.sel = min(v.sel, max(len(v.rows())-1, 0))
		return v.save(m, next)
	}
	return nil
}

func (v *modelsView) render(m *dash) string {
	lines := []string{th().Faint.Render("The models the coordinator may pick for a thread.")}
	w := m.inner(dialogWidth)
	for i, n := range v.rows() {
		mark, word := ic().todoOpen, "left out"
		if v.allowed(n) {
			mark, word = ic().todoDone, "allowed"
		}
		l := fit(mark+" "+n, 16) + "  " + th().Faint.Render(word)
		if v.stale(n) {
			l = fit(mark+" "+n, 16) + "  " + th().Bad.Render("stale: no agent lists it")
		}
		if i == v.sel {
			l = th().Sel.Render(fit(ansi.Strip(l), w))
		}
		lines = append(lines, l)
	}
	if len(v.all) == 0 {
		lines = append(lines, th().Faint.Render("no agent lists models"))
	}
	if v.err != "" {
		lines = append(lines, "", th().Bad.Render(v.err))
	}
	return m.popup(box{title: "Thread models", body: lines, sel: -1, keys: "enter allow or leave out · ↑ ↓ move · esc back", dialog: true})
}

// archiveAges are the retention settings, as Keep history lists them.
var archiveAges = []struct{ key, label, help, verb string }{
	{"archive_tasks_days", "Done tasks", "Days a done task stays on the board before it moves to the task archive (tm task list --archived).", "done tasks move to the archive"},
	{"archive_threads_days", "Resolved threads", "Days a resolved thread's folder stays before it is packed into the threads archive; tm thread show still reads it.", "resolved threads are packed"},
	{"archive_inbox_days", "Handled inbox items", "Days a handled inbox item stays a file of its own before it is bundled into its month's archive.", "handled inbox items are bundled"},
	{"archive_journal_days", "Journal", "Days a line stays in the journal before it moves to its month's compressed file.", "journal lines move out"},
}

// archiveSteps are the ages enter steps through; + and - fine-tune.
var archiveSteps = []int{7, 14, 30, 60, 90, 180, 365}

// historyWords is Keep history's value: one age when all four agree.
func historyWords(s config.Safety) string {
	n := s.ArchiveTasksDays
	if s.ArchiveThreadsDays == n && s.ArchiveInboxDays == n && s.ArchiveJournalDays == n {
		return days(n)
	}
	return fmt.Sprintf("%d · %d · %d · %d days", s.ArchiveTasksDays, s.ArchiveThreadsDays, s.ArchiveInboxDays, s.ArchiveJournalDays)
}

// historyView lists the retention ages: enter steps one, + and - change
// it by a day, X makes a project's follow all projects again.
type historyView struct {
	title string
	list  settingsList
	all   bool
}

func (v *historyView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if s := k.String(); s == "esc" || s == "q" {
		m.pop()
		return nil
	}
	cmd, _ := v.list.key(m, k)
	return cmd
}

func (v *historyView) render(m *dash) string {
	lines, sel, hits := v.list.lines(m, m.inner(viewWidth))
	keys := "enter step · + - a day · X follow all projects · ↑ ↓ move · esc back"
	if v.all {
		keys = "enter step · + - a day · ↑ ↓ move · esc back"
	}
	b := box{title: v.title, body: lines, sel: sel, hits: hits, keys: keys}
	return m.popup(b)
}

func (v *historyView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if item == noHit {
		return nil
	}
	return v.list.click(m, item, col)
}

func (v *historyView) wheel(m *dash, d int) { v.list.key(m, arrow(d)) }

// closeWords is the auto-close setting as the popup shows it.
func closeWords(s config.Safety) string {
	switch s.AutoClose {
	case config.CloseOff:
		return "off"
	case config.CloseDays:
		return days(s.AutoCloseDays) + " after it finishes"
	}
	return "when its pull request merges"
}

// completeWords is the complete-tasks setting as the popup shows it.
func completeWords(v string) string {
	if v == config.CompleteMerged {
		return "when merged"
	}
	return "by you"
}

func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// workingThreads counts slug's threads that count toward its cap
// (thread.IsWorking).
func (m *dash) workingThreads(slug string) int {
	p := m.projectData(slug)
	if p == nil {
		return 0
	}
	recs := make([]*thread.Record, 0, len(p.Threads))
	for _, t := range p.Threads {
		recs = append(recs, t.Record)
	}
	return thread.Working(recs, m.data.Sessions)
}

// workingAll counts the threads of every project that count toward the
// cap across projects (thread.IsWorking).
func (m *dash) workingAll() int {
	n := 0
	for _, p := range m.data.Projects {
		n += m.workingThreads(p.Slug)
	}
	return n
}

// remoteNote says when the running coordinator's remote control differs
// from the setting: prefix+r changed it since it started, or it dropped
// and the ticker turns it back on once the coordinator is idle.
func remoteNote(setting bool, sessions []proto.SessionInfo, slug string) []string {
	for _, s := range sessions {
		if s.Role == proto.RoleCoordinator && s.Project == slug && s.RemoteControl != setting {
			if setting && !s.RemoteHeld {
				return []string{th().Warn.Render("the running coordinator has it off; tm turns it on once the coordinator is idle")}
			}
			return []string{th().Warn.Render("the running coordinator has it " + onOff(s.RemoteControl) + " (prefix+r), until it is started anew")}
		}
	}
	return nil
}

// projectData is slug's data from the last poll.
func (m *dash) projectData(slug string) *ProjectData {
	for i := range m.data.Projects {
		if m.data.Projects[i].Slug == slug {
			return &m.data.Projects[i]
		}
	}
	return nil
}

// ContextHintChoices are the percents the Context hint setting steps
// through; 0 is never.
var ContextHintChoices = []int{0, 30, 40, 50, 60, 70, 80}

func contextHintWords(n int) string {
	if n <= 0 {
		return "off"
	}
	return fmt.Sprintf("at %d%% of the context window", n)
}
