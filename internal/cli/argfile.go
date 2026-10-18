package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

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
// "..", absolute link or symlink in between leads out of the root. In a
// scratch root (the caller's own Claude Code scratchpad) no symlink at
// all, checked on the open file (fsx.OpenNoLinks).
func (e *Env) openArg(path string) (*os.File, error) {
	path = filepath.Clean(e.abs(path))
	if !e.Caller.IsAgent() {
		return os.Open(path)
	}
	set := e.argRoots()
	var lastErr error
	for _, root := range set.roots {
		rel, ok := fsx.Within(root, path)
		if !ok {
			continue
		}
		var f *os.File
		var err error
		if real, ok := set.noLinks[root]; ok {
			if set.flat[root] && strings.ContainsRune(rel, filepath.Separator) {
				continue // only files directly in it
			}
			f, err = fsx.OpenNoLinks(real, rel)
			if err == nil && set.flat[root] {
				// Loose in the shared temp folder: the user's own, as
				// Claude's TMPDIR has a thread write them.
				if fi, serr := f.Stat(); serr != nil || !fsx.Mine(fi) {
					f.Close()
					lastErr = &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s is not a file of this user", f.Name())}
					continue
				}
			}
		} else {
			var r *os.Root
			if r, err = os.OpenRoot(root); err != nil {
				continue
			}
			// Non-blocking, so a FIFO can't hold the server; then only a
			// regular file with one link (a hard link could name a file the
			// sandbox keeps from the agent) and outside every credential store.
			f, err = fsx.OpenInNoBlock(r, rel)
			r.Close()
		}
		if err == nil {
			if err = agentFileOK(f, set.private); err == nil {
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
	return nil, &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s: an agent's files must be in %s", path, set.places())}
}

// argSet is where an agent caller's files may be.
type argSet struct {
	roots   []string
	flat    map[string]bool   // the roots of which only the files directly in them may be named
	noLinks map[string]string // the roots in which no symlink is followed, each with the folder it is, as the kernel spells it
	private []string          // folders of the roots it may not name
	note    string            // why a root it might expect is none
}

// places says where the files may be, for a refusal: the roots as they
// are (a link spelling of one left out), and the note.
func (s argSet) places() string {
	var seen []string
	for _, r := range s.roots {
		if !slices.Contains(seen, r) && !slices.ContainsFunc(seen, func(x string) bool { return realPath(x) == r }) {
			seen = append(seen, r)
		}
	}
	for i, r := range seen {
		if s.flat[r] {
			seen[i] = r + " (files directly in it)"
		}
	}
	out := "a folder of its own; none is known"
	if len(seen) > 0 {
		out = strings.Join(seen, ", ")
	}
	if s.note != "" {
		out += " (" + s.note + ")"
	}
	return out
}

// argRoots are the folders an agent caller's files may be in: a thread's
// worktree, a coordinator's project folder and its threads' worktrees,
// and the agent's own temp folder, where it writes what it hands in.
// Where the agent's temp folder is shared by all its sessions (Claude
// Code's), only the caller's own scratch folder of it, named after its
// working folder (its worktree; its project folder for a coordinator),
// and none for an adopted thread, whose worktree is the user's own
// checkout, whose scratch folder the user's sessions share. Each root
// comes also with its links resolved (/tmp is /private/tmp on macOS),
// so either spelling of a path matches.
func (e *Env) argRoots() argSet {
	var set argSet
	var roots, own []string // own: working folders whose scratch folder is the caller's
	adopted := false
	if e.Caller.Project != "" {
		if p, err := project.Open(e.Caller.Project); err == nil {
			switch e.Caller.Kind {
			case caller.Thread:
				// thread.toml is in the project folder, which the
				// coordinator writes, and an adopted thread's worktree is
				// wherever its agent ran: never a folder holding the home.
				if r, err := thread.Load(p, e.Caller.Thread); err == nil && r.Worktree != "" && !holdsHome(r.Worktree) {
					roots = append(roots, r.Worktree)
					if wts, err := home.WorktreesDir(); err == nil && !r.Adopted && !r.Checkout && underAny(r.Worktree, []string{wts}) {
						own = append(own, r.Worktree, realPath(r.Worktree))
					} else {
						adopted = true
					}
				}
			case caller.Coordinator:
				roots = append(roots, p.Dir)
				own = append(own, p.Dir, realPath(p.Dir))
				if wt, err := home.WorktreesDir(); err == nil {
					roots = append(roots, filepath.Join(wt, p.Slug))
				}
			}
		}
	}
	temp, private, scratch := tempRoots(e)
	if scratch {
		// The temp folder and the scratch folder in it are folders the
		// sandbox can write: each must be a real folder of this user, never
		// a link the agent planted to another's, so neither is resolved
		// (only the folders above the temp folder are: /tmp).
		// The agent may spell the temp folder's path above it any way
		// (/tmp, /private/tmp): the spelling only names the folder, which
		// is opened as the kernel spells it, link by link.
		set.noLinks = map[string]string{}
		set.flat = map[string]bool{}
		var scratches []string
		for _, t := range temp {
			real, err := fsx.OwnDir(t)
			if err != nil {
				continue
			}
			// Claude sets a thread's TMPDIR to the temp folder, so what
			// it writes there is loose in it: those files, but none in
			// the other sessions' folders in it (T586; T587 isolates
			// per-thread temp folders).
			for _, name := range []string{real, filepath.Clean(t)} {
				if _, dup := set.noLinks[name]; !dup {
					set.noLinks[name] = real
					set.flat[name] = true
					scratches = append(scratches, name)
				}
			}
			for _, o := range own {
				d, err := fsx.OwnDir(filepath.Join(real, scratchName(o)))
				if err != nil {
					continue
				}
				for _, name := range []string{d, filepath.Join(filepath.Clean(t), scratchName(o))} {
					if _, dup := set.noLinks[name]; !dup {
						set.noLinks[name] = d
						scratches = append(scratches, name)
					}
				}
			}
		}
		temp, private = scratches, nil
		if adopted {
			set.note = "no scratchpad for an adopted thread or a thread in a main checkout: other sessions of the user share its scratch folder"
		}
	}
	roots = append(roots, temp...)
	set.private = private
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			continue
		}
		r = filepath.Clean(r)
		set.roots = append(set.roots, r)
		if _, ok := set.noLinks[r]; ok {
			continue // opened as the kernel spells it
		}
		if real, err := filepath.EvalSymlinks(r); err == nil && real != r {
			set.roots = append(set.roots, real)
		}
	}
	return set
}

// tempRoots is the temp folder an agent caller's files may be in, and
// its subfolders they may not: its agent's own, where its sandbox has it
// write ([guard] temp, its TMPDIR, less [guard] temp_private), not every
// temp folder, which holds files that sandbox keeps from it. scratch is
// set when the folder is shared by the agent's sessions ([guard]
// temp_scratch), so that only a scratch folder in it is the caller's.
// None when the agent or its temp folder is unknown (a thread caller with
// no session is told by its record). os.TempDir() for a mod's, and for
// a call that runs in the caller's own process (no server: its sandbox
// opens the file). A variable for tests, whose own temp
// folders are elsewhere.
var tempRoots = func(e *Env) (roots, private []string, scratch bool) {
	if e.Caller.Kind == caller.Mod {
		return []string{os.TempDir()}, nil, false
	}
	if e.Caller.Session == "" && !e.InServer {
		// In the agent's own process: its sandbox opens the file, so it
		// can name only what it can already read.
		return []string{os.TempDir()}, nil, false
	}
	name := e.callerAgent()
	if name == "" {
		name = e.threadAgent()
	}
	if name == "" {
		return nil, nil, false
	}
	var reg *agent.Registry
	if paths, err := client.ResolvePaths(); err == nil {
		reg, _ = agent.Load(paths.AgentsDir())
	}
	h, _ := os.UserHomeDir()
	return agent.AgentTemps(reg, name, h, os.Getenv)
}

// threadAgent is the agent a thread caller's record says runs it, "" for
// none: the answer when the server's session list doesn't name it.
func (e *Env) threadAgent() string {
	if e.Caller.Kind != caller.Thread || e.Caller.Project == "" {
		return ""
	}
	p, err := project.Open(e.Caller.Project)
	if err != nil {
		return ""
	}
	r, err := thread.Load(p, e.Caller.Thread)
	if err != nil {
		return ""
	}
	return r.Agent
}

// scratchName is how Claude Code names a working folder's scratch folder
// in its temp folder: the path with every character but a letter or
// digit a "-" (/a/.b is -a--b).
func scratchName(dir string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return '-'
	}, dir)
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
	return underAny(h, []string{dir}) || underAny(realPath(h), []string{dir})
}

// underAny reports whether path is inside one of dirs (each taken as
// given and with its links resolved).
func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		for _, dd := range []string{filepath.Clean(d), realPath(d)} {
			if _, ok := fsx.Within(dd, path); ok {
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
	if underAny(real, private) {
		return &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s is in a folder its agent's sandbox keeps from it", f.Name())}
	}
	if underAny(real, agent.Secrets(nil, h, os.Getenv)) {
		return &tasks.Error{Code: "outside-roots", Msg: fmt.Sprintf("%s is in a credential store", f.Name())}
	}
	return nil
}
