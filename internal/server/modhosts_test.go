package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modproto"
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
	if !ok || len(specs) != 2 {
		t.Fatalf("specs %+v %v", specs, ok)
	}
	if sp := specs[0]; sp.ID != "code" || sp.Entry != filepath.Join(home, "mods", "code", "main.js") ||
		len(sp.Grants) != 1 || len(sp.Projects) != 1 || sp.Bundle != nil || sp.Eager {
		t.Fatalf("spec %+v", sp)
	}
	// The built-in PR/CI column runs its precompiled bundle, at once.
	if sp := specs[1]; sp.ID != mod.BundledPRCI || sp.Entry != "" || len(sp.Bundle) == 0 || !sp.Eager ||
		!slices.Equal(sp.Grants, []string{mod.PermProjectRead}) {
		t.Fatalf("built-in spec %s %q %d bytes eager %v %v", sp.ID, sp.Entry, len(sp.Bundle), sp.Eager, sp.Grants)
	}
	// Off, it is no spec: the rest of the test follows the user's mod.
	write("config.toml", "[mods.code]\nenabled = true\ngrants = [\"project_read\"]\nprojects = [\"demo\"]\n[mods.pr-ci-column]\nenabled = false\n")
	if specs, _ := s.modSpecs(); len(specs) != 1 || specs[0].ID != "code" {
		t.Fatalf("with the built-in off: %+v", specs)
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
	if _, perr := s.restartMod("code", caller.Caller{Kind: caller.Thread}); perr == nil || perr.Code != "human-only" {
		t.Fatalf("an agent restarted a mod: %v", perr)
	}
	if _, perr := s.restartMod("theme", human); perr == nil || perr.Code != proto.ErrBadParams {
		t.Fatalf("restarted a data mod: %v", perr)
	}
	res, perr := s.restartMod("code", human)
	if perr != nil || len(res.(proto.ModHostsResult).Hosts) != 1 {
		t.Fatalf("mod.restart: %+v %v", res, perr)
	}
	if st := s.modHosts(); st.NoMods || len(st.Hosts) != 1 || st.Hosts[0].ID != "code" {
		t.Fatalf("mod.hosts %+v", st)
	}
	write("config.toml", "[mods.code]\nenabled = false\n[mods.pr-ci-column]\nenabled = false\n")
	wait("code stopped", func(st []modsup.Status) bool { return len(st) == 0 })
}

// TestModRestartHumanOnly: mod.hosts lists the hosts as the Mods tab
// reads them; mod.restart is refused for an agent, and for a mod that
// has no host, and restarts one for the human.
func TestModRestartHumanOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	s := &Server{opts: Options{Bin: "/nonexistent/tm"}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	if res := s.modHosts(); res.Hosts == nil || len(res.Hosts) != 0 {
		t.Fatalf("no supervisor: %+v", res)
	}
	if _, perr := s.restartMod("code", caller.Caller{Kind: caller.Human}); perr == nil {
		t.Fatal("restarted a mod without a supervisor")
	}
	sup := s.newModHosts()
	defer sup.Close()
	s.modSup.Store(sup)
	// Installed and enabled, as mod.restart reads the mods again first.
	os.MkdirAll(filepath.Join(home, "mods", "code"), 0o700)
	os.WriteFile(filepath.Join(home, "mods", "code", "mod.toml"), []byte("id = \"code\"\nname = \"X\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nmain = \"main.js\"\n"), 0o600)
	os.WriteFile(filepath.Join(home, "mods", "code", "main.js"), nil, 0o600)
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("[mods.code]\nenabled = true\n"), 0o600)
	sup.Set([]modsup.Spec{{ID: "code", Entry: filepath.Join(home, "mods", "code", "main.js")}})
	if res := s.modHosts(); len(res.Hosts) != 1 || res.Hosts[0].ID != "code" || res.Hosts[0].State == "" {
		t.Fatalf("hosts: %+v", res)
	}
	for _, c := range []caller.Caller{{Kind: caller.Coordinator, Project: "p"}, {Kind: caller.Thread, Project: "p", Thread: "t-0001"}} {
		if _, perr := s.restartMod("code", c); perr == nil || perr.Code != "human-only" {
			t.Errorf("%v restarted it: %v", c.Kind, perr)
		}
	}
	if _, perr := s.restartMod("other", caller.Caller{Kind: caller.Human}); perr == nil {
		t.Error("restarted a mod that has no host")
	}
	if _, perr := s.restartMod("code", caller.Caller{Kind: caller.Human}); perr != nil {
		t.Errorf("the human's restart: %v", perr)
	}
}

