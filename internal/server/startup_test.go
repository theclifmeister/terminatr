package server

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/version"
)

var updateGolden = flag.Bool("update", false, "rewrite the start-up block golden files in testdata/startup")

// claudeCap is Claude Code's cap on a hook's additionalContext, in
// characters (UTF-16 code units, as JavaScript counts them).
const claudeCap = 10000

// fill is a line of about n bytes of multibyte text, numbered i.
func fill(i, n int) string {
	s := fmt.Sprintf("%03d Ünïcødé ✓ 日本語 ", i)
	for len(s) < n {
		s += "words that go on "
	}
	return strings.TrimSpace(s[:n])
}

// limitProject builds project demo with every file at its size budget,
// and every list at a size the project reaches in use, times scale: at
// 1, nothing is over a budget (tm doctor stays quiet); over 1, past
// every budget, as a project that hasn't been consolidated.
func limitProject(t *testing.T, scale int) *project.Project {
	t.Helper()
	p := newWatchProject(t)
	// No agent on PATH: the Project part doesn't depend on the machine.
	t.Setenv("PATH", t.TempDir())

	// CONTEXT.md at its budget, with a Needs you section.
	var ctx strings.Builder
	ctx.WriteString("# Context\n\n## Where things stand (2026-11-01)\n\n")
	needs := "\n## Needs you\n\n"
	for i := range 5 {
		needs += "- " + fill(i, 110) + "\n"
	}
	conv := "\n## Conventions learned\n\n- " + fill(0, 200) + "\n"
	for i := 0; ctx.Len()+len(needs)+len(conv)+130 < project.BudgetContext*scale; i++ {
		ctx.WriteString("- " + fill(i, 120) + "\n")
	}
	ctx.WriteString(needs + conv)
	write(t, p.Path("CONTEXT.md"), ctx.String())

	// Notes until MEMORY.md is at its budget.
	for i := 0; ; i++ {
		note := fmt.Sprintf("---\ntitle: %q\n---\n\n%s\n", fill(i, 60), fill(i, 900))
		if err := p.CreateNote(fmt.Sprintf("memory/note-%02d.md", i), []byte(note)); err != nil {
			t.Fatal(err)
		}
		if _, err := p.SyncIndex(); err != nil {
			t.Fatal(err)
		}
		if st, err := os.Stat(p.Path("MEMORY.md")); err != nil || st.Size() > int64(project.BudgetIndex*scale-120) {
			break
		}
	}

	// 300 tasks: in motion, waiting on review or the user, on deck,
	// backlog, done.
	var nt []tasks.NewTask
	for i := range 300 * scale {
		st := "open"
		switch {
		case i < 30:
			st = "started"
		case i < 40:
			st = "review"
		case i < 45:
			st = "blocked"
		case i < 60:
			st = "ready"
		case i >= 250:
			st = "done"
		}
		nt = append(nt, tasks.NewTask{Title: fill(i, 120), Status: st})
	}
	nt[0].Steps = make([]string, 20)
	for i := range nt[0].Steps {
		nt[0].Steps[i] = fill(i, 100)
	}
	if _, err := p.Tasks().Add(human, nt); err != nil {
		t.Fatal(err)
	}

	// 30 open threads, each with a report of long next lines.
	when := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i := range 30 * scale {
		rec, err := thread.Create(p, thread.Record{Title: fill(i, 120), Task: fmt.Sprintf("T%d", i+1), Model: "a-model"})
		if err != nil {
			t.Fatal(err)
		}
		rep := "## Report\n\n" + fill(i, 400) + "\n\n## Next\n\n"
		for j := range 6 {
			rep += "- " + fill(j, 100) + "\n"
		}
		if _, err := thread.StoreReport(p, rec.ID, rep, nil, when); err != nil {
			t.Fatal(err)
		}
	}

	// 50 inbox items, open questions.
	for i := range 50 * scale {
		// ASCII: an id made in the same second as another gets a suffix,
		// which must not move where a clipped line's bytes end.
		if _, err := p.AddItem("report", fmt.Sprintf("t-%04d", i+1), strings.Repeat("an item summary ", 13), i%7 == 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 * scale {
		q := project.Question{Header: "Choice", Question: fill(i, 600), Options: []project.QuestionOption{{Label: fill(1, 80)}, {Label: fill(2, 80)}}}
		if _, err := p.AddQuestion(q); err != nil {
			t.Fatal(err)
		}
	}

	// A long journal, last.
	var j strings.Builder
	for i := range 50 * scale {
		fmt.Fprintf(&j, "%s ticker thread.report t-%04d %s\n", when.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), i%30+1, fill(i, 300))
	}
	f, err := os.OpenFile(p.Path("JOURNAL.md"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(j.String())
	f.Close()

	if scale == 1 {
		if over, err := p.Oversized(); err != nil || len(over) > 0 {
			t.Fatalf("the fixture at the limit is over a budget: %v %v", over, err)
		}
	}
	return p
}

// fixedHome moves TERMINATR_HOME to a path of a fixed length, so that
// paths in the block take the same bytes in every run and the parts are
// cut at the same places (the golden file).
func fixedHome(t *testing.T) {
	t.Helper()
	const length = 160
	base := t.TempDir()
	if len(base)+2 > length {
		t.Skipf("temp dir %s is too long for the fixture", base)
	}
	home := filepath.Join(base, strings.Repeat("h", length-len(base)-1))
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMINATR_HOME", home)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// staleStamp is a system prompt launched with another tm, other rules
// and another PROJECT.md: the header carries every drift line.
var staleStamp = &PromptStamp{Version: "0.0.1", Rules: "stale", Project: "stale"}

// checkUnderCap fails when block passes the budget or Claude's cap, or
// when a part shows less than `tm context --part` prints without its
// pointer.
func checkUnderCap(t *testing.T, who, block string, secs []project.Section) {
	t.Helper()
	if n := len(block); n > project.StartupBudget {
		t.Errorf("%s: %d bytes, over the %d-byte budget", who, n, project.StartupBudget)
	}
	if n := len(utf16.Encode([]rune(block))); n > claudeCap {
		t.Errorf("%s: %d UTF-16 units, over Claude's cap", who, n)
	}
	for _, part := range project.StartupParts {
		ps := project.PartSections(secs, part.Name)
		if len(ps) == 0 {
			continue
		}
		pointer := strings.Contains(block, "tm context --part "+part.Name+"]\n")
		if full := project.RenderContext(ps); strings.Contains(block, full) || pointer {
			continue
		}
		// No pointer: each section shows whole, its Index only moving
		// lines to another part.
		for _, s := range ps {
			for _, l := range s.Index {
				if !s.IndexWhole || !strings.Contains(block, l+"\n") {
					t.Errorf("%s: part %s shows less than in full, with no pointer", who, part.Name)
					return
				}
			}
		}
	}
}

// TestStartupUnderCap: the SessionStart block of a session whose rules
// are in its system prompt stays under the budget (9,000 bytes) and
// Claude's cap (10,000 characters) for a coordinator and a thread, with
// every project file at its budget and long lists, and past every
// budget; a part cut short ends with its `tm context --part` pointer.
// The block at the limit is in testdata/startup (go test -run
// TestStartupUnderCap -update rewrites it), so a change in what agents
// see is a reviewed diff (T917).
func TestStartupUnderCap(t *testing.T) {
	for _, c := range []struct {
		name  string
		scale int
	}{{"limit", 1}, {"over", 3}} {
		t.Run(c.name, func(t *testing.T) {
			testPaths(t)
			fixedHome(t)
			p := limitProject(t, c.scale)
			state := filepath.Join(t.TempDir(), "ticker.json")
			secs, err := thread.Context(p, project.Ticked{})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range []SessionRecord{
				{Role: proto.RoleCoordinator, Project: "demo", Prompt: staleStamp},
				{Role: proto.RoleThread, Project: "demo", Thread: "t-0001", Prompt: staleStamp},
			} {
				b, err := contextFor(r, state)()
				if err != nil {
					t.Fatal(err)
				}
				block := string(b)
				parts := secs // a thread's block is its own state, not tm context's parts
				if r.Role == proto.RoleThread {
					parts = nil
				}
				checkUnderCap(t, c.name+" "+r.Role, block, parts)
				if _, cut := clipStartup(block); cut {
					t.Errorf("%s %s: the run-time clip would cut it", c.name, r.Role)
				}
				for _, drift := range []string{"tm was upgraded", "PROJECT.md changed"} {
					if r.Role == proto.RoleCoordinator && !strings.Contains(block, drift) {
						t.Errorf("%s: no %q line", r.Role, drift)
					}
				}
				if c.scale == 1 && runtime.GOOS != "windows" { // its paths and defaults differ
					golden(t, filepath.Join("testdata", "startup", r.Role+".txt"), normalize(block))
				}
			}
		})
	}
}

// normalize replaces what changes from run to run in a block: the
// test's home, tm's version, times.
func normalize(s string) string {
	s = strings.ReplaceAll(s, os.Getenv("TERMINATR_HOME"), "$TERMINATR_HOME")
	s = strings.ReplaceAll(s, "→ "+version.Version+")", "→ $VERSION)")
	for _, re := range timeREs {
		s = re.ReplaceAllString(s, "$$TIME")
	}
	return s
}

var timeREs = []*regexp.Regexp{
	regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ`),
	regexp.MustCompile(`\d{8}T\d{6}Z(-\d+)?`),
	regexp.MustCompile(`\d{4}-\d\d-\d\d \d\d:\d\d( UTC)?`),
}

// golden compares got with the file at path, or rewrites it with -update.
func golden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestStartupUnderCap -update writes it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the block (go test -run TestStartupUnderCap -update, then review the diff):\n%s", path, firstDiff(string(want), got))
	}
}

// firstDiff shows the first line where want and got differ.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\nwant %q\ngot  %q", i+1, a, b)
		}
	}
	return ""
}

// TestClipStartup: a block past the run-time cut is clipped there,
// ending with what to run, and says it was cut.
func TestClipStartup(t *testing.T) {
	if got, cut := clipStartup("short\n"); cut || got != "short\n" {
		t.Errorf("short block: %q %v", got, cut)
	}
	long := strings.Repeat(fill(1, 99)+"\n", 200)
	got, cut := clipStartup(long)
	if !cut || len(got) > project.StartupCut || !strings.HasSuffix(got, "run `tm context` now for the rest.\n") {
		t.Errorf("long block: %d bytes, cut %v, ends %q", len(got), cut, got[max(len(got)-80, 0):])
	}
}

// TestStartupCutJournaled: a block past the run-time cut reaches the
// agent clipped, and the clip is journaled for tm doctor; a session
// without a system prompt (its rules in the block) is not clipped.
func TestStartupCutJournaled(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	long := strings.Repeat(fill(1, 99)+"\n", 200)
	old := blockFor
	blockFor = func(SessionRecord, string) func() ([]byte, error) {
		return func() ([]byte, error) { return []byte(long), nil }
	}
	t.Cleanup(func() { blockFor = old })
	s := &Server{records: map[string]SessionRecord{
		"s1": {ID: "s1", Role: proto.RoleThread, Project: "demo", Thread: "t-0001", Prompt: staleStamp},
		"s2": {ID: "s2", Role: proto.RoleCoordinator, Project: "demo"},
	}}
	b, err := s.contextOf("s1")()
	if err != nil || len(b) > project.StartupCut {
		t.Fatalf("%d bytes, %v", len(b), err)
	}
	n, last, err := p.StartupCuts(time.Now().Add(-time.Minute))
	if err != nil || n != 1 || !strings.Contains(last, "t-0001 session s1: the start-up block was 20000 bytes, clipped at 9500") {
		t.Fatalf("journal: %d %q %v", n, last, err)
	}
	if b, _ := s.contextOf("s2")(); string(b) != long {
		t.Errorf("no system prompt, yet clipped")
	}
	if n, _, _ := p.StartupCuts(time.Now().Add(-time.Minute)); n != 1 {
		t.Errorf("%d cuts journaled, want 1", n)
	}
}
