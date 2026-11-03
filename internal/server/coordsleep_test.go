package server

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sleepFixture is switchFixture with the sleep maps and a run folder.
func sleepFixture(t *testing.T) *Server {
	t.Helper()
	s := switchFixture(t)
	s.asleep = map[string]bool{}
	s.opts.Paths.RunDir = t.TempDir()
	return s
}

// TestSleepLockedAsleep: a session the ticker put to sleep goes to
// dormant marked Asleep, one stopped for a deactivation unmarked; both
// keep their conversation.
func TestSleepLockedAsleep(t *testing.T) {
	s := sleepFixture(t)
	for _, id := range []string{"s-1", "s-2"} {
		s.records[id] = SessionRecord{ID: id, Role: proto.RoleCoordinator, Project: "demo", Agent: "claude", AgentSessionID: "a-" + id}
		s.sleeping[id] = true
	}
	s.asleep["s-1"] = true
	s.mu.Lock()
	s.sleepLocked("s-1")
	s.sleepLocked("s-2")
	s.mu.Unlock()
	if r, ok := s.dormant["s-1"]; !ok || !r.Asleep || r.AgentSessionID != "a-s-1" || r.Dormant.IsZero() {
		t.Errorf("asleep record %+v", r)
	}
	if r, ok := s.dormant["s-2"]; !ok || r.Asleep {
		t.Errorf("deactivated record %+v", r)
	}
	if len(s.asleep) != 0 || len(s.sleeping) != 0 || len(s.records) != 0 {
		t.Errorf("left %v %v %v", s.asleep, s.sleeping, s.records)
	}
	if got := s.asleepList(); len(got) != 1 || got[0].ID != "s-1" || got[0].Project != "demo" {
		t.Errorf("asleep list %+v", got)
	}
}

// TestWakeCoordinator: only an Asleep record of an active project is
// woken; one of an inactive project stays for its activation, one
// beside a running coordinator is dropped, and one that can't resume
// is gone, its caller starting one anew. A wake is journaled.
func TestWakeCoordinator(t *testing.T) {
	s := sleepFixture(t)
	human := caller.Caller{Kind: caller.Human}
	gone := filepath.Join(t.TempDir(), "gone")
	if _, ok := s.wakeCoordinator("demo", human, "opened"); ok {
		t.Fatal("woke with none asleep")
	}
	// A deactivation's dormant coordinator isn't the wake's.
	s.dormant["s-1"] = SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", Agent: "claude", AgentSessionID: "x", Cwd: gone}
	if _, ok := s.wakeCoordinator("demo", human, "opened"); ok || len(s.dormant) != 1 {
		t.Fatalf("woke a deactivated coordinator: %v", s.dormant)
	}
	delete(s.dormant, "s-1")
	// Inactive: kept.
	writeConfig(t, "[projects.demo]\nactive = false\n")
	s.dormant["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Project: "demo", Agent: "claude", AgentSessionID: "y", Cwd: gone, Asleep: true}
	if _, ok := s.wakeCoordinator("demo", human, "opened"); ok {
		t.Fatal("woke an inactive project's coordinator")
	}
	if _, ok := s.dormant["s-2"]; !ok {
		t.Fatal("an inactive project's asleep coordinator was dropped")
	}
	// Active, its folder gone: not resumed, and gone from dormant.
	writeConfig(t, "")
	if _, ok := s.wakeCoordinator("demo", human, "opened"); ok {
		t.Fatal("resumed a coordinator whose folder is gone")
	}
	if len(s.dormant) != 0 {
		t.Fatalf("dormant after a lost wake: %v", s.dormant)
	}
	if s.asleepList() != nil {
		t.Fatalf("asleep list %v", s.asleepList())
	}
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	if lines, _, _ := p.JournalTail(20); strings.Contains(strings.Join(lines, "\n"), "coordinator.wake") {
		t.Errorf("a wake that didn't resume was journaled: %v", lines)
	}
}

// TestSleepCoordinatorRefusals: only a running project's coordinator
// with a conversation sleeps.
func TestSleepCoordinatorRefusals(t *testing.T) {
	s := sleepFixture(t)
	if err := s.sleepCoordinator("s-1", nil); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Errorf("no session: %v", err)
	}
}
