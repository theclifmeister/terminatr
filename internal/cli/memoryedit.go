package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// memoryChange is tm memory new, edit, rm and index (docs/SPEC.md §7.6,
// Memory notes): the human's and the coordinator's; a thread only reads
// memory.
func (e *Env) memoryChange(sub string, args []string) error {
	err := e.memoryChangeRun(sub, args)
	if err != nil && !isHelp(err) {
		e.pauseOnExit(fmt.Sprintf("tm memory %s: %v", sub, err))
	}
	return err
}

// pauseOnExitEnv is set by a console that runs tm memory edit or new
// on its terminal: what tm says last is then read before the console's
// screen covers it.
const pauseOnExitEnv = "TERMINATR_PAUSE_ON_EXIT"

// pauseOnExit shows msg and waits for enter, when the console asked
// for it (pauseOnExitEnv) and stdin is a terminal.
func (e *Env) pauseOnExit(msg string) {
	if e.Getenv(pauseOnExitEnv) == "1" && stdinTTY(e) && !e.Caller.IsAgent() {
		answer(e, msg+"\nPress enter to go back. ")
	}
}

func (e *Env) memoryChangeRun(sub string, args []string) error {
	f := newFlags()
	slug, file, yes := f.String("project"), f.String("file"), f.Bool("yes")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	switch {
	case sub == "new" && len(pos) > 1,
		(sub == "edit" || sub == "rm") && len(pos) != 1,
		sub == "index" && len(pos) != 0,
		f.IsSet("file") && sub != "new" && sub != "edit",
		*yes && sub != "rm":
		return usagef("%s", memoryUsage)
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if e.Caller.Kind == caller.Thread {
		return &tasks.Error{Code: "coordinator-only", Msg: "a thread reads memory; put lessons under ## Remember in your report"}
	}
	if err := e.coordinatorOnly(p, "tm memory "+sub); err != nil {
		return err
	}
	var text *string
	if f.IsSet("file") {
		t, err := e.readArg(*file)
		if err != nil {
			return err
		}
		if len(t) > project.MaxMemoryRead {
			return usagef("the note is %d bytes; at most %d", len(t), project.MaxMemoryRead)
		}
		text = &t
	}
	switch sub {
	case "index":
		changed, err := p.RebuildIndex()
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintln(e.Stdout, "MEMORY.md lists the notes already")
			return nil
		}
		fmt.Fprintln(e.Stdout, "wrote MEMORY.md's list of the notes")
		return p.Journal(e.Caller, "memory.index", "MEMORY.md", "")
	case "new":
		return e.memoryNew(p, pos, text)
	case "edit":
		return e.memoryEdit(p, pos[0], text)
	}
	return e.memoryRm(p, pos[0], *yes)
}

// memoryNew writes a new note: from --file, else on a terminal the
// template as the human edits it (nothing when they leave it as it
// was), else the template.
func (e *Env) memoryNew(p *project.Project, pos []string, text *string) error {
	tty := stdinTTY(e) && !e.Caller.IsAgent()
	name := ""
	if len(pos) == 1 {
		name = pos[0]
	} else if tty {
		name = answer(e, "Name of the new note (memory/<name>.md): ")
	}
	if strings.TrimSpace(name) == "" {
		return usagef("tm memory new needs a name for the note\n%s", memoryUsage)
	}
	rel, err := p.NewNotePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p.Path(filepath.FromSlash(rel))); err == nil {
		return &project.Error{Code: "exists", Msg: fmt.Sprintf("%s exists: tm memory edit %s changes it", rel, rel)}
	}
	data := []byte(project.NoteTemplate)
	switch {
	case text != nil:
		data = []byte(*text)
	case tty:
		mine, err := e.runEditorOn(path.Base(rel), data)
		if err != nil {
			return err
		}
		if bytes.Equal(mine, data) || len(bytes.TrimSpace(mine)) == 0 {
			fmt.Fprintf(e.Stdout, "%s not written: the template was left as it was\n", rel)
			return nil
		}
		data = mine
	}
	if err := p.CreateNote(rel, data); err != nil {
		if errors.As(err, new(*project.Error)) && tty {
			// Made meanwhile: what the human wrote isn't lost.
			if werr := p.WriteConflict(rel, data); werr == nil {
				fmt.Fprintf(e.Stderr, "yours is in %s\n", project.ConflictPath(rel))
			}
		}
		return err
	}
	fmt.Fprintf(e.Stdout, "added %s\n", rel)
	return p.NoteChanged(e.Caller, project.NoteNew, rel)
}

