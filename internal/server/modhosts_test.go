package server

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/modsup"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// TestModHostsFollowConfig: an enabled code mod is given to the
// supervisor (its host can't start here: Bin is missing, so it backs
// off), a data mod and a disabled one are not, and disabling it in
// config.toml stops it without a restart.
func TestModHostsFollowConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv(envModsPoll, "20ms")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		path = filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	head := "name = \"X\"\nversion = \"1.0.0\"\napi = \"1.0\"\n"
	write("mods/code/mod.toml", "id = \"code\"\n"+head+"[provides]\nmain = \"main.js\"\n[permissions]\nproject_read = true\n")
	write("mods/code/main.js", "")
	write("mods/off/mod.toml", "id = \"off\"\n"+head+"[provides]\nmain = \"main.js\"\n")
	write("mods/off/main.js", "")
	write("mods/theme/mod.toml", "id = \"theme\"\n"+head+"[provides]\ntheme = \"t.toml\"\n")
	write("mods/theme/t.toml", "")
	write("config.toml", "[mods.code]\nenabled = true\ngrants = [\"project_read\"]\nprojects = [\"demo\"]\n[mods.theme]\nenabled = true\n")

	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm"}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	specs, ok := s.modSpecs()
	if !ok || len(specs) != 1 {
		t.Fatalf("specs %+v %v", specs, ok)
	}
	if sp := specs[0]; sp.ID != "code" || sp.Entry != filepath.Join(home, "mods", "code", "main.js") ||
		len(sp.Grants) != 1 || len(sp.Projects) != 1 {
		t.Fatalf("spec %+v", sp)
	}

	sup := s.newModHosts()
	s.modSup.Store(sup)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runModHosts(ctx, sup)
	}()
	defer func() {
		cancel()
		<-done
	}()
	wait := func(what string, cond func([]modsup.Status) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond(s.ModHosts()) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: %+v", what, s.ModHosts())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("code backing off", func(st []modsup.Status) bool {
		return len(st) == 1 && st[0].ID == "code" && st[0].State == modsup.Backoff
	})
	// mod.restart: the human's, for an enabled code mod.
	if _, perr := s.modRestart(proto.ModRestartParams{ID: "code"}, caller.Caller{Kind: caller.Thread}); perr == nil || perr.Code != "human-only" {
		t.Fatalf("an agent restarted a mod: %v", perr)
	}
	if _, perr := s.modRestart(proto.ModRestartParams{ID: "theme"}, human); perr == nil || perr.Code != proto.ErrRefused {
		t.Fatalf("restarted a data mod: %v", perr)
	}
	res, perr := s.modRestart(proto.ModRestartParams{ID: "code"}, human)
	if perr != nil || len(res.(proto.ModStatusResult).Hosts) != 1 {
		t.Fatalf("mod.restart: %+v %v", res, perr)
	}
	if st := s.modStatus(); st.NoMods || len(st.Hosts) != 1 || st.Hosts[0].ID != "code" {
		t.Fatalf("mod.status %+v", st)
	}
	write("config.toml", "[mods.code]\nenabled = false\n")
	wait("code stopped", func(st []modsup.Status) bool { return len(st) == 0 })
}

// TestModHostsSafeMode: in safe mode no user mod is given to the
// supervisor, mod.status says so and mod.restart refuses.
func TestModHostsSafeMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	os.MkdirAll(filepath.Join(home, "mods", "code"), 0o700)
	os.WriteFile(filepath.Join(home, "mods", "code", "mod.toml"), []byte("id = \"code\"\nname = \"X\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nmain = \"main.js\"\n"), 0o600)
	os.WriteFile(filepath.Join(home, "mods", "code", "main.js"), nil, 0o600)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("[mods.code]\nenabled = true\n"), 0o600)
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm"}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	if specs, ok := s.modSpecs(); !ok || len(specs) != 1 {
		t.Fatalf("specs %+v %v", specs, ok)
	}
	s.noMods = true
	if specs, ok := s.modSpecs(); !ok || len(specs) != 0 {
		t.Fatalf("safe mode runs %+v", specs)
	}
	if !s.modStatus().NoMods {
		t.Fatal("mod.status doesn't say safe mode")
	}
	if _, perr := s.modRestart(proto.ModRestartParams{ID: "code"}, human); perr == nil || !strings.Contains(perr.Message, "safe mode") {
		t.Fatalf("mod.restart in safe mode: %v", perr)
	}
}
