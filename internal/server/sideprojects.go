package server

// Watching the projects (projects.watch, docs/SPEC.md §3.3, Watch): what
// the consoles' sidebars show of every project, sent again whenever it
// changes; with detail, what the dashboard shows of them too. Every
// watch looks every watchPoll and on every wake, as project.watch does,
// but they share one look (sideHub): a stat pass over the projects'
// files (the project cache's stamps) at most once a poll for all of
// them, and a read of only the projects whose files changed.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modstore"
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

	// The rest is the dashboard's, sent with detail (since protocol 18).

	// Goal and Repos are the project's (PROJECT.md, its repos); Own
	// the settings it sets itself (config.Own).
	Goal  string   `json:"goal,omitempty"`
	Repos []string `json:"repos,omitempty"`
	Own   []string `json:"own,omitempty"`
	// Counts tallies the board by group (project.Counts); nil when the
	// board doesn't load, and Error says why, or why the project
	// doesn't open.
	Counts map[string]int `json:"counts,omitempty"`
	Error  string         `json:"error,omitempty"`
	// Board are the board's tasks on deck and in the backlog, in board
	// order, without their notes and steps: what the dashboard lists of
	// the tasks no thread works on yet.
	Board []SideTask `json:"board,omitempty"`
	// Items are the coordinator's unhandled inbox items, oldest first.
	Items []project.Item `json:"items,omitempty"`
	// Usage is what all the project's threads used, resolved ones
	// included; TaskUsage splits it by task number.
	Usage     thread.Usage         `json:"usage,omitzero"`
	TaskUsage map[int]thread.Usage `json:"task_usage,omitempty"`
	// Checkouts are the ticker's notes on the repos whose local default
	// branch is behind origin, by repo path.
	Checkouts map[string]string `json:"checkouts,omitempty"`
	// Badges are the annotations of the mods that run for the project,
	// enabled with project_read, by target (modstore.Task,
	// modstore.Thread), in the mods' id order.
	Badges map[string][]modstore.Badge `json:"badges,omitempty"`
	// Links are the project's links to other projects' coordinators
	// (link.Rows).
	Links []link.Row `json:"links,omitempty"`
	// LinkNeeds are what those links wait on the user for, by the ids
	// link.act takes (link.Needs).
	LinkNeeds []link.Need `json:"link_needs,omitempty"`
}

// SideThread is one unresolved thread: its record (thread.toml), its
// STATUS.md (nil before it has one) and its pull request's checks and
// number as the ticker last saw them.
//
// With detail, ReportPR is its latest report's PR line and Task its
// task, for the steps; Check is that report's ## Check (how the user
// checks the work) and PRState where its PR stands as the ticker last
// saw it, OPEN, CLOSED or MERGED (since protocol 23), for the task
// view's review.
type SideThread struct {
	Record   *thread.Record `json:"record"`
	Status   *thread.Status `json:"status,omitempty"`
	Checks   string         `json:"checks,omitempty"`
	PR       int            `json:"pr,omitempty"`
	ReportPR string         `json:"report_pr,omitempty"`
	Task     *SideTask      `json:"task,omitempty"`
	Check    []string       `json:"check,omitempty"`
	PRState  string         `json:"pr_state,omitempty"`
}

// SideTask is a task (tasks.Task) on the wire.
type SideTask struct {
	ID     int          `json:"id"`
	Title  string       `json:"title"`
	Status tasks.Status `json:"status"`
	// Priority is must, should or could, absent for none (protocol 32).
	Priority tasks.Priority `json:"priority,omitempty"`
	Notes    string         `json:"notes,omitempty"`
	Steps    []tasks.Step   `json:"steps,omitempty"`
	Thread   string         `json:"thread,omitempty"`
	Owner    string         `json:"owner,omitempty"`
	Created  string         `json:"created,omitempty"`
	Updated  string         `json:"updated,omitempty"`
	Archived bool           `json:"archived,omitempty"`
}

// sideTask is t on the wire.
func sideTask(t *tasks.Task) SideTask {
	return SideTask{ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority, Notes: t.Notes, Steps: t.Steps, Thread: t.Thread,
		Owner: t.Owner, Created: t.Created, Updated: t.Updated, Archived: t.Archived}
}

// Task is the task t is on the wire.
func (t SideTask) Task() *tasks.Task {
	return &tasks.Task{ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority, Notes: t.Notes, Steps: t.Steps, Thread: t.Thread,
		Owner: t.Owner, Created: t.Created, Updated: t.Updated, Archived: t.Archived}
}

