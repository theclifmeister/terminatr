package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// openArg opens a file an argument names (--file, --notes-file,
// --attach). The human's opens as given. An agent's command runs in the
// server with the human's rights, past the agent's sandbox, so its file
// opens only under the roots it may name (argRoots), through os.Root: no
// "..", absolute link or symlink in between leads out of the root.
func (e *Env) openArg(path string) (*os.File, error) {
	path = filepath.Clean(e.abs(path))
	if !e.Caller.IsAgent() {
		return os.Open(path)
	}
	var lastErr error
	for _, root := range e.argRoots() {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		r, err := os.OpenRoot(root)
		if err != nil {
			continue
		}
		f, err := r.Open(rel)
		r.Close()
		if err == nil {
			return f, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, usagef("%v", lastErr)
	}
	return nil, &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s: an agent's files must be in %s", path, e.argPlaces())}
}

// argPlaces says where an agent's files may be, for its refusal.
func (e *Env) argPlaces() string {
	if e.Caller.Kind == caller.Thread {
		return "its worktree or a temp folder"
	}
	return "its project's folder or worktrees, or a temp folder"
}

// argRoots are the folders an agent caller's files may be in: a thread's
// worktree, a coordinator's project folder and its threads' worktrees,
// and the temp folders where an agent writes what it hands in. Each comes
// also with its links resolved (/tmp is /private/tmp on macOS), so either
// spelling of a path matches.
func (e *Env) argRoots() []string {
	var roots []string
	if e.Caller.Project != "" {
		if p, err := project.Open(e.Caller.Project); err == nil {
			switch e.Caller.Kind {
			case caller.Thread:
				if r, err := thread.Load(p, e.Caller.Thread); err == nil && r.Worktree != "" {
					roots = append(roots, r.Worktree)
				}
			case caller.Coordinator:
				roots = append(roots, p.Dir)
				if wt, err := home.WorktreesDir(); err == nil {
					roots = append(roots, filepath.Join(wt, p.Slug))
				}
			}
		}
	}
	roots = append(roots, tempRoots()...)
	out := make([]string, 0, 2*len(roots))
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			continue
		}
		out = append(out, filepath.Clean(r))
		if real, err := filepath.EvalSymlinks(r); err == nil && real != filepath.Clean(r) {
			out = append(out, real)
		}
	}
	return out
}

// tempRoots are the temp folders an agent's files may be in (a variable
// for tests, whose own temp folders are under them).
var tempRoots = func() []string { return []string{os.TempDir(), "/tmp"} }
