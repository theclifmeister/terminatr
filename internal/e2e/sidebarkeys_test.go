package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

var (
	keyTab   = emu.Key{Special: emu.KeyTab}
	keyRight = emu.Key{Special: emu.KeyRight}
	keyLeft  = emu.Key{Special: emu.KeyLeft}
	keyEnter = emu.Key{Special: emu.KeyEnter}
)

// TestSmokeSidebarKeys: the sidebar from the keyboard (docs/SPEC.md §4).
// On the dashboard tab gives it the keyboard; ↓ moves, and the row the
// cursor rests on shows: a project its dashboard, a thread its session,
// the keyboard staying in the sidebar; ← goes up to the project; enter
// goes into the session. In the session prefix+tab gives the sidebar the
// keyboard, esc gives it back, enter on a coordinator starts and
// attaches it. Meanwhile no key reaches the pane.
func TestSmokeSidebarKeys(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	demo := "demo"
	beta, betaDir := newProject(env, "beta")
	env.Trust(betaDir)
	startThread(t, env, projDir)

	w := env.Window(120, 30)
	// beta is current; every project is expanded. The cursor starts on
	// beta.
	w.WaitFor(" ■ "+beta, wait)
	w.WaitUntil("demo's thread", wait, func(sc string) bool { return treeRow(sc, demo, "t-0001 ") >= 0 })
	w.Key(keyTab)
	w.WaitFor("esc back to the list", wait)

	// ↓ ↓ past beta's coordinator to demo: its dashboard shows.
	w.Keys(keyDown, keyDown)
	w.WaitUntil("demo's dashboard", wait, func(sc string) bool {
		return strings.Contains(sc, " "+demo+" ─") && strings.Contains(sc, "esc back to the list")
	})
	// ↓ to its coordinator, ← back up to demo, ↓ ↓ to its thread: its
	// session shows, the keyboard still in the sidebar; enter goes in.
	for _, k := range []emu.Key{keyDown, keyLeft, keyDown, keyDown} {
		w.Key(k)
	}
	w.WaitUntil("t-0001 previewed", wait, func(sc string) bool {
		return lastLine(sc, demo+" › t-0001") && lastLine(sc, "preview · ⏎ to type here")
	})
	w.Key(keyEnter)
	w.WaitUntil("in t-0001", wait, func(sc string) bool { return lastLine(sc, demo+" › t-0001") && !lastLine(sc, "preview") })

	// prefix+tab: the sidebar has the keyboard; esc gives it back.
	w.Prefix("\t")
	w.WaitUntil("sidebar focused", wait, func(sc string) bool { return lastLine(sc, "preview") })
	w.Key(keyEsc)
	w.WaitUntil("pane focused", wait, func(sc string) bool { return lastLine(sc, demo+" › t-0001") && !lastLine(sc, "preview") })

	// prefix+tab, ↑ to demo's coordinator, which doesn't run: its
	// project's dashboard shows; enter starts the coordinator and
	// attaches it.
	w.Prefix("\t")
	w.WaitUntil("sidebar focused", wait, func(sc string) bool { return lastLine(sc, "preview") })
	w.Key(keyUp)
	w.WaitUntil("demo's dashboard", wait, func(sc string) bool {
		return strings.Contains(sc, " "+demo+" ─") && strings.Contains(sc, "esc back to the list")
	})
	if got := projectSessions(env, demo); len(got) != 1 {
		t.Fatalf("previewing demo's coordinator started %v", got)
	}
	w.Key(keyEnter)
	w.WaitUntil("on demo's coordinator", agentWait, func(sc string) bool { return lastLine(sc, demo+" › coordinator") })
	coordinatorOf(t, env, demo)

	// A shell: what is typed while the sidebar has the keyboard never
	// reaches it.
	w.Detach()
	w.WaitFor("SESSIONS", wait)
	id := env.StartShell(rootDir)
	w.OpenSession(id)
	w.WaitUntil("attached", wait, func(sc string) bool { return lastLine(sc, "prefix+d dashboard") && strings.Contains(sc, "$") })
	w.Prefix("\t")
	w.WaitUntil("sidebar focused", wait, func(sc string) bool { return lastLine(sc, "preview") })
	w.Type("echoLEAK") // no S, P, W or Z: those act on a sidebar row
	w.Paste("echo PASTED")
	w.Key(keyEsc)
	w.WaitUntil("pane focused", wait, func(sc string) bool { return !lastLine(sc, "preview") })
	w.Type("echo done-$((6*7))\r")
	w.WaitFor("done-42", wait)
	if sc := w.PaneScreen(); strings.Contains(sc, "LEAK") || strings.Contains(sc, "PASTED") || strings.Contains(sc, "kecho") {
		t.Fatalf("keys leaked into the pane:\n%s", sc)
	}
	w.Detach()
	w.WaitFor("SESSIONS", wait)
	w.Quit()
	w.WaitExit(wait)
}

