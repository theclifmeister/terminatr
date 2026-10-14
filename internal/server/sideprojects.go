package server

// Watching the projects (projects.watch, docs/SPEC.md §3.3, Watch): what
// the consoles' sidebars show of every project, sent again whenever it
// changes. Every console's watch looks every watchPoll and on every
// wake, as project.watch does, but they share one look (sideHub): a
// stat pass over the projects' files (the project cache's stamps) at
// most once a poll for all of them, and a read of only the projects
// whose files changed.

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// ProjectsWatch is the answer to projects.watch: the projects as the
// sidebar shows them, in slug order, the archived ones left out.
type ProjectsWatch struct {
	Projects []SideProject `json:"projects"`
}

// ProjectsWatchEvent is a projects.changed line: the projects' new
// state, all of them.
type ProjectsWatchEvent struct {
	Event    string        `json:"event"`
	Projects []SideProject `json:"projects"`
}

// SideProject is one project as the sidebar shows it. A project that
// doesn't open has its slug alone.
type SideProject struct {
	Slug string `json:"slug"`
	// Safety are the project's settings; nil when they can't be read.
	Safety *config.Safety `json:"safety,omitempty"`
	// NeedsYou are the tasks in the board's Needs you group (review or
	// blocked), in board order.
	NeedsYou []SideTask `json:"needs_you,omitempty"`
	// Questions counts the coordinator's open questions (tm ask).
	Questions int `json:"questions,omitempty"`
	// Threads are the project's unresolved threads, in id order.
	Threads []SideThread `json:"threads,omitempty"`
	// PRHostFails counts the PR polls in a row on which the project's PR
	// host failed, as the ticker last saw them; 0 while they work, or
	// when it has no PR host.
	PRHostFails int `json:"pr_host_fails,omitempty"`
}

// SideThread is one unresolved thread: its record (thread.toml), its
// STATUS.md (nil before it has one) and its pull request's checks and
// number as the ticker last saw them.
type SideThread struct {
	Record *thread.Record `json:"record"`
	Status *thread.Status `json:"status,omitempty"`
	Checks string         `json:"checks,omitempty"`
	PR     int            `json:"pr,omitempty"`
}

// SideTask is a task (tasks.Task) on the wire.
type SideTask struct {
	ID       int          `json:"id"`
	Title    string       `json:"title"`
	Status   tasks.Status `json:"status"`
	Notes    string       `json:"notes,omitempty"`
	Steps    []tasks.Step `json:"steps,omitempty"`
	Thread   string       `json:"thread,omitempty"`
	Owner    string       `json:"owner,omitempty"`
	Created  string       `json:"created,omitempty"`
	Updated  string       `json:"updated,omitempty"`
	Archived bool         `json:"archived,omitempty"`
}

// sideTask is t on the wire.
func sideTask(t *tasks.Task) SideTask {
	return SideTask{ID: t.ID, Title: t.Title, Status: t.Status, Notes: t.Notes, Steps: t.Steps, Thread: t.Thread,
		Owner: t.Owner, Created: t.Created, Updated: t.Updated, Archived: t.Archived}
}

// Task is the task t is on the wire.
func (t SideTask) Task() *tasks.Task {
	return &tasks.Task{ID: t.ID, Title: t.Title, Status: t.Status, Notes: t.Notes, Steps: t.Steps, Thread: t.Thread,
		Owner: t.Owner, Created: t.Created, Updated: t.Updated, Archived: t.Archived}
}

// sideHub is the projects as the sidebars show them, looked at once for
// all the projects watches.
type sideHub struct {
	mu     sync.Mutex
	at     time.Time // the last look
	gen    uint64    // the watchers' generation it looked at
	looked bool
	// cfg is config.Load's, loaded again when config.toml's stamp
	// changes.
	cfgStamp uint64
	cfg      *config.Config
	cfgErr   error
	// dirs are the projects folder's project folders (valid slugs), and
	// slugs those of them with a PROJECT.md, listed again when the stamp
	// of the folder and their PROJECT.md (dirStamp) changes.
	dirStamp uint64
	listed   time.Time
	dirs     []string
	slugs    []string
	b        json.RawMessage // the projects, marshalled
	ver      uint64          // counts the changes of b
}

// sideProjects is the projects as the sidebars show them, marshalled
// (a []SideProject), and a version that changes with them. It looks
// again on a wake (gen) and once a poll at most: the first watch to
// ask after that looks for all.
func (s *Server) sideProjects(gen uint64, poll time.Duration) (json.RawMessage, uint64) {
	h := &s.sides
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.looked && h.gen == gen && time.Since(h.at) < poll*3/4 {
		return h.b, h.ver
	}
	h.looked, h.gen, h.at = true, gen, time.Now()
	list := s.lookSideProjects(gen)
	b, _ := json.Marshal(list)
	if !slices.Equal(b, h.b) {
		h.b, h.ver = b, h.ver+1
	}
	return h.b, h.ver
}

