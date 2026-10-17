package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// onTerminal makes stdin a terminal and the editor edit, for the test.
func onTerminal(t *testing.T, h *harness, edit func(file string)) {
	t.Helper()
	oldTTY, oldEditor := stdinTTY, runEditor
	stdinTTY = func(*Env) bool { return true }
	runEditor = func(_ *Env, argv []string) error {
		if argv[0] != "fake-editor" || len(argv) != 3 || argv[1] != "-w" {
			t.Errorf("editor argv %q", argv)
		}
		edit(argv[len(argv)-1])
		return nil
	}
	h.env["VISUAL"] = "fake-editor -w"
	t.Cleanup(func() { stdinTTY, runEditor = oldTTY, oldEditor })
}

func memoryItems(p *project.Project) []string {
	items, _ := p.Inbox()
	var out []string
	for _, it := range items {
		if it.Kind == project.KindMemory {
			out = append(out, it.Summary)
		}
	}
	return out
}

// TestMemoryNewEditRm: the coordinator writes notes with --file and
// removes them with --yes; MEMORY.md follows each change, and only the
// human's changes are inbox items. A thread is refused.
func TestMemoryNewEditRm(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")

	code, out, errs := h.run(coord, "+++\ntitle = \"Retries stay at 4\"\ntouches = [\"internal/ticker/\"]\n+++\n## Decided\nFour.\n", "memory", "new", "retries", "--file", "-", "--project", "demo")
	if code != 0 || !strings.Contains(out, "added memory/retries.md") {
		t.Fatalf("new: %d %s %s", code, out, errs)
	}
	h.ok(coord, "memory", "new", "Gh poller notes", "--project", "demo")
	if data, _ := os.ReadFile(p.Path("memory", "gh-poller-notes.md")); string(data) != project.NoteTemplate {
		t.Errorf("new without --file isn't the template:\n%s", data)
	}
	h.expect(1, "exists", coord, "memory", "new", "retries", "--project", "demo")
	index, _ := os.ReadFile(p.Path("MEMORY.md"))
	if !strings.Contains(string(index), "- [Retries stay at 4](memory/retries.md): Four. (touches: internal/ticker/)\n") {
		t.Errorf("MEMORY.md:\n%s", index)
	}

	if code, _, errs := h.run(coord, "# Retries\nFive now.\n", "memory", "edit", "memory/retries.md", "--file", "-", "--project", "demo"); code != 0 {
		t.Fatalf("edit: %s", errs)
	}
	index, _ = os.ReadFile(p.Path("MEMORY.md"))
	if !strings.Contains(string(index), "- [retries](memory/retries.md): Four.\n") {
		t.Errorf("MEMORY.md after the edit (its line keeps its description):\n%s", index)
	}
	h.expect(2, "--file", coord, "memory", "edit", "retries", "--project", "demo")
	h.expect(2, "--yes", coord, "memory", "rm", "retries", "--project", "demo")
	h.ok(coord, "memory", "rm", "retries", "--yes", "--project", "demo")
	if _, err := os.Stat(p.Path("memory", "retries.md")); !os.IsNotExist(err) {
		t.Errorf("not removed: %v", err)
	}
	h.expect(1, "no-note", coord, "memory", "rm", "retries", "--yes", "--project", "demo")
	if items := memoryItems(p); len(items) != 0 {
		t.Errorf("the coordinator's changes are inbox items: %q", items)
	}

	// Paths outside memory/ are refused.
	os.WriteFile(p.Path("memory", "x.md"), []byte("x\n"), 0o644)
	for _, arg := range []string{"../PROJECT.md", "MEMORY.md/../x", p.Path("PROJECT.md"), "memory/sub/x.md", "/etc/hosts"} {
		h.expect(1, "not-memory", coord, "memory", "rm", arg, "--yes", "--project", "demo")
		h.expect(1, "not-memory", coord, "memory", "edit", arg, "--file", "-", "--project", "demo")
	}
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "o.md")
		os.WriteFile(outside, []byte("keep\n"), 0o644)
		os.Symlink(outside, p.Path("memory", "l.md"))
		h.expect(1, "link", coord, "memory", "rm", "l", "--yes", "--project", "demo")
		h.expect(1, "link", coord, "memory", "edit", "l", "--file", "-", "--project", "demo")
		if data, _ := os.ReadFile(outside); string(data) != "keep\n" {
			t.Errorf("wrote through a link: %q", data)
		}
	}

	// Threads only read.
	h.expect(1, "coordinator-only", thr, "memory", "new", "x2", "--project", "demo")
	h.expect(1, "coordinator-only", thr, "memory", "edit", "x", "--file", "-", "--project", "demo")
	h.expect(1, "coordinator-only", thr, "memory", "rm", "x", "--yes", "--project", "demo")
	h.expect(1, "coordinator-only", thr, "memory", "index", "--project", "demo")

	// index alone.
	os.WriteFile(p.Path("memory", "hand.md"), []byte("written by hand\n"), 0o644)
	if out := h.ok(coord, "memory", "index", "--project", "demo"); !strings.Contains(out, "wrote MEMORY.md") {
		t.Errorf("index: %s", out)
	}
	if out := h.ok(coord, "memory", "index", "--project", "demo"); !strings.Contains(out, "already") {
		t.Errorf("index again: %s", out)
	}
	lines, _, _ := p.JournalTail(30)
	journal := strings.Join(lines, "\n")
	for _, w := range []string{"coordinator memory.new memory/retries.md", "coordinator memory.edit memory/retries.md", "coordinator memory.rm memory/retries.md", "coordinator memory.index MEMORY.md"} {
		if !strings.Contains(journal, w) {
			t.Errorf("journal lacks %q", w)
		}
	}
}

