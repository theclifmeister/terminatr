package peer

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The peer mirror (docs/SPEC.md §16.10): the latest snapshot of every
// shared project of every paired server, kept in memory and rebuilt on
// each link up. While a link is up and its peer exports (CapWatch), the
// mirror lists the peer's projects (peer.projects) and long-polls each
// (peer.watch); when the link drops, it keeps the last snapshots and
// marks them stale. Each answer carries the peer's clock, from which the
// mirror measures the offset between the two. Consoles read it with
// peer.list; nothing here writes the peer.

// links is what the mirror needs of the Manager.
type links interface {
	Status() *proto.LinkStatus
	Call(ctx context.Context, alias, method string, params, result any) error
}

// MirrorOptions configure a Mirror.
type MirrorOptions struct {
	// Changed is told when the mirror changes; it must not block. Nil
	// tells nothing.
	Changed func()
	// Logf logs what the mirror couldn't read; nil logs nothing.
	Logf func(format string, a ...any)
	// Panes is told when a peer's pane switch for this server changes
	// (proto.PanesOff …), from its peer.projects or its peer.panes; it
	// must not block. Nil tells nothing.
	Panes func(alias, level string)
}

// Mirror timing.
var (
	// mirrorPoll is how often the mirror looks at the links' states.
	mirrorPoll = 500 * time.Millisecond
	// mirrorProjects is how often an up link's project list is read
	// again: a project shared or unshared there shows within it.
	mirrorProjects = 30 * time.Second
	// mirrorRetry is the wait after a read that failed.
	mirrorRetry = 2 * time.Second
)

// Mirror keeps the paired servers' shared projects.
type Mirror struct {
	links links
	opts  MirrorOptions
	ctx   context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup

	mu    sync.Mutex
	gen   uint64
	peers map[string]*mirrorPeer // by server id
}

// mirrorPeer is one paired server.
type mirrorPeer struct {
	link proto.PeerLink
	// offset is its clock minus ours, set once an answer carried it.
	offset    time.Duration
	hasOffset bool
	// panes is the peer's pane switch for this server, as its last
	// peer.projects said.
	panes    string
	projects map[string]*mirrorProject
	// ctx is the reading's while the link is up and the peer exports,
	// nil otherwise; stop ends it and every watch under it.
	ctx  context.Context
	stop context.CancelFunc
}

// mirrorProject is one shared project of a peer.
type mirrorProject struct {
	line   proto.PeerProject
	w      *proto.PeerWatch // the last snapshot, never changed once set
	synced time.Time
	stale  bool
	// ctx is its watch's, nil while none runs; stop ends it.
	ctx  context.Context
	stop context.CancelFunc
}

// StartMirror mirrors the shared projects of l's paired servers until
// Close.
func StartMirror(l *Manager, opts MirrorOptions) *Mirror {
	m := startMirror(l, opts)
	l.OnPanes(func(id ID, level string) { m.SetPanes(id.String(), level) })
	return m
}

// SetPanes takes the peer id's word that its pane switch for this server
// is level now (peer.panes), ahead of its next peer.projects.
func (m *Mirror) SetPanes(id, level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.peers[id]; p != nil {
		m.setPanes(p, level)
	}
}

// SetPanesOf is SetPanes for the peer called alias: what a stream it
// just opened says (peer.attach's input), which a tm without peer.panes
// would otherwise only tell at its next peer.projects.
func (m *Mirror) SetPanesOf(alias, level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.peers {
		if p.link.Alias == alias {
			m.setPanes(p, level)
		}
	}
}

// setPanes records p's pane switch for this server and tells; mu held.
func (m *Mirror) setPanes(p *mirrorPeer, level string) bool {
	l, ok := proto.ParsePanes(level)
	if !ok && level != "" {
		return false // not a level: the peer's word is ignored
	}
	level = l
	if p.panes == level {
		return false
	}
	p.panes = level
	m.changed()
	if m.opts.Panes != nil {
		m.opts.Panes(p.link.Alias, level)
	}
	return true
}

func startMirror(l links, opts MirrorOptions) *Mirror {
	m := &Mirror{links: l, opts: opts, peers: map[string]*mirrorPeer{}}
	m.ctx, m.stop = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.run()
	return m
}

// Close stops reading and waits for every read.
func (m *Mirror) Close() {
	if m == nil {
		return
	}
	m.stop()
	m.wg.Wait()
}

