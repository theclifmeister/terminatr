package tui

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// The attach console's poll (pollState) reads what its sidebar, status
// bar and info panel show from the project folders twice a second. It
// keeps what it read with the stat stamps (fsx.StatStamp) of the files
// it read it from, as the server's project cache does
// (server/projectcache.go), and reads a file again only when its stamp
// changed, or sideMaxAge after, for what a stamp can't see: an idle
// console costs a few stats a tick and no file read.

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

// sideCache is what the poll last read for the sidebar: the ticker's
// PRs and each listed project's files, with the config conf gives. A
// config conf loaded again, or a change of the projects folder, reads
// every project again; a change of the ticker's state the ticker's part
// of each; one of a project's files that project.
type sideCache struct {
	conf  *configCache
	loads int    // conf's loads at the last full read
	stamp uint64 // the projects folder's
	tick  uint64 // the ticker's state file's
	fails map[string]int
	projs map[string]*sideProj
	list  []ProjectData
}

// sideProj is one listed project's sidebar data, as read (base) and
// with the ticker's PRs (data), with the stamp of the files it was read
// from (paths).
type sideProj struct {
	stamp      uint64
	paths      []string
	p          *project.Project // nil when it doesn't open
	base, data ProjectData
}

// load is the sidebar's projects (loadSideProjects), read again where
// their files changed, and whether they changed since the last load.
func (sc *sideCache) load(paths server.Paths) ([]ProjectData, bool) {
	if sc.conf == nil {
		sc.conf = &configCache{}
	}
	cfg, cfgErr := sc.conf.get()
	state := ticker.StatePath(paths.Sessions)
	root, _ := home.ProjectsDir()
	stamp := fsx.StatStamp(root)
	tick := fsx.StatStamp(state)
	read := false
	if sc.projs == nil || stamp != sc.stamp || sc.loads != sc.conf.loads {
		// Stamped before the reads: a change during them reads again.
		sc.stamp, sc.loads = stamp, sc.conf.loads
		sc.tick, sc.fails = tick, ticker.HostFails(state)
		next := map[string]*sideProj{}
		var slugs []string
		if cfgErr == nil { // none, as project.List gives
			slugs = projectSlugs(root)
		}
		for _, slug := range slugs {
			e := &sideProj{}
			if old := sc.projs[slug]; old != nil {
				e.stamp = fsx.StatStamp(old.paths...)
			}
			if e.read(slug, cfg) {
				e.data = withTicker(e.base, e.p, ticker.PRs(state, slug), sc.fails)
				next[slug] = e
			}
		}
		sc.projs, read = next, true
	} else {
		retick := tick != sc.tick
		if retick {
			sc.tick, sc.fails = tick, ticker.HostFails(state)
		}
		for slug, e := range sc.projs {
			if st := fsx.StatStamp(e.paths...); st != e.stamp {
				e.stamp = st
				if !e.read(slug, cfg) {
					delete(sc.projs, slug)
					read = true
					continue
				}
			} else if !retick {
				continue
			}
			e.data, read = withTicker(e.base, e.p, ticker.PRs(state, slug), sc.fails), true
		}
	}
	if !read && sc.list != nil {
		return sc.list, false
	}
	list := make([]ProjectData, 0, len(sc.projs))
	for _, slug := range slices.Sorted(maps.Keys(sc.projs)) {
		list = append(list, sc.projs[slug].data)
	}
	if sc.list != nil && reflect.DeepEqual(list, sc.list) {
		return sc.list, false
	}
	sc.list = list
	return list, true
}

// projectSlugs are the projects in the projects folder root, as
// project.List lists them.
func projectSlugs(root string) []string {
	if root == "" {
		return nil
	}
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !project.ValidSlug(e.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "PROJECT.md")); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}

// read reads project slug's sidebar data (but the ticker's) with config
// cfg, and the paths it read from; false for an archived project, which
// the sidebar doesn't list.
func (e *sideProj) read(slug string, cfg *config.Config) bool {
	e.base, e.p = ProjectData{Slug: slug}, nil
	dir, _ := project.Dir(slug)
	e.paths = []string{dir, filepath.Join(dir, "PROJECT.md")}
	p, err := project.OpenWith(slug, cfg)
	if err != nil {
		return true
	}
	if safety, err := cfg.Safety(slug); err == nil {
		if safety.Archived {
			return false
		}
		e.base.Safety = &safety
	}
	e.p = p
	// What NEEDS YOU counts, for the badge of a hidden sidebar.
	if b, err := p.Tasks().Load(); err == nil {
		for _, t := range b.Tasks {
			if tasks.GroupOf(t.Status) == tasks.NeedsYou {
				e.base.NeedsYou = append(e.base.NeedsYou, t)
			}
		}
	}
	qs, _ := p.Questions()
	e.base.Questions = len(qs)
	e.paths = append(e.paths, p.Path("TASKS.md"), p.Path("questions.json"), p.Path("threads"))
	for _, r := range p.Repos {
		e.paths = append(e.paths, filepath.Join(r, ".git", "config")) // their origin, for hostFails
	}
	recs, _ := thread.List(p)
	for _, r := range recs {
		// A thread's record and status are rewritten by rename, which
		// its folder's mtime doesn't show in threads'.
		e.paths = append(e.paths, thread.Path(p, r.ID, "thread.toml"))
		if r.State == thread.Resolved {
			continue
		}
		e.paths = append(e.paths, thread.Path(p, r.ID, "STATUS.md"))
		tr := ThreadRow{Record: r}
		tr.Status, _ = thread.ReadStatus(p, r.ID)
		e.base.Threads = append(e.base.Threads, tr)
	}
	return true
}

// withTicker is base with the ticker's PRs (prs, by thread) and failing
// PR polls (fails, by project) of project p.
func withTicker(base ProjectData, p *project.Project, prs map[string]ticker.PR, fails map[string]int) ProjectData {
	pd := base
	if p == nil {
		return pd
	}
	pd.Threads = slices.Clone(base.Threads)
	for i, tr := range pd.Threads {
		pd.Threads[i].Checks, pd.Threads[i].PR = prs[tr.ID].Checks, prs[tr.ID].Number
	}
	pd.PRHostFails = hostFails(fails, p)
	return pd
}

// loadSideProjects reads the projects and their open threads, with their
// STATUS.md and their PRs' checks (the ticker's), and what needs you,
// from the project folders: what the attach client's sidebar shows
// (polled through a sideCache).
func loadSideProjects(paths server.Paths) []ProjectData {
	var sc sideCache
	list, _ := sc.load(paths)
	return list
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
