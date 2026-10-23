package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The prefix commands on the dashboard (docs/SPEC.md §4, the key table).
// prefix then a key means the same everywhere: in a session, on the
// dashboard, over any popup and with the sidebar holding the keyboard.
// It never reaches a popup as a plain key: a command that opens or goes
// somewhere closes the popups first. Its keys are those of a session
// (prefixStep): d, q, r, R, C, the dashboard keys (prefixCommands), the
// sidebar's and the info panel's (paneCommands), the prefix itself and
// the mods' command keys (modcommands.go), which the core's come before.

// The kinds of prefix command (prefixCmd.kind).
const (
	cmdCore  = iota // the session's own: d, q, r, R and the prefix itself
	cmdPopup        // opens its popup, over a session too (popupCommands)
	cmdDash         // a dashboard key that runs on the dashboard (prefixCommands)
	cmdPane         // acts on the window itself (paneCommands)
)

// prefixCmd is a prefix command: its default key, its ≡ menu label and
// its kind.
type prefixCmd struct {
	key, menu string
	kind      int
}

// prefixCmds are the prefix commands, the mods' aside, in the help's
// and the ≡ menu's order: the one list the key sets (prefixCommands,
// popupCommands, paneCommands) and the menu (sessionMenu) are made of.
var prefixCmds = []prefixCmd{
	{"d", "dashboard", cmdCore},
	{"q", "quit this console", cmdCore},
	{"A", "project popup", cmdPopup},
	{"+", "new project", cmdPopup},
	{"]", "next project", cmdDash},
	{"[", "previous project", cmdDash},
	{"I", "inbox", cmdPopup},
	{"T", "tasks", cmdPopup},
	{",", "settings", cmdPopup},
	{"?", "help: keys and mouse", cmdPopup},
	{"{", "narrower sidebar", cmdPane},
	{"}", "wider sidebar", cmdPane},
	{"B", "sidebar: slim / hide / full", cmdPane},
	{"|", "info panel on / off", cmdPane},
	{"tab", "keyboard to the sidebar", cmdPane},
	{";", "back to the last place", cmdPane},
	{"r", "remote control on / off…", cmdCore},
	{"R", "repaint from its server", cmdCore},
	{"C", "save and clear the coordinator", cmdCore},
	{"prefix", "send the prefix key", cmdCore},
}

// prefixCmdKeys is the set of the keys of the prefix commands of kinds.
func prefixCmdKeys(kinds ...int) map[string]bool {
	out := map[string]bool{}
	for _, c := range prefixCmds {
		if slices.Contains(kinds, c.kind) {
			out[c.key] = true
		}
	}
	return out
}

// prefixTwins are the lower-case keys that run a shifted prefix command
// as well (prefix+a as prefix+A), and n the new project (+): a shifted
// key on the list, either case after the prefix.
var prefixTwins = func() map[string]string {
	out := map[string]string{"n": "+"}
	for _, c := range prefixCmds {
		if l := strings.ToLower(c.key); c.kind != cmdCore && len(c.key) == 1 && l != c.key {
			out[l] = c.key
		}
	}
	return out
}()

// prefixName is the command key typed after the prefix runs as: a bound
// key its action's default (keybind.go), a twin (prefixTwins) its
// shifted key, and another default key of a prefix command's action its
// first (D as |, the details or info panel).
func prefixName(key string) string {
	c := binds.list.canon(key)
	if t, ok := prefixTwins[key]; ok && !prefixCommands[c] && !paneCommands[c] {
		return t
	}
	for _, a := range actions {
		if len(a.keys) > 1 && slices.Contains(a.keys[1:], c) && (prefixCommands[a.keys[0]] || paneCommands[a.keys[0]]) {
			return a.keys[0]
		}
	}
	return c
}

// prefixHint is the footer while the prefix waits for its command, in
// the bound keys (keybind.go).
func prefixHint() string {
	return "prefix ▸ d dashboard · q quit · " + boundKeys("A", "I", "T", ",", "?", "+") + " popups · p " + boundKeys("]", "[") +
		" projects · " + boundKeys("{", "}", "B") + " sidebar · " + boundKey("|") + " details · tab area · " + boundKey(";") +
		" last place · r remote · " + boundKey("R") + " repaint · C save+clear · esc cancel"
}

