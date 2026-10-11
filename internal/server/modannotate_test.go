package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modannot"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestModAnnotate: $.annotate stores badges in the mod's folder, checks
// the target, and journals as mod:lab.
func TestModAnnotate(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(labManifest), 0o600)
	os.WriteFile(filepath.Join(dir, "main.js"), nil, 0o600)
	os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte("[mods.lab]\nenabled = true\ngrants = [\"project_read\"]\n"), 0o600)
	p := newWatchProject(t)
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "One"}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
	call := func(params any) error {
		raw, _ := json.Marshal(params)
		_, err := s.CallMod(context.Background(), "lab", "annotate", raw)
		return err
	}
	code := func(err error) int {
		if e, ok := err.(*modbroker.Error); ok {
			return e.Code
		}
		return 0
	}
	spans := []map[string]string{{"text": "docs", "role": "info"}}
	tgt := func(m map[string]string) map[string]any {
		return map[string]any{"target": m, "key": "labels", "spans": spans}
	}
	if err := call(tgt(map[string]string{"project": "demo", "task": "T1"})); err != nil {
		t.Fatal(err)
	}
	st := filepath.Join(tmHome, "state", "mods", "lab")
	if got := modannot.Read(st, "lab", "demo")["task:T1"]; len(got) != 1 || got[0].Spans[0].Text != "docs" {
		t.Fatalf("badges = %+v", got)
	}
	for name, m := range map[string]map[string]string{
		"no task":   {"project": "demo", "task": "T9"},
		"no thread": {"project": "demo", "thread": "t-0009"},
		"both":      {"project": "demo", "task": "T1", "thread": "t-1"},
		"neither":   {"project": "demo"},
		"other":     {"project": "nope", "task": "T1"},
	} {
		if err := call(tgt(m)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if code(call(map[string]any{"target": map[string]string{"project": "demo", "task": "T1"}, "key": "BAD", "spans": spans})) != modbroker.CodeBadParams {
		t.Error("bad key not refused as bad params")
	}
	if err := call(map[string]any{"target": map[string]string{"project": "demo", "task": "T1"}, "key": "labels", "spans": nil}); err != nil {
		t.Fatal(err)
	}
	if len(modannot.Read(st, "lab", "demo")) != 0 {
		t.Fatal("null did not remove")
	}
	lines, _, _ := p.JournalTail(5)
	if n := strings.Count(strings.Join(lines, "\n"), " mod:lab annotate task:T1 labels"); n != 2 {
		t.Fatalf("journal = %q", lines)
	}
}