func (m *Mirror) run() {
	defer m.wg.Done()
	t := time.NewTicker(mirrorPoll)
	defer t.Stop()
	for {
		m.sync()
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sync follows the links: a peer whose link came up (and exports) is
// read, one whose link went down is marked stale, a removed one goes.
func (m *Mirror) sync() {
	st := m.links.Status()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return
	}
	changed := false
	seen := map[string]bool{}
	if st != nil {
		for _, l := range st.Peers {
			seen[l.ID] = true
			p := m.peers[l.ID]
			if p == nil {
				p = &mirrorPeer{projects: map[string]*mirrorProject{}}
				m.peers[l.ID] = p
				changed = true
			}
			if p.link.Alias != l.Alias || p.link.State != l.State || p.link.Reason != l.Reason {
				changed = true
			}
			p.link = l
			live := l.State == proto.LinkUp && slices.Contains(l.Caps, CapWatch)
			switch {
			case live && p.ctx == nil:
				p.ctx, p.stop = context.WithCancel(m.ctx)
				m.wg.Add(1)
				go m.read(p.ctx, l.ID, l.Alias)
			case !live && p.ctx != nil:
				m.down(p)
				changed = true
			}
		}
	}
	for id, p := range m.peers {
		if !seen[id] {
			if p.stop != nil {
				p.stop()
			}
			delete(m.peers, id)
			changed = true
		}
	}
	if changed {
		m.changed()
	}
}

// down ends p's reading and marks its projects stale; mu held.
func (m *Mirror) down(p *mirrorPeer) {
	p.stop()
	p.ctx, p.stop = nil, nil
	for _, pr := range p.projects {
		pr.stale, pr.ctx, pr.stop = true, nil, nil
	}
}

// changed counts a change and tells; mu held.
func (m *Mirror) changed() {
	m.gen++
	if m.opts.Changed != nil {
		m.opts.Changed()
	}
}

func (m *Mirror) logf(format string, a ...any) {
	if m.opts.Logf != nil {
		m.opts.Logf(format, a...)
	}
}

// peer is the peer id while ctx is its reading; mu held.
func (m *Mirror) peer(ctx context.Context, id string) *mirrorPeer {
	p := m.peers[id]
	if p == nil || p.ctx != ctx || ctx.Err() != nil {
		return nil
	}
	return p
}

// read lists a peer's shared projects while ctx lasts, now and every
// mirrorProjects, and watches each.
func (m *Mirror) read(ctx context.Context, id, alias string) {
	defer m.wg.Done()
	failed := false
	for {
		var ps proto.PeerProjects
		err := m.links.Call(ctx, alias, proto.MethodPeerProjects, nil, &ps)
		recv := time.Now()
		if ctx.Err() != nil {
			return
		}
		wait := mirrorProjects
		if err != nil {
			if !failed && !errors.Is(err, errNotUp) {
				m.logf("peer %s: peer.projects: %v", alias, err)
			}
			failed, wait = true, mirrorRetry
		} else {
			failed = false
			m.setProjects(ctx, id, alias, ps, recv)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// setProjects takes a peer.projects answer: new projects are watched,
// gone ones dropped.
func (m *Mirror) setProjects(ctx context.Context, id, alias string, ps proto.PeerProjects, recv time.Time) {
	ps = cleanProjects(ps)
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.peer(ctx, id)
	if p == nil {
		return
	}
	changed := m.measure(p, ps.Now, recv)
	m.setPanes(p, ps.Panes)
	want := map[string]proto.PeerProject{}
	for _, line := range ps.Projects {
		want[line.Project] = line
	}
	for slug := range p.projects {
		if _, ok := want[slug]; !ok {
			if pr := p.projects[slug]; pr.stop != nil {
				pr.stop()
			}
			delete(p.projects, slug)
			changed = true
		}
	}
	for slug, line := range want {
		pr := p.projects[slug]
		if pr == nil {
			pr = &mirrorProject{stale: true}
			p.projects[slug] = pr
			changed = true
		}
		if pr.w == nil && pr.line != line {
			// Counted from the snapshot once there is one.
			pr.line = line
			changed = true
		} else if pr.line.Goal != line.Goal || pr.line.Paused != line.Paused {
			pr.line.Goal, pr.line.Paused = line.Goal, line.Paused
			changed = true
		}
		pr.line.Project = slug
		if pr.ctx == nil {
			pr.ctx, pr.stop = context.WithCancel(ctx)
			m.wg.Add(1)
			go m.watch(pr.ctx, ctx, id, alias, slug)
		}
	}
	if changed {
		m.changed()
	}
}

// measure takes the peer's clock now from an answer received at recv,
// less half the link's last round trip; it says whether the offset
// moved by a second or more. mu held.
func (m *Mirror) measure(p *mirrorPeer, now, recv time.Time) bool {
	if now.IsZero() {
		return false
	}
	rtt := time.Duration(p.link.RTT * float64(time.Millisecond))
	off := now.Sub(recv) + rtt/2
	moved := !p.hasOffset || (off-p.offset).Abs() >= time.Second
	p.offset, p.hasOffset = off, true
	return moved
}

// watch long-polls one project while ctx lasts: each answer is the new
// snapshot, a failed read marks it stale until the next, and one the
// peer no longer shares is dropped.
func (m *Mirror) watch(ctx, peerCtx context.Context, id, alias, slug string) {
	defer m.wg.Done()
	// "" answers at once: a link that came back clears stale without
	// waiting for a change.
	rev := ""
	for {
		var w proto.PeerWatch
		cctx, cancel := context.WithTimeout(ctx, (proto.PeerWatchMaxWait+30)*time.Second)
		err := m.links.Call(cctx, alias, proto.MethodPeerWatch, proto.PeerWatchParams{Project: slug, Rev: rev}, &w)
		cancel()
		recv := time.Now()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			var pe *proto.Error
			if errors.As(err, &pe) && pe.Code == proto.ErrNotShared {
				m.drop(ctx, peerCtx, id, slug)
				return
			}
			if m.setStale(ctx, peerCtx, id, slug) && !errors.Is(err, errNotUp) {
				m.logf("peer %s: peer.watch %s: %v", alias, slug, err)
			}
			rev = ""
			select {
			case <-ctx.Done():
				return
			case <-time.After(mirrorRetry):
			}
			continue
		}
		rev = w.Rev
		m.setWatch(ctx, peerCtx, id, slug, w, recv)
	}
}

// project is the peer's project slug while ctx is its watch; mu held.
func (m *Mirror) project(ctx, peerCtx context.Context, id, slug string) (*mirrorPeer, *mirrorProject) {
	p := m.peer(peerCtx, id)
	if p == nil {
		return nil, nil
	}
	pr := p.projects[slug]
	if pr == nil || pr.ctx != ctx {
		return nil, nil
	}
	return p, pr
}

func (m *Mirror) setWatch(ctx, peerCtx context.Context, id, slug string, w proto.PeerWatch, recv time.Time) {
	w = cleanWatch(w)
	m.mu.Lock()
	defer m.mu.Unlock()
	p, pr := m.project(ctx, peerCtx, id, slug)
	if pr == nil {
		return
	}
	changed := m.measure(p, w.Now, recv)
	if pr.stale || pr.w == nil || pr.w.Rev != w.Rev {
		changed = true
	}
	pr.w, pr.synced, pr.stale = &w, recv, false
	pr.line.NeedsYou, pr.line.Threads, pr.line.Ready = len(w.Watch.NeedsYou), len(w.Watch.Threads), len(w.Watch.Ready)
	if changed {
		m.changed()
	}
}

// setStale marks a project stale after a failed read; it says whether
// it wasn't already.
func (m *Mirror) setStale(ctx, peerCtx context.Context, id, slug string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, pr := m.project(ctx, peerCtx, id, slug); pr != nil && !pr.stale {
		pr.stale = true
		m.changed()
		return true
	}
	return false
}

func (m *Mirror) drop(ctx, peerCtx context.Context, id, slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, pr := m.project(ctx, peerCtx, id, slug); pr != nil {
		delete(p.projects, slug)
		m.changed()
	}
}

// List is the mirror for peer.list: every paired server by alias, its
// link as it is now, and its projects by slug.
func (m *Mirror) List() proto.PeerList {
	out := proto.PeerList{Peers: []proto.PeerMirror{}}
	if m == nil {
		return out
	}
	st := m.links.Status()
	m.mu.Lock()
	defer m.mu.Unlock()
	out.Gen = m.gen
	if st == nil {
		return out
	}
	for _, l := range st.Peers {
		pm := proto.PeerMirror{Alias: l.Alias, ID: l.ID, State: l.State, Reason: l.Reason, Since: l.Since, RTT: l.RTT,
			Watch: slices.Contains(l.Caps, CapWatch), Pane: slices.Contains(l.Caps, CapPane), Version: l.Version,
			Projects: []proto.PeerMirrorProject{}}
		if p := m.peers[l.ID]; p != nil {
			if pm.Pane {
				pm.Panes = p.panes
			}
			if p.hasOffset {
				pm.Offset = p.offset.Milliseconds()
			}
			for _, slug := range slices.Sorted(maps.Keys(p.projects)) {
				pr := p.projects[slug]
				mp := proto.PeerMirrorProject{PeerProject: pr.line, Stale: pr.stale || l.State != proto.LinkUp, Synced: pr.synced}
				if pr.w != nil {
					mp.Rev, mp.Watch, mp.Sessions = pr.w.Rev, &pr.w.Watch, pr.w.Sessions
				}
				pm.Projects = append(pm.Projects, mp)
			}
		}
		out.Peers = append(out.Peers, pm)
	}
	slices.SortFunc(out.Peers, func(a, b proto.PeerMirror) int { return strings.Compare(a.Alias, b.Alias) })
	return out
}
