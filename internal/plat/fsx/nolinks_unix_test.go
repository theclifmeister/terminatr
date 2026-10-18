//go:build unix

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenNoLinks: a file opens when no component under the root is a
// link, whatever the root is; a link anywhere under it is refused.
func TestOpenNoLinks(t *testing.T) {
	real := t.TempDir()
	root := filepath.Join(t.TempDir(), "rootln")
	if err := os.Symlink(real, root); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(real, "d"), 0o755)
	os.WriteFile(filepath.Join(real, "d", "f"), []byte("x"), 0o644)
	os.Symlink("f", filepath.Join(real, "d", "fl"))
	os.Symlink("d", filepath.Join(real, "dl"))
	f, err := OpenNoLinks(root, filepath.Join("d", "f"))
	if err != nil {
		t.Fatalf("plain file under a root that is a link: %v", err)
	}
	f.Close()
	for _, rel := range []string{filepath.Join("d", "fl"), filepath.Join("dl", "f")} {
		if f, err := OpenNoLinks(root, rel); !errors.Is(err, ErrLink) {
			if f != nil {
				f.Close()
			}
			t.Errorf("OpenNoLinks(%s) = %v, want ErrLink", rel, err)
		}
	}
}
