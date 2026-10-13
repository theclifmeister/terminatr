package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opensMod is a code mod with two band sections (sidebar.section): one
// counts the view.opened it hears per project, and says so; the other
// opens its full view.
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
[[provides.views]]
id = "launch"
title = "Launch"
slot = "sidebar.section"
opens = "board"
[[provides.views]]
id = "board"
title = "Board"
slot = "full"
[permissions]
views = true
`

const opensMain = `const n = {};
$.on("view.opened", async (v) => {
  const k = v.view + "/" + (v.project || "");
  n[k] = (n[k] || 0) + 1;
  await $.ui.view.set(v.view, { kind: "text", text: v.view + " opened " + n[k] + " for " + v.project }, { project: v.project });
});
$.on("ui.click", async (e) => {
  if (e.view === "launch") await $.ui.view.open("board");
});
`

// underMod is a code mod with a per-project section
// (sidebar.project.section) that counts the view.opened it hears per
// project, and says so.
const underMod = `id = "under"
name = "Under"
version = "1.0.0"
api = "1.7"
[provides]
main = "main.js"
[[provides.views]]
id = "under"
title = "Under"
slot = "sidebar.project.section"
[permissions]
views = true
`

const underMain = `const n = {};
$.on("view.opened", async (v) => {
  n[v.project] = (n[v.project] || 0) + 1;
  await $.ui.view.set(v.view, { kind: "text", text: "under opened " + n[v.project] + " for " + v.project }, { project: v.project });
});
`

// TestSmokeSidebarSections: a mod's band section below the sidebar's
// tree, and another's section under each project (the T348 design §6),
// on both screens. The dashboard and the attached session show them;
// going from one to the other and back, the mods hear view.opened once,
// per project for the one under each, as the console never says it
// closed. A fold made in one console shows in the other, on the
// dashboard and attached; attached, a header's companion view opens over
// the session.
func TestSmokeSidebarSections(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	alpha, alphaDir := newProject(env, "alpha")
	env.Trust(alphaDir)
	beta, _ := newProject(env, "beta")
	for name, files := range map[string][2]string{"opens": {opensMod, opensMain}, "under": {underMod, underMain}} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(files[0]), 0o644)
		os.WriteFile(filepath.Join(dir, "main.js"), []byte(files[1]), 0o644)
		env.MustCLI("mod", "install", dir, "--yes")
		env.MustCLI("mod", "enable", name, "--yes")
	}

	once := "opens opened 1 for " + alpha
	under := []string{"under opened 1 for " + alpha, "under opened 1 for " + beta}
	band := func(w *Window, what string) {
		t.Helper()
		w.WaitUntil(what, agentWait, func(sc string) bool {
			return strings.Contains(sc, "▾ OPENS") && strings.Contains(sc, once) &&
				strings.Contains(sc, under[0]) && strings.Contains(sc, under[1])
		})
	}
	steady := func(w *Window, what string) {
		t.Helper()
		band(w, what)
		time.Sleep(2500 * time.Millisecond) // a mod.views poll or two
		if sc := w.Screen(); !strings.Contains(sc, once) || !strings.Contains(sc, under[0]) || !strings.Contains(sc, under[1]) {
			t.Fatalf("%s: a mod heard view.opened again:\n%s", what, sc)
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

	// enter (a click) on a header that opens a view shows it over the
	// session; esc goes back.
	w1.ClickText("LAUNCH", 0)
	w1.WaitFor("board opened 1 for "+alpha, agentWait)
	w1.Key(keyEsc)
	w1.WaitUntil("back on the coordinator", wait, func(sc string) bool { return lastLine(sc, alpha+" › coordinator") })
	w1.Quit()
	w1.WaitExit(wait)
}
