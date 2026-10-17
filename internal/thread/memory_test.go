package thread

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func TestTaskPaths(t *testing.T) {
	roots := []string{"/src/api", "/wt/t-0001"}
	for text, want := range map[string][]string{
		"Fix `internal/thread/brief.go` and docs/SPEC.md.":       {"internal/thread/brief.go", "docs/SPEC.md"},
		"nothing added to go.mod (brief.md)":                     {"go.mod", "brief.md"},
		"e.g. 0.31.0, v1.2 and 4 KiB; T548":                      nil,
		"see https://example.com/a/b.html":                       nil,
		"/src/api/cmd/main.go and /wt/t-0001/x/ and /etc/passwd": {"cmd/main.go", "x"},
		"brief.go brief.go, internal/ticker/":                    {"brief.go", "internal/ticker/"},
		"~/notes/a.md stays out; ./pkg/run.go doesn't":           {"./pkg/run.go"},
		"a sentence that ends with internal/project/match.go.\n": {"internal/project/match.go"},
	} {
		if got := TaskPaths(text, roots); !slices.Equal(got, want) {
			t.Errorf("TaskPaths(%q) = %q, want %q", text, got, want)
		}
	}
}

func writeNote(t *testing.T, p *project.Project, name, text string) {
	t.Helper()
	os.MkdirAll(p.Path("memory"), 0o755)
	if err := os.WriteFile(p.Path("memory", name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBriefMemory: a brief carries every standing note and the best
// MaxMatched others for its task's words and paths, within ResetBudget.
func TestBriefMemory(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	p.Repos = []string{"/src/api", "/src/web"}
	writeNote(t, p, "standing.md", "+++\ntitle = \"Never touch prod\"\nstanding = true\n+++\n## Decided\nStaging only.\n")
	writeNote(t, p, "poller.md", "+++\ntitle = \"Retries stay at 4\"\ntouches = [\"internal/ticker/\"]\n+++\n## Decided\nKeep 4.\n\n## Watch out\nThe sweep multiplies.\n")
	writeNote(t, p, "web.md", "+++\ntitle = \"Web build quirks\"\ntouches = [\"repo:web\"]\n+++\nThe bundler caches.\n")
	writeNote(t, p, "login.md", "# Login flow\n\nThe login uses cookies.\n")
	writeNote(t, p, "big.md", "+++\ntitle = \"Login retries\"\n+++\n"+strings.Repeat("A long line about login retries.\n", 300))
	writeNote(t, p, "other.md", "# Unrelated\n\nNothing here.\n")
	// Notes for the coordinator only: never in a brief, standing or not.
	writeNote(t, p, "ops.md", "+++\ntitle = \"Merge order\"\nstanding = true\naudience = \"coordinator\"\n+++\nMerge in batches.\n")
	writeNote(t, p, "budget.md", "+++\ntitle = \"Login retries budget\"\ntouches = [\"internal/ticker/\"]\naudience = \"coordinator\"\n+++\nLogin retries cost.\n")

	res, err := p.Tasks().Add(caller.Caller{Kind: caller.Human}, []tasks.NewTask{{
		Title: "Fix login retries", Notes: "In internal/ticker/pr.go, for the web repo.", Steps: []string{"Write a test"},
	}})
	if err != nil || res[0].ID == "" {
		t.Fatalf("%+v %v", res, err)
	}
	r, _ := Create(p, Record{Title: "Fix login retries", Task: res[0].ID, Repo: "/src/api", Worktree: "/wt/t-0001"})

	m := BriefMemory(p, r)
	if !slices.Equal(m.Paths, []string{"internal/ticker/pr.go", "repo:web", "repo:api"}) {
		t.Errorf("paths %q", m.Paths)
	}
	var got []string
	for _, n := range m.Notes {
		got = append(got, n.Note.Path)
	}
	// Standing first; then the notes sharing a path, web (repo:web, and
	// web in its title) before poller (internal/ticker/, and retries in
	// its title, a word big.md has too); then big (login and retries in
	// its title) over login, which only has login.
	if want := []string{"memory/standing.md", "memory/web.md", "memory/poller.md", "memory/big.md"}; !slices.Equal(got, want) {
		t.Errorf("notes %q, want %q", got, want)
	}
	if s, n := m.Counts(); s != 1 || n != 3 {
		t.Errorf("counts %d %d", s, n)
	}

	out := m.Render(p.Dir)
	if len(out) > ResetBudget {
		t.Errorf("render is %d bytes, over %d", len(out), ResetBudget)
	}
	for _, w := range []string{"## Memory notes", p.Dir + "/memory/", "### Never touch prod (memory/standing.md; standing)",
		"### Retries stay at 4 (memory/poller.md; matched: retries, internal/ticker/pr.go)", "#### Watch out", "[… cut at the size budget]",
		"### Web build quirks (memory/web.md; matched: web, repo:web)"} {
		if !strings.Contains(out, w) {
			t.Errorf("render lacks %q:\n%s", w, out)
		}
	}

	brief, mem, err := WriteBrief(p, r, false)
	if err != nil || len(mem.Notes) != 4 {
		t.Fatalf("%v %+v", err, mem)
	}
	if b, _ := os.ReadFile(brief); !strings.Contains(string(b), "### Never touch prod") || strings.Index(string(b), "## Memory notes") > strings.Index(string(b), "# Task") {
		t.Errorf("brief's memory section:\n%s", b)
	}

	// No notes, no section.
	os.RemoveAll(p.Path("memory"))
	if m := BriefMemory(p, r); len(m.Notes) != 0 || m.Render(p.Dir) != "" {
		t.Errorf("without notes: %+v", m)
	}
}