// TestModHostReachesBroker: a TypeScript mod's $ calls go from its
// supervised host through the supervisor's Broker hook
// (Server.ModRequest) to the broker, and the answers come back to it:
// its store, a project it runs for, and a fetch it has no grant for,
// refused with the broker's code and audited in its log.
func TestModHostReachesBroker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv(envModsPoll, "20ms")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	dir := filepath.Join(home, "mods", "probe")
	if err := os.MkdirAll(filepath.Join(dir, "types"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"mod.toml": "id = \"probe\"\nname = \"P\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nmain = \"main.ts\"\n" +
			"[permissions]\nproject_read = true\n",
		"types/tm.d.ts": modapi.TS(),
		"main.ts": `import type { TM } from "./types/tm";

$.on("mod.ready", async () => {
  await $.store.set("k", 42);
  const v = (await $.store.get("k")) as number;
  const w: TM.ProjectWatch = await $.project.get("demo");
  let refused = 0;
  try {
    await $.http.fetch("https://example.com/");
  } catch (e) {
    refused = (e as { code: number }).code;
  }
  await $.store.set("seen", { v, project: w.project, refused });
});
`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.probe]\nenabled = true\ngrants = [\"project_read\"]\nprojects = [\"demo\"]\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: os.Args[0]}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
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
	store := filepath.Join(home, "state", "mods", "probe", "store.json")
	type seen struct {
		V       int    `json:"v"`
		Project string `json:"project"`
		Refused int    `json:"refused"`
	}
	want := seen{V: 42, Project: "demo", Refused: modproto.CodeDenied}
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, _ := os.ReadFile(store)
		var m struct {
			Seen *seen `json:"seen"`
		}
		if json.Unmarshal(b, &m) == nil && m.Seen != nil {
			if *m.Seen != want {
				t.Fatalf("seen %+v, want %+v", *m.Seen, want)
			}
			break
		}
		if time.Now().After(deadline) {
			logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", "probe.log"))
			t.Fatalf("store %q; hosts %+v; log:\n%s", b, s.ModHosts(), logText)
		}
		time.Sleep(20 * time.Millisecond)
	}
	logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", "probe.log"))
	if !strings.Contains(string(logText), "broker: denied http.fetch") {
		t.Fatalf("log:\n%s", logText)
	}
}

// TestModHostsSafeMode: in safe mode no user mod is given to the
// supervisor (a built-in one still is), mod.hosts says so and
// mod.restart refuses a user mod.
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
	if specs, ok := s.modSpecs(); !ok || len(specs) != 2 {
		t.Fatalf("specs %+v %v", specs, ok)
	}
	// Built-in mods are part of tm: safe mode still runs them.
	s.noMods = true
	if specs, ok := s.modSpecs(); !ok || len(specs) != 1 || specs[0].ID != mod.BundledPRCI {
		t.Fatalf("safe mode runs %+v", specs)
	}
	if !s.modHosts().NoMods {
		t.Fatal("mod.hosts doesn't say safe mode")
	}
	if _, perr := s.restartMod("code", human); perr == nil || !strings.Contains(perr.Message, "safe mode") {
		t.Fatalf("mod.restart in safe mode: %v", perr)
	}
}
