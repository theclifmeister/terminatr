package vault

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestMigrate (T716): the first start rewrites each live file's TOML
// front matter as YAML, keeps the originals in a tarball, journals the
// count and leaves the mark; what tm reads is the same before and after,
// and a second run changes nothing.
func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	pdir := filepath.Join(dir, "projects", "demo")
	p := &project.Project{Slug: "demo", Dir: pdir}
	open, _ := thread.Create(p, thread.Record{Title: "open", State: thread.Running})
	gone, _ := thread.Create(p, thread.Record{Title: "gone", State: thread.Resolved})
	files := map[string]string{
		"PROJECT.md":                        "+++\ngoal = \"Ship it\"\ncreated = 2026-01-02T03:04:05Z\n+++\n# Instructions\n",
		"TASKS.md":                          "+++\nnext_id = 716\n+++\n# Tasks\n\n## Backlog\n\n### T715 Read YAML\nstatus: open\n",
		"tasks/ARCHIVE.md":                  "+++\nnext_id = 3\n+++\n# Archive\n",
		"memory/ci.md":                      "+++\n# guide line\ntitle    = \"CI\"\ntouches  = [\"internal/e2e/\"]\nstanding = true\n+++\n## Decided\n- x\n",
		"memory/free.md":                    "# Free-form\n",
		"memory/broken.md":                  "+++\ntitle = \n+++\n",
		"memory/yaml.md":                    "---\ntitle: \"Y\"\n---\n",
		"inbox/a.md":                        "+++\nid = \"a\"\nkind = \"report\"\nsubject = \"t-0001\"\ncreated = 2026-10-04T12:00:00Z\nsummary = \"yes\"\nneeds_user = false\n+++\n",
		"inbox/done/b.md":                   "+++\nid = \"b\"\n+++\n",
		"messages/other/M-1.md":             "+++\nid = \"M-1\"\nout = true\ncreated = 2026-10-04T12:00:00Z\nbytes = 3\nstate = \"read\"\nby = \"coordinator\"\n+++\nhi\n",
		"threads/" + open.ID + "/STATUS.md": "+++\npercent = 50\nupdated = 2026-10-04T12:00:00Z\n+++\n",
		"threads/" + gone.ID + "/STATUS.md": "+++\npercent = 100\n+++\n",
	}
	for rel, s := range files {
		path := filepath.Join(pdir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	beforeNotes, _ := before.Notes()

	check, err := Check()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range check {
		names = append(names, f.Rel)
		if (f.Err != nil) != (f.Rel == "memory/broken.md") {
			t.Errorf("%s: %v", f.Rel, f.Err)
		}
	}
	want := "PROJECT.md TASKS.md tasks/ARCHIVE.md memory/broken.md memory/ci.md inbox/a.md messages/other/M-1.md threads/" + open.ID + "/STATUS.md"
	if strings.Join(names, " ") != want {
		t.Fatalf("Check lists %v, want %s", names, want)
	}
	if Migrated() {
		t.Fatal("marked by Check")
	}

	at := time.Date(2026, 10, 24, 18, 12, 56, 0, time.UTC)
	n, err := Migrate(at)
	if n != 7 || err == nil || !strings.Contains(err.Error(), "memory/broken.md") {
		t.Fatalf("Migrate = %d, %v", n, err)
	}
	if !Migrated() {
		t.Fatal("no mark")
	}
	for rel, s := range files {
		data, _ := os.ReadFile(filepath.Join(pdir, filepath.FromSlash(rel)))
		changed := string(data) != s
		left := rel == "memory/free.md" || rel == "memory/broken.md" || rel == "memory/yaml.md" || rel == "inbox/done/b.md" || strings.Contains(rel, gone.ID)
		if changed == left || (changed && !strings.HasPrefix(string(data), "---\n")) {
			t.Errorf("%s:\n%s", rel, data)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(pdir, "memory", "ci.md")); string(data) != "---\n# guide line\ntitle: \"CI\"\ntouches:\n  - \"internal/e2e/\"\nstanding: true\n---\n## Decided\n- x\n" {
		t.Errorf("memory/ci.md:\n%s", data)
	}

	after, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	afterNotes, _ := after.Notes()
	if after.Meta.Goal != before.Meta.Goal || !after.Meta.Created.Equal(before.Meta.Created) || len(afterNotes) != len(beforeNotes) {
		t.Fatalf("project reads %+v, was %+v", after.Meta, before.Meta)
	}
	for i := range afterNotes {
		a, b := afterNotes[i], beforeNotes[i]
		if a.Title != b.Title || a.Standing != b.Standing || a.Body != b.Body || a.Front != b.Front {
			t.Errorf("note %s reads %+v, was %+v", a.Path, a, b)
		}
	}
	board, _ := os.ReadFile(filepath.Join(pdir, "TASKS.md"))
	if b, err := tasks.Parse(board); err != nil || b.NextID != 716 || len(b.Tasks) != 1 {
		t.Fatalf("board: %v %+v", err, b)
	}
	if items, _ := after.Inbox(); len(items) != 1 || items[0].Summary != "yes" || items[0].Subject != "t-0001" {
		t.Fatalf("inbox %+v", items)
	}

	tarball := filepath.Join(dir, "state", "migrate", "front-yaml-20261024T181256Z.tar.gz")
	f, err := os.Open(tarball)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, _ := gzip.NewReader(f)
	tr := tar.NewReader(zr)
	held := map[string]string{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(tr)
		held[h.Name] = string(b)
	}
	if len(held) != 8 || held["demo/TASKS.md"] != files["TASKS.md"] || held["demo/memory/broken.md"] != files["memory/broken.md"] {
		t.Fatalf("tarball holds %v", held)
	}
	journal, _ := os.ReadFile(filepath.Join(pdir, "JOURNAL.md"))
	if !strings.Contains(string(journal), "ticker vault.migrate demo 7 files to YAML front matter, originals in state/migrate/front-yaml-20261024T181256Z.tar.gz") {
		t.Fatalf("journal:\n%s", journal)
	}

	// Once marked, never again: a TOML file written later stays.
	os.WriteFile(filepath.Join(pdir, "CONTEXT.md"), []byte("+++\na = 1\n+++\n"), 0o644)
	if n, err := Migrate(at.Add(time.Hour)); n != 0 || err != nil {
		t.Fatalf("second Migrate = %d, %v", n, err)
	}
	if data, _ := os.ReadFile(filepath.Join(pdir, "CONTEXT.md")); !strings.HasPrefix(string(data), "+++") {
		t.Fatal("rewritten after the mark")
	}
}

// TestMigrateEmpty: a home with no projects gets the mark and no
// tarball.
func TestMigrateEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	if n, err := Migrate(time.Now()); n != 0 || err != nil || !Migrated() {
		t.Fatalf("Migrate = %d, %v, marked %v", n, err, Migrated())
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "migrate")); err == nil {
		t.Fatal("a backup folder for nothing")
	}
}
