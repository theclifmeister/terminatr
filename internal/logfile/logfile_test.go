package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTail(t *testing.T) {
	p := write(t, "one\ntwo\nthree\nfour\n")
	c, err := Tail(p, 0, 100)
	if err != nil || string(c.Text) != "one\ntwo\nthree\nfour\n" || c.Start != 0 || c.End != 19 || c.Size != 19 {
		t.Fatalf("all: %+v %v", c, err)
	}
	// 9 bytes before the end: "hree\nfour\n" starts mid-line, cut to it.
	c, _ = Tail(p, 0, 9)
	if string(c.Text) != "four\n" || c.Start != 14 {
		t.Fatalf("tail: %q %d", c.Text, c.Start)
	}
	// The chunk before it, as scrolling up asks.
	c, _ = Tail(p, c.Start, 9)
	if string(c.Text) != "three\n" || c.Start != 8 || c.End != 14 {
		t.Fatalf("before: %q %d %d", c.Text, c.Start, c.End)
	}
	c, _ = Tail(p, c.Start, 9)
	if string(c.Text) != "one\ntwo\n" || c.Start != 0 {
		t.Fatalf("first: %q %d", c.Text, c.Start)
	}
	// One line longer than the chunk comes as its piece.
	long := write(t, strings.Repeat("x", 50))
	if c, _ := Tail(long, 0, 10); len(c.Text) != 10 || c.Start != 40 {
		t.Fatalf("long line: %q %d", c.Text, c.Start)
	}
	if c, err := Tail(filepath.Join(t.TempDir(), "none"), 0, 10); err != nil || len(c.Text) != 0 {
		t.Fatalf("missing: %+v %v", c, err)
	}
	if _, err := Tail(t.TempDir(), 0, 10); err == nil {
		t.Fatal("read a folder")
	}
}

func TestAfter(t *testing.T) {
	p := write(t, "one\ntwo\nthr")
	c, _ := After(p, 4, 100)
	if string(c.Text) != "two\n" || c.Start != 4 || c.End != 8 || c.Reset {
		t.Fatalf("after: %+v", c)
	}
	// The line still being written waits.
	if c, _ := After(p, 8, 100); len(c.Text) != 0 || c.End != 8 {
		t.Fatalf("partial: %+v", c)
	}
	// At most max, whole lines.
	if c, _ := After(p, 0, 6); string(c.Text) != "one\n" || c.End != 4 {
		t.Fatalf("max: %+v", c)
	}
	// Rotated: shorter than the offset, read as the tail.
	os.WriteFile(p, []byte("new\n"), 0o600)
	if c, _ := After(p, 8, 100); !c.Reset || string(c.Text) != "new\n" || c.End != 4 {
		t.Fatalf("reset: %+v", c)
	}
}
