package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateFailureResetsSize(t *testing.T) {
	dir := t.TempDir()
	r, err := openRotating(filepath.Join(dir, "s.log"))
	if err != nil {
		t.Fatal(err)
	}
	r.max = 10
	r.Write([]byte("0123456789"))
	// Make the reopen fail: the log is gone and its folder read-only.
	os.Remove(r.path)
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	r.Write([]byte("abcdef"))
	if r.size > r.max {
		t.Fatalf("size %d stays over max %d: every write would rotate", r.size, r.max)
	}
}
