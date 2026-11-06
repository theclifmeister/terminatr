package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestLibrary: tm library list shows every thread's files by name and
// size (--json too, no path in it), rm deletes one file or all of a
// thread's (journaled), and a thread may do neither.
func TestLibrary(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	r, _ := thread.Create(p, thread.Record{Title: "Research", Task: "T5", State: thread.Running})
	os.MkdirAll(thread.Path(p, r.ID, "library"), 0o755)
	os.WriteFile(thread.Path(p, r.ID, "library", "notes.md"), []byte("# Notes\n"), 0o644)
	os.WriteFile(thread.Path(p, r.ID, "library", "more.txt"), []byte("more"), 0o644)

	out := h.ok(coord, "library", "list", "--project", "demo")
	for _, want := range []string{"T5", "t-0001", "notes.md", "8 bytes", "more.txt", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "threads/") {
		t.Errorf("list shows a path:\n%s", out)
	}
	var files []thread.LibFile
	if err := json.Unmarshal([]byte(h.ok(human, "library", "list", "--json", "--project", "demo")), &files); err != nil || len(files) != 2 {
		t.Fatalf("json: %v %+v", err, files)
	}

	h.expect(1, "coordinator-only", thr, "library", "list", "--project", "demo")
	h.expect(1, "coordinator-only", thr, "library", "rm", "t-0001", "--all", "--project", "demo")
	h.expect(2, "", coord, "library", "rm", "t-0001", "--project", "demo")
	h.expect(1, "invalid-file", coord, "library", "rm", "t-0001", "../thread.toml", "--project", "demo")
	h.expect(1, "unknown-file", coord, "library", "rm", "t-0001", "nope.txt", "--project", "demo")

	h.ok(coord, "library", "rm", "t-0001", "notes.md", "--project", "demo")
	if out := h.ok(coord, "library", "list", "--project", "demo"); strings.Contains(out, "notes.md") || !strings.Contains(out, "more.txt") {
		t.Fatalf("after rm:\n%s", out)
	}
	h.ok(human, "library", "rm", "t-0001", "--all", "--project", "demo")
	if out := h.ok(coord, "library", "list", "--project", "demo"); out != "" {
		t.Fatalf("after rm --all:\n%s", out)
	}
	j, _ := os.ReadFile(p.Path("JOURNAL.md"))
	for _, want := range []string{"coordinator", "library.rm t-0001 notes.md", "library.rm t-0001 more.txt"} {
		if !strings.Contains(string(j), want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}

// TestLibraryAdd: a coordinator keeps a file in its own project's library,
// by the rules of a report's attachment; a file of that name is replaced;
// it lists as thread seat, and its copy never reaches through a link. A
// thread, and another project's coordinator, may not. rm stays as it is:
// the coordinator's on the user's word, and the human's.
func TestLibraryAdd(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "web")
	p, _ := project.Open("demo")
	h.env[caller.EnvProject] = "demo"
	demoCoord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	page := p.Path("site.html")
	if err := os.WriteFile(page, []byte("<h1>Site</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := h.ok(demoCoord, "library", "add", page); !strings.Contains(out, "site.html") || !strings.Contains(out, "13 bytes") {
		t.Fatalf("add: %s", out)
	}
	if got, err := os.ReadFile(p.Path("library", "site.html")); err != nil || string(got) != "<h1>Site</h1>" {
		t.Fatalf("copied: %q %v", got, err)
	}
	if err := os.WriteFile(page, []byte("<h1>Site two</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ok(demoCoord, "library", "add", page)
	if got, _ := os.ReadFile(p.Path("library", "site.html")); string(got) != "<h1>Site two</h1>" {
		t.Fatalf("not replaced: %q", got)
	}
	out := h.ok(demoCoord, "library", "list")
	if !strings.Contains(out, "seat  site.html  17 bytes") || strings.Contains(out, p.Dir) {
		t.Fatalf("list: %s", out)
	}

	// Its own only; a thread keeps its own.
	h.expect(1, "other-project", demoCoord, "library", "add", page, "--project", "web")
	h.expect(1, "coordinator-only", thr, "library", "add", page)
	h.expect(1, "other-program", demoCoord, "library", "add", page, "--program", "acme")
	h.expect(2, "", demoCoord, "library", "add")
	// Not a file it can't name, nor its own library file onto itself.
	prev := tempRoots
	tempRoots = func(*Env) ([]string, []string, bool) { return nil, nil, false }
	t.Cleanup(func() { tempRoots = prev })
	secret := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(secret, []byte("x"), 0o644)
	h.expect(1, "invalid-attachment", demoCoord, "library", "add", secret)
	h.expect(1, "invalid-attachment", demoCoord, "library", "add", p.Path("library", "site.html"))
	if got, _ := os.ReadFile(p.Path("library", "site.html")); len(got) == 0 {
		t.Fatal("adding a library file onto itself emptied it")
	}
	// Names as an attachment's: one line of plain text, nothing hidden.
	hidden := p.Path(".env")
	os.WriteFile(hidden, []byte("x"), 0o644)
	h.expect(1, "invalid-file", demoCoord, "library", "add", hidden)
	bad := p.Path("two\nlines.txt")
	os.WriteFile(bad, []byte("x"), 0o644)
	h.expect(1, "invalid-attachment", demoCoord, "library", "add", bad)

	// A link planted where the copy goes is refused, and its target stays.
	target := filepath.Join(t.TempDir(), "kept.txt")
	os.WriteFile(target, []byte("kept"), 0o644)
	if err := os.Symlink(target, p.Path("library", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	linked := p.Path("linked.txt")
	os.WriteFile(linked, []byte("new"), 0o644)
	if code, _, _ := h.run(demoCoord, "", "library", "add", linked); code == 0 {
		t.Fatal("copied through a link")
	}
	if got, _ := os.ReadFile(target); string(got) != "kept" {
		t.Fatalf("the link's target changed: %q", got)
	}
	os.Remove(p.Path("library", "linked.txt"))

	// rm by the id it lists under, journaled both ways.
	h.expect(1, "unknown-file", demoCoord, "library", "rm", "seat", "nope.txt")
	h.expect(1, "invalid-file", demoCoord, "library", "rm", "seat", "../PROJECT.md")
	h.ok(demoCoord, "library", "rm", "seat", "site.html")
	if out := h.ok(demoCoord, "library", "list"); out != "" {
		t.Fatalf("after rm: %s", out)
	}
	j, _ := os.ReadFile(p.Path("JOURNAL.md"))
	for _, want := range []string{"library.add seat site.html", "library.rm seat site.html"} {
		if !strings.Contains(string(j), want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}

// TestLibraryProgram: a director keeps files in its own program's library
// and lists them with its members'; it adds to no other, and only the user
// deletes them. All of it refused programs-off while programs are off, the
// project forms not.
func TestLibraryProgram(t *testing.T) {
	h, g := programHarness(t)
	h.env[caller.EnvProject] = "acme"
	page := g.Path("site.html")
	if err := os.WriteFile(page, []byte("<h1>Site</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ok(directorAcme, "library", "add", page)
	if _, err := os.Stat(g.Path("library", "site.html")); err != nil {
		t.Fatal(err)
	}
	member, _ := project.Open("demo")
	r, _ := thread.Create(member, thread.Record{Title: "Research", Task: "T5", State: thread.Running})
	os.MkdirAll(thread.Path(member, r.ID, "library"), 0o755)
	os.WriteFile(thread.Path(member, r.ID, "library", "notes.md"), []byte("# Notes\n"), 0o644)

	out := h.ok(directorAcme, "library", "list")
	for _, want := range []string{"acme  -  seat  site.html", "demo  T5  " + r.ID + "  notes.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	var files []thread.LibFile
	if err := json.Unmarshal([]byte(h.ok(human, "library", "list", "--program", "acme", "--json")), &files); err != nil || len(files) != 2 {
		t.Fatalf("json: %v %+v", err, files)
	}
	if files[0].Name != "site.html" || files[0].Project != "" || files[1].Project != "demo" {
		t.Fatalf("own first, then the member's: %+v", files)
	}

	h.expect(1, "other-program", directorAcme, "library", "add", page, "--program", "beta")
	h.expect(1, "other-project", directorAcme, "library", "add", page, "--project", "demo")
	h.expect(1, "human-only", directorAcme, "library", "rm", "seat", "site.html")
	h.ok(human, "library", "rm", "seat", "site.html", "--program", "acme")
	if _, err := os.Stat(g.Path("library", "site.html")); err == nil {
		t.Fatal("still there")
	}

	writeTestConfig(t, h, "")
	h.expect(1, "programs-off", directorAcme, "library", "add", page)
	h.expect(1, "programs-off", directorAcme, "library", "list")
	h.expect(1, "programs-off", human, "library", "list", "--program", "acme")
	h.env[caller.EnvProject] = "demo"
	h.ok(caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "library", "list")
}

// TestLibraryAddSources: what a seat may keep is what it may name: a file of
// its own folder or its own scratch folder, not another's, not tm's settings,
// not through a link.
func TestLibraryAddSources(t *testing.T) {
	h, g := programHarness(t)
	tmp := t.TempDir()
	prev := tempRoots
	tempRoots = func(*Env) ([]string, []string, bool) { return []string{tmp}, nil, true }
	t.Cleanup(func() { tempRoots = prev })
	member, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	scratch := func(dir string) string {
		t.Helper()
		pad := filepath.Join(tmp, scratchName(dir), "sess", "scratchpad")
		if err := os.MkdirAll(pad, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(pad, "page.html")
		if err := os.WriteFile(f, []byte("<p>page</p>"), 0o644); err != nil {
			t.Fatal(err)
		}
		return f
	}
	config := filepath.Join(h.root, "config.toml")

	h.env[caller.EnvProject] = "acme"
	h.ok(directorAcme, "library", "add", scratch(g.Dir))
	h.expect(1, "invalid-attachment", directorAcme, "library", "add", scratch(member.Dir))
	h.expect(1, "invalid-attachment", directorAcme, "library", "add", config)
	link := g.Path("linked.toml")
	if err := os.Symlink(config, link); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "invalid-attachment", directorAcme, "library", "add", link)

	coordDemo := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	h.env[caller.EnvProject] = "demo"
	h.ok(coordDemo, "library", "add", scratch(member.Dir))
	h.expect(1, "invalid-attachment", coordDemo, "library", "add", scratch(g.Dir))
	h.expect(1, "invalid-attachment", coordDemo, "library", "add", config)
}
