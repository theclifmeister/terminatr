package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	_ "github.com/theclifmeister/terminatr/internal/agent/claude"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/session"
)

// TestWorked: a kickoff the agent hasn't started on doesn't mark the
// session prompted, or a restart would resume a conversation Claude never
// saved.
func TestWorked(t *testing.T) {
	for _, c := range []struct {
		st   agent.Merged
		want bool
	}{
		{agent.Merged{State: agent.StateWorking}, true},
		{agent.Merged{State: agent.StateWorking, Reason: "background"}, true},
		{agent.Merged{State: agent.StateBlocked, Reason: "permission"}, true},
		{agent.Merged{State: agent.StateWorking, Reason: agent.ReasonKickoff}, false},
		{agent.Merged{State: agent.StateIdle}, false},
		{agent.Merged{State: agent.StateUnknown}, false},
	} {
		if got := worked(session.AgentState{Merged: c.st}); got != c.want {
			t.Errorf("worked(%s/%s) = %v", c.st.State, c.st.Reason, got)
		}
	}
}

// TestOwnWorktree: only a worktree tm made (worktrees/<slug>/<dir>) is
// trusted ahead of launch, never the folder above it or elsewhere.
func TestOwnWorktree(t *testing.T) {
	home := t.TempDir()
	s := &Server{opts: Options{Paths: client.Paths{Home: home}}}
	for dir, want := range map[string]bool{
		home + "/worktrees/demo/t-0001-fix":       true,
		home + "/worktrees/demo/t-0001-fix/sub":   false,
		home + "/worktrees/demo":                  false,
		home + "/worktrees":                       false,
		home + "/projects/demo":                   false,
		home + "/worktrees/demo/../../projects/x": false,
		"/tmp/elsewhere":                          false,
	} {
		if got := s.ownWorktree(dir); got != want {
			t.Errorf("ownWorktree(%s) = %v", dir, got)
		}
	}
}

// TestWriteLaunchFilesFails: a runtime dir that can't be written to is an
// error, not a session started without its hooks and settings.
func TestWriteLaunchFilesFails(t *testing.T) {
	if os.Geteuid() == 0 || !chmodApplies {
		t.Skip("directory permissions don't apply")
	}
	rt := t.TempDir()
	if err := writeLaunchFiles(rt, map[string][]byte{"settings.json": []byte("{}"), "hooks/a": []byte("x")}); err != nil {
		t.Fatalf("writable dir: %v", err)
	}
	for _, name := range []string{"brief.md", "sub/hook.sh"} {
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(ro, 0o700) })
		if err := writeLaunchFiles(ro, map[string][]byte{name: []byte("x")}); err == nil {
			t.Errorf("%s in an unwritable runtime dir: no error", name)
		}
	}
}

// TestCoordinatorEssentials: after a clear the coordinator gets all of
// `tm context` (T109), within the budget, with when to run it again.
func TestCoordinatorEssentials(t *testing.T) {
	secs := []project.Section{
		{Title: "Project", Lines: []string{"Project: demo (demo)"}},
		{Title: "Context (CONTEXT.md)", Lines: []string{"## Where things stand"}},
		{Title: "Memory index (MEMORY.md)", Lines: []string{"- a memory"}},
		{Title: "Tasks", Lines: []string{"T1 a task"}},
		{Title: "Journal (last 10)", Lines: []string{"a journal line"}},
	}
	got := coordinatorEssentials(secs, coordinatorBudget)
	for _, want := range []string{"## Project\n", "Where things stand", "a memory", "## Tasks\n", "a journal line",
		"That is `tm context` as of this conversation's start; run it"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q", want)
		}
	}
	long := make([]string, 2000)
	for i := range long {
		long[i] = "T1 a task with a long enough title to fill the budget"
	}
	secs[3].Lines = long
	if got := coordinatorEssentials(secs, project.StartupBudget); len(got) > project.StartupBudget || !strings.HasSuffix(got, "run `tm context` now for the rest.\n") {
		t.Fatalf("%d bytes at the start-up budget, ends %q", len(got), got[len(got)-100:])
	}
	got = coordinatorEssentials(secs, coordinatorBudget)
	if len(got) > coordinatorBudget {
		t.Fatalf("%d bytes, over the budget", len(got))
	}
	if !strings.Contains(got, "[… cut at the size budget]") || !strings.HasSuffix(got, "run `tm context` now for the rest.\n") {
		t.Fatalf("no cut mark or no last line:\n%s", got[len(got)-300:])
	}
}

