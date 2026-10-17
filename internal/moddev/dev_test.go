package moddev

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/testwait"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestTailAndFollow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.log")
	if _, _, err := Tail(p, 5); !os.IsNotExist(err) {
		t.Fatalf("no log: %v", err)
	}
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, off, err := Tail(p, 2)
	if err != nil || !slices.Equal(lines, []string{"b", "c"}) || off != 6 {
		t.Fatalf("Tail = %q %d %v", lines, off, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuf
	go Follow(ctx, p, off, 5*time.Millisecond, &out)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("d\ne")
	testwait.Eventually(t, "d", func() bool { return out.String() == "d\n" })
	f.WriteString("\n")
	f.Close()
	testwait.Eventually(t, "e", func() bool { return out.String() == "d\ne\n" })
	// Rotated: a new, shorter file is read from its start.
	os.Rename(p, p+".1")
	os.WriteFile(p, []byte("f\n"), 0o600)
	testwait.Eventually(t, "f", func() bool { return strings.HasSuffix(out.String(), "e\nf\n") })
}

// A log line that holds an escape sequence (written by something that
// didn't escape it) is shown as its text only.
func TestTailAndFollowShowText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.log")
	os.WriteFile(p, []byte("a\x1b]52;c;eA==\a\u202eb\n"), 0o600)
	lines, off, _ := Tail(p, 5)
	if !slices.Equal(lines, []string{"a]52;c;eA==b"}) {
		t.Fatalf("Tail = %q", lines)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuf
	go Follow(ctx, p, off, 5*time.Millisecond, &out)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("c\x1b[2J\u009bd\n")
	f.Close()
	testwait.Eventually(t, "c", func() bool { return out.String() == "c[2Jd\n" })
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte("1"), 0o644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	n := 0
	since, err := Stamp(dir)
	if err != nil {
		t.Fatal(err)
	}
	go Watch(ctx, dir, since, 5*time.Millisecond, func() { mu.Lock(); n++; mu.Unlock() })
	count := func() int { mu.Lock(); defer mu.Unlock(); return n }
	os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("x"), 0o644)
	time.Sleep(30 * time.Millisecond)
	if count() != 0 {
		t.Fatal("node_modules counted")
	}
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte("22"), 0o644)
	testwait.Eventually(t, "a change", func() bool { return count() == 1 })
	time.Sleep(30 * time.Millisecond)
	if count() != 1 {
		t.Fatalf("one save, %d changes", count())
	}
}
