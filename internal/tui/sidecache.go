package tui

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// The attach console's poll (pollState) reads what its status bar and
// info panel show from the project folders twice a second. It keeps
// what it read with the stat stamps (fsx.StatStamp) of the files it read
// it from, as the server's project cache does (server/projectcache.go),
// and reads a file again only when its stamp changed, or sideMaxAge
// after, for what a stamp can't see: an idle console costs a few stats a
// tick and no file read. The sidebar's projects come from the server
// (sideFeed), which looks once for every console.

// sideMaxAge is how long what the poll read serves at most.
const sideMaxAge = 30 * time.Second

// configCache is config.Load's config, loaded again when config.toml's
// stamp changed, or sideMaxAge after: the poll parses it at most once a
// tick, for all that reads it.
type configCache struct {
	stamp uint64
	at    time.Time
	cfg   *config.Config
	err   error
	loads int // how many times it loaded, for sideCache
}

// get is the config, and config.Load's error.
func (cc *configCache) get() (*config.Config, error) {
	path, _ := config.Path()
	if st := fsx.StatStamp(path); cc.loads == 0 || st != cc.stamp || time.Since(cc.at) >= sideMaxAge {
		cc.stamp, cc.at, cc.loads = st, time.Now(), cc.loads+1
		cc.cfg, cc.err = config.Load()
	}
	return cc.cfg, cc.err
}

// uiHints are the config's [ui] context_hint and needs_first.
func (cc *configCache) uiHints() (ctxHint int, needsFirst bool) {
	if cfg, err := cc.get(); err == nil {
		return cfg.ContextHint, cfg.NeedsFirst
	}
	return config.DefaultContextHint, false
}

// sideFeed is the sidebar's projects as the server's projects.watch
// sends them (server.WatchProjects): every console's sidebar from the
// one look the server takes for all, not a stat pass of its own. run
// keeps the latest; the poll takes it once.
type sideFeed struct {
	mu    sync.Mutex
	st    *server.ProjectsWatchStream // the stream run starts with; nil to dial
	list  []ProjectData
	fresh bool // list not taken yet
}

// watchSide is a sideFeed with the projects now, watched from then on
// by its run: nil projects when the server doesn't answer, which run
// asks again.
func watchSide(paths server.Paths) (*sideFeed, []ProjectData) {
	f := &sideFeed{}
	st, ps, err := server.WatchProjects(paths)
	if err != nil {
		return f, nil
	}
	f.st = st
	return f, sideProjects(ps)
}

// run keeps the feed's projects current until ctx ends, watching again
// statePoll after a lost watch.
func (f *sideFeed) run(ctx context.Context, paths server.Paths) {
	f.mu.Lock()
	st := f.st
	f.st = nil
	f.mu.Unlock()
	for {
		if st == nil {
			var ps []server.SideProject
			var err error
			if st, ps, err = server.WatchProjects(paths); err == nil {
				f.put(ps)
			}
		}
		if st != nil {
			stop := context.AfterFunc(ctx, func() { st.Close() })
			for {
				ps, err := st.Next()
				if err != nil {
					break
				}
				f.put(ps)
			}
			if stop() {
				st.Close()
			}
			st = nil
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(statePoll):
		}
	}
}

// close closes the watch watchSide opened, unless run took it.
func (f *sideFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.st != nil {
		f.st.Close()
		f.st = nil
	}
}

// put keeps ps, the projects the server sent, for take.
func (f *sideFeed) put(ps []server.SideProject) {
	list := sideProjects(ps)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list, f.fresh = list, true
}

// take is the projects the server sent since the last take, and
// whether it sent any.
func (f *sideFeed) take() ([]ProjectData, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.fresh {
		return nil, false
	}
	f.fresh = false
	return f.list, true
}

