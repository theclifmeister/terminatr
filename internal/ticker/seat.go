package ticker

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// seat is a folder's agent session the ticker keeps (remote control,
// model, auto-clear and the upkeep after it): a project's coordinator,
// or a program's director, which these behave for as for a coordinator
// (the programs design §4.1), read from the program's keys.
type seat struct {
	*project.Folder
	role string // proto.RoleCoordinator or proto.RoleDirector
	// p is the project, nil for a program (no threads).
	p *project.Project
}

func coordinatorSeat(p *project.Project) seat {
	return seat{Folder: &p.Folder, role: proto.RoleCoordinator, p: p}
}

func directorSeat(g *program.Program) seat {
	return seat{Folder: &g.Folder, role: proto.RoleDirector}
}

// key is the seat's memo and lock key (seatKey).
func (s seat) key() string { return seatKey(s.role, s.Slug) }

// seatKey is the ticker's memo and lock key of a seat of role in folder
// slug: the project's slug, or a program's _programs/<slug>, which no
// project slug can be.
func seatKey(role, slug string) string {
	if role == proto.RoleDirector {
		return vaultlink.ProgramFolder(slug)
	}
	return slug
}

// of is the seat's running session among sessions, nil for none.
func (s seat) of(sessions []proto.SessionInfo) *proto.SessionInfo {
	for i, x := range sessions {
		if x.Role == s.role && x.Project == s.Slug {
			return &sessions[i]
		}
	}
	return nil
}

// keepSeat is the sweep's look at a seat: its remote control, its
// model, auto-clear and the upkeep after a clear.
func (t *Ticker) keepSeat(s seat, sessions []proto.SessionInfo, safety config.Safety, hint int, now time.Time) {
	t.keepRemote(s, sessions, safety, now)
	t.keepModel(s, sessions)
	t.autoClear(s, sessions, safety, hint, now)
	t.upkeepAfterClear(s, sessions, safety, now)
}

// sweepDirectors keeps each running director as sweepProject keeps a
// coordinator, with its program's settings (config.DirectorSafety);
// only while programs are on (a dormant director isn't touched), and
// never a left-out program's.
func (t *Ticker) sweepDirectors(cfg *config.Config, sessions []proto.SessionInfo, now time.Time) {
	if !features.Programs(cfg) {
		return
	}
	done := map[string]bool{}
	for _, s := range sessions {
		if s.Role != proto.RoleDirector || s.Project == "" || done[s.Project] {
			continue
		}
		done[s.Project] = true
		g, err := program.Open(s.Project, cfg)
		if err != nil || g.LeftOut {
			continue
		}
		st := directorSeat(g)
		unlock := t.lockProjects(st.key())
		t.keepSeat(st, sessions, cfg.DirectorSafety(g.Slug), cfg.ContextHint, now)
		unlock()
	}
}
