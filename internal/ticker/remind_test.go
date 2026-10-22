package ticker

import (
	"strings"
	"testing"
	"time"
)

// An item the coordinator was told about and left open is reminded of
// once it idled DefaultRemind, then after twice as long each time,
// RemindCap times; a new item's nudge starts them again (T669).
func TestRemindLeftOpen(t *testing.T) {
	r := newRig(t)
	r.p.AddItem("remember", "t-0001", "x", false)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	if len(r.host.prompts) != 1 || !strings.Contains(r.host.prompts[0], "1 new inbox item") {
		t.Fatalf("nudge %q", r.host.prompts)
	}
	// It works on something else, then idles with the item open.
	r.host.set("s-1", "working", "")
	r.sweep(10 * time.Minute)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	r.sweep(DefaultRemind - time.Second)
	if len(r.host.prompts) != 1 {
		t.Fatalf("reminded within the idle interval: %q", r.host.prompts)
	}
	r.sweep(time.Second)
	want := "s-1 [tm] Still open in your inbox, told before: 1 item: T1 Fix it (t-0001) has lessons to file."
	if len(r.host.prompts) != 2 || !strings.HasPrefix(r.host.prompts[1], want) {
		t.Fatalf("reminder %q", r.host.prompts)
	}
	if text, ok := r.host.refresh(); !ok || text != strings.TrimPrefix(r.host.prompts[1], "s-1 ") {
		t.Fatalf("refresh %q %v", text, ok)
	}
	// Backoff: twice as long, then four times, then no more.
	r.sweep(2*DefaultRemind - time.Second)
	if len(r.host.prompts) != 2 {
		t.Fatalf("second reminder too soon: %q", r.host.prompts)
	}
	r.sweep(time.Second)
	r.sweep(4 * DefaultRemind)
	if len(r.host.prompts) != 4 {
		t.Fatalf("want 3 reminders: %q", r.host.prompts)
	}
	for range 4 {
		r.sweep(time.Hour)
	}
	if len(r.host.prompts) != 4 {
		t.Fatalf("reminded past the cap: %q", r.host.prompts)
	}
	// A new item is nudged alone; the reminders start again, naming both.
	r.p.AddItem("report", "t-0001", "y", false)
	r.sweep(time.Second)
	if len(r.host.prompts) != 5 || !strings.Contains(r.host.prompts[4], "1 new inbox item: T1 Fix it (t-0001) reported.") {
		t.Fatalf("new item %q", r.host.prompts)
	}
	r.sweep(time.Second)
	r.sweep(DefaultRemind)
	if len(r.host.prompts) != 6 || !strings.Contains(r.host.prompts[5], "told before: 2 items") {
		t.Fatalf("reminder after a new nudge %q", r.host.prompts)
	}
	// Handled: nothing more.
	r.handleAll()
	r.sweep(time.Hour)
	if len(r.host.prompts) != 6 {
		t.Fatalf("reminded of handled items: %q", r.host.prompts)
	}
}

// A reminder waits for a coordinator idle with nothing queued.
func TestRemindWaitsForIdle(t *testing.T) {
	r := newRig(t)
	r.p.AddItem("remember", "t-0001", "x", false)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	r.host.set("s-1", "working", "")
	r.sweep(time.Hour)
	r.host.sessions[0].State, r.host.sessions[0].Queued = "idle", 1
	r.sweep(time.Hour)
	if len(r.host.prompts) != 1 {
		t.Fatalf("reminded a busy coordinator: %q", r.host.prompts)
	}
	r.host.sessions[0].Queued = 0
	r.sweep(time.Second)
	r.sweep(DefaultRemind)
	if len(r.host.prompts) != 2 {
		t.Fatalf("no reminder once idle: %q", r.host.prompts)
	}
}

// The user's clear with an item still open says so in the journal, and
// the fresh conversation is nudged about it (T669).
func TestRenudgeAfterRequestedClear(t *testing.T) {
	r := clearRig(t, false, "")
	r.p.AddItem("remember", "t-0001", "x", false)
	r.sweep(time.Second)
	if len(r.host.prompts) != 1 {
		t.Fatalf("nudge %q", r.host.prompts)
	}
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.sweep(time.Second)
	r.sweep(DefaultClearGrace)
	if len(r.host.cleared) != 1 {
		t.Fatalf("not cleared: asked %v cleared %v", r.host.asked, r.host.cleared)
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.clear demo s-1 at 50% of its context window, on the user's request, still waiting: inbox not empty: 1 item open (remember t-0001)") {
		t.Fatalf("journal %q", j)
	}
	if len(r.host.prompts) != 1 {
		t.Fatalf("nudged before the clear took: %q", r.host.prompts)
	}
	r.sweep(time.Second)
	if len(r.host.prompts) != 2 || !strings.Contains(r.host.prompts[1], "1 new inbox item: T1 Fix it (t-0001) has lessons to file.") {
		t.Fatalf("not nudged after the clear: %q", r.host.prompts)
	}
}

// A conversation the ticker didn't clear (the user's own /clear: a new
// agent session id; a relaunch without one known) is nudged about what
// is still open; a resume under the same session id is not.
func TestRenudgeNewConversation(t *testing.T) {
	r := newRig(t)
	c := &r.host.sessions[0]
	c.State, c.PID, c.AgentSID = "idle", 100, "a"
	r.p.AddItem("remember", "t-0001", "x", false)
	r.sweep(time.Second)
	// A resume (server restart): another process, the same conversation.
	c.PID = 101
	r.sweep(time.Minute)
	if len(r.host.prompts) != 1 {
		t.Fatalf("nudged a resumed conversation: %q", r.host.prompts)
	}
	// Its session id not known yet: undecided.
	c.AgentSID = ""
	r.sweep(time.Minute)
	if len(r.host.prompts) != 1 {
		t.Fatalf("nudged with the session id unknown: %q", r.host.prompts)
	}
	c.AgentSID = "b"
	r.sweep(time.Minute)
	if len(r.host.prompts) != 2 || !strings.Contains(r.host.prompts[1], "1 new inbox item") {
		t.Fatalf("not nudged after /clear: %q", r.host.prompts)
	}

	// Without session ids, the process decides.
	r = newRig(t)
	c = &r.host.sessions[0]
	c.State, c.PID = "idle", 100
	r.p.AddItem("remember", "t-0001", "x", false)
	r.sweep(time.Second)
	r.sweep(time.Minute)
	c.PID = 102
	r.sweep(time.Minute)
	if len(r.host.prompts) != 2 {
		t.Fatalf("relaunch: %q", r.host.prompts)
	}
}
