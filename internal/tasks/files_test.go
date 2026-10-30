package tasks

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestFiles: where the board is kept, which only this package names
// (T871); the names on disk are as before.
func TestFiles(t *testing.T) {
	dir := t.TempDir()
	if got := Rels(); !slices.Equal(got, []string{"TASKS.md", "tasks/ARCHIVE.md"}) {
		t.Fatalf("Rels %q", got)
	}
	board, archive := filepath.Join(dir, "TASKS.md"), filepath.Join(dir, "tasks", "ARCHIVE.md")
	if got := Paths(dir); !slices.Equal(got, []string{board, archive}) {
		t.Fatalf("Paths %q", got)
	}
	if got := BoardPaths(dir); !slices.Equal(got, []string{board}) {
		t.Fatalf("BoardPaths %q", got)
	}
	s := &Store{Dir: dir}
	if s.TasksPath() != board || s.ArchivePath() != archive {
		t.Fatalf("store paths %s %s", s.TasksPath(), s.ArchivePath())
	}
	if BoardNote() != "TASKS" {
		t.Fatalf("BoardNote %q", BoardNote())
	}
	dirs, files := Owned(dir)
	if !slices.Equal(dirs, []string{filepath.Join(dir, "tasks")}) || !slices.Equal(files, []string{board, filepath.Join(dir, ".TASKS.md.lock")}) {
		t.Fatalf("Owned %q %q", dirs, files)
	}
	if !LinkLine("status: ready · thread: [[x/threads/t-1|t-1]]\n") || LinkLine("notes [[x/threads/t-1|t-1]]\n") {
		t.Fatal("LinkLine")
	}
}

func TestScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Scaffold(dir); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "tasks")); err != nil || !fi.IsDir() {
		t.Fatalf("tasks/: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "TASKS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string((&Board{NextID: 1}).Render()) {
		t.Fatalf("board:\n%s", data)
	}
	b, err := (&Store{Dir: dir}).Load()
	if err != nil || b.NextID != 1 || len(b.Tasks) != 0 {
		t.Fatalf("load %+v %v", b, err)
	}
}
