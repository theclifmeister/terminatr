package server

// The commands user mods declare (docs/SPEC.md §5.3, Commands; mods
// design §2 tier 4): the human runs one with its key or its ≡ menu entry
// on a console, or with tm mod run (mod.command), and the mod gets
// command.run, starting its host if it waits for its first event. From
// a console's key or menu entry it may then open one of its views there,
// as after a key in its view: a session's console then shows it over
// the session, by the answer's After.

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// modCommand is mod.command from console (or tm mod run) pid.
func (s *Server) modCommand(p proto.ModCommandParams, pid int, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "only the human runs a mod's commands")
	}
	m, ok := s.modNamed(p.Mod)
	switch {
	case !ok:
		return nil, proto.Errorf(proto.ErrBadParams, "no mod %q is installed", p.Mod)
	case m.State != mod.Enabled || m.Manifest == nil || !m.Manifest.IsCode():
		return nil, proto.Errorf(proto.ErrRefused, "%s isn't an enabled code mod (%s)", p.Mod, m.Status())
	case s.noMods && !m.Builtin:
		return nil, proto.Errorf(proto.ErrRefused, "the server runs in safe mode (%s): no user mod runs", mod.EnvNoMods)
	}
	if _, ok := m.Manifest.DeclaredCommand(p.Command); !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%s has no command %q", p.Mod, p.Command)
	}
	if p.Project != "" {
		if !project.ValidSlug(p.Project) {
			return nil, proto.Errorf(proto.ErrBadParams, "%q isn't a project", p.Project)
		}
		if !m.Runs(p.Project) {
			return nil, proto.Errorf(proto.ErrBadParams, "%s doesn't run for project %s", p.Mod, p.Project)
		}
	}
	for _, st := range s.ModHosts() {
		if st.ID == m.ID && st.State == modsup.Failed {
			return nil, proto.Errorf(proto.ErrRefused, "%s failed: %s (tm mod logs %s)", m.ID, oneLine(st.Err, 200), m.ID)
		}
	}
	ev := modbus.Command{Command: p.Command, Project: p.Project}
	if t := p.Selection; t != nil && *t != (proto.ModTarget{}) {
		// The selection is a row of the command's project.
		if t.Project != "" && t.Project != p.Project {
			return nil, proto.Errorf(proto.ErrBadParams, "the selection is in %s, not %s", t.Project, p.Project)
		}
		for _, f := range []string{t.Task, t.Thread, t.Session} {
			if len(f) > 128 {
				return nil, proto.Errorf(proto.ErrBadParams, "selection too long")
			}
		}
		ev.Selection = &modbus.Target{Project: t.Project, Task: t.Task, Thread: t.Thread, Session: t.Session}
	}
	var res proto.ModCommandResult
	if p.Console {
		res.After = s.modViewStore().Input(m.ID, pid, p.Project)
	}
	s.log.Printf("mod.command %s by %s", mod.CommandName(m.ID, p.Command), c)
	if s.modEv != nil {
		s.modEv.bus.PublishTo(m.ID, modbus.CommandRun, p.Project, ev)
	}
	return res, nil
}
