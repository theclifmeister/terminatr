package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// TestFolderLink (T873): init links the vault, never overwrites, records
// only a link it made; off removes it only while it is still ours; doctor
// reports it gone or broken. All inside a temp home.
func TestFolderLink(t *testing.T) {
	dir, _ := yamlHome(t)
	root := filepath.Join(dir, "projects")
	human := caller.Caller{Kind: caller.Human}
	run := func(path string) *Plan {
		t.Helper()
		pl, err := NewPlan()
		if err != nil {
			t.Fatal(err)
		}
		pl.PlanLink(path)
		if _, err := Init(pl, human, time.Now()); err != nil {
			t.Fatal(err)
		}
		return pl
	}

	// Never overwritten: a file, a folder, a link elsewhere.
	file := filepath.Join(dir, "afile")
	os.WriteFile(file, []byte("x"), 0o644)
	folder := filepath.Join(dir, "afolder")
	os.Mkdir(folder, 0o755)
	elsewhere := filepath.Join(dir, "elsewhere")
	os.Symlink(folder, elsewhere)
	for path, why := range map[string]string{file: "a file", folder: "a folder", elsewhere: "a link to somewhere else"} {
		pl := run(path)
		if lp := pl.Link(); lp.What != "kept" || lp.Why != why {
			t.Errorf("%s: %+v", path, lp)
		}
		if RecordedLink() != "" {
			t.Errorf("%s: recorded", path)
		}
	}
	if b, _ := os.ReadFile(file); string(b) != "x" {
		t.Error("file overwritten")
	}

	link := filepath.Join(dir, "terminatr-obsidian")
	if pl := run(link); pl.Link().What != "new" || RecordedLink() != link {
		t.Fatalf("new: %+v, recorded %q", pl.Link(), RecordedLink())
	}
	if !pointsAtVault(link, root) {
		t.Fatal("link doesn't lead to the vault")
	}
	if pl := run(link); pl.Link().What != "ours" || RecordedLink() != link {
		t.Errorf("again: %+v", pl.Link())
	}
	if f := finding(Health(), "folder link"); f.Status != "ok" {
		t.Errorf("doctor: %+v", f)
	}

	os.Remove(link)
	if f := finding(Health(), "folder link"); f.Status != "warn" {
		t.Errorf("gone: %+v", f)
	}
	os.Symlink(folder, link)
	if f := finding(Health(), "folder link"); f.Status != "warn" {
		t.Errorf("elsewhere: %+v", f)
	}
	if _, err := Off(human); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != folder {
		t.Errorf("off removed or changed a link that is no longer ours: %q", got)
	}
	if RecordedLink() != "" {
		t.Error("off keeps the record")
	}

	// Still ours: off removes it.
	os.Remove(link)
	run(link)
	if _, err := Off(human); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("off left our link")
	}
	if _, err := os.Stat(root); err != nil {
		t.Error("off touched the vault")
	}
}
