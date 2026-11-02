package project

import "path/filepath"

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
