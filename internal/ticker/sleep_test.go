package ticker

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// restRig is newRig whose coordinator s-1 and thread s-2 rest, with
// config body; the thread's idle item is nudged and so told, which
// leaves the inbox with nothing unread.
func restRig(t *testing.T, body string) *rig {
	t.Helper()
	r := newRig(t)
	r.setConfig(body)
	r.host.set("s-1", "idle", "")
	r.host.set("s-2", "idle", "")
	r.sweep(time.Second)
	r.sweep(time.Second)
	return r
}

const sleep1h = "[projects.demo]\ncoordinator_sleep_after = \"1h\"\n"

// TestSleepAfterIdle: a coordinator resting for its setting sleeps,
// once, journaled; not a moment before.
func TestSleepAfterIdle(t *testing.T) {
	r := restRig(t, sleep1h)
	r.sweep(59 * time.Minute)
	if len(r.host.slept) != 0 {
		t.Fatalf("slept before the hour: %v", r.host.slept)
	}
	r.sweep(2 * time.Minute)
	if !slices.Equal(r.host.slept, []string{"s-1"}) {
		t.Fatalf("slept %v, want s-1", r.host.slept)
	}
	if j := r.journal(); !strings.Contains(j, "coordinator.sleep s-1 idle 1h") {
		t.Errorf("journal lacks the sleep:\n%s", j)
	}
	r.sweep(2 * time.Hour)
	if len(r.host.slept) != 1 {
		t.Errorf("slept again: %v", r.host.slept)
	}
}

// TestSleepConditions: each thing that waits on the coordinator, or
// shows it, keeps it awake, and starts its rest again.
func TestSleepConditions(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *fakeHost)
	}{
		{"a turn running", func(h *fakeHost) { h.set("s-1", "working", "") }},
		{"a permission prompt", func(h *fakeHost) { h.set("s-1", "blocked", "permission") }},
		{"a prompt queued", func(h *fakeHost) { h.sessions[0].Queued = 1 }},
		{"a question menu", func(h *fakeHost) { h.sessions[0].Question = &proto.Question{Tool: "AskUserQuestion"} }},
		{"an escape prompt", func(h *fakeHost) { h.sessions[0].Escape = true }},
		{"a console showing it", func(h *fakeHost) { h.sessions[0].Clients = 1 }},
		{"a thread working", func(h *fakeHost) { h.set("s-2", "working", "") }},
		{"a thread blocked", func(h *fakeHost) { h.set("s-2", "blocked", "permission") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := restRig(t, sleep1h)
			saved := slices.Clone(r.host.sessions)
			tc.set(r.host)
			r.sweep(30 * time.Minute)
			r.sweep(2 * time.Hour)
			if len(r.host.slept) != 0 {
				t.Fatalf("slept with %s", tc.name)
			}
			// Back to rest: the hour starts again.
			r.host.sessions = saved
			r.sweep(time.Second)
			r.sweep(59 * time.Minute)
			if len(r.host.slept) != 0 {
				t.Fatalf("slept before an hour of rest after %s", tc.name)
			}
			r.sweep(2 * time.Minute)
			if len(r.host.slept) != 1 {
				t.Fatalf("not asleep an hour after %s ended", tc.name)
			}
		})
	}
}

// TestSleepUnread: an inbox item the coordinator hasn't been told about
// keeps it awake; one it was told about and left open doesn't.
func TestSleepUnread(t *testing.T) {
	r := restRig(t, sleep1h)
	if _, err := r.p.AddItem("remember", "t-0001", "x", false); err != nil {
		t.Fatal(err)
	}
	r.sweep(2 * time.Minute) // nudged: told
	s := coordinatorSeat(r.p)
	coord := s.of(r.host.Sessions())
	if len(r.items()) == 0 || len(r.tk.projectMemo("demo").Nudged) == 0 {
		t.Fatal("no told item to test with")
	}
	if why := r.tk.sleepBlocked(s, coord, r.host.Sessions()); why != "" {
		t.Fatalf("told item blocks: %s", why)
	}
	if _, err := r.p.AddItem("report", "t-0001", "x", false); err != nil {
		t.Fatal(err)
	}
	if why := r.tk.sleepBlocked(s, coord, r.host.Sessions()); why != "unread inbox item" {
		t.Fatalf("unread item: %q", why)
	}
}

