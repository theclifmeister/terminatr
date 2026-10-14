package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The keys list: every key, grouped, in the prefix+<key> hint style. The
// help (?) and the project popup's Keys tab both draw it from here, the
// dashboard's part from the actions table (actions.go), so none of them
// can disagree with what the keys do.

// keyHelp is one key's line: its keys and what they do.
type keyHelp struct{ keys, help string }

// keyGroup is a heading and its keys.
type keyGroup struct {
	title string
	keys  []keyHelp
}

// sessionKeys are the prefix commands (attach.go, prefix.go), in the
// order the help lists them, in the bound keys (keybind.go). Each means
// the same everywhere: in a session, on the dashboard, over any popup
// (which it closes first).
func sessionKeys() []keyHelp {
	return []keyHelp{
		{"prefix+d", "back to the dashboard, on every console of the view (the session keeps running); on the dashboard it closes the popups"},
		{"prefix+q", "quit this console, from anywhere: a session, the dashboard, a popup, the sidebar, tm attach (the server, the sessions and other consoles keep running)"},
		{"prefix+" + boundKeys("a", "i", "t", ",", "?", "n"), "the project popup, settings, help or the new-project prompt, over the session or the dashboard; t and i are the project popup on its Tasks and Inbox tabs; esc closes it"},
		{"prefix+" + boundKeys("]", "["), "the next / previous project's coordinator"},
		{"prefix+" + boundKeys("{", "}", "b"), "narrow / widen the sidebar; b a slim strip, hidden, full again"},
		{"prefix+" + boundKey("|"), "show or hide the panel on the right: beside a thread's pane its info panel (task, steps, state, PR, last report; drag its border to resize it), on the dashboard the details panel"},
		{"prefix+tab", "the keyboard to the next area: in a session the projects sidebar (its keys below), the info panel (↑ ↓ scroll, enter its task) and back to the pane, which gets no keys meanwhile; on the dashboard as tab"},
		{"prefix+" + boundKey(";"), "back to the last place: the area, sidebar row and session the keyboard was in before, such as the thread you were typing in; again goes back"},
		{"prefix+r", "turn remote control of a coordinator on or off, to continue it from another device (asks first): the session's, or on the dashboard the selected project's; ⌁ beside the coordinator's state in the sidebar while it is on. With the project's setting on, tm turns it back on when it drops, but your off holds until the coordinator is started anew"},
		{"prefix+" + boundKey("R"), "in a paired server's pane (docs: remote panes): ask that server for a fresh repaint, when the pane looks off; the pane repaints by itself after a slow link or a dropped one"},
		{"prefix+<key>", "a mod's command on that key (Mod commands): in a session it runs in the session's project with the session selected, and a view it opens shows over the session; the plain key still goes to the program"},
		{"prefix+prefix", "send the prefix key itself to the program"},
	}
}

// popupKeys are the project popup's own keys (projectView). None is a
// prefix command (TestNoPlainKeyIsAPrefixCommand).
var popupKeys = []keyHelp{
	{"← → 1-9", "previous / next tab, or pick one; mods' tabs come after the core's"},
	{"↑ ↓ / tab", "on a mod's tab or card: move, filter (enter keeps it, esc drops it) and change list; the keys the mod lists go to it"},
	{"↑ ↓ pgup pgdown", "move in the tab, or scroll it (Keys, Memory)"},
	{"enter space + - x", "on the Settings tab: as in the settings, below"},
	{"+ x", "on the overview: add a repository / remove the selected one (asks first)"},
	{"enter", "on a task (Tasks tab): show it, with what it is blocked on, or how to check it and whether its pull request merged"},
	{"enter", "on a file (Library tab): read it, read-only: text as it is, JSON indented, other files by name and size"},
	{"d D", "on a file (Library tab): delete it / all of its thread's files (asks first, journaled)"},
	{"b", "on the Tasks tab: list the done tasks (collapsed to a line by default), or collapse them again"},
	{"D", "on an open, ready or blocked task (Tasks tab): delegate it; the coordinator starts a thread for it (asks first)"},
	{"A", "on a task in review (Tasks tab): accept it; the coordinator marks it done (asks first)"},
	{"x", "on a task in review: send it back with a note on what to change; the coordinator passes it on"},
	{"c", "on a task: open the project's coordinator, to answer what a blocked task waits on"},
	{"esc", "close; esc is the only key that closes a popup"},
}

