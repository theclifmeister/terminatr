//go:build unix

package flock

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockRefusesLink: a lock file that is a link is refused, so taking
// a lock never creates or opens the file it points at.
func TestLockRefusesLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "x.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if l, err := TryLock(link); err == nil {
		l.Unlock()
		t.Fatal("locked through a link")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("target created: %v", err)
	}
}