// sideProjects are the projects as the server sent them, as the sidebar
// keeps them; never nil.
func sideProjects(ps []server.SideProject) []ProjectData {
	out := make([]ProjectData, 0, len(ps))
	for _, sp := range ps {
		pd := ProjectData{Slug: sp.Slug, Safety: sp.Safety, Questions: sp.Questions, PRHostFails: sp.PRHostFails}
		for _, t := range sp.NeedsYou {
			pd.NeedsYou = append(pd.NeedsYou, t.Task())
		}
		for _, th := range sp.Threads {
			if th.Record == nil {
				continue
			}
			pd.Threads = append(pd.Threads, ThreadRow{Record: th.Record, Status: th.Status, Checks: th.Checks, PR: th.PR})
		}
		out = append(out, pd)
	}
	return out
}

// statusCache keeps the STATUS.md of the thread sessions the panes show
// (threadStatuses), by path, with its stamp.
type statusCache map[string]statusEntry

type statusEntry struct {
	stamp uint64
	st    *thread.Status
	seen  bool
}

// statuses reads the STATUS.md of every thread session, by session id,
// so the status bar shows the progress the dashboard shows (docs/SPEC.md
// §7.3), again only where it changed.
func (sc statusCache) statuses(sessions []proto.SessionInfo) map[string]*thread.Status {
	out := map[string]*thread.Status{}
	for k, e := range sc {
		e.seen = false
		sc[k] = e
	}
	for _, s := range sessions {
		if s.Role != proto.RoleThread || s.Project == "" || !thread.ValidID(s.Thread) || s.Server != "" {
			continue
		}
		dir, err := project.Dir(s.Project)
		if err != nil {
			continue
		}
		p := &project.Project{Slug: s.Project, Dir: dir}
		path := thread.Path(p, s.Thread, "STATUS.md")
		stamp := fsx.StatStamp(path)
		e, ok := sc[path]
		if !ok || e.stamp != stamp {
			e = statusEntry{stamp: stamp}
			if _, err := os.Stat(p.Path("PROJECT.md")); err == nil {
				e.st, _ = thread.ReadStatus(p, s.Thread)
			}
		}
		e.seen = true
		sc[path] = e
		if e.st != nil {
			out[s.ID] = e.st
		}
	}
	for k, e := range sc {
		if !e.seen {
			delete(sc, k)
		}
	}
	return out
}

// infoCache is the info panel's data as the poll last read it (loadInfo)
// for a thread, with the stamp of its files; a coordinator's is built
// from the project snapshot snaps keeps (server.ProjectSnaps), as the
// server's project.watch.
type infoCache struct {
	key   string // the thread: project/id
	stamp uint64
	at    time.Time
	d     *infoData
	snaps server.ProjectSnaps
}

// load is loadInfo's data for session s, one of sessions, read again
// when its files changed.
func (ic *infoCache) load(paths server.Paths, s proto.SessionInfo, sessions []proto.SessionInfo) *infoData {
	ts := ""
	if paths.Sessions != "" {
		ts = ticker.StatePath(paths.Sessions)
	}
	if s.Role == proto.RoleCoordinator && s.Project != "" {
		d := &infoData{slug: s.Project, session: s}
		w, err := ic.snaps.Watch(s.Project, sessions, ts)
		if err != nil {
			w = proto.ProjectWatch{Project: s.Project}
		}
		d.watch = &w
		return d
	}
	if s.Role != proto.RoleThread || s.Project == "" || !thread.ValidID(s.Thread) {
		return nil
	}
	dir, err := project.Dir(s.Project)
	if err != nil {
		return &infoData{slug: s.Project, session: s}
	}
	p := &project.Project{Slug: s.Project, Dir: dir}
	key := s.Project + "/" + s.Thread
	files := []string{p.Path("PROJECT.md"), p.Path("TASKS.md"), p.Path("JOURNAL.md"), ts,
		thread.Dir(p, s.Thread), thread.Path(p, s.Thread, "library")}
	for _, name := range []string{"thread.toml", "STATUS.md", "REPORT.md"} {
		files = append(files, thread.Path(p, s.Thread, name))
	}
	stamp := fsx.StatStamp(files...)
	if ic.d == nil || ic.key != key || ic.stamp != stamp || time.Since(ic.at) >= sideMaxAge {
		ic.key, ic.stamp, ic.at = key, stamp, time.Now()
		ic.d = loadInfo(paths, s, sessions)
	}
	d := *ic.d
	d.session = s
	return &d
}
