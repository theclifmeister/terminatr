package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The prefix commands on the dashboard (docs/SPEC.md §4, the key table).
// prefix then a key means the same everywhere: in a session, on the
// dashboard, over any popup and with the sidebar holding the keyboard.
// It never reaches a popup as a plain key: a command that opens or goes
// somewhere closes the popups first. Its keys are those of a session
// (prefixStep): d, q, r, the dashboard keys (prefixCommands), the
// sidebar's and the info panel's (paneCommands), the prefix itself and
// the mods' command keys (modcommands.go), which the core's come before.

// prefixHint is the footer while the prefix waits for its command, in
// the bound keys (keybind.go).
func prefixHint() string {
	return "prefix ▸ d dashboard · q quit · " + boundKeys("a", "i", "t", ",", "?", "n") + " popups · p " + boundKeys("]", "[") +
		" projects · " + boundKeys("{", "}", "b") + " sidebar · " + boundKey("|") + " details · tab area · " + boundKey(";") +
		" last place · r remote · " + boundKey("R") + " repaint · esc cancel"
}

// prefixCommand runs the key typed after the prefix: a bound key runs as
// its action's default (keybind.go), which the switch speaks.
func (m *dash) prefixCommand(key string) tea.Cmd {
	typed := key
	key = binds.list.canon(key)
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
	case paneCommands[key] && key != "tab":
		// The sidebar's width and slim strip; the popup stays.
		return m.listKey(key)
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