// sideThread is demo's thread row in the sidebar: its id, then its
// state glyph and a blank column before the border.
var sideThread = regexp.MustCompile(`t-0001 [^│]*│`)

// TestSmokeSidebarIcons: the sidebar's tree in the unicode, ascii and nerd
// icon sets (docs/SPEC.md §4), as the settings file picks them; every
// project expanded, its rows on tree connectors, columns aligned.
func TestSmokeSidebarIcons(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	beta, betaDir := newProject(env, "beta")
	env.Trust(betaDir)
	th := startThread(t, env, projDir)
	for _, set := range []string{"unicode", "ascii", "nerd"} {
		t.Run(set, func(t *testing.T) {
			os.WriteFile(filepath.Join(env.Home, "config.toml"), []byte("[ui]\nicons = \""+set+"\"\n"), 0o600)
			w := env.Window(100, 12)
			w.WaitFor(beta, wait)
			w.WaitUntil("demo's thread", wait, func(sc string) bool {
				return sideThread.MatchString(sc) && strings.Contains(sc, "PROJECTS")
			})
			env.WaitState(th, "idle", agentWait) // its glyph: idle, not caught working
			side := SideCols(100)
			WaitGolden(t, DefaultTimeout, func() string {
				lines := strings.Split(w.Screen(), "\n")
				for i, l := range lines {
					r := []rune(l)
					lines[i] = string(r[:min(side, len(r))])
				}
				return strings.Join(lines, "\n")
			}, "sidebar-"+set+".txt")
			w.Quit()
			w.WaitExit(wait)
		})
	}
}

// TestSmokeSidebarClickFocus: a click gives its area the keyboard
// (docs/SPEC.md §4). A click on a project row in the dashboard's sidebar
// puts its cursor there, so ↓ moves it at once; a click on a sidebar row
// that attaches a session keeps the keyboard in the sidebar, where keys
// on a thread's row don't reach a pane (↓ previews the next row, enter
// goes into it) and keys on a running coordinator's row type through to
// it; a click on the pane gives the program the keys.
func TestSmokeSidebarClickFocus(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	demo := "demo"
	beta, betaDir := newProject(env, "beta")
	env.Trust(betaDir)
	startThread(t, env, projDir)

	w := env.Window(120, 30)
	w.WaitFor(" ■ "+beta, wait)
	w.WaitUntil("demo's thread", wait, func(sc string) bool { return treeRow(sc, demo, "t-0001 ") >= 0 })

	// The dashboard: a click on demo, then ↓ ↓ to its thread, enter.
	w.Click(5, projectRow(w.Screen(), demo))
	w.WaitFor("esc back to the list", wait)
	w.Keys(keyDown, keyDown, keyEnter)
	w.WaitUntil("on t-0001", wait, func(sc string) bool { return lastLine(sc, demo+" › t-0001") })

	// In the session: a click on demo's coordinator attaches it, and the
	// sidebar keeps the keyboard. ↓ moves to the thread, where typing
	// doesn't reach any pane, and enter attaches it.
	coordinator := func() {
		w.Click(5, treeRow(w.Screen(), demo, "coordinator"))
		w.WaitUntil("on demo's coordinator, the sidebar focused", agentWait, func(sc string) bool {
			return lastLine(sc, demo+" › coordinator") && lastLine(sc, "preview")
		})
	}
	coordinator()
	coordinatorOf(t, env, demo)
	w.Key(keyDown)
	w.WaitUntil("the cursor on the thread", wait, func(sc string) bool { return lastLine(sc, demo+" › t-0001") })
	w.Type("xqxq")
	w.Quiet(100 * time.Millisecond)
	w.Key(keyEnter)
	w.WaitUntil("on t-0001 again", wait, func(sc string) bool { return lastLine(sc, demo+" › t-0001") && !lastLine(sc, "preview") })
	if sc := w.PaneScreen(); strings.Contains(sc, "xqxq") {
		t.Fatalf("keys typed on the thread's sidebar row reached its pane:\n%s", sc)
	}

	// Back on the coordinator by a click, the sidebar holding the
	// keyboard: typing on its row writes to it (type-through), the
	// keyboard following into its pane.
	coordinator()
	w.Type("Hi side")
	w.WaitFor("❯ Hi side", agentWait)
	w.WaitUntil("pane focused", wait, func(sc string) bool { return !lastLine(sc, "preview") })
	if sc := w.PaneScreen(); strings.Contains(sc, "xqxq") {
		t.Fatalf("keys typed on the thread's row reached the coordinator:\n%s", sc)
	}
	w.Type("-and-more")
	w.WaitFor("❯ Hi side-and-more", wait)

	// The keyboard back to the sidebar (prefix+tab); a click on the pane
	// gives it the keys again.
	w.Prefix("\t")
	w.WaitFor("type to write to it", wait)
	w.Click(80, 10)
	w.WaitFor("prefix+tab sidebar", wait)
	w.Type("hello-click")
	w.WaitFor("hello-click", wait)
	w.Quit()
	w.WaitExit(wait)
}
