package ticker

// Coordinator sleep (docs/SPEC.md §7.5, Sleep): a coordinator idle for
// its project's coordinator_sleep_after (config.SleepAfter: 4h for a
// program's member unless set, never for a standalone project), with
// nothing waiting on it and nobody watching, is stopped with its
// conversation kept (Host.Sleep); a new inbox item wakes it (nudge,
// Host.Wake), as do enter on its row, an app opening it and tm program
// wake (the server's). A director never sleeps: only coordinators come
// here. Each sleep is journaled as ticker coordinator.sleep, each wake
// by the server as coordinator.wake.

import (
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sleepMemo is what sleepIdle saw of a coordinator: process Seen has
// been resting (sleepBlocked had nothing to say) since Since.
type sleepMemo struct {
	Seen  string
	Since time.Time
}

// sleepBlocked says why coordinator p's session coord may not sleep now,
// "" when it may: it runs a turn, has a prompt queued, a question or a
// permission prompt open, a console or app shows its pane, one of its
// project's threads works or waits on a question, or its inbox has an
// item it hasn't been told about (unread).
func (t *Ticker) sleepBlocked(p seat, coord *proto.SessionInfo, sessions []proto.SessionInfo) string {
	switch {
	case coord.State != "idle":
		return "not idle"
	case coord.Queued > 0:
		return "prompt queued"
	case coord.Question != nil || coord.Escape:
		return "question open"
	case coord.Clients > 0:
		return "shown"
	}
	for _, s := range sessions {
		if s.Project == p.Slug && s.Role == proto.RoleThread && (s.State == "working" || s.State == "blocked" || s.Question != nil) {
			return "thread " + s.Thread + " works"
		}
	}
	items, err := p.Inbox()
	if err != nil {
		return "inbox: " + err.Error()
	}
	told := map[string]bool{}
	for _, id := range t.projectMemo(p.key()).Nudged {
		told[id] = true
	}
	for _, it := range items {
		if !told[it.ID] {
			return "unread inbox item"
		}
	}
	return ""
}

// sleepIdle puts project seat p's coordinator to sleep once it rested
// (sleepBlocked silent) sweep after sweep for its sleep setting; never
// while paused (the caller's), never for a setting of off. The rest
// starts again with any activity and with a new process; a server start
// starts it again too.
func (t *Ticker) sleepIdle(p seat, cfg *config.Config, safety config.Safety, sessions []proto.SessionInfo, now time.Time) {
	pm := t.projectMemo(p.key())
	inProgram := features.Programs(cfg) && cfg.ProgramOf(p.Slug) != ""
	after := config.SleepAfter(safety, inProgram)
	coord := p.of(sessions)
	if after <= 0 || coord == nil || p.role != proto.RoleCoordinator {
		pm.sleep = nil
		return
	}
	seen := process(coord)
	if why := t.sleepBlocked(p, coord, sessions); why != "" || pm.sleep == nil || pm.sleep.Seen != seen {
		pm.sleep = &sleepMemo{Seen: seen, Since: now}
		return
	}
	if now.Sub(pm.sleep.Since) < after {
		return
	}
	// Asked again right before the stop: a console may have attached.
	still := func(info proto.SessionInfo) bool {
		return process(&info) == seen && t.sleepBlocked(p, &info, t.o.Host.Sessions()) == ""
	}
	if err := t.o.Host.Sleep(coord.ID, still); err != nil {
		t.o.Log.Printf("ticker: %s: sleep %s: %v", p.Slug, coord.ID, err)
		pm.sleep = &sleepMemo{Seen: seen, Since: now}
		return
	}
	idle := "idle " + shortDuration(now.Sub(pm.sleep.Since))
	pm.sleep = nil
	if err := p.Journal(caller.Caller{Kind: caller.Ticker}, "coordinator.sleep", coord.ID, idle); err != nil {
		t.o.Log.Printf("ticker: %s: journal: %v", p.Slug, err)
	}
	t.o.Log.Printf("ticker: %s: coordinator %s asleep after %s", p.Slug, coord.ID, idle)
}

// shortDuration is d as "4h", "30m" or "1h30m": no trailing zero units.
func shortDuration(d time.Duration) string {
	s := d.Round(time.Minute).String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// wakeFor wakes project seat p's sleeping coordinator for fresh, the
// inbox items it hasn't been told about: none runs, so they would wait.
// The next sweep nudges the woken one.
func (t *Ticker) wakeFor(p seat, fresh int) {
	if p.role != proto.RoleCoordinator || fresh == 0 {
		return
	}
	if id, ok := t.o.Host.Wake(p.Slug); ok {
		t.o.Log.Printf("ticker: %s: coordinator %s woke for %d new inbox item(s)", p.Slug, id, fresh)
		t.Kick()
	}
}
