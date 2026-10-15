package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
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
	roots, private := e.argRoots()
	var lastErr error
	for _, root := range roots {
		rel, ok := fsx.Within(root, path)
		if !ok {
			continue
		}
		r, err := os.OpenRoot(root)
		if err != nil {
			continue
		}
		// Non-blocking, so a FIFO can't hold the server; then only a
		// regular file with one link (a hard link could name a file the
		// sandbox keeps from the agent) and outside every credential store.
		f, err := fsx.OpenInNoBlock(r, rel)
		r.Close()
		if err == nil {
			if err = agentFileOK(f, private); err == nil {
				return f, nil
			}
			f.Close()
		}
		lastErr = err
	}
	var refused *tasks.Error
	if errors.As(lastErr, &refused) {
		return nil, refused
	}
	if lastErr != nil {
		return nil, usagef("%v", lastErr)
	}
	return nil, &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s: an agent's files must be in %s", path, e.argPlaces())}
}

// argPlaces says where an agent's files may be, for its refusal.
func (e *Env) argPlaces() string {
	if e.Caller.Kind == caller.Thread {
		return "its worktree or its own temp folder ($TMPDIR)"
	}
	return "its project's folder or worktrees, or its own temp folder ($TMPDIR)"
}

// argRoots are the folders an agent caller's files may be in: a thread's
// worktree, a coordinator's project folder and its threads' worktrees,
// and the agent's own temp folder, where it writes what it hands in. Each comes
// also with its links resolved (/tmp is /private/tmp on macOS), so either
// spelling of a path matches.
func (e *Env) argRoots() (roots, private []string) {
	if e.Caller.Project != "" {
		if p, err := project.Open(e.Caller.Project); err == nil {
			switch e.Caller.Kind {
			case caller.Thread:
				// thread.toml is in the project folder, which the
				// coordinator writes, and an adopted thread's worktree is
				// wherever its agent ran: never a folder holding the home.
				if r, err := thread.Load(p, e.Caller.Thread); err == nil && r.Worktree != "" && !holdsHome(r.Worktree) {
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
	temp, private := tempRoots(e)
	roots = append(roots, temp...)
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
	return out, private
}

// tempRoots is the temp folder an agent caller's files may be in, and
// its subfolders they may not: its agent's own, where its sandbox has it
// write ([guard] temp, its TMPDIR, less [guard] temp_private), not every
// temp folder, which holds files that sandbox keeps from it. None when
// the agent or its temp folder is unknown. os.TempDir() for a call
// that runs in the caller's own process (no server: its sandbox opens
// the file) and for a mod's. A variable for tests, whose own temp
// folders are elsewhere.
var tempRoots = func(e *Env) (roots, private []string) {
	if e.Caller.Kind == caller.Mod || e.Caller.Session == "" {
		return []string{os.TempDir()}, nil
	}
	name := e.callerAgent()
	if name == "" {
		return nil, nil
	}
	var reg *agent.Registry
	if paths, err := client.ResolvePaths(); err == nil {
		reg, _ = agent.Load(paths.AgentsDir())
	}
	h, _ := os.UserHomeDir()
	dir, private := agent.AgentTemp(reg, name, h, os.Getenv)
	if dir == "" {
		return nil, nil
	}
	return []string{dir}, private
}

// callerAgent is the agent that runs in the caller's session, "" when
// there is none or no server says.
func (e *Env) callerAgent() string {
	if e.Caller.Session == "" {
		return ""
	}
	var res proto.SessionListResult
	if err := e.call(proto.MethodSessionList, nil, &res); err != nil {
		return ""
	}
	for _, s := range res.Sessions {
		if s.ID == e.Caller.Session {
			return s.Agent
		}
	}
	return ""
}

// holdsHome reports whether dir is the home folder or holds it.
func holdsHome(dir string) bool {
	h, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	for _, d := range []string{filepath.Clean(dir), realPath(dir)} {
		for _, hd := range []string{filepath.Clean(h), realPath(h)} {
			if _, ok := fsx.Within(d, hd); ok {
				return true
			}
		}
	}
	return false
}

// realPath is path with its links resolved, path itself when that fails.
func realPath(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

// agentFileOK refuses an agent's open file f unless it is a regular file
// with one link, outside every credential store (agent.Secrets) and the
// private folders of its temp folder.
func agentFileOK(f *os.File, private []string) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return &tasks.Error{Code: "invalid-file", Msg: fmt.Sprintf("%s is not a regular file", f.Name())}
	}
	if fsx.Links(fi) > 1 {
		return &tasks.Error{Code: "invalid-file", Msg: fmt.Sprintf("%s has other hard links; an agent's files may not", f.Name())}
	}
	real := realPath(f.Name())
	h, _ := os.UserHomeDir()
	for _, p := range private {
		for _, pp := range []string{filepath.Clean(p), realPath(p)} {
			if _, ok := fsx.Within(pp, real); ok {
				return &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s is in a folder its agent's sandbox keeps from it", f.Name())}
			}
		}
	}
	for _, sec := range agent.Secrets(nil, h, os.Getenv) {
		for _, sp := range []string{filepath.Clean(sec), realPath(sec)} {
			if _, ok := fsx.Within(sp, real); ok {
				return &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s is in a credential store", f.Name())}
			}
		}
	}
	return nil
}