// memoryEdit replaces note arg with --file, or on a terminal opens it
// in the editor, with the overwrite guard.
func (e *Env) memoryEdit(p *project.Project, arg string, text *string) error {
	rel, err := p.NotePath(arg)
	if err != nil {
		return err
	}
	if text != nil {
		if err := p.WriteNote(rel, []byte(*text)); err != nil {
			return noNote(rel, err)
		}
		fmt.Fprintf(e.Stdout, "saved %s\n", rel)
		return p.NoteChanged(e.Caller, project.NoteEdit, rel)
	}
	if !stdinTTY(e) || e.Caller.IsAgent() {
		return usagef("tm memory edit opens an editor on a terminal; else pass the new text with --file <path>|-")
	}
	stamp, old, err := p.StampNote(rel)
	if err != nil {
		return err
	}
	if stamp.Gone {
		return noNote(rel, fs.ErrNotExist)
	}
	mine, err := e.runEditorOn(path.Base(rel), old)
	if err != nil {
		return err
	}
	if bytes.Equal(mine, old) {
		fmt.Fprintf(e.Stdout, "%s unchanged\n", rel)
		return nil
	}
	changed, err := stamp.Changed(p, rel)
	if err != nil {
		return err
	}
	if changed {
		gone, _, _ := p.StampNote(rel)
		what := "changed"
		if gone.Gone {
			what = "was removed"
		}
		q := fmt.Sprintf("%s %s while you edited it. o: overwrite it with yours; k: keep both, yours in %s [o/K] ", rel, what, project.ConflictPath(rel))
		if a := strings.ToLower(answer(e, q)); a != "o" && a != "overwrite" {
			if err := p.WriteConflict(rel, mine); err != nil {
				return err
			}
			e.pauseOnExit(fmt.Sprintf("kept both: %s as it is now, yours in %s", rel, project.ConflictPath(rel)))
			fmt.Fprintf(e.Stdout, "kept both: %s as it is now, yours in %s\n", rel, project.ConflictPath(rel))
			return nil
		}
		if gone.Gone {
			if err := p.CreateNote(rel, mine); err != nil {
				return err
			}
			fmt.Fprintf(e.Stdout, "saved %s again\n", rel)
			return p.NoteChanged(e.Caller, project.NoteNew, rel)
		}
	}
	if err := p.WriteNote(rel, mine); err != nil {
		return noNote(rel, err)
	}
	fmt.Fprintf(e.Stdout, "saved %s\n", rel)
	return p.NoteChanged(e.Caller, project.NoteEdit, rel)
}

// memoryRm removes note arg, after a y on a terminal, else with --yes.
func (e *Env) memoryRm(p *project.Project, arg string, yes bool) error {
	rel, err := p.NotePath(arg)
	if err != nil {
		return err
	}
	if _, err := p.ReadNote(rel); err != nil {
		return noNote(rel, err)
	}
	if !yes {
		if !stdinTTY(e) || e.Caller.IsAgent() {
			return usagef("tm memory rm %s deletes the note: confirm with --yes", rel)
		}
		if !ask(e, fmt.Sprintf("Delete %s? It leaves MEMORY.md too. [y/N] ", rel)) {
			return &project.Error{Code: "not-confirmed", Msg: rel + " kept"}
		}
	}
	if err := p.RemoveNote(rel); err != nil {
		return noNote(rel, err)
	}
	fmt.Fprintf(e.Stdout, "removed %s\n", rel)
	return p.NoteChanged(e.Caller, project.NoteRm, rel)
}

// noNote is err, said as no-note when note rel isn't there.
func noNote(rel string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &project.Error{Code: "no-note", Msg: "the project has no note " + rel + " (tm memory new makes one)"}
	}
	return err
}

// runEditorOn has the human edit text in a file named name, in a
// private temporary folder (never the note itself: tm saves it, after
// the overwrite guard), and answers what the file holds then.
func (e *Env) runEditorOn(name string, text []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "tm-memory-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, text, 0o600); err != nil {
		return nil, err
	}
	argv := editorArgv(e.Getenv)
	if len(argv) == 0 {
		return nil, errors.New("no editor: set $VISUAL or $EDITOR")
	}
	if err := runEditor(e, append(argv, file)); err != nil {
		return nil, fmt.Errorf("the editor (%s) failed: %w; nothing saved", argv[0], err)
	}
	return os.ReadFile(file)
}

// editorArgv is the editor's command line: $VISUAL, else $EDITOR (split
// on white space, as "code -w"), else open.Editor's.
func editorArgv(getenv func(string) string) []string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(getenv(k)); len(f) > 0 {
			return f
		}
	}
	return open.Editor()
}

// runEditor runs argv on the terminal and waits for it; tests replace
// it.
var runEditor = func(e *Env, argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, os.Stdout, os.Stderr
	if f, ok := e.Stdout.(*os.File); ok {
		cmd.Stdout = f
	}
	return cmd.Run()
}
