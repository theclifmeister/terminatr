package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
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

// texts are the texts of p's messages with other, oldest first, and
// their states.
func texts(t *testing.T, p *project.Project, other string) (txt, states []string) {
	t.Helper()
	msgs, err := Log(p, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		txt, states = append(txt, m.Text), append(states, m.State)
	}
	return txt, states
}

// TestQueue: a reply in nine parts at once. The burst goes; the rest
// wait queued, each with when it should go, and go in order as the
// bucket refills; nothing is refused or lost. The other direction has
// its own bucket.
func TestQueue(t *testing.T) {
	a, b, clock := setup(t)
	ca := coord
	ca.Project = "a"
	start := *clock
	var parts []string
	for i := range 9 {
		parts = append(parts, fmt.Sprintf("part %d of 9", i+1))
		m, err := Send(ca, a, "b", Opts{Text: parts[i]})
		if err != nil {
			t.Fatalf("part %d: %v", i+1, err)
		}
		want := StateSent
		if i >= config.DefaultLinkBurst {
			want = StateQueued
			// the 4th waits a token (6 min), the 9th six of them
			if due := start.Add(time.Duration(i-config.DefaultLinkBurst+1) * time.Hour / config.DefaultLinkPerHour); !m.Due.Equal(due) {
				t.Errorf("part %d due %v, want %v", i+1, m.Due, due)
			}
		}
		if m.State != want {
			t.Fatalf("part %d %s", i+1, m.State)
		}
	}
	if got, _ := texts(t, b, "a"); len(got) != config.DefaultLinkBurst {
		t.Fatalf("b has %v", got)
	}
	cb := caller.Caller{Kind: caller.Coordinator, Project: "b"}
	if _, err := Send(cb, b, "a", Opts{Text: "back"}); err != nil {
		t.Fatal(err)
	}
	if rows := Rows(mustConfig(t), a); len(rows) != 1 || rows[0].Queued != 6 {
		t.Fatalf("rows %+v", rows)
	}
	// Half a token: nothing more goes.
	*clock = clock.Add(3 * time.Minute)
	Drain()
	if got, _ := texts(t, b, "a"); len(got) != config.DefaultLinkBurst+1 {
		t.Fatalf("after 3 min b has %d", len(got))
	}
	// A token each 6 minutes, one at a time, in order.
	for i := 1; i <= 6; i++ {
		*clock = start.Add(time.Duration(i) * time.Hour / config.DefaultLinkPerHour)
		Drain()
		got, _ := texts(t, b, "a")
		if n := len(got) - 1; n != config.DefaultLinkBurst+i { // less b's own
			t.Fatalf("after %d tokens b has %d of a's", i, n)
		}
	}
	got, _ := texts(t, b, "a")
	var in []string
	for _, x := range got {
		if x != "back" {
			in = append(in, x)
		}
	}
	if !slices.Equal(in, parts) {
		t.Fatalf("arrived %q", in)
	}
	if _, states := texts(t, a, "b"); slices.Contains(states, StateQueued) {
		t.Fatalf("a's copies %v", states)
	}
	if rows := Rows(mustConfig(t), a); rows[0].Queued != 0 {
		t.Fatalf("still queued %+v", rows[0])
	}
}

// TestQueueOrder: a message sent while others wait goes behind them,
// even once a token is back; a drain at the send lets the first go.
func TestQueueOrder(t *testing.T) {
	a, b, clock := setup(t)
	ca := coord
	ca.Project = "a"
	for i := range 5 {
		Send(ca, a, "b", Opts{Text: fmt.Sprint(i)})
	}
	*clock = clock.Add(time.Hour / config.DefaultLinkPerHour)
	m, err := Send(ca, a, "b", Opts{Text: "5"})
	if err != nil || m.State != StateQueued {
		t.Fatalf("%v %+v", err, m)
	}
	got, _ := texts(t, b, "a")
	if !slices.Equal(got, []string{"0", "1", "2", "3"}) {
		t.Fatalf("b has %q", got)
	}
}

