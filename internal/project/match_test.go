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
	got := Terms("Why is the GH poller's retry at 4? Retries, gh-poller, polling 2026-10-17 T12 go")
	want := []string{"poller", "retri", "poll", "t12"}
	if !slices.Equal(got, want) {
		t.Errorf("Terms = %q, want %q", got, want)
	}
	for _, q := range []string{"the of and is to what when", "2026 10 17 4.2", "go tm a T5 gh", "it isn't what we'd use"} {
		if got := Terms(q); len(got) != 0 {
			t.Errorf("Terms(%q) = %q, want none", q, got)
		}
	}
}

func TestMatch(t *testing.T) {
	notes := []Note{
		ParseNote("memory/retries.md", "+++\ntitle = \"Retries stay at 4 for the gh poller\"\ntouches = [\"internal/ticker/pr.go\", \"repo:api\"]\n+++\n## Decided\nKeep retries at 4; gh rate limits.\n"),
		ParseNote("memory/sweep.md", "# Sweep interval\n\nThe ticker sweeps every 15 s; retries multiply it.\n"),
		ParseNote("memory/ui.md", "+++\ntitle = \"Popup tabs\"\ntouches = [\"internal/tui/\"]\n+++\nTabs draw from mods.\n"),
		ParseNote("memory/notes.md", "# Misc\n\nretries once.\n"),
		ParseNote("memory/theme.md", "# Themes\n\nColours come from the theme mod; light and dark.\n"),
		ParseNote("memory/relay.md", "# Relay\n\nThe relay pins its key; pairing again after a new key.\n"),
		ParseNote("memory/vms.md", "# Test VMs\n\nOne thread holds the VM lock at a time.\n"),
	}
	for _, c := range []struct {
		name  string
		query string
		paths []string
		limit int
		want  []string
	}{
		{"title word clears alone", "poller", nil, 0, []string{"retries"}},
		{"stems meet", "polling retried", nil, 0, []string{"retries"}},
		{"one body word of two doesn't clear", "multiply colours", nil, 0, nil},
		{"two body words clear", "sweeps multiply", nil, 0, []string{"sweep"}},
		{"a word many notes hold: the title first, a long body not at all", "retries", nil, 0, []string{"retries", "notes"}},
		{"a rare word clears in a body", "pairing", nil, 0, []string{"relay"}},
		{"shared file", "", []string{"internal/ticker/pr.go"}, 0, []string{"retries"}},
		{"file under a touched folder", "", []string{"internal/tui/popup.go"}, 0, []string{"ui"}},
		{"folder holding a touched file", "", []string{"internal/ticker"}, 0, []string{"retries"}},
		{"sibling path doesn't meet", "", []string{"internal/tick"}, 0, nil},
		{"repo entry", "", []string{"repo:api"}, 0, []string{"retries"}},
		{"repo entry is not a path", "", []string{"repo"}, 0, nil},
		{"a shared path outranks words", "sweeps multiply ticker", []string{"./internal/tui/"}, 0, []string{"ui", "sweep"}},
		{"limit keeps the best", "retries ticker sweeps", nil, 1, []string{"sweep"}},
		{"stop words never match", "the of and", nil, 0, nil},
		{"numbers and short words never match", "4 15 s gh", nil, 0, nil},
		{"nothing asked", "", nil, 0, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			hits := Match(notes, c.query, c.paths, c.limit)
			if got := hitNames(hits); !slices.Equal(got, c.want) {
				t.Fatalf("Match(%q, %q) = %q, want %q", c.query, c.paths, got, c.want)
			}
		})
	}
	h := Match(notes, "poller retrying", []string{"repo:api"}, 0)
	if len(h) != 1 || !slices.Equal(h[0].Terms, []string{"poller", "retrying"}) || !slices.Equal(h[0].Paths, []string{"repo:api"}) || h[0].Score <= ScorePath {
		t.Errorf("hit = %+v, want the query's own words, the path and a score above %d", h, ScorePath)
	}
}

func hitNames(hs []Hit) []string {
	var out []string
	for _, h := range hs {
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(h.Note.Path, "memory/"), ".md"))
	}
	return out
}

