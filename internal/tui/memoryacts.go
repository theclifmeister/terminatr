package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// The memory note actions a mod binds (the Memory tab's e, n and d;
// docs/SPEC.md §7.6, Memory notes): edit and new hand the terminal to
// tm memory edit or new, which open the human's editor, and come back
// to the popup when it exits; delete asks with the core's question, then
// removes the note as tm memory rm does. Each is the core's command,
// run as the human.

// pauseEnv makes tm memory wait for enter before it hands the terminal
// back, when it has something to say (an error, a kept conflict), which
// the console's screen would otherwise cover at once.
const pauseEnv = "TERMINATR_PAUSE_ON_EXIT"

// memoryCommand is the tm command a memory binding runs on the
// terminal; tests replace it.
var memoryCommand = func(args ...string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), pauseEnv+"=1")
	return cmd, nil
}

// runMemory runs tm memory <args> on the console's terminal, for
// binding b, and tells the mod once it is back.
func (m *dash) runMemory(b *binding, what string, args ...string) tea.Cmd {
	if m.out == nil || m.out.File == nil {
		m.msg = "not available: " + what + " needs this console's terminal"
		return m.bindingDone(b, false)
	}
	if m.busy {
		m.queued = append(m.queued, func(m *dash) tea.Cmd { return m.runMemory(b, what, args...) })
		return nil
	}
	cmd, err := memoryCommand(append([]string{"memory"}, args...)...)
	if err != nil {
		m.msg = "not available: " + err.Error()
		return m.bindingDone(b, false)
	}
	cmd.Stdout, cmd.Stderr = m.out.File, m.out.File
	m.out.flush()
	m.busy = true
	m.acts++
	src := m.src
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		_ = src.ModActionDone(b.mod, b.action.ID, b.action.Target, err == nil, b.token)
		if err != nil {
			return actionMsg{msg: fmt.Sprintf("%s: %v", what, err)}
		}
		return actionMsg{msg: what + ": back from the editor"}
	})
}

// removeNote asks, then deletes note path of project slug (binding b).
func (m *dash) removeNote(slug, path string, b *binding) tea.Cmd {
	question := fmt.Sprintf("Delete %s of %s? It leaves MEMORY.md, and the coordinator is told you removed it.", path, slug)
	m.confirmNo("Delete note", question, path+" kept", func() tea.Cmd {
		src := m.src
		return m.actVia(b, func() (actionMsg, bool) {
			if err := src.RemoveNote(slug, path); err != nil {
				return actionMsg{err: err}, false
			}
			return actionMsg{msg: "deleted " + path}, true
		})
	})
	return nil
}
