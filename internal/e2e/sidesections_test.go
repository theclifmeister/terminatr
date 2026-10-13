package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opensMod is a code mod with a band section (sidebar.section) that
// counts the view.opened it hears per project, and says so.
const opensMod = `id = "opens"
name = "Opens"
version = "1.0.0"
api = "1.7"
[provides]
main = "main.js"
[[provides.views]]
id = "opens"
title = "Opens"
slot = "sidebar.section"
[permissions]
views = true
`

const opensMain = `const n = {};
$.on("view.opened", async (v) => {
  const k = v.view + "/" + (v.project || "");
  n[k] = (n[k] || 0) + 1;
  await $.ui.view.set(v.view, { kind: "text", text: "opened " + n[k] + " for " + v.project }, { project: v.project });
});
`

// TestSmokeSidebarSections: a mod's band section below the sidebar's
// tree (the T348 design §6) on both screens. The dashboard and the
// attached session show it; going from one to the other and back, the
// mod hears view.opened once, as the console never says it closed. A
// fold made in one console shows in the other, on the dashboard and
// attached.
func TestSmokeSidebarSections(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	alpha, alphaDir := newProject(env, "alpha")
	env.Trust(alphaDir)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(opensMod), 0o644)
	os.WriteFile(filepath.Join(dir, "main.js"), []byte(opensMain), 0o644)
	env.MustCLI("mod", "install", dir, "--yes")
	env.MustCLI("mod", "enable", "opens", "--yes")

	once := "opened 1 for " + alpha
	band := func(w *Window, what string) {
		t.Helper()
		w.WaitUntil(what, agentWait, func(sc string) bool { return strings.Contains(sc, "▾ OPENS") && strings.Contains(sc, once) })
	}
	steady := func(w *Window, what string) {
		t.Helper()
		band(w, what)
		time.Sleep(2500 * time.Millisecond) // a mod.views poll or two
		if sc := w.Screen(); !strings.Contains(sc, once) {
			t.Fatalf("%s: the mod heard view.opened again:\n%s", what, sc)
		}
	}

	w1 := env.Window(140, 34)
	steady(w1, "the band on the dashboard")
	clickCoordinator(t, w1, alpha)
	w1.WaitUntil("attached to alpha", agentWait, func(sc string) bool { return lastLine(sc, alpha+" › coordinator") })
	steady(w1, "the band attached")
	w1.Detach()
	w1.WaitFor("SESSIONS", wait)
	steady(w1, "the band on the dashboard again")

	// A second console on the view: a fold in one shows in both.
	w2 := env.Window(140, 34)
	band(w2, "the band in the second console")
	fold := func(from, to *Window, where string) {
		t.Helper()
		from.ClickText("OPENS", 0)
		for _, w := range []*Window{from, to} {
			w.WaitUntil(where+": folded", wait, func(sc string) bool { return strings.Contains(sc, "▸ OPENS") && !strings.Contains(sc, once) })
		}
		from.ClickText("OPENS", 0)
		for _, w := range []*Window{from, to} {
			band(w, where+": unfolded")
		}
	}
	fold(w1, w2, "on the dashboard")
	clickCoordinator(t, w1, alpha)
	for _, w := range []*Window{w1, w2} {
		w.WaitUntil("attached to alpha", agentWait, func(sc string) bool { return lastLine(sc, alpha+" › coordinator") })
	}
	fold(w2, w1, "attached")
	if sc := w1.Screen(); !strings.Contains(sc, once) {
		t.Fatalf("the mod heard view.opened again:\n%s", sc)
	}
	w2.Quit()
	w2.WaitExit(wait)
	w1.Quit()
	w1.WaitExit(wait)
}
