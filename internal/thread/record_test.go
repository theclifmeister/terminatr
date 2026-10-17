package thread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
)

// TestReportAttachRefusesLinks: an attachment whose place in library/,
// or library/ itself, is a link is refused, and what the link points at
// is left as it was.
func TestReportAttachRefusesLinks(t *testing.T) {
	p := libFixture(t)
	r, _ := Create(p, Record{Title: "A", State: Running})
	src := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(src, []byte("ssh-ed25519 AAAA evil\n"), 0o644)
	victim := filepath.Join(t.TempDir(), "authorized_keys")
	os.WriteFile(victim, []byte("keep\n"), 0o600)
	report := "## Report\nok\n## Next\nMerge\n"

	os.MkdirAll(Path(p, r.ID, "library"), 0o755)
	if err := os.Symlink(victim, Path(p, r.ID, "library", "notes.md")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := StoreReport(p, r.ID, report, []string{src}, time.Now()); err == nil {
		t.Error("file: copied through a link")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Errorf("file: victim %q", b)
	}

	elsewhere := t.TempDir()
	os.RemoveAll(Path(p, r.ID, "library"))
	os.Symlink(elsewhere, Path(p, r.ID, "library"))
	if _, err := StoreReport(p, r.ID, report, []string{src}, time.Now()); err == nil {
		t.Error("folder: copied through a link")
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Errorf("folder: wrote %v", ents)
	}
}

// TestLoadChecksRecord: a thread.toml whose paths aren't absolute, or
// whose branch or base reads as an option, is refused by Load and
// Update; a plain one loads.
func TestLoadChecksRecord(t *testing.T) {
	p := libFixture(t)
	r, _ := Create(p, Record{Title: "A", State: Running})
	write := func(line string) {
		data, _ := os.ReadFile(recordPath(p, r.ID))
		var out []string
		key, _, _ := strings.Cut(line, " =")
		for _, l := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(l, key+" =") {
				out = append(out, l)
			}
		}
		os.WriteFile(recordPath(p, r.ID), []byte(line+"\n"+strings.Join(out, "\n")), 0o644)
	}
	for _, bad := range []string{
		`branch = "-Bkeep"`, `branch = "a b"`, `base = "--orphan"`,
		`worktree = "rel/dir"`, `repo = "repo"`, "worktree = \"/a\\nb\"",
	} {
		write(bad)
		if _, err := Load(p, r.ID); err == nil {
			t.Errorf("%s: loaded", bad)
		}
		if _, err := Update(p, r.ID, func(*Record) error { return nil }); err == nil {
			t.Errorf("%s: updated", bad)
		}
		write(`branch = ""`)
		write(`base = ""`)
		write(`worktree = "/w"`)
		write(`repo = "/r"`)
	}
	write(`branch = "tm/demo/t-0001-a"`)
	if got, err := Load(p, r.ID); err != nil || got.Branch != "tm/demo/t-0001-a" {
		t.Fatalf("plain: %+v %v", got, err)
	}
}

// TestOwnWorktree: only a folder directly in <home>/worktrees/<slug>/
// is a worktree tm made for the project's thread.
func TestOwnWorktree(t *testing.T) {
	p := libFixture(t)
	root, _ := home.WorktreesDir()
	for dir, want := range map[string]bool{
		filepath.Join(root, "demo", "t-0001-a"):       true,
		filepath.Join(root, "demo"):                   false,
		filepath.Join(root, "demo", "t-0001-a", "x"):  false,
		filepath.Join(root, "other", "t-0001-a"):      false,
		filepath.Join(root, "demo", "..", "t-0001-a"): false,
		filepath.Join(t.TempDir(), "autostart"):       false,
		"":                                            false,
	} {
		if got := (&Record{Worktree: dir}).OwnWorktree(p.Slug); got != want {
			t.Errorf("%q: %v, want %v", dir, got, want)
		}
	}
}
