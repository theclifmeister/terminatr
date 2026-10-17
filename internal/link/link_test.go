package link

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

var coord = caller.Caller{Kind: caller.Coordinator}

// setup makes projects a and b on a fresh home, linked both ways, and a
// clock tests move.
func setup(t *testing.T) (a, b *project.Project, clock *time.Time) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[projects.a.links]\nb = \"both\"\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	var err error
	if a, err = project.New(project.Options{Slug: "a"}); err != nil {
		t.Fatal(err)
	}
	if b, err = project.New(project.Options{Slug: "b"}); err != nil {
		t.Fatal(err)
	}
	c := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock = &c
	old := now
	now = func() time.Time { return *clock }
	t.Cleanup(func() { now = old })
	return a, b, clock
}

func code(err error) string {
	var e *tasks.Error
	if errors.As(err, &e) {
		return e.Code
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func TestRate(t *testing.T) {
	a, _, clock := setup(t)
	ca := coord
	ca.Project = "a"
	for i := range Burst {
		if _, err := Send(ca, a, "b", Opts{Text: "hi"}); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
	}
	_, err := Send(ca, a, "b", Opts{Text: "one more"})
	if code(err) != "rate-limited" || !strings.Contains(err.Error(), "the next fits at") {
		t.Fatalf("over the burst: %v", err)
	}
	*clock = clock.Add(time.Hour / PerHour)
	if _, err := Send(ca, a, "b", Opts{Text: "later"}); err != nil {
		t.Fatalf("after a token came back: %v", err)
	}
	// The other direction has its own bucket.
	b, _ := project.Open("b")
	if _, err := Send(caller.Caller{Kind: caller.Coordinator, Project: "b"}, b, "a", Opts{Text: "back"}); err != nil {
		t.Fatal(err)
	}
}

func TestBreaker(t *testing.T) {
	a, b, clock := setup(t)
	ca, cb := coord, coord
	ca.Project, cb.Project = "a", "b"
	for i := range Exchanges {
		*clock = clock.Add(7 * time.Minute) // a token each way comes back
		if _, err := Send(ca, a, "b", Opts{Text: "ping"}); err != nil {
			t.Fatalf("exchange %d a: %v", i, err)
		}
		if _, err := Send(cb, b, "a", Opts{Text: "pong"}); err != nil {
			t.Fatalf("exchange %d b: %v", i, err)
		}
	}
	if !Paused("a", "b") {
		t.Fatal("not paused after the exchanges")
	}
	for _, p := range []*project.Project{a, b} {
		items, _ := p.Inbox()
		n := 0
		for _, it := range items {
			if it.Kind == KindPaused && it.NeedsUser {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s: %d paused items", p.Slug, n)
		}
	}
	*clock = clock.Add(time.Hour)
	if _, err := Send(ca, a, "b", Opts{Text: "again"}); code(err) != "link-paused" {
		t.Fatalf("while paused: %v", err)
	}
	if err := Resume(ca, a, "b", false); code(err) != "needs-approval" {
		t.Fatalf("resume unasked: %v", err)
	}
	if err := Resume(ca, a, "b", true); err != nil {
		t.Fatal(err)
	}
	if err := Resume(ca, a, "b", true); code(err) != "not-paused" {
		t.Fatalf("resume twice: %v", err)
	}
	if _, err := Send(ca, a, "b", Opts{Text: "again"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*project.Project{a, b} {
		items, _ := p.Inbox()
		for _, it := range items {
			if it.Kind == KindPaused {
				t.Fatalf("%s: paused item stayed", p.Slug)
			}
		}
	}
}

// TestBreakerHuman: the user's own message on the link starts the count
// again.
func TestBreakerHuman(t *testing.T) {
	a, b, clock := setup(t)
	ca, cb := coord, coord
	ca.Project, cb.Project = "a", "b"
	for i := range Exchanges {
		*clock = clock.Add(7 * time.Minute)
		if i == Exchanges-1 {
			if _, err := Send(caller.Caller{Kind: caller.Human}, a, "b", Opts{Text: "me"}); err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(7 * time.Minute)
		}
		Send(ca, a, "b", Opts{Text: "ping"})
		Send(cb, b, "a", Opts{Text: "pong"})
	}
	if Paused("a", "b") {
		t.Fatal("paused despite the user's message")
	}
}

func TestClean(t *testing.T) {
	if got := Clean("  a\r\nb\x00c\td\x1b \xff "); got != "a\nbc\td" {
		t.Fatalf("%q", got)
	}
	// what renders other than it reads goes too: a bidi override, a
	// zero-width space, tag characters, a line separator, an OSC
	// sequence's introducer and C1 controls
	in := "pay \u202eredro\u200b\U000E0069\U000E0067nore\u2028x\x1b]52;c;eA==\a\u009b2J"
	if got := Clean(in); got != "pay redronore x]52;c;eA==2J" {
		t.Fatalf("%q", got)
	}
}

// TestCopyAttachRefusesLinks: an attachment's place in messages/ that
// is a link, or its message folder, is refused, and what the link
// points at is left as it was.
func TestCopyAttachRefusesLinks(t *testing.T) {
	a, _, _ := setup(t)
	src := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(src, []byte("evil\n"), 0o644)
	victim := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(victim, []byte("keep\n"), 0o600)
	m := &Message{ID: "m1", Peer: "b"}
	att := []attachment{{name: "notes.md", src: src}}
	d := filepath.Join(dir(a, "b"), "m1")
	os.MkdirAll(d, 0o755)
	if err := os.Symlink(victim, filepath.Join(d, "notes.md")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := copyAttach(a, m, att); err == nil {
		t.Error("file: copied through a link")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Errorf("victim %q", b)
	}
	elsewhere := t.TempDir()
	os.RemoveAll(d)
	os.Symlink(elsewhere, d)
	if err := copyAttach(a, m, att); err == nil {
		t.Error("folder: copied through a link")
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Errorf("folder: wrote %v", ents)
	}
}
