package ticker

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// clearRig is newRig with auto_clear set to on, and a coordinator idle at
// 50% of its context window (the hint is 40% by default).
func clearRig(t *testing.T, on bool, extra string) *rig {
	t.Helper()
	r := newRig(t)
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	val := map[bool]string{true: "true", false: "false"}[on]
	if err := os.WriteFile(cfg, []byte(extra+"[projects.demo]\nauto_clear = "+val+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &r.host.sessions[0]
	c.PID, c.State, c.Context, c.ContextWindow = 100, "idle", 100_000, 200_000
	return r
}

// With the setting on, an idle coordinator past the hint with nothing
// waiting is first asked to save what it learned once every condition
// held for the grace, then cleared at the next idle gate; both are
// journaled, and it isn't cleared again before ClearEvery.
func TestAutoClearClearsIdleCoordinator(t *testing.T) {
	r := clearRig(t, true, "")
	r.sweep(0)
	r.sweep(DefaultClearGrace - time.Second)
	if len(r.host.asked) != 0 || len(r.host.cleared) != 0 {
		t.Fatalf("asked or cleared within the grace: %v %v", r.host.asked, r.host.cleared)
	}
	r.sweep(time.Second)
	if want := []string{"s-1 " + SaveText(50, false)}; !slices.Equal(r.host.asked, want) || len(r.host.cleared) != 0 {
		t.Fatalf("asked %v, cleared %v; want the ask alone", r.host.asked, r.host.cleared)
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.clear.ask demo s-1 at 50% of its context window (hint 40%): asked to save what it learned") {
		t.Fatalf("journal %q", j)
	}
	if !r.host.still() {
		t.Fatal("ask dropped with nothing waiting")
	}
	// The coordinator saves, then rests: the grace starts again.
	r.host.set("s-1", "working", "")
	r.sweep(time.Second)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	r.sweep(DefaultClearGrace - time.Second)
	if len(r.host.cleared) != 0 || len(r.host.asked) != 1 {
		t.Fatalf("cleared %v asked %v before the grace after the save", r.host.cleared, r.host.asked)
	}
	r.sweep(time.Second)
	if want := []string{"s-1"}; !slices.Equal(r.host.cleared, want) || len(r.host.asked) != 1 {
		t.Fatalf("cleared %v asked %v, want %v once", r.host.cleared, r.host.asked, want)
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.clear demo s-1 at 50% of its context window (hint 40%)") {
		t.Fatalf("journal %q", j)
	}
	// It didn't take (context still high): not again before ClearEvery,
	// and after it asked again first.
	r.sweep(time.Second)
	r.sweep(DefaultClearEvery - 2*time.Second)
	if len(r.host.cleared) != 1 || len(r.host.asked) != 1 {
		t.Fatalf("again within ClearEvery: %v %v", r.host.cleared, r.host.asked)
	}
	r.sweep(time.Second)
	if len(r.host.asked) != 2 || len(r.host.cleared) != 1 {
		t.Fatalf("not asked again after ClearEvery: %v %v", r.host.asked, r.host.cleared)
	}
}

// An ask whose turn ended between two sweeps (the coordinator never seen
// working) is followed by the clear once the grace went by since it.
func TestAutoClearAfterUnseenSave(t *testing.T) {
	r := clearRig(t, true, "")
	r.sweep(0)
	r.sweep(DefaultClearGrace)
	if len(r.host.asked) != 1 {
		t.Fatalf("not asked: %v", r.host.asked)
	}
	r.sweep(DefaultClearGrace - time.Second)
	if len(r.host.cleared) != 0 {
		t.Fatalf("cleared within the grace after the ask: %v", r.host.cleared)
	}
	r.sweep(time.Second)
	if len(r.host.cleared) != 1 {
		t.Fatalf("not cleared: %v", r.host.cleared)
	}
}

// clearNow sweeps a clearRig through the ask and the grace after it, up
// to its clear.
func clearNow(r *rig) {
	r.sweep(DefaultClearGrace)
	r.sweep(DefaultClearGrace)
}

// Each safety condition alone holds the clear back; once it lifts, the
// grace starts again.
func TestAutoClearWaitsForEveryCondition(t *testing.T) {
	cases := []struct {
		name string
		set  func(r *rig)
		undo func(r *rig)
	}{
		{"working", func(r *rig) { r.host.set("s-1", "working", "") }, func(r *rig) { r.host.set("s-1", "idle", "") }},
		{"queued prompt", func(r *rig) { r.host.sessions[0].Queued = 1 }, func(r *rig) { r.host.sessions[0].Queued = 0 }},
		{"coordinator question", func(r *rig) { r.host.sessions[0].Question = &proto.Question{} }, func(r *rig) { r.host.sessions[0].Question = nil }},
		{"context below the hint", func(r *rig) { r.host.sessions[0].Context = 70_000 }, func(r *rig) { r.host.sessions[0].Context = 100_000 }},
		{"context unknown", func(r *rig) { r.host.sessions[0].Context = 0 }, func(r *rig) { r.host.sessions[0].Context = 100_000 }},
		{"thread question", func(r *rig) { r.host.sessions[1].Question = &proto.Question{} }, func(r *rig) { r.host.sessions[1].Question = nil }},
		{"thread blocked", func(r *rig) { r.host.set("s-2", "blocked", "permission") }, func(r *rig) { r.host.set("s-2", "working", ""); r.handleAll() }},
		{"inbox item", func(r *rig) {
			if _, err := r.p.AddItem("report", "t-0001", "done", false); err != nil {
				r.t.Fatal(err)
			}
		}, func(r *rig) { r.handleAll() }},
		{"thread needs you", func(r *rig) { needsYou(r, "which colour?") }, func(r *rig) { needsYou(r, "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := clearRig(t, true, "")
			c.set(r)
			r.sweep(0)
			r.sweep(time.Hour)
			if len(r.host.cleared) != 0 {
				t.Fatalf("cleared while %s: %v", c.name, r.host.cleared)
			}
			if len(r.host.asked) != 0 {
				t.Fatalf("asked while %s: %v", c.name, r.host.asked)
			}
			c.undo(r)
			r.sweep(time.Second)
			if len(r.host.asked) != 0 {
				t.Fatalf("asked without a new grace: %v", r.host.asked)
			}
			clearNow(r)
			if len(r.host.cleared) != 1 {
				t.Fatalf("not cleared once free: %v (journal %q)", r.host.cleared, r.journal())
			}
		})
	}
}

func needsYou(r *rig, q string) {
	r.t.Helper()
	if _, err := thread.UpdateStatus(r.p, "t-0001", func(st *thread.Status) error { st.NeedsYou = q; return nil }); err != nil {
		r.t.Fatal(err)
	}
}

// At delivery the clear is dropped when something came up meanwhile; the
// clear's own place in the queue doesn't count.
func TestAutoClearStillAtDelivery(t *testing.T) {
	r := clearRig(t, true, "")
	r.sweep(0)
	clearNow(r)
	if len(r.host.cleared) != 1 {
		t.Fatalf("not cleared: %v", r.host.cleared)
	}
	r.host.sessions[0].Queued = 1
	if !r.host.still() {
		t.Fatal("dropped for its own place in the queue")
	}
	if _, err := r.p.AddItem("report", "t-0001", "done", false); err != nil {
		t.Fatal(err)
	}
	if r.host.still() {
		t.Fatal("delivered over a new inbox item")
	}
}

// Off (the default), paused, or with the hint at 0, the ticker never
// clears; nor when the agent has no clear prompt, which is tried only
// once per ClearEvery.
func TestAutoClearOff(t *testing.T) {
	for name, r := range map[string]*rig{
		"off":    clearRig(t, false, ""),
		"hint 0": clearRig(t, true, "[ui]\ncontext_hint = 0\n"),
	} {
		r.sweep(0)
		for range 5 {
			r.sweep(time.Hour)
		}
		if len(r.host.cleared) != 0 || len(r.host.asked) != 0 || strings.Contains(r.journal(), "coordinator.clear") {
			t.Fatalf("%s: cleared: %v", name, r.host.cleared)
		}
	}
	r := newRig(t)
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	if err := os.WriteFile(cfg, []byte("[projects.demo]\npaused = true\nauto_clear = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.host.sessions[0].State, r.host.sessions[0].Context, r.host.sessions[0].ContextWindow = "idle", 190_000, 200_000
	r.sweep(0)
	r.sweep(time.Hour)
	if len(r.host.cleared) != 0 || len(r.host.asked) != 0 {
		t.Fatalf("cleared while paused: %v", r.host.cleared)
	}

	r = clearRig(t, true, "")
	r.host.clearErr = errors.New("no clear prompt")
	r.sweep(0)
	clearNow(r)
	if strings.Contains(r.journal(), "coordinator.clear") || len(r.host.asked) != 0 {
		t.Fatalf("asked or journaled for an agent without a clear: %q", r.journal())
	}
}

// A relaunched coordinator (a new pid) waits the grace again.
func TestAutoClearGraceAfterRelaunch(t *testing.T) {
	r := clearRig(t, true, "")
	r.sweep(0)
	r.sweep(DefaultClearGrace - time.Second)
	r.host.sessions[0].PID = 101
	r.sweep(2 * time.Second)
	if len(r.host.asked) != 0 {
		t.Fatalf("asked a fresh process: %v", r.host.asked)
	}
	clearNow(r)
	if len(r.host.cleared) != 1 {
		t.Fatalf("not cleared: %v", r.host.cleared)
	}
}

// The user's save and clear (RequestClear) takes auto-clear's steps with
// auto_clear off, paused, and the context below the hint: the ask at the
// next sweep, the clear once the coordinator rested the grace after it,
// both journaled as auto-clear's. Nothing waiting in the project holds it
// back; the coordinator at work does. It is done once.
func TestRequestClearAsksThenClears(t *testing.T) {
	r := clearRig(t, false, "")
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	if err := os.WriteFile(cfg, []byte("[projects.demo]\npaused = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.host.sessions[0].Context = 20_000
	needsYou(r, "which colour?")
	r.host.set("s-1", "working", "")
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.sweep(DefaultClearGrace)
	if len(r.host.asked) != 0 {
		t.Fatalf("asked a working coordinator: %v", r.host.asked)
	}
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	if want := []string{"s-1 " + SaveText(10, true)}; !slices.Equal(r.host.asked, want) || len(r.host.cleared) != 0 {
		t.Fatalf("asked %v, cleared %v; want the ask alone", r.host.asked, r.host.cleared)
	}
	if !strings.Contains(SaveText(10, true), "The user asked to clear") {
		t.Fatalf("ask %q", SaveText(10, true))
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.clear.ask demo s-1 at 10% of its context window, on the user's request: asked to save") {
		t.Fatalf("journal %q", j)
	}
	if !r.host.still() {
		t.Fatal("ask dropped over the project's waits")
	}
	r.host.set("s-1", "working", "")
	r.sweep(time.Second)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	r.sweep(DefaultClearGrace - time.Second)
	if len(r.host.cleared) != 0 {
		t.Fatalf("cleared before the grace after the save: %v", r.host.cleared)
	}
	r.sweep(time.Second)
	if want := []string{"s-1"}; !slices.Equal(r.host.cleared, want) || len(r.host.asked) != 1 {
		t.Fatalf("cleared %v asked %v", r.host.cleared, r.host.asked)
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.clear demo s-1 at 10% of its context window, on the user's request, idle with nothing waiting") {
		t.Fatalf("journal %q", j)
	}
	for range 3 {
		r.sweep(time.Hour)
	}
	if len(r.host.cleared) != 1 || len(r.host.asked) != 1 {
		t.Fatalf("done again: %v %v", r.host.cleared, r.host.asked)
	}
}

// A request is for the process it names: a relaunch drops it, and so
// does ClearEvery going by; an agent without a clear drops it at the ask.
func TestRequestClearDropped(t *testing.T) {
	r := clearRig(t, false, "")
	r.host.set("s-1", "working", "")
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.host.sessions[0].PID = 101
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	if len(r.host.asked) != 0 {
		t.Fatalf("asked a relaunched coordinator: %v", r.host.asked)
	}

	r = clearRig(t, false, "")
	r.host.set("s-1", "working", "")
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.sweep(DefaultClearEvery)
	r.host.set("s-1", "idle", "")
	r.sweep(time.Second)
	if len(r.host.asked) != 0 {
		t.Fatalf("asked after ClearEvery: %v", r.host.asked)
	}

	r = clearRig(t, false, "")
	r.host.clearErr = errors.New("no clear prompt")
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.sweep(0)
	r.host.clearErr = nil
	clearNow(r)
	if len(r.host.asked) != 0 || len(r.host.cleared) != 0 {
		t.Fatalf("asked or cleared after a failed ask: %v %v", r.host.asked, r.host.cleared)
	}
}

// upkeepRig is a clearRig whose coordinator was just cleared, with
// CONTEXT.md over its budget; the clear took (context unknown) unless
// took is false.
func upkeepRig(t *testing.T, took bool) *rig {
	t.Helper()
	r := clearRig(t, true, "")
	if err := os.WriteFile(r.p.Path("CONTEXT.md"), []byte(strings.Repeat("x", project.BudgetContext+512)), 0o644); err != nil {
		t.Fatal(err)
	}
	r.sweep(0)
	clearNow(r)
	if len(r.host.cleared) != 1 || len(r.host.asked) != 1 {
		t.Fatalf("not cleared: %v %v", r.host.cleared, r.host.asked)
	}
	if took {
		r.host.sessions[0].Context = 0
	}
	return r
}

// After a clear, once the fresh conversation rested the grace, the
// coordinator is asked once to consolidate the files over their budgets,
// journaled; never before the clear.
func TestUpkeepAfterClear(t *testing.T) {
	r := upkeepRig(t, true)
	if strings.Contains(r.host.asked[0], "Upkeep") {
		t.Fatalf("upkeep asked before the clear: %q", r.host.asked[0])
	}
	r.sweep(DefaultClearGrace - time.Second)
	if len(r.host.asked) != 1 {
		t.Fatalf("upkeep within the grace: %v", r.host.asked)
	}
	r.sweep(time.Second)
	over, _ := r.p.Oversized()
	want := "s-1 " + UpkeepText(over)
	if len(r.host.asked) != 2 || r.host.asked[1] != want {
		t.Fatalf("asked %v, want %q last", r.host.asked, want)
	}
	for _, w := range []string{"[tm] Upkeep after the clear: CONTEXT.md is 6.5 KB (~1.7k tokens), over its 6 KB budget (~1.5k tokens). Consolidate them:",
		"Check version-specific facts (agent versions, flakes) against today's versions and main", "as candidates for a code task", "don't move text into code",
		"Drop notes that steer agents around tm's sandbox, guard or access", "a fix in tm or an access rule"} {
		if !strings.Contains(want, w) {
			t.Fatalf("upkeep text %q lacks %q", want, w)
		}
	}
	if !r.host.still() {
		t.Fatal("upkeep dropped with the coordinator idle")
	}
	if j := r.journal(); !strings.Contains(j, " ticker coordinator.upkeep demo s-1 after its clear: asked to consolidate CONTEXT.md") {
		t.Fatalf("journal %q", j)
	}
	for range 3 {
		r.sweep(DefaultClearGrace)
	}
	if len(r.host.asked) != 2 {
		t.Fatalf("upkeep asked again: %v", r.host.asked)
	}
}

// The upkeep waits for the coordinator to rest, and only follows a clear
// that took; nothing over budget, a pause, a relaunch or ClearEvery going
// by drops it.
func TestUpkeepAfterClearHeld(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		r := upkeepRig(t, true)
		r.host.set("s-1", "working", "")
		r.sweep(DefaultClearGrace)
		if len(r.host.asked) != 1 {
			t.Fatalf("upkeep while working: %v", r.host.asked)
		}
		r.host.set("s-1", "idle", "")
		r.sweep(time.Second)
		if len(r.host.asked) != 2 {
			t.Fatalf("no upkeep once idle: %v", r.host.asked)
		}
	})
	t.Run("clear not taken", func(t *testing.T) {
		r := upkeepRig(t, false)
		r.sweep(DefaultClearGrace)
		if len(r.host.asked) != 1 {
			t.Fatalf("upkeep into the old conversation: %v", r.host.asked)
		}
	})
	drops := []struct {
		name string
		set  func(r *rig)
	}{
		{"within budget", func(r *rig) { os.WriteFile(r.p.Path("CONTEXT.md"), []byte("small\n"), 0o644) }},
		{"paused", func(r *rig) {
			cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
			os.WriteFile(cfg, []byte("[projects.demo]\npaused = true\nauto_clear = true\n"), 0o600)
		}},
		{"relaunched", func(r *rig) { r.host.sessions[0].PID = 101 }},
		{"ClearEvery", func(r *rig) {
			r.host.set("s-1", "working", "")
			r.sweep(DefaultClearEvery)
			r.host.set("s-1", "idle", "")
		}},
	}
	for _, c := range drops {
		t.Run(c.name, func(t *testing.T) {
			r := upkeepRig(t, true)
			c.set(r)
			r.sweep(DefaultClearGrace)
			if strings.Contains(r.host.asked[len(r.host.asked)-1], "Upkeep") {
				t.Fatalf("upkeep asked: %v", r.host.asked)
			}
			// Once dropped, it stays dropped.
			os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte("[projects.demo]\nauto_clear = false\n"), 0o600)
			os.WriteFile(r.p.Path("CONTEXT.md"), []byte(strings.Repeat("x", project.BudgetContext+512)), 0o644)
			r.sweep(time.Second)
			for _, a := range r.host.asked {
				if strings.Contains(a, "Upkeep") {
					t.Fatalf("upkeep asked later: %v", r.host.asked)
				}
			}
		})
	}
}

// The user's save and clear is followed by the upkeep too, auto_clear
// off.
func TestUpkeepAfterRequestedClear(t *testing.T) {
	r := clearRig(t, false, "")
	os.WriteFile(r.p.Path("CONTEXT.md"), []byte(strings.Repeat("x", project.BudgetContext+512)), 0o644)
	r.tk.RequestClear("demo", r.host.sessions[0])
	r.sweep(0)
	r.sweep(DefaultClearGrace)
	if len(r.host.cleared) != 1 {
		t.Fatalf("not cleared: %v", r.host.cleared)
	}
	r.host.sessions[0].Context = 0
	r.sweep(DefaultClearGrace)
	if len(r.host.asked) != 2 || !strings.Contains(r.host.asked[1], "[tm] Upkeep after the clear") {
		t.Fatalf("asked %v", r.host.asked)
	}
}
