package thread

import (
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func libFixture(t *testing.T) *project.Project {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func addLib(t *testing.T, p *project.Project, id, name, text string, at time.Time) {
	t.Helper()
	os.MkdirAll(Path(p, id, "library"), 0o755)
	f := Path(p, id, "library", name)
	if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(f, at, at)
}

// TestLibrary: every thread's files are listed, an archived (resolved)
// thread's from its tarball, newest first; a file reads, truncated at
// the cap; a name with a path is refused.
func TestLibrary(t *testing.T) {
	p := libFixture(t)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	a, _ := Create(p, Record{Title: "Old", Task: "T1", State: Resolved, ResolvedAt: day(1)})
	b, _ := Create(p, Record{Title: "New", Task: "T2", State: Running})
	addLib(t, p, a.ID, "notes.md", "# Notes\n", day(2))
	addLib(t, p, a.ID, "data.json", `{"a":1}`, day(3))
	addLib(t, p, b.ID, "shot.png", "png", day(5))
	if err := Archive(p, a); err != nil {
		t.Fatal(err)
	}
	files, err := Library(p)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Thread+"/"+f.Name+" "+f.State)
	}
	want := "t-0002/shot.png running,t-0001/data.json archived,t-0001/notes.md archived"
	if strings.Join(got, ",") != want {
		t.Fatalf("library %v, want %s", got, want)
	}
	if files[1].Task != "T1" || files[1].Title != "Old" || files[1].Size != 7 {
		t.Errorf("archived file: %+v", files[1])
	}
	data, trunc, err := ReadLibraryFile(p, a.ID, "data.json", 100)
	if err != nil || trunc || string(data) != `{"a":1}` {
		t.Fatalf("read archived: %q %v %v", data, trunc, err)
	}
	if data, trunc, _ := ReadLibraryFile(p, b.ID, "shot.png", 2); !trunc || string(data) != "pn" {
		t.Fatalf("read capped: %q %v", data, trunc)
	}
	for _, bad := range []string{"../thread.toml", "a/b", ".hidden", ""} {
		if _, _, err := ReadLibraryFile(p, b.ID, bad, 10); err == nil {
			t.Errorf("read %q", bad)
		}
	}
	if _, _, err := ReadLibraryFile(p, b.ID, "gone.txt", 10); err == nil {
		t.Error("read a missing file")
	}
}

// TestRemoveLibrary: a live thread's file, an archived thread's file and
// then all of a thread's files go, each journaled; a missing file is
// refused and nothing else is touched.
// TestLibraryRepo: a project with several repositories names each file's
// by its folder, live and archived, and never its path; with one, none.
func TestLibraryRepo(t *testing.T) {
	p := libFixture(t)
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a, _ := Create(p, Record{Title: "Old", State: Resolved, ResolvedAt: day, Repo: "/work/api"})
	b, _ := Create(p, Record{Title: "New", State: Running, Repo: "/work/site/"})
	c, _ := Create(p, Record{Title: "Plain", State: Running})
	addLib(t, p, a.ID, "old.md", "x", day)
	addLib(t, p, b.ID, "new.md", "x", day.Add(time.Hour))
	addLib(t, p, c.ID, "plain.md", "x", day.Add(2*time.Hour))
	if err := Archive(p, a); err != nil {
		t.Fatal(err)
	}
	repos := func() map[string]string {
		files, err := Library(p)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, f := range files {
			out[f.Name] = f.Repo
		}
		return out
	}
	p.Repos = []string{"/work/api"}
	if got := repos(); got["old.md"] != "" || got["new.md"] != "" {
		t.Errorf("one repo: %v", got)
	}
	p.Repos = []string{"/work/api", "/work/site"}
	if got := repos(); got["old.md"] != "api" || got["new.md"] != "site" || got["plain.md"] != "" {
		t.Errorf("several repos: %v", got)
	}
}

// TestLibraryTaskStatus: a file carries its task's status, a task on the
// board or in its archive and an archived thread's too; no task, or a task
// that's gone, carries none.
func TestLibraryTaskStatus(t *testing.T) {
	p := libFixture(t)
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	human := caller.Caller{Kind: caller.Human}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "On the board"}, {Title: "Archived thread"}, {Title: "Task archived"}}); err != nil {
		t.Fatal(err)
	}
	for id, st := range map[int]tasks.Status{1: tasks.Review, 2: tasks.Done, 3: tasks.Done} {
		if _, err := p.Tasks().SetStatus(human, id, st, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Tasks().Archive(human, 3); err != nil {
		t.Fatal(err)
	}
	a, _ := Create(p, Record{Title: "A", Task: "T1", State: Running})
	b, _ := Create(p, Record{Title: "B", Task: "T2", State: Resolved, ResolvedAt: day})
	c, _ := Create(p, Record{Title: "C", Task: "T3", State: Running})
	d, _ := Create(p, Record{Title: "D", State: Running})
	e, _ := Create(p, Record{Title: "E", Task: "T999", State: Running})
	for name, id := range map[string]string{"a.md": a.ID, "b.md": b.ID, "c.md": c.ID, "d.md": d.ID, "e.md": e.ID} {
		addLib(t, p, id, name, "x", day)
	}
	if err := Archive(p, b); err != nil {
		t.Fatal(err)
	}
	files, err := Library(p)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Name] = f.TaskStatus
	}
	want := map[string]string{"a.md": "review", "b.md": "done", "c.md": "done", "d.md": "", "e.md": ""}
	if !maps.Equal(got, want) {
		t.Errorf("statuses %v, want %v", got, want)
	}
}

