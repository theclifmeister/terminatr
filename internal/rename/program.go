package rename

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// tm program rename (the programs design §3.4, Rename and delete): a
// program's folder _programs/<from>/ moves to _programs/<to>/, its
// [programs.<from>] table becomes [programs.<to>], the links into it in
// every folder point at the new one (vaultlink.Rename with
// vaultlink.ProgramFolder), its members' channel folders messages/<from>/
// move, and its members' boards name it anew where tm wrote it: a
// director's owner (director:<from>), its note lines, and the umbrella
// refs <from>/T<n>. The members' own settings don't name a local
// program, so they don't change.
//
// The caller makes sure the program's director doesn't run (the server).
// Members on paired servers know the program by its name there
// (program = "<server>/<from>"): a program with one is refused unless
// the caller tells their servers (ProgramOptions.TellPaired, the
// program-rename peer kind).

// ProgramOptions say which program to rename.
type ProgramOptions struct {
	From, To string
	Caller   caller.Caller
	// Taken says why to can't name a program beyond what this package
	// checks: a paired server's alias or name, which only the caller
	// knows (§3.4). "" when it can; nil checks none.
	Taken func(slug string) string
	// MoveAgentDir carries the director's state kept by folder path, as
	// Options.MoveAgentDir.
	MoveAgentDir func(from, to string) error
	// TellPaired tells the servers of the program's paired members
	// (<alias>/<slug>, in the program's order) that it is called to now,
	// once the rename here stands: the program-rename peer message
	// (§9.2). It returns what didn't go through, as notes. nil refuses a
	// program with paired members (paired-members), so no paired side is
	// left naming a program that is gone.
	TellPaired func(from, to string, paired []string) []string
}

// ProgramResult is what a program rename did.
type ProgramResult struct {
	Dir   string // the program's folder now; "" when it had only a table
	Notes []string
}

// CheckProgram refuses a program rename that can't be done, before
// anything moves: an unknown program, an invalid or taken slug (a
// program, a project or a paired server has it: one namespace), a table
// tm can't rename, members on paired servers.
func CheckProgram(o ProgramOptions) error {
	oldDir, err := program.Dir(o.From)
	if err != nil {
		return err
	}
	cfg, _ := config.Load() // a safety error still gives the tables
	t, table := cfg.Program(o.From)
	_, statErr := os.Stat(oldDir)
	if !table && statErr != nil {
		return refuse("unknown-program", "no program %q", o.From)
	}
	newDir, err := program.Dir(o.To)
	if err != nil {
		return err
	}
	if o.To == o.From {
		return refuse("unchanged", "%s is already called %s; pass a new slug", o.From, o.To)
	}
	if _, ok := cfg.Program(o.To); ok {
		return refuse("program-exists", "a program %s exists already ([programs.%s] in config.toml)", o.To, o.To)
	}
	if _, err := os.Lstat(newDir); err == nil {
		return refuse("program-exists", "a program %s exists already (%s)", o.To, newDir)
	}
	if pd, err := project.Dir(o.To); err == nil {
		if _, err := os.Lstat(pd); err == nil {
			return refuse("slug-taken", "%s is a project", o.To)
		}
	}
	if o.Taken != nil {
		if why := o.Taken(o.To); why != "" {
			return refuse("slug-taken", "%s: %s", o.To, why)
		}
	}
	paired := pairedMembers(t.Projects)
	if len(paired) > 0 && o.TellPaired == nil {
		return refuse("paired-members", "%s has members on paired servers (%s), whose servers know it as %s: take them out of the program first, then rename it", o.From, strings.Join(paired, ", "), o.From)
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if data != nil {
		switch _, err := config.RenameProgramTable(data, o.From, o.To); {
		case errors.Is(err, config.ErrProgramSet):
			return refuse("program-exists", "%s already has settings for %s ([programs.%s]); remove them first", path, o.To, o.To)
		case errors.Is(err, config.ErrForm):
			return refuse("settings-form", "%s sets %s's settings in a form tm can't rename (dotted keys or an inline table): make them a [programs.%s] table first", path, o.From, o.From)
		case err != nil:
			return err
		}
	}
	return nil
}

// Program renames program From to To. It checks first (CheckProgram);
// an error means nothing changed. Once the folder has moved the rename
// stands, and what didn't go through after that is in the result's
// Notes.
func Program(o ProgramOptions) (*ProgramResult, error) {
	if err := CheckProgram(o); err != nil {
		return nil, err
	}
	oldDir, _ := program.Dir(o.From)
	newDir, _ := program.Dir(o.To)
	cfg, _ := config.Load()
	before, _ := cfg.Program(o.From)
	paired := pairedMembers(before.Projects)
	res := &ProgramResult{}
	if _, err := os.Stat(oldDir); err == nil {
		if err := os.Rename(oldDir, newDir); err != nil {
			return nil, err
		}
		res.Dir = newDir
	}
	note := func(format string, a ...any) { res.Notes = append(res.Notes, fmt.Sprintf(format, a...)) }
	if err := config.RenameProgram(o.From, o.To); err != nil {
		if res.Dir == "" {
			return nil, err // nothing had moved
		}
		note("config.toml still has [programs.%s]: %v (rename it to [programs.%s] by hand)", o.From, err, o.To)
	}
	moveMessages(o.From, o.To, newDir, note)
	if res.Dir != "" && o.MoveAgentDir != nil {
		if err := o.MoveAgentDir(oldDir, newDir); err != nil {
			note("%s: agent state not carried over: %v", newDir, err)
		}
	}
	if n, err := relink(vaultlink.ProgramFolder(o.From), vaultlink.ProgramFolder(o.To), o.From, o.To); err != nil {
		note("links to %s not all renamed (%d files done): %v", o.From, n, err)
	}
	if len(paired) > 0 && o.TellPaired != nil {
		res.Notes = append(res.Notes, o.TellPaired(o.From, o.To, paired)...)
	}
	if res.Dir == "" {
		return res, nil
	}
	f := project.Folder{Slug: o.To, Dir: newDir, Kind: project.KindProgram}
	return res, f.Journal(o.Caller, "program.rename", o.To, o.From+" → "+o.To)
}

// pairedMembers are the members on paired servers (<alias>/<slug>).
func pairedMembers(members []string) []string {
	var out []string
	for _, m := range members {
		if alias, _, ok := config.SplitMember(m); ok && alias != "" {
			out = append(out, m)
		}
	}
	return out
}
