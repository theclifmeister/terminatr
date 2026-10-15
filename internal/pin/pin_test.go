//go:build unix

package pin

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestPinBinary: every build pins to the same path; a new build replaces
// the file by a rename, so the original being replaced, and a process
// holding the old pin open, both keep what they had; per-build pins of
// older servers go.
func TestPinBinary(t *testing.T) {
	run := t.TempDir()
	dir := filepath.Join(run, pinDir)
	os.MkdirAll(dir, 0o700)
	old := filepath.Join(dir, "tm-v0.11.3+abc+111")
	os.WriteFile(old, []byte("T87 pin"), 0o700)
	os.WriteFile(filepath.Join(dir, ".tm-1.tmp"), nil, 0o700)

	bin := filepath.Join(t.TempDir(), "tm")
	os.WriteFile(bin, []byte("build one"), 0o755)
	link := filepath.Join(t.TempDir(), "tm")
	os.Symlink(bin, link)
	p1, err := Binary(run, link)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(dir, pinName) {
		t.Fatalf("pinned at %s", p1)
	}
	if b, _ := os.ReadFile(p1); string(b) != "build one" {
		t.Fatalf("pin reads %q", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("other pins kept: %v", ents)
	}
	// An upgrade renames a new binary over the old one.
	next := bin + ".new"
	os.WriteFile(next, []byte("build two"), 0o755)
	if err := os.Rename(next, bin); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p1); string(b) != "build one" {
		t.Fatalf("pinned copy reads %q after the upgrade", b)
	}
	// The same build again leaves the file alone.
	same := filepath.Join(t.TempDir(), "tm")
	os.WriteFile(same, []byte("build one"), 0o755)
	before, _ := os.Stat(p1)
	if p, err := Binary(run, same); err != nil || p != p1 {
		t.Fatalf("repin: %s %v", p, err)
	}
	if after, _ := os.Stat(p1); !os.SameFile(before, after) {
		t.Fatal("same build: the pin was replaced")
	}
	// A new build swaps it at the same path; an open old pin still reads
	// the old build.
	f, err := os.Open(p1)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p2, err := Binary(run, bin)
	if err != nil || p2 != p1 {
		t.Fatalf("second pin: %s %v", p2, err)
	}
	if b, _ := os.ReadFile(p2); string(b) != "build two" {
		t.Fatalf("second pin reads %q", b)
	}
	if b, _ := io.ReadAll(f); string(b) != "build one" {
		t.Fatalf("the running old pin reads %q", b)
	}
	if fi, _ := os.Stat(p2); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("pin not executable: %v", fi.Mode())
	}
	if _, err := Binary(run, filepath.Join(run, "missing")); err == nil {
		t.Fatal("pinning a missing binary: no error")
	}
}

// TestPinFor: launchd runs the pin and the launch file names the tm that
// started it (T129). The server execs only when it runs another file or
// the pin now holds another build, and a gone source leaves the pin.
func TestPinFor(t *testing.T) {
	run := t.TempDir()
	pin := filepath.Join(run, pinDir, pinName)
	keg := func(v, content string) string {
		p := filepath.Join(t.TempDir(), v, "tm")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o755)
		return p
	}
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, f) }
	check := func(name, bin, source, content string, wantExec bool) {
		t.Helper()
		got, again, err := For(run, bin, source, logf)
		if err != nil || got != pin || again != wantExec {
			t.Fatalf("%s: %s exec=%v %v", name, got, again, err)
		}
		if b, _ := os.ReadFile(pin); string(b) != content {
			t.Fatalf("%s: pin reads %q", name, b)
		}
	}
	one := keg("1.0", "build one")
	// An older tm's start: launchd ran the versioned binary.
	check("from the keg", one, "", "build one", true)
	// launchd runs the pin, which holds the build that started it.
	check("from the pin", pin, one, "build one", false)
	// An upgrade: the launch file names the new build.
	two := keg("2.0", "build two")
	check("upgraded", pin, two, "build two", true)
	// Homebrew removed the keg the launch file names: keep the pin.
	os.Remove(two)
	check("keg gone", pin, two, "build two", false)
	if len(logged) != 1 {
		t.Fatalf("logged %q", logged)
	}
	// Through a symlink, as Homebrew's bin/tm.
	link := filepath.Join(t.TempDir(), "tm")
	os.Symlink(one, link)
	check("through a link", pin, link, "build one", true)
}