// prefixCommand runs the key typed after the prefix: a bound key runs as
// its action's default (keybind.go), which the switch speaks.
func (m *dash) prefixCommand(key string) tea.Cmd {
	typed := key
	key = prefixName(key)
	switch {
	case key == "q":
		m.quitting = true
		return tea.Quit
	case key == "esc" || key == m.prefix:
		// Cancelled; prefix twice sends the prefix to a program, and
		// the dashboard has none.
		return nil
	case key == "d" && m.over != nil:
		// Over a session, as in it: the view's dashboard.
		m.dropOver()
		m.stack = nil
		return m.toDashboard()
	case key == "d":
		// Already here: back to the bare list.
		m.stack, m.focus = nil, areaMain
		return nil
	case m.over != nil && (key == "r" || key == "R" || key == "tab" || key == "|" || key == ";"):
		// The session's own: back to it, which runs the command (the
		// remote control question, the sidebar's keyboard, its info
		// panel, the last place).
		m.stack, m.result.Command = nil, key
		return nil
	case key == "r":
		m.stack = nil
		return m.askRemote()
	case key == "R":
		m.stack = nil
		return m.listKey(key)
	case key == "C" && m.over != nil:
		// The session's project's coordinator: back to the session,
		// which runs it.
		m.stack, m.result.Command = nil, key
		return nil
	case key == "C":
		// The selected (or current) project's coordinator, no question.
		m.stack = nil
		slug := m.needProject()
		if slug == "" {
			return nil
		}
		return m.saveClear(slug)
	case paneCommands[key] && key != "tab":
		// The sidebar's width and slim strip; the popup stays.
		return m.listKey(key)
	case m.over == nil && m.remoteHere() && slices.ContainsFunc(actions, func(a action) bool { return runsAs(a.id, a.keys, key) }):
		// A paired server's dashboard: what it keeps (settings, help, the
		// next project), never an action on this server's project its
		// list rows are of (peerdash.go).
		m.stack = nil
		return m.remoteAction(key)
	case m.over == nil && m.remoteHere() && m.commandKeyed(typed):
		alias, slug, _ := m.remote()
		m.stack, m.msg = nil, remoteNothing(peerShown(m.data.Peers, alias), slug)
		return nil
	case popupCommands[key], key == "tab":
		m.stack = nil
		return m.listKey(key)
	case prefixCommands[key]:
		// p ] [ run on the dashboard: over a session, go back to it.
		m.stack = nil
		var back tea.Cmd
		if m.over != nil {
			m.dropOver()
			back = m.toDashboard()
		}
		return tea.Batch(back, m.listKey(key))
	case m.over == nil && slices.ContainsFunc(actions, func(a action) bool { return runsAs(a.id, a.keys, key) }):
		// Any other list key, as on the list: on a coordinator's row a
		// plain one types through (typethrough.go), so S, O, Z, E and the
		// rest are prefix+key there.
		m.stack = nil
		return m.listKey(key)
	case m.commandKeyed(typed) && m.over != nil:
		// A mod's command over a session: the session runs it, with
		// itself selected, and shows the view the mod opens.
		m.stack, m.result.Command = nil, typed
		return nil
	case m.commandKeyed(typed):
		m.stack = nil
		c, _ := m.commandFor(typed)
		return m.runCommand(c)
	}
	m.msg = "prefix+" + typed + " isn't a command; " + boundKey("?") + " lists them"
	return nil
}

// toDashboard shows the view's dashboard, on every console.
func (m *dash) toDashboard() tea.Cmd {
	if m.view == nil {
		return nil
	}
	return m.call(proto.MethodViewDashboard, proto.ViewParams{})
}

// askRemote asks whether to turn the selected project's coordinator's
// remote control on or off (prefix+r on the dashboard; in a session it
// is the focused coordinator's).
func (m *dash) askRemote() tea.Cmd {
	slug := m.needProject()
	if slug == "" {
		return nil
	}
	for _, s := range m.data.Sessions {
		if s.Role != proto.RoleCoordinator || s.Project != slug {
			continue
		}
		on := !s.RemoteControl
		m.confirmNo("Remote control", remoteQuestion(s), "remote control unchanged", func() tea.Cmd {
			src := m.src
			return m.act(func() actionMsg {
				msg, err := src.SetRemote(slug, on)
				return actionMsg{msg: msg, err: err}
			})
		})
		return nil
	}
	m.msg = "no coordinator runs for " + slug + "; enter on the project starts it"
	return nil
}