func TestRemoveLibrary(t *testing.T) {
	p := libFixture(t)
	at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	live, _ := Create(p, Record{Title: "Live", State: Running})
	arch, _ := Create(p, Record{Title: "Arch", State: Resolved, ResolvedAt: at})
	addLib(t, p, live.ID, "a.txt", "a", at)
	addLib(t, p, live.ID, "b.txt", "b", at)
	addLib(t, p, arch.ID, "c.txt", "c", at)
	addLib(t, p, arch.ID, "d.txt", "d", at)
	if err := Archive(p, arch); err != nil {
		t.Fatal(err)
	}
	c := caller.Caller{Kind: caller.Human}

	if n, err := RemoveLibrary(p, c, live.ID, "a.txt"); n != 1 || err != nil {
		t.Fatalf("rm live: %d %v", n, err)
	}
	if n, err := RemoveLibrary(p, c, live.ID, "a.txt"); n != 0 || err == nil {
		t.Fatalf("rm again: %d %v", n, err)
	}
	if n, err := RemoveLibrary(p, c, arch.ID, "c.txt"); n != 1 || err != nil {
		t.Fatalf("rm archived: %d %v", n, err)
	}
	files, _ := Library(p)
	if len(files) != 2 {
		t.Fatalf("after two: %+v", files)
	}
	// The archive still loads, with the rest of the thread in it.
	if at, err := LoadArchived(p, arch.ID); err != nil || string(at.Files["library/d.txt"]) != "d" || at.Files["library/c.txt"] != nil {
		t.Fatalf("archive after rm: %v %v", err, at)
	}
	if n, err := RemoveLibrary(p, c, arch.ID, ""); n != 1 || err != nil {
		t.Fatalf("rm all archived: %d %v", n, err)
	}
	if n, err := RemoveLibrary(p, c, live.ID, ""); n != 1 || err != nil {
		t.Fatalf("rm all live: %d %v", n, err)
	}
	if files, _ := Library(p); len(files) != 0 {
		t.Fatalf("left: %+v", files)
	}
	if _, err := os.Stat(Dir(p, live.ID)); err != nil {
		t.Errorf("the thread's folder went: %v", err)
	}
	j, _ := os.ReadFile(p.Path("JOURNAL.md"))
	for _, want := range []string{"library.rm t-0001 a.txt", "library.rm t-0002 c.txt", "library.rm t-0002 d.txt", "library.rm t-0001 b.txt"} {
		if !strings.Contains(string(j), want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
	if _, err := RemoveLibrary(p, c, live.ID, "../x"); err == nil {
		t.Error("rm with a path")
	}
}

// TestReadLibraryChunk: a chunk from an offset, of a live thread's file
// and an archived one's, with the file's size and time; past the end, a
// bad name and a link are refused.
func TestReadLibraryChunk(t *testing.T) {
	p := libFixture(t)
	at := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	live, _ := Create(p, Record{Title: "Live", State: Running})
	arch, _ := Create(p, Record{Title: "Arch", State: Resolved, ResolvedAt: at})
	addLib(t, p, live.ID, "a.txt", "abcdef", at)
	addLib(t, p, arch.ID, "b.txt", "uvwxyz", at)
	if err := Archive(p, arch); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{live.ID, arch.ID} {
		name := map[bool]string{true: "a.txt", false: "b.txt"}[id == live.ID]
		data, size, mod, err := ReadLibraryChunk(p, id, name, 2, 3)
		if err != nil || len(data) != 3 || size != 6 || !mod.Equal(at) {
			t.Fatalf("%s: %q %d %v %v", id, data, size, mod, err)
		}
		if data, _, _, err := ReadLibraryChunk(p, id, name, 4, 10); err != nil || len(data) != 2 {
			t.Fatalf("%s at the end: %q %v", id, data, err)
		}
		if _, _, _, err := ReadLibraryChunk(p, id, name, 7, 1); err == nil {
			t.Errorf("%s: read past the end", id)
		}
	}
	if _, _, _, err := ReadLibraryChunk(p, live.ID, "../thread.toml", 0, 10); err == nil {
		t.Error("read a path")
	}
	os.WriteFile(p.Path("secret"), []byte("key"), 0o600)
	if err := os.Symlink(p.Path("secret"), Path(p, live.ID, "library", "link.txt")); err != nil {
		t.Fatal(err)
	}
	if data, _, _, err := ReadLibraryChunk(p, live.ID, "link.txt", 0, 10); err == nil {
		t.Errorf("read through a link: %q", data)
	}
}
