// Package rename renames a project's slug (tm project rename,
// docs/SPEC.md §5.1): its folder, its threads' worktrees folder, its
// settings in config.toml and the paths in its thread records. The
// journal, inbox, tasks and memory move with the folder, and the links
// tm wrote into the project, in every project's files, name the new slug
// (relink).
// Branches keep their names: tm/<slug>/ names only new threads' branches.
//
// The caller makes sure no agent of the project runs: the server, or
// the CLI when no server runs, and carries the ticker's memos over.
package rename

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// Options say what to rename.
type Options struct {
	From, To string
	Caller   caller.Caller
	// MoveAgentDir carries agents' state kept by folder path (their
	// conversations, to resume) from a moved folder to its new path; nil
	// moves none (agent.Mover).
	MoveAgentDir func(from, to string) error
}

// Result is what a rename did.
type Result struct {
	Dir       string // the project's folder now
	Worktrees string // its threads' worktrees folder now; "" if it had none
	Threads   int    // thread records whose worktree path changed
	// Notes are what didn't go through after the rename stood, e.g. a
	// worktree git couldn't reconnect.
	Notes []string
}

func refuse(code, format string, a ...any) error {
	return &project.Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Check refuses a rename that can't be done, before anything moves:
// an unknown project, an invalid or taken slug, settings tm can't
// rename.
func Check(o Options) error {
	if _, err := project.Open(o.From); err != nil {
		return err
	}
	if !project.ValidSlug(o.To) {
		return refuse("invalid-project", "%q is not a project slug (a-z, 0-9 and -)", o.To)
	}
	if o.To == o.From {
		return refuse("unchanged", "%s is already called %s; pass a new slug", o.From, o.To)
	}
	dir, err := project.Dir(o.To)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dir); err == nil {
		return refuse("project-exists", "a project %s exists already (%s)", o.To, dir)
	}
	wt, err := home.WorktreesDir()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(wt, o.To)); err == nil {
		return refuse("project-exists", "%s exists already: worktrees of an earlier project %s (tm doctor lists leftovers)", filepath.Join(wt, o.To), o.To)
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
		switch _, err := config.RenameTable(data, o.From, o.To); {
		case errors.Is(err, config.ErrProjectSet):
			return refuse("project-exists", "%s already has settings for %s ([projects.%s]); remove them first", path, o.To, o.To)
		case errors.Is(err, config.ErrForm):
			return refuse("settings-form", "%s sets %s's settings in a form tm can't rename (dotted keys or an inline table): rename them to [projects.%s] by hand, or make them a [projects.%s] table first", path, o.From, o.To, o.From)
		case err != nil:
			return err
		}
	}
	return nil
}

// Project renames project From to To. It checks first (Check); an error
// means nothing changed. Once the folders have moved the rename stands,
// and what didn't go through after that is in Result.Notes.
func Project(o Options) (*Result, error) {
	if err := Check(o); err != nil {
		return nil, err
	}
	oldDir, _ := project.Dir(o.From)
	newDir, _ := project.Dir(o.To)
	root, err := home.WorktreesDir()
	if err != nil {
		return nil, err
	}
	oldWT, newWT := filepath.Join(root, o.From), filepath.Join(root, o.To)
	res := &Result{Dir: newDir}

	// The two moves are the rename; the second failing undoes the first.
	movedWT := false
	if _, err := os.Lstat(oldWT); err == nil {
		if err := os.Rename(oldWT, newWT); err != nil {
			return nil, err
		}
		movedWT, res.Worktrees = true, newWT
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if movedWT {
			os.Rename(newWT, oldWT)
		}
		return nil, err
	}
	note := func(format string, a ...any) { res.Notes = append(res.Notes, fmt.Sprintf(format, a...)) }

	if err := config.RenameProject(o.From, o.To); err != nil {
		note("config.toml still has [projects.%s]: %v (rename it to [projects.%s] by hand)", o.From, err, o.To)
	}
	// Linked projects keep their messages with it in messages/<slug>/
	// (docs/SPEC.md §7.9).
	if list, err := project.List(); err == nil {
		for _, sum := range list {
			other, err := project.Open(sum.Slug)
			if err != nil || sum.Slug == o.To {
				continue
			}
			from, to := other.Path("messages", o.From), other.Path("messages", o.To)
			if _, err := os.Lstat(from); err != nil {
				continue
			}
			if err := os.Rename(from, to); err != nil {
				note("%s: messages with %s not moved to messages/%s: %v", sum.Slug, o.From, o.To, err)
			}
		}
	}
	p, err := project.Open(o.To)
	if err != nil {
		return res, err
	}
	// Thread records name their worktree by its full path.
	recs, err := thread.List(p)
	if err != nil {
		note("thread records unreadable: %v", err)
	}
	var moved [][2]string // worktree paths, old and new
	for _, r := range recs {
		rel, ok := under(oldWT, r.Worktree)
		if !ok {
			continue // an adopted thread's folder elsewhere stays
		}
		to := filepath.Join(newWT, rel)
		if _, err := thread.Update(p, r.ID, func(x *thread.Record) error { x.Worktree = to; return nil }); err != nil {
			note("thread %s: record not updated: %v", r.ID, err)
			continue
		}
		res.Threads++
		if _, err := os.Stat(to); err == nil {
			moved = append(moved, [2]string{r.Worktree, to})
		}
	}
	// git knows each worktree by path: run in a moved worktree, repair
	// points its repo's record of it at the new path.
	if movedWT {
		ents, _ := os.ReadDir(newWT)
		for _, e := range ents {
			dir := filepath.Join(newWT, e.Name())
			if fi, err := os.Lstat(filepath.Join(dir, ".git")); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			if err := worktree.Reconnect(dir); err != nil {
				note("%s: git doesn't know its new place: %v", dir, err)
			}
		}
	}
	if o.MoveAgentDir != nil {
		for _, m := range append([][2]string{{oldDir, newDir}}, moved...) {
			if err := o.MoveAgentDir(m[0], m[1]); err != nil {
				note("%s: agent state not carried over: %v", m[1], err)
			}
		}
	}
	if n, err := relink(o.From, o.To); err != nil {
		note("links to %s not all renamed (%d files done): %v", o.From, n, err)
	}
	detail := o.From + " → " + o.To
	if err := p.WriteRoleFile(); err != nil {
		note("AGENTS.md not rewritten: %v", err)
	}
	return res, p.Journal(o.Caller, "project.rename", o.To, detail)
}

