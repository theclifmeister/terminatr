package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
)

// shareRow is the Settings tab's Shared row of project slug (docs/SPEC.md
// §16.9): whether the servers paired with this one (tm peer) see the
// project, read-only. Only the human turns it on, and it asks first.
func shareRow(slug string) setting {
	shared := func(m *dash) bool {
		p := m.projectData(slug)
		return p != nil && p.Safety != nil && p.Safety.Share
	}
	set := func(m *dash, on bool) tea.Cmd {
		if p := m.projectData(slug); p != nil {
			s := config.Defaults
			if p.Safety != nil {
				s = *p.Safety
			}
			s.Share = on
			p.Safety = &s
		}
		msg := slug + " is no longer shared with paired servers"
		if on {
			msg = slug + " is shared with paired servers, read-only"
		}
		return m.setSetting("projects."+slug, "share", on, msg)
	}
	return setting{label: "Shared with paired servers", help: "Let the servers paired with this one (tm peer pair) see this project, read-only: what waits for you, the inbox, threads, tasks on deck and its sessions' states. Never its files, settings, paths or worktrees, and nobody there can act on it. Only an active project is offered. Off by default; turning it on asks first.",
		value: func(m *dash) string { return onOff(shared(m)) },
		change: func(m *dash) tea.Cmd {
			if shared(m) {
				return set(m, false)
			}
			m.confirmNo("Share "+slug, "Share "+slug+" with every paired server? They see its state read-only (what waits for you, the inbox, threads, tasks on deck), never its files.", "not shared", func() tea.Cmd {
				return set(m, true)
			})
			return nil
		}}
}
