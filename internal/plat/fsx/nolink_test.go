package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// symlink makes link point at target, skipping the test where links
// need a privilege the runner hasn't (Windows).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
}

// TestOpenNoLink: a link as the last element is refused, whether its
// target exists or not, and the target is left as it was; a plain file
// opens.
func TestOpenNoLink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep\n"), 0o600)
	symlink(t, victim, filepath.Join(dir, "log"))
	symlink(t, filepath.Join(dir, "absent"), filepath.Join(dir, "dangling"))
	for _, name := range []string{"log", "dangling"} {
		for _, flag := range []int{os.O_CREATE | os.O_WRONLY | os.O_APPEND, os.O_CREATE | os.O_WRONLY | os.O_TRUNC} {
			f, err := OpenNoLink(filepath.Join(dir, name), flag, 0o644)
			if err == nil {
				f.Close()
				t.Errorf("%s: opened through a link", name)
			} else if !errors.Is(err, ErrLink) {
				t.Errorf("%s: %v, want ErrLink", name, err)
			}
		}
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Errorf("victim %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dangling target created: %v", err)
	}
	f, err := OpenNoLink(filepath.Join(dir, "plain"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestMkdirNoLink: a folder that is a link is refused; a new or plain
// one is made or kept.
func TestMkdirNoLink(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	symlink(t, elsewhere, filepath.Join(dir, "lib"))
	if err := MkdirNoLink(filepath.Join(dir, "lib"), 0o755); !errors.Is(err, ErrLink) {
		t.Errorf("link: %v, want ErrLink", err)
	}
	if err := MkdirNoLink(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Errorf("new: %v", err)
	}
	if err := MkdirNoLink(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Errorf("again: %v", err)
	}
}
