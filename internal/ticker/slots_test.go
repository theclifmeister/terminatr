package ticker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func slotItems(t *testing.T, slug string) []project.Item {
	t.Helper()
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := p.Inbox()
	var out []project.Item
	for _, it := range items {
		if it.Kind == KindSlotFree {
			out = append(out, it)
		}
	}
	return out
}

func writeConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	d, _ := home.Dir()
	os.WriteFile(filepath.Join(d, "config.toml"), []byte(body), 0o600)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestSlotFree (T364): a coordinator refused at its project's cap gets
// one slot-free item once a thread stops working, and is forgotten.
func TestSlotFree(t *testing.T) {
	r := newRig(t)
	cfg := writeConfig(t, "[projects.demo]\nparallel_threads = 1\n")
	thread.Refused("demo", "over-cap", "T2 Write docs", r.now)
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	if got := slotItems(t, "demo"); len(got) != 0 {
		t.Fatalf("at the cap: %+v", got)
	}
	r.host.set("s-2", "idle", "")
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	got := slotItems(t, "demo")
	if len(got) != 1 || !strings.Contains(got[0].Summary, "a thread slot is free: 0 of 1 working in demo") ||
		!strings.Contains(got[0].Summary, "refused (over-cap)") || !strings.Contains(got[0].Summary, "for T2 Write docs") {
		t.Fatalf("items %+v", got)
	}
	if ws := thread.Waiters(); len(ws) != 1 || ws[0].Told.IsZero() {
		t.Fatalf("not held: %+v", ws)
	}
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	if got := slotItems(t, "demo"); len(got) != 1 {
		t.Fatalf("told twice: %+v", got)
	}
	// Its start forgets it.
	thread.Started("demo")
	if ws := thread.Waiters(); len(ws) != 0 {
		t.Fatalf("after the start: %+v", ws)
	}
	if !strings.Contains(NudgeText(got, nil), "a thread slot is free") {
		t.Fatalf("nudge %q", NudgeText(got, nil))
	}
}

// TestSlotFreeGlobal: one free slot across projects tells one
// coordinator, the one refused first; a paused project's waits.
func TestSlotFreeGlobal(t *testing.T) {
	r := newRig(t)
	for _, s := range []string{"other", "third", "parked"} {
		if _, err := project.New(project.Options{Slug: s}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := writeConfig(t, "[defaults]\nglobal_parallel_threads = 1\n\n[projects.parked]\npaused = true\n")
	thread.Refused("parked", "over-global-cap", "", r.now.Add(-2*60e9))
	thread.Refused("other", "over-global-cap", "Fix the build", r.now.Add(-60e9))
	thread.Refused("third", "over-global-cap", "", r.now)
	r.host.set("s-2", "idle", "")
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	if got := slotItems(t, "other"); len(got) != 1 || !strings.Contains(got[0].Summary, "0 of 1 across projects") {
		t.Fatalf("other: %+v", got)
	}
	if got := slotItems(t, "third"); len(got) != 0 {
		t.Fatalf("third told too: %+v", got)
	}
	if got := slotItems(t, "parked"); len(got) != 0 {
		t.Fatalf("parked told: %+v", got)
	}
	// The slot is held for other: third isn't told on the next sweep,
	// but is once the hold ends without other starting.
	r.tk.slotFree(cfg, r.host.sessions, r.now.Add(60e9))
	if got := slotItems(t, "third"); len(got) != 0 {
		t.Fatalf("third told during the hold: %+v", got)
	}
	r.tk.slotFree(cfg, r.host.sessions, r.now.Add(thread.SlotHold+60e9))
	r.tk.slotFree(cfg, r.host.sessions, r.now.Add(thread.SlotHold+60e9))
	if got := slotItems(t, "third"); len(got) != 1 {
		t.Fatalf("third after the hold: %+v", got)
	}
	ws := thread.Waiters()
	if len(ws) != 2 || ws[0].Project != "parked" || ws[1].Project != "third" {
		t.Fatalf("waiters %+v", ws)
	}
	// A day later the rest are forgotten.
	r.tk.slotFree(cfg, r.host.sessions, r.now.Add(thread.WaiterTTL+60e9))
	if ws := thread.Waiters(); len(ws) != 0 {
		t.Fatalf("waiters after a day %+v", ws)
	}
}
