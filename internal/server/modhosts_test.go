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
	write("config.toml", "[mods.code]\nenabled = false\n")
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
	sup.Set([]modsup.Spec{{ID: "code", Entry: filepath.Join(home, "main.js")}})
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

// TestModHostReachesBroker: a supervised host's $ request goes through
// the supervisor's Broker hook (Server.ModRequest) to the broker, and
// the answer comes back to the mod.
func TestModHostReachesBroker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv(envModsPoll, "20ms")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "mods", "probe")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"mod.toml": "id = \"probe\"\nname = \"P\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nmain = \"main.js\"\n",
		"main.js": `$.on("mod.ready", async () => {
  await $.call("store.set", {key: "k", value: 42});
  const v = await $.call("store.get", {key: "k"});
  try { await $.call("http.fetch", {url: "https://example.com/"}); } catch (e) { $.log("refused", e.code); }
  await $.call("store.set", {key: "seen", value: v});
});`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[mods.probe]\nenabled = true\n"), 0o600); err != nil {
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
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(store)
		if strings.Contains(string(b), `"seen":42`) {
			break
		}
		if time.Now().After(deadline) {
			logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", "probe.log"))
			t.Fatalf("store %q; hosts %+v; log:\n%s", b, s.ModHosts(), logText)
		}
		time.Sleep(20 * time.Millisecond)
	}
	logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", "probe.log"))
	if !strings.Contains(string(logText), "refused -32010") || !strings.Contains(string(logText), "broker: denied http.fetch") {
		t.Fatalf("log:\n%s", logText)
	}
}
