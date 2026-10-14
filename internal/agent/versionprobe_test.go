package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestVersionsCache: a binary's version is run once while its size and
// modification time hold, again once either changes; a failure or an
// answer without a version is not kept.
func TestVersionsCache(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(bin, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{}
	m.Identify.VersionArgs = []string{"--version"}
	runs, out, fail := 0, "2.1.291 (Claude Code)\n", error(nil)
	var vs Versions
	vs.Run = func(ctx context.Context, path string, args, _ []string) ([]byte, error) {
		runs++
		if path != bin || strings.Join(args, " ") != "--version" {
			t.Errorf("ran %s %v", path, args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("no deadline")
		}
		return []byte(out), fail
	}
	ask := func() (string, error) { return vs.Of(context.Background(), bin, m, nil) }
	for range 2 {
		if v, err := ask(); v != "2.1.291" || err != nil || runs != 1 {
			t.Fatalf("got %q, %v after %d runs", v, err, runs)
		}
	}
	later := time.Now().Add(time.Hour)
	os.Chtimes(bin, later, later)
	out = "2.2.0\n"
	if v, _ := ask(); v != "2.2.0" || runs != 2 {
		t.Fatalf("after an upgrade: %q, %d runs", v, runs)
	}
	os.WriteFile(bin, []byte("longer"), 0o700)
	os.Chtimes(bin, later, later)
	fail = errors.New("boom")
	if _, err := ask(); err == nil {
		t.Fatal("a failed run gave a version")
	}
	fail, out = nil, "no number here"
	if _, err := ask(); err == nil || runs != 4 {
		t.Fatalf("no version: %v after %d runs", err, runs)
	}
	m2 := &Manifest{}
	if _, err := vs.Of(context.Background(), bin, m2, nil); err == nil || runs != 4 {
		t.Fatalf("no version_args: %v", err)
	}
	if _, err := vs.Of(context.Background(), bin+"-missing", m, nil); err == nil || runs != 4 {
		t.Fatalf("missing binary: %v", err)
	}
}
