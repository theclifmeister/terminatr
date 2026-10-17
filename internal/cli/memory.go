package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

const memoryUsage = `usage: tm memory find [<words>…] [--path <path>]… [--limit <n>] [--project <slug>] [--json]
       tm memory check [--thread <id>] [--project <slug>] [--json]
       tm memory template
       tm memory new [<name>] [--file <path>|-] [--project <slug>]
       tm memory edit <note> [--file <path>|-] [--project <slug>]
       tm memory rm <note> [--yes] [--project <slug>]
       tm memory index [--project <slug>]

find lists the project's memory notes (memory/*.md) that match the
words and the paths (a file or folder in a repo, or repo:<name>), best
first, at most --limit (5 when left out). Words are compared by their
English stem and scored with BM25, a word most notes have weighing
little and a word in a title three times one in the body; numbers,
words under three letters and stop words never match. A path the note
touches adds 10 and ranks it first; a note without one shows when its
words stand out from the other notes'.

check reads a thread's diff since its base (a thread's own, else
--thread's) and, for the notes that touch its files, flags a number
the note's Decided section holds that the diff changes, and words of
its Ruled out section the diff adds; tm report adds what it flags to
the report's ## Check, never refusing for it. template prints a new note with the optional
front matter (title, touches, standing, audience, source) and its
sections (Decided, Ruled out, Watch out). A note with audience =
"coordinator" is never shown to a thread: not in its brief, find or
check.

new, edit and rm change the notes in the project's memory/ folder
(a note is <name>, <name>.md or memory/<name>.md; tm refuses any other
path and a link). new writes memory/<name>.md from the template, or
from --file; edit replaces a note with --file. On a terminal, new and
edit open the note in $VISUAL or $EDITOR (else nano or vi; notepad on
Windows) and save it when the editor exits; if the note changed
meanwhile, they ask whether to overwrite it or keep both (yours in
memory/<name>.md.conflict). rm asks first, unless --yes. After each,
tm writes MEMORY.md's list of the notes again (index does only that;
tm context does it too when the list is out of date): one line per
note, its title, description (a line's own is kept, a new note's is its
first line) and touches. The human's
change is an inbox item for the coordinator. Threads only read memory.`

