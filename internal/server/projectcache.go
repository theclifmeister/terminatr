package server

// The project cache: what project watches read from files, one copy per
// project for all its watchers. Every watch looks every watchPoll; the
// cache reads the files again only after a wake (what the server
// changes itself) or when a stamp of their stats changes (what changes
// behind its back), so an idle project costs a few stats a second, and
// no file read or git.

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// snapMaxAge is how long a project's snapshot serves at most: a change
// the stamp can't see (a file rewritten within its mtime's granularity,
// to the same size) shows after it.
const snapMaxAge = 30 * time.Second

// projectCache is the latest projectSnap of each watched project.
type projectCache struct {
	mu      sync.Mutex
	entries map[string]*snapEntry
}

// snapEntry is one project's snapshot, with what it was read at.
type snapEntry struct {
	mu    sync.Mutex // one reader at a time; the others wait and share
	snap  *projectSnap
	stamp uint64
	gen   uint64 // the watchers' generation it was read at
	at    time.Time
	state string // the ticker's state file it was read with
}

// get is project slug's snapshot, read again when the server woke its
// watchers since (gen), when the files' stamp changed, or snapMaxAge
// after the last read. tickerState is the ticker's state file.
func (c *projectCache) get(slug, tickerState string, gen uint64) (*projectSnap, error) {
	return c.look(slug, tickerState, gen, true)
}

// stamped is get read again on the stamp alone, not on a wake: what
// the server changes itself is in the files, which a wake's look stats
// at once. The sidebars' hub (sideprojects.go) reads every project
// this way, so a wake about one project reads only that one again.
func (c *projectCache) stamped(slug, tickerState string, gen uint64) (*projectSnap, error) {
	return c.look(slug, tickerState, gen, false)
}

// look is get, with byGen whether a wake since the last read reads
// again.
func (c *projectCache) look(slug, tickerState string, gen uint64, byGen bool) (*projectSnap, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*snapEntry{}
	}
	e := c.entries[slug]
	if e == nil {
		e = &snapEntry{}
		c.entries[slug] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	var stamp uint64
	if e.snap != nil {
		stamp = snapStamp(e.snap, tickerState)
		if (e.gen == gen || !byGen) && e.stamp == stamp && e.state == tickerState && time.Since(e.at) < snapMaxAge {
			return e.snap, nil
		}
	}
	p, err := project.Open(slug)
	if err != nil {
		e.snap = nil
		return nil, err
	}
	// Stamped before the read, so a change during it is read again next
	// time; a first read has no stamp (0) and is read once more.
	snap := readProjectSnap(p, tickerState)
	e.snap, e.stamp, e.gen, e.at, e.state = snap, stamp, gen, time.Now(), tickerState
	return snap, nil
}

// snapStamp is the stat stamp (fsx.StatStamp) of what snap was read
// from: the project's files and folders, those of every thread folder
// (a new thread changes the threads folder), the ticker's state file,
// the config, the repos' .git/config (their origin) and the links'
// files (link.StampPaths). The thread folders are listed, not taken
// from snap: a read between thread.Create's mkdir and its thread.toml
// lists no thread, and the record landing must still change the stamp
// (T457).
func snapStamp(snap *projectSnap, tickerState string) uint64 {
	p := snap.p
	var paths []string
	for _, name := range []string{"PROJECT.md", "TASKS.md", "questions.json", "inbox", "threads"} {
		paths = append(paths, p.Path(name))
	}
	ents, _ := os.ReadDir(p.Path("threads"))
	for _, e := range ents {
		if !e.IsDir() || !thread.ValidID(e.Name()) {
			continue
		}
		paths = append(paths, thread.Dir(p, e.Name()))
		for _, name := range []string{"thread.toml", "STATUS.md", "REPORT.md"} {
			paths = append(paths, thread.Path(p, e.Name(), name))
		}
	}
	paths = append(paths, tickerState)
	if path, err := config.Path(); err == nil {
		paths = append(paths, path)
	}
	for _, repo := range p.Repos {
		paths = append(paths, filepath.Join(repo, ".git", "config"))
	}
	paths = append(paths, link.StampPaths(p, snap.links)...)
	return fsx.StatStamp(paths...)
}

// ProjectSnaps is a project cache for a client that builds project
// watches itself (the attach console's coordinator panel): it reads a
// project's files again only when their stamp changed, or snapMaxAge
// after. Its zero value is ready; one goroutine uses it.
type ProjectSnaps struct{ c projectCache }

// Watch is project slug's ProjectWatch for sessions, as ProjectWatchOf
// gives it, from the cached snapshot. tickerState is the ticker's state
// file.
func (s *ProjectSnaps) Watch(slug string, sessions []proto.SessionInfo, tickerState string) (proto.ProjectWatch, error) {
	snap, err := s.c.get(slug, tickerState, 0)
	if err != nil {
		return proto.ProjectWatch{}, err
	}
	return snap.watch(sessions), nil
}