// sideHub is the projects as the sidebars show them, and as the
// dashboard does (detail), looked at once for all the projects watches.
type sideHub struct {
	mu sync.Mutex
	// out are the sidebars' looks [0] and the dashboard's [1].
	out [2]sideLook
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
	// badges are the mods' annotations read for the dashboard, by mod
	// id and project slug.
	badges map[string]badgeEntry
}

// sideLook is one kind of look: when, and what it found.
type sideLook struct {
	at     time.Time // the last look
	gen    uint64    // the watchers' generation it looked at
	looked bool
	b      json.RawMessage // the projects, marshalled
	ver    uint64          // counts the changes of b
	// parts are b's projects, each marshalled whole and, for an
	// inactive one, as its slug and settings alone (lean): what a lean
	// watch sends. A new look replaces the slice, never its items.
	parts []sidePart
}

// sidePart is one project of a look: whole, and lean when it is
// inactive (nil for an active one).
type sidePart struct {
	slug        string
	whole, lean json.RawMessage
}

// badgeEntry is a mod's annotations in a project (modstore.ReadBadges), with
// the stamp of its file.
type badgeEntry struct {
	stamp uint64
	at    time.Time
	b     map[string][]modstore.Badge
}

// sideProjects is the projects as the sidebars show them, or with detail
// as the dashboard does, marshalled (a []SideProject), and a version
// that changes with them. It looks again on a wake (gen) and once a
// poll at most: the first watch to ask after that looks for all.
func (s *Server) sideProjects(gen uint64, poll time.Duration, detail bool) (json.RawMessage, uint64) {
	l := s.sideLook(gen, poll, detail)
	return l.b, l.ver
}

// sideLook is the look sideProjects answers from, a copy.
func (s *Server) sideLook(gen uint64, poll time.Duration, detail bool) sideLook {
	h := &s.sides
	h.mu.Lock()
	defer h.mu.Unlock()
	l := &h.out[0]
	if detail {
		l = &h.out[1]
	}
	if l.looked && l.gen == gen && time.Since(l.at) < poll*3/4 {
		return *l
	}
	l.looked, l.gen, l.at = true, gen, time.Now()
	parts := sideParts(s.lookSideProjects(gen, detail))
	if b := joinParts(parts, false, ""); !slices.Equal(b, l.b) {
		l.b, l.ver, l.parts = b, l.ver+1, parts
	}
	return *l
}

// sideParts are list's projects marshalled, whole and lean.
func sideParts(list []SideProject) []sidePart {
	parts := make([]sidePart, 0, len(list))
	for _, sp := range list {
		pt := sidePart{slug: sp.Slug}
		pt.whole, _ = json.Marshal(sp)
		if sp.Safety != nil && !sp.Safety.Active {
			pt.lean, _ = json.Marshal(SideProject{Slug: sp.Slug, Safety: sp.Safety})
		}
		parts = append(parts, pt)
	}
	return parts
}

// joinParts is parts as a JSON array, as json.Marshal has the list.
// With lean the inactive projects but shown are their lean selves.
func joinParts(parts []sidePart, lean bool, shown string) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, pt := range parts {
		if i > 0 {
			b.WriteByte(',')
		}
		if lean && pt.lean != nil && pt.slug != shown {
			b.Write(pt.lean)
		} else {
			b.Write(pt.whole)
		}
	}
	b.WriteByte(']')
	return b.Bytes()
}

// leanOf is look l as a watch with p sends it: lean when p asks.
func leanOf(l sideLook, p proto.ProjectsWatchParams) json.RawMessage {
	if !p.Lean {
		return l.b
	}
	return joinParts(l.parts, true, p.Shown)
}

// lookSideProjects reads the projects as the sidebars show them, with
// detail as the dashboard does, the project cache reading again only
// those whose files changed.
func (s *Server) lookSideProjects(gen uint64, detail bool) []SideProject {
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
	var badgers []mod.Mod
	if detail {
		badgers = s.badgeMods()
	}
	out := []SideProject{}
	for _, slug := range h.slugs {
		snap := func() (*projectSnap, error) { return s.projects.stamped(slug, ts, gen) }
		var badges func() map[string][]modstore.Badge
		if detail {
			badges = func() map[string][]modstore.Badge { return h.badgesOf(badgers, slug) }
		}
		if sp, ok := sideProject(h.cfg, slug, snap, badges); ok {
			out = append(out, sp)
		}
	}
	return out
}

