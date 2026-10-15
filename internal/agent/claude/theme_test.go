package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/theme"
)

// claudeTheme is a Claude config-folder theme as tm writes it.
type claudeTheme struct {
	Name      string            `json:"name"`
	Base      string            `json:"base"`
	Overrides map[string]string `json:"overrides"`
}

// TestWriteTheme: tm's theme lands in Claude's config folder as
// themes/terminatr.json, the tm theme's colours under a pane palette,
// else the user's own Claude theme; a write that changes nothing leaves
// the file alone, and nothing else in the folder is written (T345).
func TestWriteTheme(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	lt, ok := load(t).(agent.LiveThemer)
	if !ok {
		t.Fatal("the claude agent is no LiveThemer")
	}
	path := filepath.Join(cfg, "themes", "terminatr.json")
	read := func() claudeTheme {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var ct claudeTheme
		if err := json.Unmarshal(b, &ct); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return ct
	}
	write := func(t0 *theme.Agent, scheme string) claudeTheme {
		t.Helper()
		if err := lt.WriteTheme("/no/home", t0, scheme, false); err != nil {
			t.Fatal(err)
		}
		return read()
	}
	settings := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// No settings: Claude's default, dark, whatever the terminal.
	if ct := write(nil, "light"); ct.Name != "terminatr" || ct.Base != "dark" || len(ct.Overrides) != 0 {
		t.Fatalf("no settings: %+v", ct)
	}
	settings(`{"theme": "light-daltonized", "model": "x"}`)
	if ct := write(nil, "dark"); ct.Base != "light-daltonized" || len(ct.Overrides) != 0 {
		t.Fatalf("built-in theme: %+v", ct)
	}
	// auto, a plugin's theme and our own follow the terminal's scheme.
	for _, name := range []string{"auto", "custom:p:x", "custom:terminatr", "custom:gone"} {
		settings(`{"theme": "` + name + `"}`)
		if ct := write(nil, "light"); ct.Base != "light" || len(ct.Overrides) != 0 {
			t.Fatalf("%s, light terminal: %+v", name, ct)
		}
		if ct := write(nil, ""); ct.Base != "dark" {
			t.Fatalf("%s, unknown scheme: %+v", name, ct)
		}
	}
	// One of the user's own: its base and overrides.
	if err := os.WriteFile(filepath.Join(cfg, "themes", "mine.json"),
		[]byte(`{"name": "Mine", "base": "light-ansi", "overrides": {"claude": "#010203", "text": "ansi:red"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings(`{"theme": "custom:mine"}`)
	if ct := write(nil, "dark"); ct.Name != "terminatr" || ct.Base != "light-ansi" || ct.Overrides["claude"] != "#010203" || ct.Overrides["text"] != "ansi:red" {
		t.Fatalf("user's custom theme: %+v", ct)
	}

	// A pane palette: the tm theme over Claude's ANSI theme.
	th := &theme.Agent{Scheme: "dark", PaneFg: "#d8dee9", PaneBg: "#2e3440",
		Fg: map[string]string{"head": "", "sel": "", "sel_focus": "", "faint": "#4c566a", "accent": "#88c0d0", "title": "",
			"bad": "#bf616a", "warn": "#ebcb8b", "good": "#a3be8c", "info": "#81a1c1", "plain": ""},
		Bg: map[string]string{"sel": "#434c5e"}}
	if ct := write(th, "dark"); ct.Base != "dark-ansi" || ct.Overrides["claude"] != "#88c0d0" || ct.Overrides["text"] != "#d8dee9" {
		t.Fatalf("pane palette: %+v", ct)
	}

	// The same theme again: the file isn't rewritten, so no Claude redraws.
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	write(th, "dark")
	if fi, err := os.Stat(path); err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("an unchanged theme was rewritten: %v %v", fi.ModTime(), err)
	}
	// The palette goes: back to the user's own.
	if ct := write(nil, "dark"); ct.Base != "light-ansi" || ct.Overrides["claude"] != "#010203" {
		t.Fatalf("palette gone: %+v", ct)
	}
	// The built-in tm theme follows the terminal: Claude's ANSI theme
	// for the scheme, whatever the user's own, dark for an unknown one.
	for scheme, want := range map[string]string{"dark": "dark-ansi", "light": "light-ansi", "": "dark-ansi"} {
		if err := lt.WriteTheme("/no/home", nil, scheme, true); err != nil {
			t.Fatal(err)
		}
		if ct := read(); ct.Base != want || len(ct.Overrides) != 0 {
			t.Fatalf("follow terminal, scheme %q: %+v", scheme, ct)
		}
	}
	// A pane palette wins over it.
	if err := lt.WriteTheme("/no/home", th, "light", true); err != nil {
		t.Fatal(err)
	}
	if ct := read(); ct.Base != "dark-ansi" || ct.Overrides["claude"] != "#88c0d0" {
		t.Fatalf("pane palette under follow terminal: %+v", ct)
	}

	entries, _ := os.ReadDir(filepath.Join(cfg, "themes"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != "mine.json" || names[1] != "terminatr.json" {
		t.Fatalf("themes folder holds %v", names)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "settings.json")); string(b) != `{"theme": "custom:mine"}` {
		t.Fatalf("settings changed: %s", b)
	}
}

// TestWriteThemeHome: without CLAUDE_CONFIG_DIR the theme goes to
// ~/.claude/themes.
func TestWriteThemeHome(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	if err := load(t).(agent.LiveThemer).WriteTheme(home, nil, "dark", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "themes", "terminatr.json")); err != nil {
		t.Fatal(err)
	}
}

// TestLiveThemeRender runs a real Claude on tm's theme in a scratch
// config folder and rewrites the theme under it: Claude redraws with the
// new colours, a new base included, without a restart. It needs the
// Claude Code CLI (no model call is made), so it runs only with
// TERMINATR_CLAUDE_THEME_CHECK=1, outside a sandbox that denies ptys.
func TestLiveThemeRender(t *testing.T) {
	if os.Getenv("TERMINATR_CLAUDE_THEME_CHECK") != "1" {
		t.Skip("set TERMINATR_CLAUDE_THEME_CHECK=1 to run a real Claude on a live theme")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	cfg, work := t.TempDir(), t.TempDir()
	work, _ = filepath.EvalSymlinks(work)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	// Past onboarding and the folder-trust screen.
	state, _ := json.Marshal(map[string]any{"hasCompletedOnboarding": true, "numStartups": 5,
		"projects": map[string]any{work: map[string]any{"hasTrustDialogAccepted": true}}})
	if err := os.WriteFile(filepath.Join(cfg, ".claude.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	lt := load(t).(agent.LiveThemer)
	accent := func(scheme, hex string) *theme.Agent {
		return &theme.Agent{Scheme: scheme, PaneFg: "#d8dee9", PaneBg: "#2e3440",
			Fg: map[string]string{"head": "", "sel": "", "sel_focus": "", "faint": hex, "accent": hex, "title": "",
				"bad": "", "warn": "", "good": "", "info": "", "plain": ""},
			Bg: map[string]string{"sel": ""}}
	}
	if err := lt.WriteTheme("", accent("dark", "#010203"), "dark", false); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(claude, "--settings", `{"theme": "custom:terminatr"}`)
	cmd.Dir = work
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CODE_") && !strings.HasPrefix(kv, "CLAUDECODE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait(); f.Close() }()
	var mu sync.Mutex
	var out []byte
	go func() {
		b := make([]byte, 64<<10)
		for {
			n, err := f.Read(b)
			mu.Lock()
			out = append(out, b[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	start := time.Now()
	// waitFor waits for Claude to draw in the colour rgb ("1;2;3") after
	// byte from of its output, and returns where its output is then.
	waitFor := func(rgb string, from int) int {
		t.Helper()
		for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			mu.Lock()
			n, hit := len(out), strings.Contains(string(out[from:]), "38;2;"+rgb+"m")
			mu.Unlock()
			if hit {
				t.Logf("drawn in %s, %v in", rgb, time.Since(start).Round(time.Millisecond))
				return n
			}
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("Claude never drew in %s; it drew:\n%q", rgb, out[from:])
		return 0
	}
	at := waitFor("1;2;3", 0)
	if err := lt.WriteTheme("", accent("dark", "#040506"), "dark", false); err != nil {
		t.Fatal(err)
	}
	at = waitFor("4;5;6", at)
	if err := lt.WriteTheme("", accent("light", "#070809"), "light", false); err != nil {
		t.Fatal(err)
	}
	waitFor("7;8;9", at)
}