// TestMemoryHumanEditor: on a terminal the human edits in $VISUAL; the
// change is saved, journaled and an inbox item. new leaves nothing when
// the template is left as it was; rm asks.
func TestMemoryHumanEditor(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	var write string
	onTerminal(t, h, func(file string) {
		if write != "" {
			os.WriteFile(file, []byte(write), 0o600)
		}
	})

	if out := h.ok(human, "memory", "new", "idle", "--project", "demo"); !strings.Contains(out, "not written") {
		t.Errorf("an untouched template: %s", out)
	}
	if _, err := os.Stat(p.Path("memory", "idle.md")); !os.IsNotExist(err) {
		t.Error("an untouched template was written")
	}
	write = "# Keys\nq quits.\n"
	code, out, _ := h.run(human, "keys\n", "memory", "new", "--project", "demo")
	if code != 0 || !strings.Contains(out, "added memory/keys.md") {
		t.Fatalf("new asked for its name: %d %s", code, out)
	}
	write = "# Keys\nq quits, esc goes back.\n"
	if out := h.ok(human, "memory", "edit", "keys", "--project", "demo"); !strings.Contains(out, "saved memory/keys.md") {
		t.Errorf("edit: %s", out)
	}
	if data, _ := os.ReadFile(p.Path("memory", "keys.md")); string(data) != write {
		t.Errorf("saved %q", data)
	}
	if out := h.ok(human, "memory", "edit", "keys", "--project", "demo"); !strings.Contains(out, "unchanged") {
		t.Errorf("an edit that changes nothing: %s", out)
	}
	if code, _, errs := h.run(human, "n\n", "memory", "rm", "keys", "--project", "demo"); code != 1 || !strings.Contains(errs, "not-confirmed") {
		t.Errorf("rm without a y: %d %s", code, errs)
	}
	if code, out, _ := h.run(human, "y\n", "memory", "rm", "keys", "--project", "demo"); code != 0 || !strings.Contains(out, "removed memory/keys.md") {
		t.Errorf("rm with a y: %d %s", code, out)
	}
	want := []string{
		"the user added memory/keys.md: read it",
		"the user edited memory/keys.md: read it again before you change it",
		"the user removed memory/keys.md: don't write its content back",
	}
	if got := memoryItems(p); !slices.Equal(got, want) {
		t.Errorf("items:\n%q\nwant\n%q", got, want)
	}
	lines, _, _ := p.JournalTail(10)
	if j := strings.Join(lines, "\n"); !strings.Contains(j, "human memory.edit memory/keys.md") || !strings.Contains(j, "human memory.rm memory/keys.md") {
		t.Errorf("journal:\n%s", j)
	}
}

// TestMemoryEditGuard: a note that changed while the human edited it is
// not overwritten unasked: k (or nothing) keeps both, o overwrites.
func TestMemoryEditGuard(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	note := p.Path("memory", "a.md")
	os.MkdirAll(filepath.Dir(note), 0o755)
	os.WriteFile(note, []byte("one\n"), 0o644)
	meanwhile := func(file string) {
		os.WriteFile(note, []byte("theirs\n"), 0o644)
		os.WriteFile(file, []byte("mine\n"), 0o600)
	}
	onTerminal(t, h, meanwhile)

	code, out, errs := h.run(human, "\n", "memory", "edit", "a", "--project", "demo")
	if code != 0 || !strings.Contains(errs, "memory/a.md changed while you edited it") || !strings.Contains(out, "kept both") {
		t.Fatalf("keep both: %d %s %s", code, out, errs)
	}
	if data, _ := os.ReadFile(note); string(data) != "theirs\n" {
		t.Errorf("the note was overwritten: %q", data)
	}
	if data, _ := os.ReadFile(note + ".conflict"); string(data) != "mine\n" {
		t.Errorf("the conflict copy: %q", data)
	}
	if len(memoryItems(p)) != 0 {
		t.Error("keeping both made an item")
	}

	if code, _, errs := h.run(human, "o\n", "memory", "edit", "a", "--project", "demo"); code != 0 {
		t.Fatalf("overwrite: %s", errs)
	}
	if data, _ := os.ReadFile(note); string(data) != "mine\n" {
		t.Errorf("not overwritten: %q", data)
	}

	// Removed meanwhile: o writes it again.
	onTerminal(t, h, func(file string) {
		os.Remove(note)
		os.WriteFile(file, []byte("back\n"), 0o600)
	})
	code, _, errs = h.run(human, "o\n", "memory", "edit", "a", "--project", "demo")
	if code != 0 || !strings.Contains(errs, "was removed while you edited it") {
		t.Fatalf("removed meanwhile: %d %s", code, errs)
	}
	if data, _ := os.ReadFile(note); string(data) != "back\n" {
		t.Errorf("not written again: %q", data)
	}
}
