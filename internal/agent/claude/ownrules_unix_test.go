//go:build unix

package claude

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestOwnRules: the user's settings (through a link to their dotfiles
// too) and the folder's are read; in the folder, a FIFO doesn't block
// the read and a link out of it is refused, while a link inside it is
// followed.
func TestOwnRules(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, dir, outside := t.TempDir(), t.TempDir(), t.TempDir()
	dotfiles := filepath.Join(t.TempDir(), "claude-settings.json")
	os.WriteFile(dotfiles, []byte(`{"permissions":{"allow":["Bash(make:*)"]}}`), 0o600)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if err := os.Symlink(dotfiles, filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	secret := filepath.Join(outside, "other.json")
	os.WriteFile(secret, []byte(`{"permissions":{"allow":["Bash(leaked)"]}}`), 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, ".claude", "settings.local.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Agent{}
	done := make(chan []agent.OwnRule, 1)
	go func() { done <- a.OwnRules(home, dir) }()
	var got []agent.OwnRule
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO blocked the read")
	}
	want := []agent.OwnRule{{Kind: "allow", Value: "Bash(make:*)", File: filepath.Join("~", ".claude", "settings.json")}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// A relative link that stays in the folder is followed (os.Root
	// refuses an absolute one).
	os.Remove(filepath.Join(dir, ".claude", "settings.json"))
	os.WriteFile(filepath.Join(dir, "mine.json"), []byte(`{"sandbox":{"excludedCommands":["make bench"]}}`), 0o600)
	os.Symlink(filepath.Join("..", "mine.json"), filepath.Join(dir, ".claude", "settings.json"))
	got = a.OwnRules(home, dir)
	if len(got) != 2 || got[1] != (agent.OwnRule{Kind: "excluded", Value: "make bench", File: filepath.Join(".claude", "settings.json")}) {
		t.Errorf("link inside: %+v", got)
	}
}