// TestSleepDefaults: unset, a program's member sleeps after 4h, a
// standalone project never; off holds in a program too; [defaults]
// reaches every project; a paused project doesn't sleep.
func TestSleepDefaults(t *testing.T) {
	const member = programsOnCfg + "[programs.acme]\nprojects = [\"demo\"]\n"
	for _, tc := range []struct {
		name  string
		body  string
		after time.Duration // 0: never
	}{
		{"standalone", "", 0},
		{"a member", member, 4 * time.Hour},
		{"a member, programs off", "[programs.acme]\nprojects = [\"demo\"]\n", 0},
		{"a member, off", member + "[projects.demo]\ncoordinator_sleep_after = \"off\"\n", 0},
		{"a member, 0", member + "[projects.demo]\ncoordinator_sleep_after = \"0\"\n", 0},
		{"standalone, defaults", "[defaults]\ncoordinator_sleep_after = \"2h\"\n", 2 * time.Hour},
		{"paused", "[projects.demo]\ncoordinator_sleep_after = \"1h\"\npaused = true\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := restRig(t, tc.body)
			if tc.after == 0 {
				for range 5 {
					r.sweep(24 * time.Hour)
				}
				if len(r.host.slept) != 0 {
					t.Fatalf("slept: %v", r.host.slept)
				}
				return
			}
			r.sweep(tc.after - time.Minute)
			if len(r.host.slept) != 0 {
				t.Fatalf("slept before %v", tc.after)
			}
			r.sweep(2 * time.Minute)
			if len(r.host.slept) != 1 {
				t.Fatalf("not asleep after %v", tc.after)
			}
		})
	}
}

// TestSleepNotDirector: a director never sleeps, whatever its
// program's members say.
func TestSleepNotDirector(t *testing.T) {
	r := restRig(t, programsOnCfg+"[programs.acme]\nprojects = [\"demo\"]\n[defaults]\ncoordinator_sleep_after = \"1h\"\n")
	r.host.sessions = append(r.host.sessions, proto.SessionInfo{ID: "s-9", Role: proto.RoleDirector, Project: "acme", State: "idle"})
	r.sweep(time.Second)
	r.sweep(3 * time.Hour)
	if slices.Contains(r.host.slept, "s-9") {
		t.Fatalf("the director slept: %v", r.host.slept)
	}
}

// TestSleepStill: the conditions are asked again right before the
// stop: a coordinator shown by then stays awake, and its rest starts
// again.
func TestSleepStill(t *testing.T) {
	r := restRig(t, sleep1h)
	r.host.attachAtSleep = true
	r.sweep(61 * time.Minute)
	if len(r.host.slept) != 0 {
		t.Fatalf("slept although shown at the stop: %v", r.host.slept)
	}
	r.host.attachAtSleep = false
	r.sweep(59 * time.Minute)
	if len(r.host.slept) != 0 {
		t.Fatalf("the rest didn't start again: %v", r.host.slept)
	}
	r.sweep(2 * time.Minute)
	if len(r.host.slept) != 1 {
		t.Fatalf("not asleep after a new hour: %v", r.host.slept)
	}
}

// TestWakeForItem: a new inbox item wakes the sleeping coordinator,
// which the next sweep nudges about it.
func TestWakeForItem(t *testing.T) {
	r := restRig(t, sleep1h)
	r.sweep(61 * time.Minute)
	if len(r.host.slept) != 1 {
		t.Fatal("not asleep")
	}
	r.sweep(time.Hour)
	if len(r.host.woke) != 0 {
		t.Fatalf("woke with nothing new: %v", r.host.woke)
	}
	n := len(r.host.prompts)
	if _, err := r.p.AddItem("report", "t-0001", "x", false); err != nil {
		t.Fatal(err)
	}
	r.sweep(time.Second)
	if !slices.Equal(r.host.woke, []string{"s-1"}) {
		t.Fatalf("woke %v, want s-1", r.host.woke)
	}
	r.sweep(time.Minute)
	if len(r.host.prompts) != n+1 || !strings.Contains(r.host.prompts[n], "1 new inbox item") {
		t.Fatalf("no nudge after the wake: %q", r.host.prompts[n:])
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{4 * time.Hour: "4h", 30 * time.Minute: "30m", 90 * time.Minute: "1h30m", 4*time.Hour + 20*time.Second: "4h"} {
		if got := shortDuration(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
