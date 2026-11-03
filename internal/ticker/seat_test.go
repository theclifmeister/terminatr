package ticker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A running director is kept as a coordinator is (the programs design
// §4.1), from its program's keys: its remote control turned on, its
// model synced, and auto-clear's ask and clear, journaled in the
// program as director.*; nothing while programs are off.
func TestDirectorSeat(t *testing.T) {
	r := newRig(t)
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	on := "[features]\nprograms = true\n"
	table := "[programs.acme]\nprojects = [\"demo\"]\nauto_clear = true\nremote_control = true\n"
	if err := os.WriteFile(cfg, []byte(on+table), 0o600); err != nil {
		t.Fatal(err)
	}
	r.host.sessions = append(r.host.sessions, proto.SessionInfo{ID: "s-3", Role: proto.RoleDirector, Project: "acme", Agent: "claude",
		PID: 300, State: "idle", Context: 100_000, ContextWindow: 200_000})
	r.sweep(0)
	if !slices.Contains(r.host.synced, "s-3") {
		t.Errorf("director's model not synced: %v", r.host.synced)
	}
	r.sweep(DefaultClearGrace)
	if !slices.Contains(r.host.remote, "s-3 on") {
		t.Errorf("director's remote control not kept on: %v", r.host.remote)
	}
	if want := "s-3 " + SaveText(50, false); !slices.Contains(r.host.asked, want) {
		t.Fatalf("director not asked to save: %v", r.host.asked)
	}
	r.sweep(DefaultClearGrace)
	if !slices.Equal(r.host.cleared, []string{"s-3"}) {
		t.Fatalf("cleared %v, want the director", r.host.cleared)
	}
	j, err := os.ReadFile(g.Path("JOURNAL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{" ticker director.clear.ask acme s-3 at 50%", " ticker director.clear acme s-3 at 50%", " ticker remote.on acme s-3"} {
		if !strings.Contains(string(j), want) {
			t.Errorf("program journal lacks %q:\n%s", want, j)
		}
	}
	if pj := r.journal(); strings.Contains(pj, "s-3") {
		t.Errorf("the director's acts reached the project's journal:\n%s", pj)
	}
	// The user's save and clear goes the same way.
	r.tk.RequestClear("acme", r.host.sessions[2])
	r.sweep(time.Second)
	if len(r.host.asked) != 2 {
		t.Errorf("requested clear not asked: %v", r.host.asked)
	}
	// Off: the director is dormant, not touched.
	if err := os.WriteFile(cfg, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	r.host.synced, r.host.remote, r.host.asked, r.host.cleared = nil, nil, nil, nil
	r.host.sessions[2].RemoteControl = false
	r.sweep(DefaultClearEvery)
	r.sweep(DefaultClearEvery)
	if slices.Contains(r.host.synced, "s-3") || len(r.host.remote)+len(r.host.asked)+len(r.host.cleared) != 0 {
		t.Errorf("off: director touched: %v %v %v %v", r.host.synced, r.host.remote, r.host.asked, r.host.cleared)
	}
}
