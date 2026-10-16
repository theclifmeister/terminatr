package logrot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	l, err := Open(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 59) + "\n")
	for i := 0; i < 10; i++ {
		l.Write(line)
	}
	l.Close()
	for _, f := range []string{path, path + ".1", path + ".2", path + ".3"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s missing", f)
		}
	}
	if _, err := os.Stat(path + ".4"); err == nil {
		t.Error("kept more than 3 old logs")
	}
}

func TestRotationCountsOtherWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.log")
	l, err := Open(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Write([]byte("first\n"))
	other, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	other.Write([]byte(strings.Repeat("y", 99) + "\n"))
	other.Close()
	l.Write([]byte("second\n"))
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "second\n" {
		t.Fatalf("log = %q, %v; want it rotated before the second line", b, err)
	}
}

func TestRotateFailureDropsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.log")
	l, err := Open(path, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	var failed error
	l.Failed = func(err error) { failed = err }
	l.Write([]byte("0123456789"))
	// Make the reopen fail: the log is gone and its folder read-only.
	os.Remove(path)
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	if n, err := l.Write([]byte("abcdef")); n != 6 || err != nil {
		t.Fatalf("Write after a failed rotation = %d, %v", n, err)
	}
	if failed == nil {
		t.Error("Failed wasn't told")
	}
	if l.size > l.max {
		t.Fatalf("size %d stays over max %d: every write would rotate", l.size, l.max)
	}
}
