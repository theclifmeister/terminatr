package project

import (
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
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
func (p *Folder) IsProgram() bool { return p.Kind == KindProgram }

// Path joins a name onto the folder.
func (p *Folder) Path(elem ...string) string {
	return filepath.Join(append([]string{p.Dir}, elem...)...)
}

// SharedOwned is what in the folder only tm writes, of the parts every
// Folder has (Project.Owned lists a project's, which adds threads and
// the role files): the inbox, journal archives, messages, uploads, the
// journal, questions.json and designs.json, with their lock files.
func (p *Folder) SharedOwned() (dirs, files []string) {
	for _, d := range []string{"inbox", "journal", "messages", "uploads"} {
		dirs = append(dirs, p.Path(d))
	}
	for _, name := range []string{"JOURNAL.md", questionsFile, designsFile} {
		files = append(files, p.Path(name), mdfile.LockPath(p.Path(name)))
	}
	return dirs, files
}

// LinkSlug is the slug the folder's vault links lead with: a project's
// slug, or a program's folder in the vault, _programs/<slug>
// (vaultlink.ProgramFolder), so a program's ids link to its own notes.
func (p *Folder) LinkSlug() string {
	if p.IsProgram() {
		return vaultlink.ProgramFolder(p.Slug)
	}
	return p.Slug
}
