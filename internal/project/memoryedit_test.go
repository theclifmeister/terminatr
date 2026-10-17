package project

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

func memoryProject(t *testing.T) *Project {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// TestNotePath: only a note directly in memory/ is named; anything
// else is refused.
func TestNotePath(t *testing.T) {
	p := memoryProject(t)
	for arg, want := range map[string]string{
		"retries":                "memory/retries.md",
		"retries.md":             "memory/retries.md",
		"memory/retries.md":      "memory/retries.md",
		"Code_Gotchas.v2":        "memory/Code_Gotchas.v2.md",
		p.Path("memory", "x.md"): "memory/x.md",
	} {
		if got, err := p.NotePath(arg); err != nil || got != want {
			t.Errorf("NotePath(%q) = %q, %v; want %q", arg, got, err, want)
		}
	}
	for _, arg := range []string{
		"", "../PROJECT.md", "memory/../PROJECT.md", "MEMORY.md/x", "memory/sub/x.md", "a..b",
		".hidden", "/etc/passwd", p.Path("PROJECT.md"), p.Path("memory", "sub", "x.md"), "x y", "-rf",
	} {
		if got, err := p.NotePath(arg); code(err) != "not-memory" {
			t.Errorf("NotePath(%q) = %q, %v; want not-memory", arg, got, err)
		}
	}
	if got, err := p.NewNotePath("Retries for the poller"); err != nil || got != "memory/retries-for-the-poller.md" {
		t.Errorf("NewNotePath = %q, %v", got, err)
	}
	for _, arg := range []string{"../x", "a/b c", "!!!"} {
		if _, err := p.NewNotePath(arg); code(err) != "not-memory" {
			t.Errorf("NewNotePath(%q) = %v; want not-memory", arg, err)
		}
	}
}

// TestNoteLinks: a note that is a link, or a memory/ folder that is
// one, is never read, written or removed through.
func TestNoteLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	p := memoryProject(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("secret\n"), 0o644)
	os.MkdirAll(p.Path("memory"), 0o755)
	if err := os.Symlink(outside, p.Path("memory", "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadNote("memory/link.md"); code(err) != "link" {
		t.Errorf("read through a link: %v", err)
	}
	if err := p.WriteNote("memory/link.md", []byte("x")); code(err) != "link" {
		t.Errorf("write through a link: %v", err)
	}
	if err := p.CreateNote("memory/link.md", []byte("x")); err == nil {
		t.Error("created over a link")
	}
	if err := p.RemoveNote("memory/link.md"); code(err) != "link" {
		t.Errorf("remove a link: %v", err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "secret\n" {
		t.Errorf("the link's target changed: %q", data)
	}
	if _, err := os.Lstat(p.Path("memory", "link.md")); err != nil {
		t.Errorf("the link is gone: %v", err)
	}

	// The memory folder itself a link.
	q := memoryProject(t)
	elsewhere := t.TempDir()
	os.WriteFile(filepath.Join(elsewhere, "a.md"), []byte("# A\n"), 0o644)
	os.RemoveAll(q.Path("memory"))
	if err := os.Symlink(elsewhere, q.Path("memory")); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateNote("memory/b.md", []byte("x")); code(err) != "link" {
		t.Errorf("create in a linked folder: %v", err)
	}
	if err := q.RemoveNote("memory/a.md"); code(err) != "link" {
		t.Errorf("remove in a linked folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "a.md")); err != nil {
		t.Errorf("a file of the linked folder is gone: %v", err)
	}
}

// TestNoteWrite: create refuses an existing note, write and remove need
// one.
func TestNoteWrite(t *testing.T) {
	p := memoryProject(t)
	if err := p.CreateNote("memory/a.md", []byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if err := p.CreateNote("memory/a.md", []byte("two\n")); code(err) != "exists" {
		t.Errorf("created twice: %v", err)
	}
	if err := p.WriteNote("memory/a.md", []byte("two\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := p.ReadNote("memory/a.md"); string(data) != "two\n" {
		t.Errorf("read %q", data)
	}
	if err := p.WriteNote("memory/b.md", []byte("x")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote a missing note: %v", err)
	}
	if err := p.RemoveNote("memory/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveNote("memory/a.md"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("removed twice: %v", err)
	}
}

// TestRebuildIndex: one line per note, free-form ones by file name and
// first line, the heading and other text of MEMORY.md kept.
func TestRebuildIndex(t *testing.T) {
	p := memoryProject(t)
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "retries.md"), []byte(`+++
title   = "Retries stay at 4"
touches = ["internal/ticker/pr.go", "repo:api"]
+++
## Decided
Four retries, then a backoff.
`), 0o644)
	os.WriteFile(p.Path("memory", "gotchas.md"), []byte("\n# Code gotchas\n\nDashboard races.\n"), 0o644)
	os.WriteFile(p.Path("memory", "plain.md"), []byte("just a line\nand more\n"), 0o644)
	os.WriteFile(p.Path("memory", "notes.txt"), []byte("not a note"), 0o644)
	os.WriteFile(p.Path("MEMORY.md"), []byte(`# Memory

Our own words about the list.

- [Old title](memory/gone.md): a removed note
- [Gotchas](memory/gotchas.md): hand-written

## Elsewhere
- [a link](https://example.com) stays
`), 0o644)
	changed, err := p.RebuildIndex()
	if err != nil || !changed {
		t.Fatalf("RebuildIndex = %v, %v", changed, err)
	}
	got, _ := os.ReadFile(p.Path("MEMORY.md"))
	want := `# Memory

Our own words about the list.

- [gotchas](memory/gotchas.md): Code gotchas
- [plain](memory/plain.md): just a line
- [Retries stay at 4](memory/retries.md): Four retries, then a backoff. (touches: internal/ticker/pr.go, repo:api)

## Elsewhere
- [a link](https://example.com) stays
`
	if string(got) != want {
		t.Errorf("MEMORY.md:\n%s\nwant\n%s", got, want)
	}
	if changed, err := p.RebuildIndex(); err != nil || changed {
		t.Errorf("again: %v, %v", changed, err)
	}

	// A missing MEMORY.md starts from the template; no notes, no list.
	os.Remove(p.Path("MEMORY.md"))
	os.RemoveAll(p.Path("memory"))
	if _, err := p.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(p.Path("MEMORY.md"))
	if string(got) != memoryTemplate {
		t.Errorf("empty memory:\n%s", got)
	}
}

// TestNoteChanged: the human's change is a journal line and one memory
// item; the coordinator's only a journal line. Both rebuild the index.
func TestNoteChanged(t *testing.T) {
	p := memoryProject(t)
	human := caller.Caller{Kind: caller.Human}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	p.CreateNote("memory/a.md", []byte("# A\nfirst\n"))
	if err := p.NoteChanged(human, NoteEdit, "memory/a.md"); err != nil {
		t.Fatal(err)
	}
	p.NoteChanged(human, NoteEdit, "memory/a.md") // still unhandled: no second item
	p.RemoveNote("memory/a.md")
	p.NoteChanged(human, NoteRm, "memory/a.md")
	p.CreateNote("memory/b.md", []byte("b\n"))
	p.NoteChanged(coord, NoteNew, "memory/b.md")
	items, _ := p.Inbox()
	var got []string
	for _, it := range items {
		if it.Kind == KindMemory {
			got = append(got, it.Subject+" | "+it.Summary)
		}
	}
	want := []string{
		"memory/a.md | the user edited memory/a.md: read it again before you change it",
		"memory/a.md | the user removed memory/a.md: don't write its content back",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("items:\n%s", strings.Join(got, "\n"))
	}
	lines, _, _ := p.JournalTail(10)
	journal := strings.Join(lines, "\n")
	for _, w := range []string{"human memory.edit memory/a.md", "human memory.rm memory/a.md", "coordinator memory.new memory/b.md"} {
		if !strings.Contains(journal, w) {
			t.Errorf("journal lacks %q:\n%s", w, journal)
		}
	}
	index, _ := os.ReadFile(p.Path("MEMORY.md"))
	if strings.Contains(string(index), "memory/a.md") || !strings.Contains(string(index), "- [b](memory/b.md)\n") {
		t.Errorf("MEMORY.md:\n%s", index)
	}
}

// TestNoteStamp: the guard sees a change, a removal and a creation, and
// not a rewrite with the same text.
func TestNoteStamp(t *testing.T) {
	p := memoryProject(t)
	p.CreateNote("memory/a.md", []byte("one\n"))
	s, text, err := p.StampNote("memory/a.md")
	if err != nil || string(text) != "one\n" {
		t.Fatal(text, err)
	}
	if ch, _ := s.Changed(p, "memory/a.md"); ch {
		t.Error("unchanged note changed")
	}
	p.WriteNote("memory/a.md", []byte("one\n"))
	if ch, _ := s.Changed(p, "memory/a.md"); ch {
		t.Error("same text rewritten counts as changed")
	}
	p.WriteNote("memory/a.md", []byte("two\n"))
	if ch, _ := s.Changed(p, "memory/a.md"); !ch {
		t.Error("changed note unchanged")
	}
	p.RemoveNote("memory/a.md")
	if ch, _ := s.Changed(p, "memory/a.md"); !ch {
		t.Error("removed note unchanged")
	}
	gone, _, _ := p.StampNote("memory/a.md")
	if ch, _ := gone.Changed(p, "memory/a.md"); ch || !gone.Gone {
		t.Error("a missing note changed")
	}
	p.CreateNote("memory/a.md", []byte("new\n"))
	if ch, _ := gone.Changed(p, "memory/a.md"); !ch {
		t.Error("a note made meanwhile unchanged")
	}
	if err := p.WriteConflict("memory/a.md", []byte("mine\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p.Path("memory", "a.md.conflict")); string(data) != "mine\n" {
		t.Errorf("conflict copy %q", data)
	}
}