// settingsKeys are the settings' keys (settings.go): the , popup and
// the project popup's Settings tab. None is a prefix command.
var settingsKeys = []keyHelp{
	{"↑ ↓ pgup pgdown", "move through the settings"},
	{"enter space", "change the selected setting (yolo mode asks first); on the prefix key, the next ctrl+<key> is the new prefix"},
	{"+ -", "on a number: one step up / down"},
	{"← → 1 2", "the , settings: General, or All projects (the settings every project follows unless it sets its own)"},
	{"x", "on a project's setting it sets itself (its Settings tab): follow all projects again"},
	{"n j", "on the , settings' Peers tab: pair with another server through a relay, or join the pairing code another shows; both compare six digits (j doesn't move there: ↓ does)"},
	{"space ← →", "on the Peers tab: share or stop sharing the selected project (sharing asks first), or move the selected server's panes setting along off · view · write (more asks first)"},
	{"x", "on the Peers tab: remove the selected server (asks first)"},
	{"esc", "close"},
}

// mouseKeys are the mouse's ways, in the help beside the keys: every
// key has one (TestEveryKeyHasMousePath).
var mouseKeys = []keyHelp{
	{"click", "a row selects it and gives its area the keyboard (the list, the details panel); a footer hint presses its key; a popup's tab, row or setting picks it (a number's − + step it); outside a popup closes it"},
	{"double-click", "a row opens it: a coordinator or a thread attaches, a task shows"},
	{"right-click", "a row of the list or the sidebar: a menu of its actions (open or attach, its popup, …); in a session, the status bar or a pane that doesn't take the mouse: the session's menu"},
	{menuButton + " menu", "every action, first in the footer; in a session, every prefix command, on the status bar"},
	{"status bar", "prefix+? help, prefix+d dashboard, " + menuButton + " menu, y yes"},
	{"state label", "at the right of the project's header on the dashboard (● active — space to deactivate): as space"},
	{"sidebar", "a row takes the keyboard, its cursor on it, and a project shows its dashboard, its coordinator or a thread attaches; a click on the pane gives the keyboard back. Below the tree, two lines describe the highlighted row (its full title, state, PR and checks) and a key bar lists the keys that work on it"},
	{"info panel", "beside a thread's pane: its task opens in the Tasks tab, its PR the browser; a click elsewhere gives it the keyboard"},
	{"mod section", "in the info or details panel: a click on its fold mark folds or unfolds it; on its header or body it opens the view its mod names, else folds or unfolds it"},
	{"hover", "a glyph in the full sidebar: what it means, at the sidebar's foot"},
	{"wheel", "moves through a list; scrolls the details panel and long popups"},
	{"drag", "the divider beside the details panel, the sidebar's border, the info panel's border"},
	{"drag in a pane", "a pane whose program doesn't take the mouse (a shell): select text, copied on release to this terminal's clipboard (OSC 52, over SSH too); Claude Code selects and copies itself"},
	{"shift+drag", "your terminal's own selection (option+drag in some macOS terminals)"},
}

// keyGroups is every key, grouped; mods are the mods' commands
// (commandHelp), a group of their own when there are any.
func keyGroups(mods []keyHelp) []keyGroup {
	lines := func(t keyTable, rows []bindable) []keyHelp {
		var out []keyHelp
		for i, r := range rows {
			if r.label != "" {
				out = append(out, keyHelp{keyLabel(t, rows, i), r.help})
			}
		}
		return out
	}
	dash, side, panel := lines(binds.list, listBindables()), lines(binds.side, sideBindables()), lines(binds.panel, panelBindables())
	groups := []keyGroup{
		{"On the dashboard", dash},
		{"With the mouse", mouseKeys},
		{"Prefix commands: the same everywhere", sessionKeys()},
		{"In the sidebar (tab, or prefix+tab in a session)", side},
		{"In the info and details panels (tab, or a click)", panel},
		{"In the project popup", popupKeys},
		{"In the settings (, or the project popup's Settings tab)", settingsKeys},
	}
	if len(mods) > 0 {
		groups = slices.Insert(groups, 1, keyGroup{"Mod commands: the key on the dashboard, prefix+key anywhere, the ≡ menu", mods})
	}
	return groups
}

