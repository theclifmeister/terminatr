package peer

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// fakeLinks is a Manager whose links a test sets, and whose peers
// answer peer.projects and peer.watch from their projects' thread
// counts, their clocks skew ahead.
type fakeLinks struct {
	mu      sync.Mutex
	st      *proto.LinkStatus
	skew    time.Duration
	threads map[string]int // by slug: the shared projects
	calls   []string
	changes chan struct{}
	panes   string // the peers' pane switch, in peer.projects
}

func newFakeLinks(skew time.Duration, threads map[string]int, peers ...proto.PeerLink) *fakeLinks {
	return &fakeLinks{st: &proto.LinkStatus{ID: "own", Peers: peers}, skew: skew, threads: threads, changes: make(chan struct{})}
}

func (f *fakeLinks) Status() *proto.LinkStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.st == nil {
		return nil
	}
	st := *f.st
	st.Peers = slices.Clone(st.Peers)
	return &st
}

// set changes alias's link state, or removes it with state "".
func (f *fakeLinks) set(alias, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st.Peers = slices.DeleteFunc(f.st.Peers, func(p proto.PeerLink) bool {
		if p.Alias != alias {
			return false
		}
		return state == ""
	})
	for i := range f.st.Peers {
		if f.st.Peers[i].Alias == alias {
			f.st.Peers[i].State = state
		}
	}
	f.signal()
}

// change sets a project's thread count (-1: no longer shared).
func (f *fakeLinks) change(slug string, threads int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if threads < 0 {
		delete(f.threads, slug)
	} else {
		f.threads[slug] = threads
	}
	f.signal()
}

// signal wakes every waiting peer.watch; mu held.
func (f *fakeLinks) signal() {
	close(f.changes)
	f.changes = make(chan struct{})
}

func (f *fakeLinks) up(alias string) bool {
	for _, p := range f.st.Peers {
		if p.Alias == alias {
			return p.State == proto.LinkUp
		}
	}
	return false
}

func (f *fakeLinks) Call(ctx context.Context, alias, method string, params, result any) error {
	for {
		f.mu.Lock()
		f.calls = append(f.calls, method)
		if !f.up(alias) {
			f.mu.Unlock()
			return errNotUp
		}
		now := time.Now().Add(f.skew)
		switch method {
		case proto.MethodPeerProjects:
			var ps proto.PeerProjects
			for _, slug := range slices.Sorted(maps.Keys(f.threads)) {
				ps.Projects = append(ps.Projects, proto.PeerProject{Project: slug, Goal: "goal of " + slug, Threads: f.threads[slug]})
			}
			ps.Now, ps.Panes = now, f.panes
			f.mu.Unlock()
			return roundTrip(ps, result)
		case proto.MethodPeerWatch:
			p := params.(proto.PeerWatchParams)
			n, ok := f.threads[p.Project]
			if !ok {
				f.mu.Unlock()
				return proto.Errorf(proto.ErrNotShared, "no shared project %q", p.Project)
			}
			w := proto.PeerWatch{Watch: proto.ProjectWatch{Project: p.Project}, Sessions: []proto.PeerSession{{ID: "s-1", Project: p.Project}}}
			for i := range n {
				w.Watch.Threads = append(w.Watch.Threads, proto.WatchThread{ID: fmt.Sprintf("t-%04d", i+1)})
			}
			w.Rev = rev(w)
			if w.Rev != p.Rev {
				w.Now = now
				f.mu.Unlock()
				return roundTrip(w, result)
			}
			ch := f.changes
			f.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ch:
			}
		default:
			f.mu.Unlock()
			return proto.Errorf(proto.ErrUnknownMethod, "unknown link method %q", method)
		}
	}
}

func roundTrip(v, result any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

func fastMirror(t *testing.T) {
	t.Helper()
	poll, projects, retry := mirrorPoll, mirrorProjects, mirrorRetry
	mirrorPoll, mirrorProjects, mirrorRetry = 5*time.Millisecond, 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { mirrorPoll, mirrorProjects, mirrorRetry = poll, projects, retry })
}

// waitList waits until the mirror's list satisfies ok.
func waitList(t *testing.T, m *Mirror, what string, ok func(proto.PeerList) bool) proto.PeerList {
	t.Helper()
	var last proto.PeerList
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		last = m.List()
		if ok(last) {
			return last
		}
	}
	b, _ := json.MarshalIndent(last, "", "  ")
	t.Fatalf("the mirror never %s:\n%s", what, b)
	return last
}

