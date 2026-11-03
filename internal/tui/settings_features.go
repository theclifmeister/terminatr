package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Settings → all projects → Features (the programs design §3.5): the
// server-wide opt-ins of config.toml's [features] table. Programs is
// off by default; turning it on gains reach (a new agent role that
// works across projects) and turning it off stops directors, so each
// way has its own confirm.

// ProgramsState is the programs switch and the programs here, as the
// , popup shows them.
type ProgramsState struct {
	On       bool
	Programs []program.Summary
}

// featureSettings are the Features rows, under all projects' settings.
func featureSettings() []setting {
	return []setting{{section: "Features", label: "Programs",
		help:  "Group projects into programs, each with a director: an agent that works across its projects, reads their boards and files tasks into them, as far as each program's authority lets it. Off by default; while off, nothing of programs shows or runs, and programs already made are kept, dormant.",
		value: func(m *dash) string { return onOff(m.programs.On) },
		note: func(m *dash) []string {
			if n := len(m.programs.Programs); n > 0 && !m.programs.On {
				return []string{th().Faint.Render(programsDormant(m.programs.Programs))}
			}
			return nil
		},
		change: func(m *dash) tea.Cmd {
			on := !m.programs.On
			title, q := "Turn programs on", ProgramsOnQuestion(m.programs.Programs)
			if !on {
				title, q = "Turn programs off", ProgramsOffQuestion(m.programs.Programs, runningDirectors(m.data.Sessions))
			}
			m.confirmNo(title, q, "programs stay "+onOff(!on), func() tea.Cmd { return m.setPrograms(on) })
			return nil
		}}}
}

// setPrograms turns programs on or off in the background, showing the
// new value at once.
func (m *dash) setPrograms(on bool) tea.Cmd {
	m.programs.On = on
	src := m.src
	return m.act(func() actionMsg {
		msg, err := src.SetPrograms(on)
		return actionMsg{msg: msg, err: settingsErr(err)}
	})
}

// programsDormant names the programs kept while programs are off.
func programsDormant(list []program.Summary) string {
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Slug
	}
	word := "programs are"
	if len(list) == 1 {
		word = "program is"
	}
	return fmt.Sprintf("dormant: %s (%d %s kept)", joinAnd(names), len(list), word)
}

// runningDirectors names the director sessions running now.
func runningDirectors(sessions []proto.SessionInfo) []string {
	var out []string
	for _, s := range sessions {
		if s.Role == proto.RoleDirector {
			out = append(out, "director "+s.Project)
		}
	}
	return out
}

// ProgramsOnQuestion is the confirm of turning programs on.
func ProgramsOnQuestion(list []program.Summary) string {
	q := "Turn programs on for this server? A program's director is an agent of its own that works across the program's projects: it reads their boards and histories, routes requests to their coordinators and files tasks into their boards. It proposes; it starts work only as each program's authority allows."
	if len(list) > 0 {
		q += " The programs kept while they were off come back: " + programNames(list) + "; their directors resume."
	}
	return q
}

// ProgramsOffQuestion is the confirm of turning programs off: what goes
// dormant.
func ProgramsOffQuestion(list []program.Summary, directors []string) string {
	q := "Turn programs off for this server? Nothing is deleted:"
	if len(list) == 0 {
		q += " there are no programs yet."
	} else {
		q += " " + programNames(list) + " go dormant, their folders and settings kept."
	}
	if len(directors) > 0 {
		q += " Running now, and stopped to resume when you turn programs on: " + joinAnd(directors) + "."
	}
	return q + " Their projects carry on on their own."
}

// programNames is "acme (acme-api, acme-web), side (no projects)".
func programNames(list []program.Summary) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = s.Slug + " (" + cmp.Or(strings.Join(s.Members, ", "), "no projects") + ")"
	}
	return joinAnd(parts)
}

func (s *ServerSource) Programs() ProgramsState {
	cfg, _ := config.Load() // a safety error still gives the tables
	list, _ := program.List(cfg)
	return ProgramsState{On: features.Programs(cfg), Programs: list}
}

func (s *ServerSource) SetPrograms(on bool) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	var res proto.ProgramsSwitchResult
	if err := s.call(proto.MethodProgramsSwitch, proto.ProgramsSwitchParams{On: on}, &res); err != nil {
		return "", err
	}
	return ProgramsSwitched(on, res), nil
}

// ProgramsSwitched says what programs.switch did, in the footer.
func ProgramsSwitched(on bool, res proto.ProgramsSwitchResult) string {
	msg := "programs " + onOff(on)
	if !res.Changed {
		msg = "programs are already " + onOff(on)
	}
	if len(res.Stopped) > 0 {
		msg += "; stopped " + joinAnd(res.Stopped) + ", to resume when you turn programs on"
	}
	if len(res.Resumed) > 0 {
		msg += "; resumed " + joinAnd(res.Resumed)
	}
	if len(res.Lost) > 0 {
		msg += "; couldn't resume " + joinAnd(res.Lost)
	}
	return msg
}
