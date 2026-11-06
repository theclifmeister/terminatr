package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
)

const libraryUsage = `usage: tm library add <file>... [--project <slug> | --program <slug>]
       tm library list [--project <slug> | --program <slug>] [--json]
       tm library rm <thread> <file> | --all [--project <slug>]
       tm library rm M-12 <file> [--project <slug>]
       tm library rm seat <file> | --all [--project <slug> | --program <slug>]

add copies files into the caller's own library: a coordinator's, in its
project's folder, or a director's, in its program's folder. It keeps pages
and files the user will want, and a thread keeps its own with tm report
--attach. A file of that name is replaced. The user finds them in the
Files view's Attached tab, and in the project popup's Library tab.

list shows the coordinator's own files (thread seat), the files every
thread of the project attached to its reports (its library), the resolved
and archived threads' too, and the files linked projects sent with their
messages, newest first, by name and size. A director's, or --program's,
shows the director's own files, then its members' on this server. rm
deletes one file of a thread, or all of its files with --all, or the file
a message brought; it is the coordinator's, and only on the user's word
in chat. A program's own files are the user's alone to delete.`

// runLibrary keeps a file in the caller's own library, lists the
// project's or program's library files, or deletes them (docs/SPEC.md
// §7.2, Library).
func runLibrary(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", libraryUsage)
	}
	f := newFlags()
	slug, prog, asJSON, all := f.String("project"), f.String("program"), f.Bool("json"), f.Bool("all")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	if e.Caller.Kind == caller.Thread {
		if args[0] == "add" {
			return &project.Error{Code: "coordinator-only", Msg: "a thread keeps its own files: tm report --attach FILE"}
		}
		return &project.Error{Code: "coordinator-only", Msg: "the library is the coordinator's to list and clean up"}
	}
	if *slug != "" && *prog != "" {
		return usagef("%s", libraryUsage)
	}
	g, err := e.libraryProgram(*slug, *prog)
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if len(pos) == 0 {
			return usagef("%s", libraryUsage)
		}
		return e.libraryAdd(*slug, g, pos)
	case "list", "ls":
		if g != nil {
			if len(pos) > 0 {
				return usagef("%s", libraryUsage)
			}
			return e.programLibraryList(g, *asJSON)
		}
	case "rm":
		if g != nil {
			return e.programLibraryRm(g, pos, *all)
		}
	}
	switch args[0] {
	case "list", "ls":
		if len(pos) > 0 {
			return usagef("%s", libraryUsage)
		}
		p, err := e.openProject(*slug)
		if err != nil {
			return err
		}
		files, err := thread.Library(p)
		if err != nil {
			return err
		}
		files = append(files, link.LibraryFiles(p)...)
		sort.SliceStable(files, func(i, j int) bool { return files[i].Time.After(files[j].Time) })
		if *asJSON {
			if files == nil {
				files = []thread.LibFile{}
			}
			return e.printJSON(files)
		}
		for _, l := range files {
			if l.Peer != "" {
				fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s  -  from %s  %s/%s  %d bytes  %s", l.Time.Local().Format(time.DateOnly), l.Peer, l.Message, l.Name, l.Size, l.State)))
				continue
			}
			task := l.Task
			if task == "" {
				task = "-"
			}
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s  %s  %s  %s  %d bytes  %s", l.Time.Local().Format(time.DateOnly), task, l.Thread, l.Name, l.Size, l.State)))
		}
		return nil
	case "rm":
		name := ""
		switch {
		case *all && len(pos) == 1:
		case !*all && len(pos) == 2:
			name = pos[1]
		default:
			return usagef("%s", libraryUsage)
		}
		p, err := e.openProject(*slug)
		if err != nil {
			return err
		}
		if err := e.coordinatorOnly(p, "tm library rm"); err != nil {
			return err
		}
		if link.ValidID(pos[0]) {
			if *all {
				return usagef("%s", libraryUsage)
			}
			if err := link.RemoveFile(p, e.Caller, pos[0], name); err != nil {
				return err
			}
			fmt.Fprintf(e.Stdout, "deleted %s of %s\n", shown(name), pos[0])
			return nil
		}
		n, err := thread.RemoveLibrary(p, e.Caller, pos[0], name)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "deleted %d file(s) of %s\n", n, pos[0])
		return nil
	}
	return usagef("unknown subcommand %q\n%s", args[0], libraryUsage)
}