// TestQueueLimits: the limits are settings, the lower of the two sides'
// for a direction here; a size over them is refused.
func TestQueueLimits(t *testing.T) {
	a, b, _ := setup(t)
	os.WriteFile(filepath.Join(os.Getenv(home.Env), "config.toml"), []byte("[defaults]\nlink_burst = 5\nlink_max_kb = 1\n\n[projects.a]\nlink_max_kb = 2\n\n[projects.a.links]\nb = \"both\"\n\n[projects.b]\nlink_burst = 2\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	ca := coord
	ca.Project = "a"
	if _, err := Send(ca, a, "b", Opts{Text: strings.Repeat("x", 1025)}); code(err) != "too-long" || !strings.Contains(err.Error(), "at most 1.0 KB") {
		t.Fatalf("size: %v", err)
	}
	for i := range 3 {
		m, err := Send(ca, a, "b", Opts{Text: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]string{true: StateSent, false: StateQueued}[i < 2]; m.State != want {
			t.Fatalf("message %d %s", i, m.State)
		}
	}
	if got, _ := texts(t, b, "a"); len(got) != 2 {
		t.Fatalf("b has %d", len(got))
	}
}

// TestQueuePausedAndOff: what waits stays while the breaker holds the
// link and goes once the user resumes it; a send meanwhile is still
// refused. A link turned off since marks what waits undelivered.
func TestQueuePausedAndOff(t *testing.T) {
	a, b, clock := setup(t)
	ca := coord
	ca.Project = "a"
	for i := range 4 {
		Send(ca, a, "b", Opts{Text: fmt.Sprint(i)})
	}
	withStore(func(s *store) error { s.pair("a", "b").Paused = true; return nil })
	*clock = clock.Add(time.Hour)
	Drain()
	if got, _ := texts(t, b, "a"); len(got) != 3 {
		t.Fatalf("paused: b has %d", len(got))
	}
	if _, err := Send(ca, a, "b", Opts{Text: "more"}); code(err) != "link-paused" {
		t.Fatalf("send while paused: %v", err)
	}
	if err := Resume(caller.Caller{Kind: caller.Human}, a, "b", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := texts(t, b, "a"); len(got) != 4 {
		t.Fatalf("resumed: b has %d", len(got))
	}
	for i := range 4 {
		Send(ca, a, "b", Opts{Text: fmt.Sprint("x", i)})
	}
	os.WriteFile(filepath.Join(os.Getenv(home.Env), "config.toml"), []byte("[projects.b.links]\na = \"both\"\n"), 0o600)
	*clock = clock.Add(time.Hour)
	Drain()
	_, states := texts(t, a, "b")
	if n := strings.Count(strings.Join(states, " "), StateUndelivered); n == 0 || slices.Contains(states, StateQueued) {
		t.Fatalf("after the link went: %v", states)
	}
	if len(items(t, a, KindUndelivered)) == 0 {
		t.Fatal("no undelivered item")
	}
}

// TestApproveQueues: a held message the user approves over the rate
// waits queued, then goes as the user's input (the breaker's count
// starts again).
func TestApproveQueues(t *testing.T) {
	a, b, clock := setup(t)
	os.WriteFile(filepath.Join(os.Getenv(home.Env), "config.toml"), []byte("[projects.a.links]\nb = { allow = \"both\", ask = true }\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	human := caller.Caller{Kind: caller.Human}
	for i := range 3 {
		Send(human, a, "b", Opts{Text: fmt.Sprint(i)})
	}
	ca := coord
	ca.Project = "a"
	h, err := Send(ca, a, "b", Opts{Text: "held"})
	if err != nil || h.State != StateHeld {
		t.Fatalf("%v %+v", err, h)
	}
	m, err := Approve(human, a, h.ID, false)
	if err != nil || m.State != StateQueued || m.Due.IsZero() {
		t.Fatalf("approve: %v %+v", err, m)
	}
	*clock = clock.Add(time.Hour / config.DefaultLinkPerHour)
	Drain()
	got, _ := texts(t, b, "a")
	if len(got) != 4 || got[3] != "held" {
		t.Fatalf("b has %q", got)
	}
	readStore(func(s *store) {
		if p := s.Pairs[pairKey("a", "b")]; p == nil || !p.Human.Equal(*clock) {
			t.Errorf("not the user's input: %+v", p)
		}
	})
}

func mustConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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

// TestNewestAcrossSeries (T683): with a paired server's project each
// side numbers its own ids, so the last message is the latest created
// of each series' newest, not the highest number.
func TestNewestAcrossSeries(t *testing.T) {
	a, _, clock := setup(t)
	other := "mini/api"
	for _, m := range []*Message{
		{ID: "M-F885-30", Out: true, Peer: other, State: StateRead, Created: clock.Add(-time.Hour)},
		{ID: "M-8F49-4", Peer: other, State: StateSent, Created: *clock},
		{ID: "M-8F49-3", Peer: other, State: StateRead, Created: clock.Add(-2 * time.Hour)},
	} {
		if err := write(a, m); err != nil {
			t.Fatal(err)
		}
	}
	if m := newest(a, other); m == nil || m.ID != "M-8F49-4" {
		t.Fatalf("newest %+v", m)
	}
	if m := newest(a, "b"); m != nil {
		t.Fatalf("none with b: %+v", m)
	}
}
