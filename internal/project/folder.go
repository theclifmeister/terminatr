package project

import (
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// Kind is what a Folder holds: a project or a program.
type Kind string

const (
	KindProject Kind = "project"
	KindProgram Kind = "program"
)

// Folder is the part of a project folder that a program folder shares
// (the programs design §3.4): its journal, inbox items, questions.json,
// memory with MEMORY.md upkeep, CONTEXT.md budgets, designs, uploads and
// messages/. Project embeds it and adds PROJECT.md, repos, the code host
// and threads; program.Program embeds it and adds PROGRAM.md, its members
// and the umbrella board. Only Dir, List, Open and New (and their program
// counterparts) resolve a slug to a folder: the rest take a Folder.
type Folder struct {
	Slug string
	Dir  string
	// Kind is KindProject or KindProgram; "" reads as a project.
	Kind Kind
}

// IsProgram reports whether the folder is a program's.
func (f *Folder) IsProgram() bool { return f.Kind == KindProgram }

// Path joins a name onto the folder.
func (f *Folder) Path(elem ...string) string {
	return filepath.Join(append([]string{f.Dir}, elem...)...)
}

// SharedOwned is what in the folder only tm writes, of the parts every
// Folder has (Project.Owned lists a project's, which adds threads and
// the role files): the inbox, journal archives, messages, uploads, the
// journal, questions.json and designs.json, with their lock files.
func (f *Folder) SharedOwned() (dirs, files []string) {
	for _, d := range []string{"inbox", "journal", "messages", "uploads"} {
		dirs = append(dirs, f.Path(d))
	}
	for _, name := range []string{"JOURNAL.md", questionsFile, designsFile} {
		files = append(files, f.Path(name), mdfile.LockPath(f.Path(name)))
	}
	return dirs, files
}

// linkSlug is the slug the folder's vault links name: the project's.
// A program's folder writes its ids as plain text for now ("" makes
// vaultlink give the alias alone): its links take the vault's
// _programs/<slug>/ form, which comes with the vault's program support.
func (f *Folder) linkSlug() string {
	if f.IsProgram() {
		return ""
	}
	return f.Slug
}