// linkFiles are the files tm writes links in (vaultlink), as patterns
// below a project's folder (messages/ is walked whole), and the part of
// each that is tm's: the rest is someone's words, which tm doesn't
// rewrite.
var linkFiles = []struct {
	pat  string
	part func(line string) bool // the lines that are tm's; nil: the front matter
}{
	{"TASKS.md", metaLine}, {"tasks/ARCHIVE.md", metaLine}, {"MEMORY.md", indexLine},
	{"memory/*.md", nil}, {"inbox/*.md", nil}, {"inbox/done/*.md", nil},
	{"threads/*/STATUS.md", nil}, {"threads/*/brief.md", func(string) bool { return true }},
}

// metaLine is a task's metadata line; indexLine a note's in MEMORY.md.
func metaLine(l string) bool  { return strings.HasPrefix(l, "status:") }
func indexLine(l string) bool { return strings.HasPrefix(l, "- [[") }

// relinked is text with the links into from pointed at to in the part
// that is tm's (linkFiles).
func relinked(text, from, to string, part func(string) bool) string {
	if part != nil {
		lines := strings.SplitAfter(text, "\n")
		for i, l := range lines {
			if part(l) {
				lines[i] = vaultlink.Rename(l, from, to)
			}
		}
		return strings.Join(lines, "")
	}
	front, _, err := mdfile.Split([]byte(text))
	if err != nil || front == nil {
		return text
	}
	start := strings.IndexByte(text, '\n') + 1
	return text[:start] + vaultlink.Rename(string(front), from, to) + text[start+len(front):]
}

// relink points the links into project from at project to
// (vaultlink.Rename) in every project's linkFiles and messages, each
// file that changes under its lock. A link, or anything but a regular
// file, is left alone. It says how many files it rewrote.
func relink(from, to string) (int, error) {
	list, err := project.List()
	if err != nil {
		return 0, err
	}
	type file struct {
		path string
		part func(string) bool
	}
	n := 0
	var errs []error
	for _, sum := range list {
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		var files []file
		for _, lf := range linkFiles {
			m, _ := filepath.Glob(p.Path(filepath.FromSlash(lf.pat)))
			for _, f := range m {
				files = append(files, file{f, lf.part})
			}
		}
		filepath.WalkDir(p.Path("messages"), func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() && filepath.Ext(path) == ".md" {
				files = append(files, file{path, nil})
			}
			return nil
		})
		for _, f := range files {
			if fi, err := os.Lstat(f.path); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			data, err := os.ReadFile(f.path)
			if err != nil || relinked(string(data), from, to, f.part) == string(data) {
				continue
			}
			rewrite := func(old []byte) ([]byte, error) { return []byte(relinked(string(old), from, to, f.part)), nil }
			if filepath.Dir(f.path) == p.Path("inbox", "done") {
				// A handled item has no writer: no lock file for it.
				out, _ := rewrite(data)
				err = fsx.WriteAtomic(f.path, out, 0o644)
			} else {
				err = mdfile.Update(f.path, rewrite)
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			n++
		}
	}
	return n, errors.Join(errs...)
}

// under reports path's place below dir, when it is below it.
func under(dir, path string) (string, bool) {
	rel, ok := fsx.Within(dir, filepath.Clean(path))
	if !ok || rel == "." {
		return "", false
	}
	return rel, true
}