// helpLines are the help's lines w cells wide: the prefix key, every key
// (keyLines), then the symbols.
func helpLines(prefix string, w int, mods []keyHelp) []string {
	out := faintLines("The prefix key is "+prefix+": prefix+d means "+prefix+", then d. Change it in the settings (,).", w)
	out = append(out, "")
	out = append(out, keyLines(w, mods)...)
	out = append(out, "")
	return append(out, symbolLines(w)...)
}

// symbolLines lists every glyph tm draws with its one meaning, in this
// console's icon set (docs/STYLE.md, glyphs).
func symbolLines(w int) []string {
	i := ic()
	syms := []keyHelp{
		{i.working, "working: a session at work, a task started"},
		{i.idle, "idle: waiting for a prompt; an open or ready task"},
		{i.blocked, "blocked: waits on a permission prompt, a question or a task it needs"},
		{i.review, "in review: done by its thread, waits for you to accept it"},
		{i.starting, "starting"},
		{i.running, "running: a shell or another program"},
		{i.done, "done"},
		{i.other, "stopped, exited or resolved"},
		{i.hint, "in the sidebar: something in this project needs you"},
		{i.remote, "remote control is on for this coordinator"},
		{i.paused, "the project is paused"},
		{i.inactive, "the project is inactive: its coordinator and threads don't run"},
		{i.linked, "with paired servers, a server's header: linked (" + i.starting + " connecting, " + i.idle + " offline, " + i.inactive + " refused)"},
		{i.ciPass + " " + i.ciFail + " " + i.ciRun, "a thread's PR checks, in the sidebar's PR column: pass, fail, running; blank without a PR"},
		{i.todoDone + " " + i.todoNow + " " + i.todoOpen, "a step done, under way, to do"},
		{i.current, "the step under way"},
		{strings.Repeat(i.barOn, 2) + strings.Repeat(i.barOff, 3), "progress, always beside its count (2/5)"},
	}
	out := []string{th().Head.Render("Symbols")}
	for _, s := range syms {
		out = append(out, keyLine(s, w)...)
	}
	return append(out, faintLines("The sidebar's columns, on the right of each row: the count or percent, the PR checks, the state, remote control, paused, inactive and needs you. Hover a glyph there and the sidebar says what it means.", w)...)
}

// keyCol is the width of the keys column.
const keyCol = 19

// keyLines draws the keys list w cells wide: each help wraps under
// itself, so nothing is cut off.
func keyLines(w int, mods []keyHelp) []string {
	var out []string
	for i, g := range keyGroups(mods) {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, th().Head.Render(g.title))
		for _, k := range g.keys {
			out = append(out, keyLine(k, w)...)
		}
	}
	out = append(out, "")
	out = append(out, faintLines("A prefix command does the same in a session, on the dashboard and over a popup, and no plain key means something else, but ] and [ in a panel with mod sections: popups close with esc, and the lists refresh by themselves. The prefix is in the settings (,); inside tmux, which takes ctrl+b, pick another.", w)...)
	return append(out, faintLines("Every tm shows the same view: what one does, the others show, sized by the one typed in. tm --own keeps to itself. You talk to coordinators; they run the threads, their reports and the tasks.", w)...)
}

// keyLine is one key's lines: the keys in the accent colour, the help
// wrapped beside them.
func keyLine(k keyHelp, w int) []string {
	hw := max(w-keyCol-1, 10)
	help := strings.Split(ansi.Wordwrap(k.help, hw, ""), "\n")
	out := make([]string, 0, len(help))
	for i, h := range help {
		key := ""
		if i == 0 {
			key = k.keys
		}
		out = append(out, th().Accent.Render(fit(key, keyCol))+" "+h)
	}
	return out
}

// wrapLines is text wrapped to w cells.
func wrapLines(text string, w int) []string {
	return strings.Split(ansi.Wordwrap(text, max(w, 10), ""), "\n")
}

// faintLines is text wrapped to w cells, faint.
func faintLines(text string, w int) []string {
	lines := wrapLines(text, w)
	for i, l := range lines {
		lines[i] = th().Faint.Render(l)
	}
	return lines
}