// lookSideProjects reads the projects as the sidebars show them, the
// project cache reading again only those whose files changed.
func (s *Server) lookSideProjects(gen uint64) []SideProject {
	h := &s.sides
	path, _ := config.Path()
	cfgChanged := false
	if st := fsx.StatStamp(path); h.cfg == nil && h.cfgErr == nil || st != h.cfgStamp || time.Since(h.listed) >= snapMaxAge {
		h.cfgStamp, cfgChanged = st, true
		h.cfg, h.cfgErr = config.Load()
	}
	if h.cfgErr != nil {
		return []SideProject{} // none, as project.List gives
	}
	root, _ := home.ProjectsDir()
	if st := h.dirsStamp(root); cfgChanged || st != h.dirStamp || h.slugs == nil {
		h.listed = time.Now()
		h.dirs, h.slugs = projectDirs(root)
		h.dirStamp = h.dirsStamp(root) // a change while listing lists again
	}
	ts := ticker.StatePath(s.opts.Paths.Sessions)
	out := []SideProject{}
	for _, slug := range h.slugs {
		safety, serr := h.cfg.Safety(slug)
		if serr == nil && safety.Archived {
			continue // hidden: tm project unarchive brings it back
		}
		sp := SideProject{Slug: slug}
		snap, err := s.projects.stamped(slug, ts, gen)
		if err != nil {
			out = append(out, sp)
			continue
		}
		if serr == nil {
			sp.Safety = &safety
		}
		out = append(out, snap.side(sp))
	}
	return out
}

// dirsStamp is the stamp of the projects folder root and of the
// PROJECT.md of each project folder in it: a folder made, or its
// PROJECT.md written after it, lists the projects again.
func (h *sideHub) dirsStamp(root string) uint64 {
	paths := []string{root}
	for _, d := range h.dirs {
		paths = append(paths, filepath.Join(root, d, "PROJECT.md"))
	}
	return fsx.StatStamp(paths...)
}

// projectDirs are the project folders in the projects folder root (a
// valid slug), and the projects among them, with a PROJECT.md, as
// project.List lists them.
func projectDirs(root string) (dirs, slugs []string) {
	slugs = []string{}
	if root == "" {
		return nil, slugs
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || !project.ValidSlug(e.Name()) {
			continue
		}
		dirs = append(dirs, e.Name())
		if _, err := os.Stat(filepath.Join(root, e.Name(), "PROJECT.md")); err == nil {
			slugs = append(slugs, e.Name())
		}
	}
	return dirs, slugs
}

// side is sp, a project's slug and settings, with what the sidebar shows
// of the snapshot.
func (sn *projectSnap) side(sp SideProject) SideProject {
	if sn.board != nil {
		for _, t := range sn.board.Tasks {
			if tasks.GroupOf(t.Status) == tasks.NeedsYou {
				sp.NeedsYou = append(sp.NeedsYou, sideTask(t))
			}
		}
	}
	sp.Questions = sn.questions
	for _, r := range sn.recs {
		if r.State == thread.Resolved {
			continue
		}
		pr := sn.prs[r.ID]
		sp.Threads = append(sp.Threads, SideThread{Record: r, Status: sn.status[r.ID], Checks: pr.Checks, PR: pr.Number})
	}
	if !sn.noPRHost {
		sp.PRHostFails = sn.timing.GHFails
	}
	return sp
}

// serveProjectsWatch answers projects.watch and then streams the
// projects' state: a projects.changed line for every new one, until the
// client hangs up or the server stops.
func (s *Server) serveProjectsWatch(c net.Conn, br *bufio.Reader, req proto.Request) {
	wake := s.watch.add()
	defer s.watch.remove(wake)
	poll := envDuration(envWatchPoll)
	if poll <= 0 {
		poll = defaultWatchPoll
	}
	b, last := s.sideProjects(s.watch.generation(), poll)
	resp := proto.Response{ID: req.ID}
	resp.Result, _ = json.Marshal(struct {
		Projects json.RawMessage `json:"projects"`
	}{b})
	if err := writeJSONLine(c, resp); err != nil {
		return
	}
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, br)
		close(gone)
	}()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-gone:
			return
		case <-wake:
		case <-t.C:
		}
		if s.isStopping() {
			return
		}
		b, ver := s.sideProjects(s.watch.generation(), poll)
		if ver == last {
			continue
		}
		last = ver
		ev := struct {
			Event    string          `json:"event"`
			Projects json.RawMessage `json:"projects"`
		}{proto.EventProjectsChanged, b}
		if err := writeJSONLine(c, ev); err != nil {
			return
		}
	}
}

// ProjectsWatchStream is a watch of the projects (projects.watch).
type ProjectsWatchStream struct{ c *Client }

// WatchProjects watches the projects and returns the stream and the
// projects now.
func WatchProjects(p Paths) (*ProjectsWatchStream, []SideProject, error) {
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		return nil, nil, err
	}
	var w ProjectsWatch
	if err := c.Call(proto.MethodProjectsWatch, nil, &w); err != nil {
		c.Close()
		return nil, nil, err
	}
	return &ProjectsWatchStream{c: c}, w.Projects, nil
}

// Next blocks until the projects' next state.
func (s *ProjectsWatchStream) Next() ([]SideProject, error) {
	for {
		var ev ProjectsWatchEvent
		if err := readJSONLine(s.c.br, &ev); err != nil {
			return nil, err
		}
		if ev.Event == proto.EventProjectsChanged {
			return ev.Projects, nil
		}
	}
}

// Close ends the watch.
func (s *ProjectsWatchStream) Close() error { return s.c.Close() }