// TestPaneScheme: a new agent hears of the pane palette a console last
// drew the panes with, by its scheme, and of none in safe mode (T342).
func TestPaneScheme(t *testing.T) {
	for _, c := range []struct {
		known  uint32
		noMods bool
		want   string
	}{
		{0, false, ""},
		{emu.KnownBg | 1<<3, false, ""},
		{emu.KnownBg | emu.PaneDark, false, "dark"},
		{emu.PaneLight, false, "light"},
		{emu.KnownBg | emu.PaneDark, true, ""},
	} {
		s := &Server{colors: emu.Colors{Known: c.known}, noMods: c.noMods}
		if got := s.paneScheme(); got != c.want {
			t.Errorf("known %b, safe mode %v: %q, want %q", c.known, c.noMods, got, c.want)
		}
	}
}

// TestLaunchTheme: a new agent gets [ui] theme's colours for the scheme
// whose pane palette a console draws, read by the server itself; none
// for the other scheme without one, the built-in theme, a mod that is
// off, or in safe mode (T342).
func TestLaunchTheme(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	writeNordMod(t)
	cfgPath, _ := config.Path()
	writeCfg := func(body string) {
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCfg("[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n")
	s := &Server{log: log.New(io.Discard, "", 0)}
	a := s.launchTheme("dark")
	if a == nil || a.Fg["accent"] != "#88c0d0" || a.PaneBg != "#2e3440" || a.Scheme != "dark" {
		t.Fatalf("nord dark: %+v", a)
	}
	if a := s.launchTheme("light"); a != nil {
		t.Fatalf("nord light has no pane palette, yet %+v", a)
	}
	if a := s.launchTheme(""); a != nil {
		t.Fatalf("no pane scheme, yet %+v", a)
	}
	s.noMods = true
	if a := s.launchTheme("dark"); a != nil {
		t.Fatalf("safe mode, yet %+v", a)
	}
	s.noMods = false
	writeCfg("[ui]\ntheme = \"default\"\n")
	if a := s.launchTheme("dark"); a != nil {
		t.Fatalf("built-in theme, yet %+v", a)
	}
	writeCfg("[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = false\n")
	s.forgetMods()
	if a := s.launchTheme("dark"); a != nil {
		t.Fatalf("nord is off, yet %+v", a)
	}
}

// writeNordMod writes the nord theme mod, with a dark pane palette, into
// TERMINATR_HOME's mods.
func writeNordMod(t *testing.T) {
	t.Helper()
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"mod.toml":   "id = \"nord\"\nname = \"Nord\"\nversion = \"0.1.0\"\napi = \"1.6\"\n[provides]\ntheme = \"theme.toml\"\n",
		"theme.toml": "[dark]\naccent = { fg = [\"cyan\", \"#88c0d0\"] }\n[dark.pane]\nbg = \"#2e3440\"\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, "nord", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestColorsSurviveRestart: the colours and pane scheme a console last
// reported outlive the server, so the next one's agents, resumed before
// any console attached, get [ui] theme's colours (T344).
func TestColorsSurviveRestart(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	writeNordMod(t)
	cfgPath, _ := config.Path()
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	newServer := func() *Server {
		return &Server{opts: Options{Paths: client.Paths{Sessions: path}}, log: log.New(io.Discard, "", 0),
			prevProject: map[string]string{}, dormant: map[string]SessionRecord{}, records: map[string]SessionRecord{}}
	}
	old := newServer()
	old.colors = emu.Colors{Known: emu.KnownBg | emu.PaneDark, Bg: emu.RGB{R: 0x2e, G: 0x34, B: 0x40}}
	old.scheme = emu.SchemeDark
	if err := old.saveLocked("clean"); err != nil {
		t.Fatal(err)
	}

	s := newServer()
	s.loadPrevious()
	if s.colors != old.colors || s.scheme != emu.SchemeDark {
		t.Errorf("restarted with colours %+v scheme %v, want %+v dark", s.colors, s.scheme, old.colors)
	}
	scheme := s.paneScheme()
	if a := s.launchTheme(scheme); scheme != "dark" || a == nil || a.PaneBg != "#2e3440" {
		t.Errorf("a session resumed at start: pane scheme %q, theme %+v", scheme, a)
	}

	// A server that never heard of a console's colours keeps none.
	if err := newServer().saveLocked(""); err != nil {
		t.Fatal(err)
	}
	s = newServer()
	s.loadPrevious()
	if got := s.paneScheme(); got != "" {
		t.Errorf("no colours reported, yet pane scheme %q", got)
	}
}

// TestThemeChangeReachesClaude: a launch writes Claude's live theme into
// its config folder and selects it; a console's new colours (another
// theme's pane palette, a scheme switch, a theme without one) rewrite it
// for the sessions already running, which Claude reloads; safe mode
// writes none (T345).
func TestThemeChangeReachesClaude(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	writeNordMod(t)
	// With a light pane palette too.
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nord", "theme.toml"), []byte("[dark]\naccent = { fg = [\"cyan\", \"#88c0d0\"] }\n[dark.pane]\nbg = \"#2e3440\"\n[light]\naccent = { fg = [\"blue\", \"#5e81ac\"] }\n[light.pane]\nbg = \"#eceff4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := config.Path()
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	claude, _ := reg.Get("claude")
	themePath := filepath.Join(cfgDir, "themes", "terminatr.json")
	read := func() (ct struct {
		Base      string
		Overrides map[string]string
	}) {
		t.Helper()
		b, err := os.ReadFile(themePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &ct); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return
	}

	// Safe mode: none, and the launch doesn't select it.
	s := &Server{log: log.New(io.Discard, "", 0), agents: reg, noMods: true}
	if s.writeLiveTheme(claude) {
		t.Fatal("a live theme in safe mode")
	}
	if _, err := os.Stat(themePath); err == nil {
		t.Fatal("safe mode wrote a theme")
	}
	// No console yet: the user's own theme (Claude's default, dark).
	s.noMods = false
	if !s.writeLiveTheme(claude) {
		t.Fatal("no live theme for Claude")
	}
	if ct := read(); ct.Base != "dark" || len(ct.Overrides) != 0 {
		t.Fatalf("before any console: %+v", ct)
	}
	// A console draws nord's dark palette: the running Claude gets it.
	s.mu.Lock()
	s.colors = emu.Colors{Known: emu.KnownBg | emu.PaneDark}
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "dark-ansi" || ct.Overrides["claude"] != "#88c0d0" || ct.Overrides["inverseText"] != "#2e3440" {
		t.Fatalf("nord dark: %+v", ct)
	}
	// The terminal turns light: nord's light palette.
	s.mu.Lock()
	s.colors, s.scheme = emu.Colors{Known: emu.KnownBg | emu.PaneLight}, emu.SchemeLight
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "light-ansi" || ct.Overrides["claude"] != "#5e81ac" {
		t.Fatalf("nord light: %+v", ct)
	}
	// A theme without a pane palette: the user's own theme again, which
	// here follows the terminal (auto).
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.json"), []byte(`{"theme": "auto"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.colors = emu.Colors{Known: emu.KnownBg, Bg: emu.RGB{R: 0xec, G: 0xef, B: 0xf4}}
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "light" || len(ct.Overrides) != 0 {
		t.Fatalf("no pane palette: %+v", ct)
	}
	// The built-in theme follows the terminal: Claude's ANSI theme for
	// the scheme over the user's own, live.
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"default\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "light-ansi" || len(ct.Overrides) != 0 {
		t.Fatalf("follow terminal, light: %+v", ct)
	}
	s.mu.Lock()
	s.colors, s.scheme = emu.Colors{Known: emu.KnownBg, Bg: emu.RGB{R: 0x10, G: 0x12, B: 0x16}}, emu.SchemeDark
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "dark-ansi" {
		t.Fatalf("follow terminal, dark: %+v", ct)
	}
	// A dark terminal that reports a light scheme (the system's, say):
	// Claude follows the background the user sees (T520).
	s.mu.Lock()
	s.scheme = emu.SchemeLight
	s.themeChangedLocked()
	s.mu.Unlock()
	if ct := read(); ct.Base != "dark-ansi" {
		t.Fatalf("follow terminal, dark background, light scheme: %+v", ct)
	}
}

// TestSchemeName: an agent's theme follows the terminal's background
// when a console reported it, whatever scheme it reported (a terminal's
// may be the system's appearance), else the reported scheme (T520).
func TestSchemeName(t *testing.T) {
	dark, light := emu.RGB{R: 0x10, G: 0x12, B: 0x16}, emu.RGB{R: 0xff, G: 0xff, B: 0xff}
	for _, c := range []struct {
		colors emu.Colors
		scheme emu.Scheme
		want   string
	}{
		{emu.Colors{}, 0, ""},
		{emu.Colors{}, emu.SchemeLight, "light"},
		{emu.Colors{}, emu.SchemeDark, "dark"},
		{emu.Colors{Known: emu.KnownBg, Bg: dark}, emu.SchemeLight, "dark"},
		{emu.Colors{Known: emu.KnownBg, Bg: dark}, 0, "dark"},
		{emu.Colors{Known: emu.KnownBg, Bg: light}, emu.SchemeDark, "light"},
		{emu.Colors{Known: emu.KnownFg, Bg: light}, emu.SchemeDark, "dark"}, // a background not reported
	} {
		s := &Server{colors: c.colors, scheme: c.scheme}
		if got := s.schemeName(); got != c.want {
			t.Errorf("colours %+v scheme %d: %q, want %q", c.colors, c.scheme, got, c.want)
		}
	}
}

// TestThemePickReachesClaude: picking the built-in theme after a theme
// mod without a pane palette changes no colour a console reports, yet
// the running Claude panes follow the terminal from the next look at
// config.toml.
func TestThemePickReachesClaude(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv(envModsPoll, "20ms")
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	writeNordMod(t)
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	// Nord without a pane palette: Claude keeps the user's own theme.
	if err := os.WriteFile(filepath.Join(dir, "nord", "theme.toml"), []byte("[dark]\naccent = { fg = [\"cyan\", \"#88c0d0\"] }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.json"), []byte(`{"theme": "light"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := config.Path()
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	claude, _ := reg.Get("claude")
	p, err := client.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm"}, log: log.New(io.Discard, "", 0), agents: reg,
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	base := func() string {
		t.Helper()
		var ct struct{ Base string }
		b, err := os.ReadFile(filepath.Join(cfgDir, "themes", "terminatr.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &ct); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return ct.Base
	}
	s.mu.Lock()
	ok := s.writeLiveTheme(claude)
	s.mu.Unlock()
	if !ok || base() != "light" {
		t.Fatalf("under nord without a pane palette: %v %q", ok, base())
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
	// Let the loop read nord's config first, so the pick below is a change.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"default\"\n[mods.nord]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for base() != "dark-ansi" {
		if time.Now().After(deadline) {
			t.Fatalf("follow terminal didn't reach the running Claude: base %q", base())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