// runMemory searches the project's memory notes (docs/SPEC.md §7.6).
func runMemory(e *Env, args []string) error {
	if done, err := helpOr(e, args, memoryUsage); done {
		return err
	}
	switch args[0] {
	case "new", "edit", "rm", "index":
		return e.memoryChange(args[0], args[1:])
	}
	f := newFlags()
	slug, asJSON, limit, paths, tid := f.String("project"), f.Bool("json"), f.String("limit"), f.List("path"), f.String("thread")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	if args[0] != "check" && f.IsSet("thread") {
		return usagef("%s", memoryUsage)
	}
	switch args[0] {
	case "check":
		if len(pos) > 0 || f.anySet("limit", "path") {
			return usagef("%s", memoryUsage)
		}
		p, err := e.openProject(*slug)
		if err != nil {
			return err
		}
		id := *tid
		if e.Caller.Kind == caller.Thread {
			if id != "" && id != e.Caller.Thread {
				return &tasks.Error{Code: "coordinator-only", Msg: "a thread checks only its own diff"}
			}
			id = e.Caller.Thread
		}
		if id == "" {
			return usagef("outside a thread, pass --thread <id>")
		}
		r, err := thread.Load(p, id)
		if err != nil {
			return err
		}
		found, err := memoryConflicts(p, r, e.Caller.Kind == caller.Thread)
		if err != nil {
			return err
		}
		if *asJSON {
			if found == nil {
				found = []project.Conflict{}
			}
			return e.printJSON(map[string]any{"project": p.Slug, "thread": r.ID, "conflicts": found})
		}
		if len(found) == 0 {
			fmt.Fprintf(e.Stdout, "no note of %s's memory speaks against %s's diff\n", p.Slug, r.ID)
			return nil
		}
		for _, c := range found {
			fmt.Fprintln(e.Stdout, shown(c.String()))
		}
		return nil
	case "template":
		if len(pos) > 0 || f.anySet("project", "json", "limit", "path") {
			return usagef("%s", memoryUsage)
		}
		_, err := fmt.Fprint(e.Stdout, project.NoteTemplate)
		return err
	case "find":
		n := 5
		if *limit != "" {
			if n, err = strconv.Atoi(*limit); err != nil || n < 1 || n > 100 {
				return usagef("--limit takes a number from 1 to 100")
			}
		}
		query := strings.Join(pos, " ")
		if len(project.Terms(query)) == 0 && len(*paths) == 0 {
			return usagef("tm memory find needs words to look for or a --path\n%s", memoryUsage)
		}
		p, err := e.openProject(*slug)
		if err != nil {
			return err
		}
		notes, err := p.Notes()
		if err != nil {
			return err
		}
		if e.Caller.Kind == caller.Thread {
			notes = project.ThreadNotes(notes)
		}
		hits := project.Match(notes, query, *paths, n)
		if *asJSON {
			if hits == nil {
				hits = []project.Hit{}
			}
			return e.printJSON(map[string]any{"project": p.Slug, "dir": p.Dir, "hits": hits})
		}
		if len(hits) == 0 {
			fmt.Fprintf(e.Stdout, "no note of %s's memory matches\n", p.Slug)
			return nil
		}
		for _, h := range hits {
			why := append(append([]string{}, h.Terms...), h.Paths...)
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s  %s  (score %g: %s)", h.Note.Path, h.Note.Title, h.Score, strings.Join(why, ", "))))
			var meta []string
			if len(h.Note.Touches) > 0 {
				meta = append(meta, "touches "+strings.Join(h.Note.Touches, ", "))
			}
			if h.Note.Standing {
				meta = append(meta, "standing")
			}
			if h.Note.Source != "" {
				meta = append(meta, "from "+h.Note.Source)
			}
			if len(meta) > 0 {
				fmt.Fprintln(e.Stdout, shown("  "+strings.Join(meta, " · ")))
			}
		}
		fmt.Fprintln(e.Stdout, shown("(paths are in "+p.Dir+")"))
		return nil
	}
	return usagef("unknown subcommand %q\n%s", args[0], memoryUsage)
}

// Bounds of a memory check: its time, and the touched files whose
// patches it reads.
const (
	memoryCheckTimeout = 15 * time.Second
	memoryCheckFiles   = 20
)

// memoryConflicts checks thread r's diff since its base against the
// project's notes (project.Conflicts), only those threads may see when
// forThread. Only the files a note with a Decided or Ruled out section
// touches are read; with none, git doesn't run.
func memoryConflicts(p *project.Project, r *thread.Record, forThread bool) ([]project.Conflict, error) {
	notes, err := p.Notes()
	if err != nil {
		return nil, err
	}
	if forThread {
		notes = project.ThreadNotes(notes)
	}
	notes = slices.DeleteFunc(notes, func(n project.Note) bool {
		return len(n.Touches) == 0 || n.Section(project.SectionDecided) == "" && n.Section(project.SectionRuledOut) == ""
	})
	if len(notes) == 0 || r.Repo == "" || r.Worktree == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryCheckTimeout)
	defer cancel()
	o := changes.Options{Repo: r.Repo, Base: r.Base}
	cs, err := changes.Read(ctx, r.Worktree, o)
	if err != nil {
		return nil, err
	}
	repo := filepath.Base(r.Repo)
	var diffs []project.FileDiff
	for _, f := range cs.Files {
		if f.Binary || f.Status == "D" || len(diffs) >= memoryCheckFiles {
			continue
		}
		if !slices.ContainsFunc(notes, func(n project.Note) bool {
			return n.TouchesPath(repo, f.Path) || f.Old != "" && n.TouchesPath(repo, f.Old)
		}) {
			continue
		}
		o.Path = f.Path
		one, err := changes.Read(ctx, r.Worktree, o)
		if err != nil {
			return nil, err
		}
		diffs = append(diffs, project.FileDiff{Path: f.Path, Old: f.Old, Patch: one.Patch})
	}
	return project.Conflicts(notes, repo, diffs), nil
}