// sideProject is project slug as the sidebars show it, of the config cfg
// and its snapshot (snap); with badges, the mods' annotations of it, as
// the dashboard shows it (detail). False for an archived project.
func sideProject(cfg *config.Config, slug string, snap func() (*projectSnap, error), badges func() map[string][]modstore.Badge) (SideProject, bool) {
	safety, serr := cfg.Safety(slug)
	if serr == nil && safety.Archived {
		return SideProject{}, false // hidden: tm project unarchive brings it back
	}
	sp := SideProject{Slug: slug}
	sn, err := snap()
	if err != nil {
		if badges != nil {
			sp.Error = err.Error()
		}
		return sp, true
	}
	if serr == nil {
		sp.Safety = &safety
	}
	sp = sn.side(sp)
	if badges != nil {
		sp = sn.detail(sp, cfg.Own(slug))
		sp.Badges = badges()
	}
	return sp, true
}

// ReadProjects reads the projects as projects.watch with detail sends
// them, from their files and the ticker's state file at tickerState,
// with the annotations of mods, the installed mods, that show
// (BadgeMods): for a client that reads them itself while no watch runs
// (the dashboard without a server). It caches nothing. The error is
// config.toml's, with no projects.
func ReadProjects(tickerState string, mods []mod.Mod) ([]SideProject, error) {
	cfg, err := config.Load()
	if err != nil {
		return []SideProject{}, err
	}
	root, err := home.ProjectsDir()
	if err != nil {
		return []SideProject{}, err
	}
	_, slugs := projectDirs(root)
	badgers := BadgeMods(mods)
	out := []SideProject{}
	for _, slug := range slugs {
		snap := func() (*projectSnap, error) {
			p, err := project.Open(slug)
			if err != nil {
				return nil, err
			}
			return readProjectSnap(p, tickerState), nil
		}
		badges := func() map[string][]modstore.Badge { return readBadges(badgers, slug) }
		if sp, ok := sideProject(cfg, slug, snap, badges); ok {
			out = append(out, sp)
		}
	}
	return out, nil
}

// badgeMods are the installed mods whose annotations show on the
// dashboard (BadgeMods).
func (s *Server) badgeMods() []mod.Mod {
	reg := s.modRegistry()
	if reg == nil {
		return nil
	}
	return BadgeMods(reg.Mods)
}

// BadgeMods are the mods of mods whose annotations show on the
// dashboard: enabled, with the project_read grant that $.annotate
// needs. A mod disabled, regranted or removed shows no badges.
func BadgeMods(mods []mod.Mod) []mod.Mod {
	var out []mod.Mod
	for _, m := range mods {
		if m.State == mod.Enabled && m.Granted(mod.PermProjectRead) {
			out = append(out, m)
		}
	}
	return out
}

// readBadges reads the annotations of mods that run for project slug,
// by target, in the mods' order, as badgesOf has them uncached.
func readBadges(mods []mod.Mod, slug string) map[string][]modstore.Badge {
	state, err := home.ModStateDir()
	if err != nil {
		return nil
	}
	var out map[string][]modstore.Badge
	for _, m := range mods {
		if !m.Runs(slug) {
			continue
		}
		for target, bs := range modstore.ReadBadges(filepath.Join(state, m.ID), m.ID, slug) {
			if out == nil {
				out = map[string][]modstore.Badge{}
			}
			out[target] = append(out[target], bs...)
		}
	}
	return out
}

