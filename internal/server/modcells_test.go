package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const ageManifest = `id = "review-age"
name = "Review age"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
columns = [{ id = "age", title = "age", width = 4, where = ["sidebar.thread", "dashboard.task"] }]
`

// TestModCells: a mod publishes cells in its own columns for the
// projects it runs for, and a status segment; session.list and the
// project's watch carry them; a bad span, another mod's column or
// another project is refused; the cells come back after a restart and go
// when the mod is turned off.
func TestModCells(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "review-age")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": ageManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(tmHome, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[mods.review-age]\nenabled = true\nprojects = [\"demo\"]\n[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server { return &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()} }
	s := newServer()
	call := func(method string, params string) error {
		_, err := s.CallMod(context.Background(), "review-age", method, json.RawMessage(params))
		return err
	}
	code := func(err error) int {
		if e, ok := err.(*modbroker.Error); ok {
			return e.Code
		}
		return 0
	}
	if err := call("ui.cell.set", `{"column":"age","target":{"project":"demo","task":"T1"},"spans":[{"text":"3d\u001b[2J","role":"warn"},{"icon":"review"}]}`); err != nil {
		t.Fatal(err)
	}
	if err := call("ui.status.set", `{"spans":[{"text":"2 waiting"}]}`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		params string
		code   int
	}{
		{`{"column":"ci","target":{"project":"demo"},"spans":[]}`, modbroker.CodeBadParams},
		{`{"column":"age","target":{"project":"other"},"spans":[{"text":"x"}]}`, modbroker.CodeDenied},
		{`{"column":"age","target":{"task":"T1"},"spans":[{"text":"x"}]}`, modbroker.CodeBadParams},
		{`{"column":"age","target":{"project":"demo"},"spans":[{"text":"x","role":"blink"}]}`, modbroker.CodeBadParams},
		{`{"column":"age","target":{"session":"s-99"},"spans":[{"text":"x"}]}`, modbroker.CodeNotFound},
	} {
		if err := call("ui.cell.set", bad.params); code(err) != bad.code {
			t.Errorf("%s: %v, want code %d", bad.params, err, bad.code)
		}
	}

	cfg, _ := config.Load()
	reg, err := mod.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.noteMods(reg)
	res := s.sessionList()
	if res.Mods == nil || len(res.Mods.Columns) != 1 || res.Mods.Columns[0].ID != "review-age.age" || res.Mods.Columns[0].Width != 4 {
		t.Fatalf("columns %+v", res.Mods)
	}
	if c := res.Mods.Cells; len(c) != 1 || c[0].Spans[0].Text != "3d[2J" || c[0].Spans[1].Icon != "review" {
		t.Fatalf("cells %+v", c)
	}
	if st := res.Mods.Status; len(st) != 1 || st[0].Name != "Review age" {
		t.Fatalf("status %+v", st)
	}
	w, err := s.projectWatchState("demo")
	if err != nil || len(w.Cells) != 1 || w.Cells[0].Target != (proto.CellTarget{Project: "demo", Task: "T1"}) {
		t.Fatalf("watch cells %+v %v", w.Cells, err)
	}
	if w, _ := s.projectWatchState("other"); len(w.Cells) != 0 {
		t.Fatalf("another project's watch %+v", w.Cells)
	}

	// The server stops and starts again: the cells are there before the
	// mod publishes.
	if err := s.cells().Flush(); err != nil {
		t.Fatal(err)
	}
	s = newServer()
	if res := s.sessionList(); res.Mods == nil || len(res.Mods.Cells) != 1 || len(res.Mods.Status) != 1 {
		t.Fatalf("after a restart %+v", res.Mods)
	}

	// Turned off: its cells go.
	if err := os.WriteFile(cfgPath, []byte("[mods.review-age]\nenabled = false\n[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load()
	if reg, err = mod.Load(cfg); err != nil {
		t.Fatal(err)
	}
	s.noteMods(reg)
	if res := s.sessionList(); res.Mods != nil {
		t.Fatalf("a mod turned off %+v", res.Mods)
	}
}
