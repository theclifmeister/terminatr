package project

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseNote(t *testing.T) {
	for _, c := range []struct {
		name, text, title string
		touches           []string
		standing, front   bool
		source, body      string
	}{
		{"free-form", "# Retries\n\nKeep 4.\n", "Retries", nil, false, false, "", "# Retries\n\nKeep 4.\n"},
		{"no heading", "just text\n", "plain", nil, false, false, "", "just text\n"},
		{"front", "+++\ntitle = \"Retries stay at 4\"\ntouches = [\"internal/ticker/pr.go\", \"repo:api\"]\nstanding = true\nsource = \"T481\"\n+++\n## Decided\nKeep 4.\n",
			"Retries stay at 4", []string{"internal/ticker/pr.go", "repo:api"}, true, true, "T481", "## Decided\nKeep 4.\n"},
		{"front, title from heading", "+++\ntouches = [\"a.go\"]\n+++\n# From heading\n", "From heading", []string{"a.go"}, false, true, "", "# From heading\n"},
		{"broken front is free-form", "+++\ntitle = \n+++\n# Broken\n", "Broken", nil, false, false, "", "+++\ntitle = \n+++\n# Broken\n"},
		{"unclosed front is free-form", "+++\ntitle = \"x\"\n# Open\n", "Open", nil, false, false, "", "+++\ntitle = \"x\"\n# Open\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			n := ParseNote("memory/plain.md", c.text)
			if n.Title != c.title || !slices.Equal(n.Touches, c.touches) || n.Standing != c.standing || n.Front != c.front || n.Source != c.source || n.Body != c.body {
				t.Errorf("got %+v", n)
			}
		})
	}
}

func TestNoteSection(t *testing.T) {
	n := ParseNote("memory/x.md", "# X\n\n## Decided\nKeep 4.\n### Detail\nsub\n## Ruled out\nBackoff.\n## watch out\nSweep.\n")
	for name, want := range map[string]string{
		SectionDecided:  "Keep 4.\n### Detail\nsub",
		SectionRuledOut: "Backoff.",
		SectionWatchOut: "Sweep.",
		"Missing":       "",
	} {
		if got := n.Section(name); got != want {
			t.Errorf("Section(%q) = %q, want %q", name, got, want)
		}
	}
	tmpl := ParseNote("memory/t.md", NoteTemplate)
	if !tmpl.Front || tmpl.Section(SectionDecided) == "" || tmpl.Section(SectionRuledOut) == "" || tmpl.Section(SectionWatchOut) == "" {
		t.Errorf("NoteTemplate doesn't parse as the schema: %+v", tmpl)
	}
}

func TestTerms(t *testing.T) {
	got := Terms("Why is the GH poller's retry at 4? retry, gh-poller")
	want := []string{"why", "gh", "poller", "retry"}
	if !slices.Equal(got, want) {
		t.Errorf("Terms = %q, want %q", got, want)
	}
}

func TestMatch(t *testing.T) {
	notes := []Note{
		ParseNote("memory/retries.md", "+++\ntitle = \"Retries stay at 4 for the gh poller\"\ntouches = [\"internal/ticker/pr.go\", \"repo:api\"]\n+++\n## Decided\nKeep retries at 4; gh rate limits.\n"),
		ParseNote("memory/sweep.md", "# Sweep interval\n\nThe ticker sweeps every 15 s; retries multiply it.\n"),
		ParseNote("memory/ui.md", "+++\ntitle = \"Popup tabs\"\ntouches = [\"internal/tui/\"]\n+++\nTabs draw from mods.\n"),
		ParseNote("memory/notes.md", "# Misc\n\nretries once.\n"),
	}
	paths := func(hs []Hit) []string {
		var out []string
		for _, h := range hs {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(h.Note.Path, "memory/"), ".md"))
		}
		return out
	}
	for _, c := range []struct {
		name   string
		query  string
		paths  []string
		limit  int
		want   []string
		scores []int
	}{
		{"title term clears alone", "poller", nil, 0, []string{"retries"}, []int{3}},
		{"one body term doesn't clear", "multiply", nil, 0, nil, nil},
		{"two body terms clear only at score 3", "sweeps multiply", nil, 0, nil, nil},
		{"three body terms clear", "sweeps multiply every", nil, 0, []string{"sweep"}, []int{3}},
		{"title term beats two body terms under 3", "retries ticker", nil, 0, []string{"retries"}, []int{3}},
		{"shared file", "", []string{"internal/ticker/pr.go"}, 0, []string{"retries"}, []int{4}},
		{"file under a touched folder", "", []string{"internal/tui/popup.go"}, 0, []string{"ui"}, []int{4}},
		{"folder holding a touched file", "", []string{"internal/ticker"}, 0, []string{"retries"}, []int{4}},
		{"sibling path doesn't meet", "", []string{"internal/tick"}, 0, nil, nil},
		{"repo entry", "", []string{"repo:api"}, 0, []string{"retries"}, []int{4}},
		{"repo entry is not a path", "", []string{"repo"}, 0, nil, nil},
		{"path and terms add up", "gh retries", []string{"./internal/ticker/pr.go"}, 0, []string{"retries"}, []int{3 + 3 + 4}},
		{"limit keeps the best", "retries sweep ticker", nil, 1, []string{"sweep"}, []int{3 + 1 + 1}},
		{"stop words never match", "the of and", nil, 0, nil, nil},
		{"nothing asked", "", nil, 0, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			hits := Match(notes, c.query, c.paths, c.limit)
			if got := paths(hits); !slices.Equal(got, c.want) {
				t.Fatalf("Match(%q, %q) = %q, want %q", c.query, c.paths, got, c.want)
			}
			for i, s := range c.scores {
				if hits[i].Score != s {
					t.Errorf("hit %d (%s) score %d, want %d", i, hits[i].Note.Path, hits[i].Score, s)
				}
			}
		})
	}
}

func TestNotesAndMemoryFiles(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Path("memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"b.md":     "+++\ntitle = \"Alpha rule\"\ntouches = [\"cmd/\"]\n+++\nbody\n",
		"a.md":     "# Zulu\n",
		"skip.txt": "not a note",
	} {
		if err := os.WriteFile(p.Path("memory", name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := p.Notes()
	if err != nil || len(notes) != 2 || notes[0].Title != "Alpha rule" || notes[1].Title != "Zulu" {
		t.Fatalf("Notes = %+v, %v", notes, err)
	}
	files, err := p.MemoryFiles()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(files, func(f MemoryFile) bool { return f.Path == "memory/b.md" })
	if i < 0 || files[i].Title != "Alpha rule" || !slices.Equal(files[i].Touches, []string{"cmd/"}) {
		t.Errorf("MemoryFiles = %+v", files)
	}
}

// TestNoteTemplateInMemoryTab: the Memory tab's t shows tm's own note
// template.
func TestNoteTemplateInMemoryTab(t *testing.T) {
	src, err := os.ReadFile("../../examples/mods/memory-tab/main.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "const TEMPLATE = `"+NoteTemplate+"`;") {
		t.Error("examples/mods/memory-tab/main.ts's TEMPLATE isn't NoteTemplate: copy it over and run go generate ./internal/mod")
	}
}