// badgesOf are the annotations of mods that run for project slug, by
// target, in the mods' order; a mod's file is read again when its stamp
// changed, or snapMaxAge after.
func (h *sideHub) badgesOf(mods []mod.Mod, slug string) map[string][]modstore.Badge {
	state, err := home.ModStateDir()
	if err != nil {
		return nil
	}
	if h.badges == nil {
		h.badges = map[string]badgeEntry{}
	}
	var out map[string][]modstore.Badge
	for _, m := range mods {
		if !m.Runs(slug) {
			continue
		}
		dir := filepath.Join(state, m.ID)
		key, st := m.ID+"/"+slug, fsx.StatStamp(filepath.Join(dir, modstore.AnnotationsFile))
		e, ok := h.badges[key]
		if !ok || e.stamp != st || time.Since(e.at) >= snapMaxAge {
			e = badgeEntry{stamp: st, at: time.Now(), b: modstore.ReadBadges(dir, m.ID, slug)}
			h.badges[key] = e
		}
		for target, bs := range e.b {
			if out == nil {
				out = map[string][]modstore.Badge{}
			}
			out[target] = append(out[target], bs...)
		}
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

// detail is sp, the snapshot's side, with what the dashboard shows of
// it besides; own are the settings the project sets itself.
func (sn *projectSnap) detail(sp SideProject, own []string) SideProject {
	sp.Goal, sp.Repos, sp.Own = sn.p.Meta.Goal, sn.p.Repos, own
	if sn.board != nil {
		sp.Counts = project.Counts(sn.board)
		for _, t := range sn.board.Tasks {
			if g := tasks.GroupOf(t.Status); g == tasks.OnDeck || g == tasks.Backlog {
				st := sideTask(t)
				st.Notes, st.Steps = "", nil
				sp.Board = append(sp.Board, st)
			}
		}
	} else if sn.boardErr != nil {
		sp.Error = sn.boardErr.Error()
	}
	sp.Items = sn.items
	sp.Usage, sp.TaskUsage = sn.usage, sn.taskUsage
	if len(sn.checkouts) > 0 {
		sp.Checkouts = sn.checkouts
	}
	sp.Links, sp.LinkNeeds = sn.links, sn.linkNeeds
	for i := range sp.Threads {
		th := &sp.Threads[i]
		th.ReportPR, th.Check = sn.reportPR[th.Record.ID], sn.reportCheck[th.Record.ID]
		th.PRState = prState(sn.prs[th.Record.ID])
		if sn.board != nil && th.Record.TaskID() > 0 {
			if t := sn.board.Find(th.Record.TaskID()); t != nil {
				st := sideTask(t)
				th.Task = &st
			}
		}
	}
	return sp
}

// prState is where pr stands, OPEN, CLOSED or MERGED; MERGED once the
// ticker saw its merge commit, "" when it hasn't seen the PR.
func prState(pr ticker.PR) string {
	if pr.Merge != "" {
		return "MERGED"
	}
	return pr.State
}

// serveProjectsWatch answers projects.watch and then streams the
// projects' state: a projects.changed line for every new one, until the
// client hangs up or the server stops.
func (s *Server) serveProjectsWatch(c net.Conn, br *bufio.Reader, req proto.Request) {
	var p proto.ProjectsWatchParams
	if perr := decodeParams(req.Params, &p); perr != nil {
		refuse(c, req, perr)
		return
	}
	wake := s.watch.add()
	defer s.watch.remove(wake)
	poll := watchPollEvery()
	l := s.sideLook(s.watch.generation(), poll, p.Detail)
	last, sent := l.ver, leanOf(l, p)
	if !answer(c, req, struct {
		Projects json.RawMessage `json:"projects"`
	}{sent}) {
		return
	}
	pollStream(c, clientGone(br), wake, poll, func() (any, bool) {
		if s.isStopping() {
			return nil, true
		}
		l := s.sideLook(s.watch.generation(), poll, p.Detail)
		if l.ver == last {
			return nil, false
		}
		last = l.ver
		b := leanOf(l, p)
		if p.Lean && bytes.Equal(b, sent) {
			return nil, false // a change in a lean project alone
		}
		sent = b
		return struct {
			Event    string          `json:"event"`
			Projects json.RawMessage `json:"projects"`
		}{proto.EventProjectsChanged, b}, false
	})
}

// ProjectsWatchStream is a watch of the projects (projects.watch).
type ProjectsWatchStream struct{ c *client.Client }

// WatchProjects watches the projects, with detail what the dashboard
// shows of them too, and returns the stream and the projects now.
func WatchProjects(p client.Paths, detail bool) (*ProjectsWatchStream, []SideProject, error) {
	var params any
	if detail {
		params = proto.ProjectsWatchParams{Detail: true}
	}
	var w ProjectsWatch
	c, err := client.OpenStream(p, proto.MethodProjectsWatch, params, &w)
	if err != nil {
		return nil, nil, err
	}
	return &ProjectsWatchStream{c: c}, w.Projects, nil
}

// Next blocks until the projects' next state.
func (s *ProjectsWatchStream) Next() ([]SideProject, error) {
	ev, err := client.NextEvent(s.c, func(ev ProjectsWatchEvent) bool { return ev.Event == proto.EventProjectsChanged })
	return ev.Projects, err
}

// Close ends the watch.
func (s *ProjectsWatchStream) Close() error { return s.c.Close() }