func peerOf(l proto.PeerList, alias string) *proto.PeerMirror {
	for i := range l.Peers {
		if l.Peers[i].Alias == alias {
			return &l.Peers[i]
		}
	}
	return nil
}

func projectOf(l proto.PeerList, alias, slug string) *proto.PeerMirrorProject {
	p := peerOf(l, alias)
	if p == nil {
		return nil
	}
	for i := range p.Projects {
		if p.Projects[i].Project == slug {
			return &p.Projects[i]
		}
	}
	return nil
}

func upLink(alias string, caps ...string) proto.PeerLink {
	return proto.PeerLink{Alias: alias, ID: "id-" + alias, State: proto.LinkUp, Caps: caps}
}

// TestMirrorFollowsPeer: a link up to a peer that exports fills the
// mirror with its shared projects and their snapshots, counted from
// them; a change shows; a link that drops keeps the snapshots, stale,
// and one that comes back clears stale at once, without waiting for a
// change; the peer's clock offset is measured from its now.
func TestMirrorFollowsPeer(t *testing.T) {
	fastMirror(t)
	f := newFakeLinks(5*time.Minute, map[string]int{"web": 2, "api": 0}, upLink("mini", CapWatch))
	var changes sync.WaitGroup
	changes.Add(1)
	var once sync.Once
	m := startMirror(f, MirrorOptions{Logf: t.Logf, Changed: func() { once.Do(changes.Done) }})
	defer m.Close()

	l := waitList(t, m, "synced web and api", func(l proto.PeerList) bool {
		w, a := projectOf(l, "mini", "web"), projectOf(l, "mini", "api")
		return w != nil && a != nil && !w.Stale && !a.Stale && w.Watch != nil && w.Threads == 2
	})
	changes.Wait()
	p := peerOf(l, "mini")
	if p.ID != "id-mini" || p.State != proto.LinkUp || !p.Watch {
		t.Fatalf("peer %+v", p)
	}
	if d := time.Duration(p.Offset) * time.Millisecond; (d - 5*time.Minute).Abs() > 2*time.Second {
		t.Fatalf("offset %v, want about 5m", d)
	}
	if slugs := []string{p.Projects[0].Project, p.Projects[1].Project}; !slices.Equal(slugs, []string{"api", "web"}) {
		t.Fatalf("projects in order %v", slugs)
	}
	w := projectOf(l, "mini", "web")
	if w.Goal != "goal of web" || len(w.Watch.Threads) != 2 || len(w.Sessions) != 1 || w.Rev == "" || w.Synced.IsZero() {
		t.Fatalf("web %+v", w)
	}
	gen := l.Gen

	f.change("web", 3)
	l = waitList(t, m, "saw web's third thread", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && w.Threads == 3 && len(w.Watch.Threads) == 3
	})
	if l.Gen <= gen {
		t.Fatalf("gen %d after %d", l.Gen, gen)
	}

	f.set("mini", proto.LinkOffline)
	l = waitList(t, m, "marked web stale", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && w.Stale && peerOf(l, "mini").State == proto.LinkOffline
	})
	if w := projectOf(l, "mini", "web"); w.Threads != 3 || w.Watch == nil || len(w.Watch.Threads) != 3 {
		t.Fatalf("a stale project lost its snapshot: %+v", w)
	}
	synced := projectOf(l, "mini", "web").Synced
	// The list reads the link's state as it is; let the mirror see it
	// too, so the link is down for it as well.
	time.Sleep(20 * mirrorPoll)

	f.set("mini", proto.LinkUp)
	l = waitList(t, m, "fresh again", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && !w.Stale && w.Synced.After(synced)
	})
}

// TestMirrorProjectsComeAndGo: a project the peer stops sharing leaves
// the mirror (its watch answers not-shared), one it starts sharing
// shows at the next list, and a peer unpaired leaves with its projects.
// TestMirrorPanes: peer.list says whether a peer has remote panes, its
// switch for this server and its release (docs/SPEC.md §16.12).
func TestMirrorPanes(t *testing.T) {
	fastMirror(t)
	panes := upLink("mini", CapWatch, CapPane)
	panes.Version, panes.RTT = "0.28.0", 12.5
	f := newFakeLinks(0, map[string]int{"web": 1}, panes, upLink("old", CapWatch))
	f.panes = proto.PanesView
	m := startMirror(f, MirrorOptions{})
	defer m.Close()
	l := waitList(t, m, "both synced", func(l proto.PeerList) bool {
		w, o := projectOf(l, "mini", "web"), projectOf(l, "old", "web")
		return w != nil && o != nil && !w.Stale && !o.Stale
	})
	if p := peerOf(l, "mini"); !p.Pane || p.Panes != proto.PanesView || p.Version != "0.28.0" || p.RTT != 12.5 {
		t.Errorf("mini %+v", p)
	}
	if p := peerOf(l, "old"); p.Pane || p.Panes != "" {
		t.Errorf("a peer without pane-vt: %+v", p)
	}
}