// libraryProgram is the program a tm library command is about, nil for a
// project: --program, a director's own, or for the human the program
// whose folder it is in. A director names only its own, and may still
// list a member with --project; a coordinator names none.
func (e *Env) libraryProgram(slug, prog string) (*program.Program, error) {
	switch {
	case slug != "":
		return nil, nil
	case prog == "" && e.Caller.Kind == caller.Human:
		if prog = programAt(e.Cwd); prog == "" {
			return nil, nil
		}
	case prog == "" && e.Caller.Kind != caller.Director:
		return nil, nil
	case prog != "" && e.Caller.Kind == caller.Coordinator:
		return nil, &project.Error{Code: "other-program", Msg: "a coordinator's library is its project's; only a director keeps files in a program's"}
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if !features.Programs(cfg) {
		return nil, &project.Error{Code: features.OffCode, Msg: features.OffMsg}
	}
	return e.ownProgram(cfg, prog)
}

// libraryAdd copies the files into the caller's own library: g's, or the
// project's. A coordinator's and a director's own only, each file as an
// agent may name it (openArg).
func (e *Env) libraryAdd(slug string, g *program.Program, files []string) error {
	var folder *project.Folder
	if g != nil {
		folder = &g.Folder
	} else {
		p, err := e.openProject(slug)
		if err != nil {
			return err
		}
		if err := e.coordinatorOnly(p, "tm library add"); err != nil {
			return err
		}
		folder = &p.Folder
	}
	for _, file := range files {
		l, err := thread.AddSeatFile(folder, e.Caller, e.abs(file), e.openArg)
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("kept %s in the library (%d bytes): the Files view's Attached tab shows it", l.Name, l.Size)))
	}
	return nil
}

// programLibraryList lists program g's own files and those of its members
// on this server. A paired member's take its server's answer: the Files
// view shows those.
func (e *Env) programLibraryList(g *program.Program, asJSON bool) error {
	files, paired := server.LocalProgramLibrary(g)
	if asJSON {
		if files == nil {
			files = []thread.LibFile{}
		}
		return e.printJSON(files)
	}
	for _, l := range files {
		in := l.Project
		if in == "" {
			in = g.Slug
		}
		task, id := l.Task, l.Thread
		if task == "" {
			task = "-"
		}
		if l.Peer != "" {
			task, id = "from "+l.Peer, l.Message
		}
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s  %s  %s  %s  %s  %d bytes  %s", l.Time.Local().Format(time.DateOnly), in, task, id, l.Name, l.Size, l.State)))
	}
	if paired > 0 {
		fmt.Fprintf(e.Stdout, "%d paired member(s) not listed here: the Files view's Attached tab shows those whose pages are shared\n", paired)
	}
	return nil
}

// programLibraryRm deletes program g's own files: the user's alone.
func (e *Env) programLibraryRm(g *program.Program, pos []string, all bool) error {
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "a program's library files are deleted by the user"}
	}
	name := ""
	switch {
	case all && len(pos) == 1:
	case !all && len(pos) == 2:
		name = pos[1]
	default:
		return usagef("%s", libraryUsage)
	}
	if pos[0] != thread.SeatID {
		return usagef("%s", libraryUsage)
	}
	n, err := thread.RemoveSeatFiles(&g.Folder, e.Caller, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "deleted %d file(s) of %s\n", n, g.Slug)
	return nil
}
