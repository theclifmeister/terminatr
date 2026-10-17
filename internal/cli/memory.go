package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/project"
)

const memoryUsage = `usage: tm memory find [<words>…] [--path <path>]… [--limit <n>] [--project <slug>] [--json]
       tm memory template

find lists the project's memory notes (memory/*.md) that match the
words and the paths (a file or folder in a repo, or repo:<name>), best
first, at most --limit (5 when left out). A word in a note's title
scores 3, one only in its body 1, a path the note touches 4; a note
shows with a shared path, two matched words or a title word, and a
score of at least 3. template prints a new note with the optional
front matter (title, touches, standing, source) and its sections
(Decided, Ruled out, Watch out).`

// runMemory searches the project's memory notes (docs/SPEC.md §7.6).
func runMemory(e *Env, args []string) error {
	if done, err := helpOr(e, args, memoryUsage); done {
		return err
	}
	f := newFlags()
	slug, asJSON, limit, paths := f.String("project"), f.Bool("json"), f.String("limit"), f.List("path")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
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
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s  %s  (score %d: %s)", h.Note.Path, h.Note.Title, h.Score, strings.Join(why, ", "))))
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