func TestMirrorProjectsComeAndGo(t *testing.T) {
	fastMirror(t)
	f := newFakeLinks(0, map[string]int{"web": 1}, upLink("mini", CapWatch))
	m := startMirror(f, MirrorOptions{Logf: t.Logf})
	defer m.Close()
	waitList(t, m, "synced web", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && !w.Stale
	})
	f.change("docs", 0)
	f.change("web", -1)
	waitList(t, m, "swapped web for docs", func(l proto.PeerList) bool {
		d := projectOf(l, "mini", "docs")
		return projectOf(l, "mini", "web") == nil && d != nil && !d.Stale
	})
	f.set("mini", "")
	waitList(t, m, "dropped mini", func(l proto.PeerList) bool { return len(l.Peers) == 0 })
}

// TestMirrorPeerWithoutWatch: a peer whose hello lacks the watch
// capability (an older tm, or one that exports nothing) is listed with
// its link and no projects, and never asked.
func TestMirrorPeerWithoutWatch(t *testing.T) {
	fastMirror(t)
	f := newFakeLinks(0, map[string]int{"web": 1}, upLink("old"))
	m := startMirror(f, MirrorOptions{})
	defer m.Close()
	waitList(t, m, "listed old", func(l proto.PeerList) bool { return peerOf(l, "old") != nil })
	time.Sleep(50 * time.Millisecond)
	l := m.List()
	if p := peerOf(l, "old"); p.Watch || len(p.Projects) != 0 || p.Offset != 0 {
		t.Fatalf("old %+v", p)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 {
		t.Fatalf("asked a peer that doesn't export: %v", f.calls)
	}
}

// TestMirrorNoIdentity: a server never paired lists no peers, and a nil
// Mirror lists the same.
func TestMirrorNoIdentity(t *testing.T) {
	fastMirror(t)
	f := &fakeLinks{changes: make(chan struct{})}
	m := startMirror(f, MirrorOptions{})
	defer m.Close()
	if l := m.List(); l.Peers == nil || len(l.Peers) != 0 {
		t.Fatalf("%+v", l)
	}
	var none *Mirror
	if l := none.List(); l.Peers == nil || len(l.Peers) != 0 {
		t.Fatalf("%+v", l)
	}
	none.Close()
}

// TestMirrorThroughRelay: two Managers paired through a relay, mini
// exporting: studio's mirror holds mini's shared project, follows a
// change, and keeps it stale once mini stops.
func TestMirrorThroughRelay(t *testing.T) {
	fastMirror(t)
	_, _, url, tc := startRelay(t)
	studio, mini := newServer(t), newServer(t)
	studio.pair(t, "mini", mini, url)
	mini.pair(t, "studio", studio, url)
	ex := newFakeExport()
	ex.change()
	ms := studio.start(t, "test", tc)
	mm := Start(Options{Dir: mini.dir, Version: "test", Logf: t.Logf, RelayTLS: tc, Export: ex,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	defer mm.Close()

	m := StartMirror(ms, MirrorOptions{Logf: t.Logf})
	defer m.Close()
	waitList(t, m, "synced mini's web", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && !w.Stale && w.Threads == 1
	})
	ex.change()
	l := waitList(t, m, "saw the change", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && w.Threads == 2
	})
	if d := time.Duration(peerOf(l, "mini").Offset) * time.Millisecond; d.Abs() > time.Second {
		t.Fatalf("one machine, offset %v", d)
	}
	m.mu.Lock()
	measured := m.peers[peerOf(l, "mini").ID].hasOffset
	m.mu.Unlock()
	if !measured {
		t.Fatal("mini's answers carried no now")
	}
	mm.Close()
	waitList(t, m, "marked web stale", func(l proto.PeerList) bool {
		w := projectOf(l, "mini", "web")
		return w != nil && w.Stale && w.Threads == 2 && peerOf(l, "mini").State != proto.LinkUp
	})
}
