package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeFilesPanel: the info panel's Files view against a real server
// and worktree (docs/SPEC.md §4): F shows the tree with git's marks, a new
// file appears while it shows, / filters, enter reads a file in the
// Library's viewer over the session and esc comes back to the same tree;
// an escape in a file's name or text never reaches the screen.
func TestSmokeFilesPanel(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	env.MustCLI("thread", "start", "Browse", "--project", "demo")
	var rec struct{ Session, Worktree string }
	readTOML(t, filepath.Join(projDir, "threads", "t-0001", "thread.toml"), &rec)
	th := &Session{ID: rec.Session}
	env.WaitState(th, "idle", agentWait)
	put := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rec.Worktree, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("added.txt", "the first line\x1b[31m of text\n")

	w := env.Window(160, 30)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("the sidebar's thread row", wait, func(sc string) bool { y = treeRow(sc, "demo", "t-0001"); return y >= 0 })
	w.Click(8, y)
	w.WaitFor("worktree", wait)

	// A click on the panel gives it the keyboard, then F its Files view.
	w.Click(150, 27)
	w.Type("F")
	w.WaitUntil("the tree", wait, func(sc string) bool {
		return strings.Contains(sc, "FILES") && strings.Contains(sc, "added.txt") && strings.Contains(sc, "1 changed")
	})

	// A file made while it shows comes in by itself.
	put("later.txt", "later\n")
	w.WaitUntil("the new file", wait, func(sc string) bool {
		return strings.Contains(sc, "later.txt") && strings.Contains(sc, "2 changed")
	})

	// A filter by name.
	w.Type("/lat")
	w.WaitUntil("the filter", wait, func(sc string) bool { return strings.Contains(sc, "/ lat") && !strings.Contains(sc, "added.txt") })
	w.Key(keyEsc)
	w.WaitFor("added.txt", wait)

	// Reading it, over the session, and back.
	w.ClickText("added.txt", 0)
	w.ClickText("added.txt", 0)
	w.WaitUntil("the viewer", wait, func(sc string) bool {
		return strings.Contains(sc, "the first line") && strings.Contains(sc, "of text") && strings.Contains(sc, "added.txt ·")
	})
	if strings.Contains(w.Screen(), "\x1b") {
		t.Errorf("an escape on the screen:\n%q", w.Screen())
	}
	w.Key(keyEsc)
	w.WaitUntil("back on the tree", wait, func(sc string) bool {
		return strings.Contains(sc, "FILES") && strings.Contains(sc, "later.txt")
	})
	w.Quit()
	w.WaitExit(wait)
}
