package tasks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// Where the board is kept in a project folder. Nothing outside this
// package names these files: callers that stat, guard, scan or relink
// the board ask for its paths here, so the store can move (T727) by
// changing this package alone.
const (
	boardRel   = "TASKS.md"
	archiveRel = "tasks/ARCHIVE.md"
	tasksDir   = "tasks"
)

// Rels lists the files the board is kept in, the live board first then
// the archive, slash-separated and relative to the project folder.
func Rels() []string { return []string{boardRel, archiveRel} }

// Paths is Rels below the project folder dir.
func Paths(dir string) []string {
	var paths []string
	for _, rel := range Rels() {
		paths = append(paths, filepath.Join(dir, filepath.FromSlash(rel)))
	}
	return paths
}

// BoardPaths lists the files the live board (no archive) is kept in
// below dir: what a watcher of the open tasks stats.
func BoardPaths(dir string) []string {
	return []string{filepath.Join(dir, filepath.FromSlash(boardRel))}
}

// BoardNote is the live board's note in the vault, for a wikilink
// below the project's folder ([[<slug>/<BoardNote>|board]]).
func BoardNote() string { return strings.TrimSuffix(boardRel, ".md") }

// Owned is what of the board only tm writes below dir, as absolute
// paths: folders and single files with the lock files tm takes on them
// (project.Owned, the guard's floor).
func Owned(dir string) (dirs, files []string) {
	board := filepath.Join(dir, filepath.FromSlash(boardRel))
	return []string{filepath.Join(dir, tasksDir)}, []string{board, mdfile.LockPath(board)}
}

// Scaffold makes an empty board in the new project folder dir. A fresh
// folder has no other writers yet: no lock.
func Scaffold(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, tasksDir), 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(dir, filepath.FromSlash(boardRel)), (&Board{NextID: 1}).Render(), 0o644)
}

// LinkLine reports whether line of a board file is tm's: a task's
// metadata line, where tm writes links. The rest is someone's words,
// which a rename doesn't rewrite.
func LinkLine(line string) bool { return strings.HasPrefix(line, "status:") }
