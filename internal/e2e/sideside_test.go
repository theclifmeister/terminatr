package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/view"
)

// TestSmokeSidebarSide: a layout mod puts the sidebar on the right at a
// share of the window (docs/SPEC.md §4). The dashboard and an attached
// pane start at the window's left, the sidebar's border is its first
// column, and the pane gets the window less the sidebar; prefix+b shows
// the slim strip, again hides the sidebar (the pane takes the window),
// and again brings the full one back.
func TestSmokeSidebarSide(t *testing.T) {
	env := New(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mod.toml"), []byte("id = \"right\"\nname = \"Right\"\nversion = \"1.0.0\"\napi = \"1.5\"\n[provides]\nlayout = \"layout.toml\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "layout.toml"), []byte("[sidebar]\nside = \"right\"\nwidth = \"25%\"\n"), 0o644)
	env.MustCLI("mod", "install", dir, "--yes")
	env.MustCLI("mod", "enable", "right", "--yes")
	cfg := filepath.Join(env.Home, "config.toml")
	old, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append([]byte("[ui]\nlayout = \"right\"\n\n"), old...), 0o600); err != nil {
		t.Fatal(err)
	}

	const cols = 160
	side := cols / 4 // 25%
	s1 := env.StartSize(cols-side, 28, "shell")
	w := env.Window(cols, 30)
	w.WaitUntil("the sidebar on the right", wait, func(sc string) bool {
		first := []rune(strings.Split(sc, "\n")[0])
		return strings.HasPrefix(string(first), " tm dashboard") && len(first) > cols-side && strings.HasPrefix(string(first[cols-side:]), "│ PROJECTS")
	})
	w.WaitFor(s1.ID+" ", wait)
	w.Key(Enter)
	w.WaitUntil("attached", wait, func(sc string) bool { return lastLine(sc, `prefix+d dashboard`) })
	w.Type("echo pane-$((2*3))\r")
	w.WaitUntil("the pane at the left", wait, func(sc string) bool {
		for _, l := range strings.Split(sc, "\n") {
			if strings.HasPrefix(l, "pane-6") {
				return true
			}
		}
		return false
	})
	waitPaneSize(t, env, s1, uint16(cols-side), 28)
	if r := []rune(strings.Split(w.Screen(), "\n")[0]); len(r) <= cols-side || r[cols-side] != '│' {
		t.Fatalf("no sidebar border at column %d:\n%s", cols-side, w.Screen())
	}

	// The slim strip; hidden: the pane has the window; full again.
	w.Prefix("b")
	waitPaneSize(t, env, s1, uint16(cols-view.SideSlim), 28)
	w.Prefix("b")
	waitPaneSize(t, env, s1, cols, 28)
	if !Poll(wait, func() bool { return strings.Contains(readFile(env.Home, "ui.json"), `"mode": "hidden"`) }) {
		t.Fatalf("ui.json after b b: %s", readFile(env.Home, "ui.json"))
	}
	w.Prefix("b")
	waitPaneSize(t, env, s1, uint16(cols-side), 28)
	if !Poll(wait, func() bool {
		u := readFile(env.Home, "ui.json")
		return strings.Contains(u, `"slim": false`) && !strings.Contains(u, "hidden")
	}) {
		t.Fatalf("ui.json after b b b: %s", readFile(env.Home, "ui.json"))
	}
	w.Quit()
	w.WaitExit(wait)
}