// fixtureNotes are testdata/notes, a memory shaped like a real
// project's: front matter with sections, a free-form note with YAML
// front matter, two standing notes and one for the coordinator only.
func fixtureNotes(t *testing.T) []Note {
	t.Helper()
	ents, err := os.ReadDir("testdata/notes")
	if err != nil {
		t.Fatal(err)
	}
	var notes []Note
	for _, e := range ents {
		data, err := os.ReadFile("testdata/notes/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		notes = append(notes, ParseNote("memory/"+e.Name(), string(data)))
	}
	return notes
}

// TestMatchRanking runs realistic queries, a task's words as tm memory
// find or a brief has them, against fixtureNotes: the note that bears
// on them ranks first, and words every note has, stop words, numbers
// and short words match nothing.
func TestMatchRanking(t *testing.T) {
	notes := fixtureNotes(t)
	for _, c := range []struct {
		query string
		paths []string
		first string
		none  bool
	}{
		{query: "VM lock for the Windows tests", first: "test-vms"},
		{query: "prlctl exec PATH", first: "test-vms"},
		{query: "Bump the pinned Zig to 0.16", first: "research-platform"},
		{query: "release-please formula checksums", first: "decisions-release"},
		{query: "Azure DevOps pull requests through az rest", first: "code-hosts"},
		{query: "sidebar goldens changed after the popup fix", first: "gotchas-tui"},
		{query: "goja mod host memory limit", first: "mods-runtime"},
		{query: "flaky e2e test on macOS runners", first: "ci-and-flakes"},
		{query: "Codex sandbox and approvals", first: "research-codex"},
		{query: "restart the relay systemd service", first: "relay-vps"},
		{query: "force-push refused by the guard", first: "gotchas-git-guard"},
		{query: "merge batches", first: "coordinator-merges"},
		{query: "Windows: weekly CI job (unit + e2e smoke)\nRun the unit tests and an e2e smoke on a Windows runner once a week; " +
			"macOS and Linux jobs stay as they are. Report a failure as an issue, never retry the workflow.\n" +
			"- [ ] Workflow with the Windows job\n- [ ] Smoke test list\n- [ ] Docs", first: "ci-and-flakes"},
		{query: "Theme: feed the console's light/dark report into setTheme", paths: []string{"internal/tui/theme.go"}, first: "gotchas-tui"},
		{query: "Codex sandbox and approvals", paths: []string{"repo:homebrew-tap"}, first: "decisions-release"},
		{query: "the of and is to what when where which", none: true},
		{query: "2026 10 17 0.32.0 5 s 256", none: true},
		{query: "go tm a T5 gh CI PR id", none: true},
		{query: "nothing here is about kubernetes", none: true},
	} {
		t.Run(c.query, func(t *testing.T) {
			hits := Match(notes, c.query, c.paths, 5)
			got := hitNames(hits)
			if c.none {
				if len(got) > 0 {
					t.Errorf("Match = %q, want none", got)
				}
				return
			}
			if len(got) == 0 || got[0] != c.first {
				t.Errorf("Match = %q, want %s first", got, c.first)
			}
		})
	}
}

func TestAudience(t *testing.T) {
	notes := fixtureNotes(t)
	for _, n := range notes {
		if want := n.Path != "memory/coordinator-merges.md"; n.ForThreads() != want {
			t.Errorf("%s ForThreads = %v, want %v", n.Path, n.ForThreads(), want)
		}
	}
	if got := hitNames(Match(ThreadNotes(slices.Clone(notes)), "merge batches", nil, 0)); slices.Contains(got, "coordinator-merges") {
		t.Errorf("a thread's match = %q, has the coordinator's note", got)
	}
	for audience, threads := range map[string]bool{"": true, "everyone": true, " Everyone ": true, "coordinator": false, "Coordinator": false, "coordinators": false} {
		if got := (Note{NoteFront: NoteFront{Audience: audience}}).ForThreads(); got != threads {
			t.Errorf("audience %q: ForThreads = %v, want %v", audience, got, threads)
		}
	}
	if n := ParseNote("memory/t.md", NoteTemplate); !n.ForThreads() || n.Audience != "everyone" {
		t.Errorf("NoteTemplate's audience = %q", n.Audience)
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
