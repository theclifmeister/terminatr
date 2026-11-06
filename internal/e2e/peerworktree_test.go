package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPeerWorktreeFiles (docs/SPEC.md §16.18): two real servers through a
// relay, and studio's console on mini's thread. The files switch off, F says
// so. On, the Files view reads mini's worktree: read only, a secret's name
// withheld, a git-ignored file not there, text read in the viewer. Off mid-view,
// all of it goes at the next look. The link down, what shows stays, marked.
func TestPeerWorktreeFiles(t *testing.T) {
	mini, projDir, _ := threadEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\n")

	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "panes", "studio", "view")
	mini.MustCLI("thread", "start", "Browse", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	var rec struct{ Worktree string }
	readTOML(t, filepath.Join(projDir, "threads", "t-0001", "thread.toml"), &rec)
	for name, text := range map[string]string{".env": "API_KEY=hunter2\n", ".gitignore": "build.log\n", "build.log": "ignored line\n", "added.txt": "the first line\n"} {
		if err := os.WriteFile(filepath.Join(rec.Worktree, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const cols, rows = 150, 34
	w := studio.Window(cols, rows)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("mini's thread row", agentWait, func(sc string) bool { y = treeRow(sc, "demo", "t-0001"); return y >= 0 })
	w.Click(4, y)
	w.WaitUntil("mini's thread pane", agentWait, func(sc string) bool { return strings.Contains(sc, "Fake Claude Code") })
	w.WaitFor("synced", agentWait)
	w.Click(cols-10, rows-4) // the keyboard to the info panel

	// Off, the default.
	w.Type("F")
	w.WaitFor("mini doesn't share demo's files", wait)

	// On: the mirror says so within its read, and F opens it.
	if out := mini.MustCLI("peer", "files", "studio", "demo", "on"); !strings.Contains(out, "Files on for studio on demo") {
		t.Fatalf("on: %q", out)
	}
	opened := Poll(40*time.Second, func() bool {
		w.Type("F")
		time.Sleep(500 * time.Millisecond)
		return strings.Contains(w.Screen(), "on mini · read only")
	})
	if !opened {
		t.Fatalf("no Files view:\n%s", w.Screen())
	}
	screen := w.WaitUntil("the tree", agentWait, func(sc string) bool {
		return strings.Contains(sc, "added.txt") && strings.Contains(sc, "stays on mini")
	})
	if strings.Contains(screen, "build.log") {
		t.Fatalf("a git-ignored file crossed:\n%s", screen)
	}
	if !strings.Contains(screen, ".env") {
		t.Fatalf("no withheld row:\n%s", screen)
	}

	// The text reads; the secret doesn't.
	w.ClickText("added.txt", 0)
	w.ClickText("added.txt", 0)
	w.WaitFor("the first line", wait)
	w.Key(keyEsc)
	w.WaitFor("on mini · read only", wait)
	w.ClickText(".env", 0)
	w.ClickText(".env", 0)
	screen = w.WaitFor("may hold secrets", wait)
	if strings.Contains(screen, "hunter2") {
		t.Fatalf("a withheld file's text crossed:\n%s", screen)
	}
	if out := mini.MustCLI("peer", "list"); !strings.Contains(out, "files: demo") {
		t.Fatalf("peer list: %q", out)
	}

	// Off mid-view: it goes at the next look, and says so.
	mini.MustCLI("peer", "files", "studio", "demo", "off")
	screen = w.WaitFor("stopped sharing", 15*time.Second)
	if strings.Contains(screen, "added.txt") {
		t.Fatalf("what showed stayed:\n%s", screen)
	}
	// On again: it comes back by itself.
	mini.MustCLI("peer", "files", "studio", "demo", "on")
	w.WaitFor("added.txt", 15*time.Second)

	// The link down: what shows stays, marked.
	rs.Shutdown()
	screen = w.WaitFor("mini is offline: as of", 40*time.Second)
	if !strings.Contains(screen, "added.txt") {
		t.Fatalf("what showed left with the link:\n%s", screen)
	}

	journal, err := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	if err != nil || !strings.Contains(string(journal), "peer.files") {
		t.Fatalf("journal (%v):\n%s", err, journal)
	}
}
