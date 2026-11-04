package ticker

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/seat"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// seatLabel names an inbox item's subject id for a nudge: a project's
// thread or task by its label (thread.Label, thread.TaskLabel); a
// program's bare, since its board's titles aren't the director's
// alone to write.
func seatLabel(s seat.Seat, id string) string {
	switch {
	case s.Project == nil:
		return id
	case thread.ValidID(id):
		return thread.Label(s.Project, id)
	}
	return thread.TaskLabel(s.Project, id)
}

// keepSeat is the sweep's look at a seat: its remote control, its
// model, auto-clear and the upkeep after a clear.
func (t *Ticker) keepSeat(s seat.Seat, sessions []proto.SessionInfo, safety config.Safety, hint int, now time.Time) {
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
		st := seat.Director(g)
		unlock := t.lockProjects(st.Key())
		safety := cfg.DirectorSafety(g.Slug)
		if !safety.Paused {
			t.nudge(st, sessions, now)
		}
		t.keepSeat(st, sessions, safety, cfg.ContextHint, now)
		unlock()
	}
}
