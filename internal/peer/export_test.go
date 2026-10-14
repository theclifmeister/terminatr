package peer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// fakeExport shares one project, "web", whose state a test changes.
type fakeExport struct {
	mu      sync.Mutex
	threads int
	watchFn func(slug string) error // an error Watch answers
	callers []caller.Caller
	changes chan struct{}
}

func newFakeExport() *fakeExport { return &fakeExport{changes: make(chan struct{}, 1)} }

func (f *fakeExport) Projects(c caller.Caller) ([]proto.PeerProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callers = append(f.callers, c)
	return []proto.PeerProject{{Project: "web", Threads: f.threads}}, nil
}

func (f *fakeExport) Watch(c caller.Caller, slug string) (proto.PeerWatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callers = append(f.callers, c)
	if f.watchFn != nil {
		if err := f.watchFn(slug); err != nil {
			return proto.PeerWatch{}, err
		}
	}
	if slug != "web" {
		return proto.PeerWatch{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
	}
	w := proto.PeerWatch{Watch: proto.ProjectWatch{Project: "web"}}
	for i := range f.threads {
		w.Watch.Threads = append(w.Watch.Threads, proto.WatchThread{ID: fmt.Sprintf("t-%04d", i+1)})
	}
	return w, nil
}

func (f *fakeExport) seen() []caller.Caller {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.callers)
}

func (f *fakeExport) Changes() (<-chan struct{}, func()) { return f.changes, func() {} }

// change adds a thread and signals it.
func (f *fakeExport) change() {
	f.mu.Lock()
	f.threads++
	f.mu.Unlock()
	select {
	case f.changes <- struct{}{}:
	default:
	}
}

// exportPair is two sessions over loopback TCP, b serving ex's methods
// to a's calls.
func exportPair(t *testing.T, ex Exporter) (a *session) {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	sb.methods = (&dispatcher{ex: ex, caller: caller.ForPeer("studio")}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa
}

func code(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// TestDispatcherOnlyPeerMethods: a link reaches peer.projects and
// peer.watch and nothing else: no control method, none that writes,
// whatever its name; without an Exporter, not even those.
func TestDispatcherOnlyPeerMethods(t *testing.T) {
	ex := newFakeExport()
	a := exportPair(t, ex)
	ctx := context.Background()
	for _, m := range []string{"session.list", "session.start", "session.send", "project.watch", "server.status", "server.stop",
		"peer.reload", "cli.run", "config.set", "tasks.add", "inbox.add", "link.send", "peer.send", "peer.attach", "peer.list"} {
		if err := a.call(ctx, m, map[string]string{"project": "web"}, nil); code(err) != proto.ErrUnknownMethod {
			t.Errorf("%s: %v, want unknown-method", m, err)
		}
	}
	var ps proto.PeerProjects
	if err := a.call(ctx, proto.MethodPeerProjects, nil, &ps); err != nil || len(ps.Projects) != 1 || ps.Projects[0].Project != "web" {
		t.Fatalf("peer.projects: %+v %v", ps, err)
	}
	for _, c := range ex.seen() {
		if c.Kind != caller.Peer || c.Alias != "studio" {
			t.Errorf("the exporter was called as %+v", c)
		}
	}

	none := exportPair(t, nil)
	for _, m := range []string{proto.MethodPeerProjects, proto.MethodPeerWatch} {
		if err := none.call(ctx, m, proto.PeerWatchParams{Project: "web"}, nil); code(err) != proto.ErrUnknownMethod {
			t.Errorf("no exporter, %s: %v", m, err)
		}
	}
}

// TestPeerWatchWaits: peer.watch answers at once without a rev, waits
// for a change with the current one, and answers the same state when
// its wait runs out.
func TestPeerWatchWaits(t *testing.T) {
	ex := newFakeExport()
	a := exportPair(t, ex)
	ctx := context.Background()
	var w proto.PeerWatch
	if err := a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web"}, &w); err != nil || w.Rev == "" {
		t.Fatalf("first: %+v %v", w, err)
	}
	// No change: the same state after the wait.
	t0 := time.Now()
	var same proto.PeerWatch
	if err := a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web", Rev: w.Rev, Wait: 1}, &same); err != nil || same.Rev != w.Rev {
		t.Fatalf("unchanged: %+v %v", same, err)
	}
	if d := time.Since(t0); d < 900*time.Millisecond {
		t.Errorf("answered an unchanged state after %v", d)
	}
	// A change wakes it.
	done := make(chan proto.PeerWatch, 1)
	go func() {
		var next proto.PeerWatch
		if err := a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web", Rev: w.Rev, Wait: 30}, &next); err != nil {
			t.Error(err)
		}
		done <- next
	}()
	time.Sleep(100 * time.Millisecond)
	ex.change()
	select {
	case next := <-done:
		if next.Rev == w.Rev || len(next.Watch.Threads) != 1 {
			t.Errorf("after a change: %+v", next)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a change didn't wake peer.watch")
	}
	// A ping still goes through while a watch waits.
	go a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web", Rev: "x" + w.Rev, Wait: 30}, nil)
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.call(pctx, MethodPing, nil, nil); err != nil {
		t.Errorf("ping behind a watch: %v", err)
	}
	if err := a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("no project: %v", err)
	}
	if err := a.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "secret"}, nil); code(err) != proto.ErrNotShared {
		t.Errorf("unshared: %v", err)
	}
}

// TestExportErrorWords: an error that isn't a protocol error crosses as
// internal, without its words: they may name a path.
func TestExportErrorWords(t *testing.T) {
	ex := newFakeExport()
	ex.watchFn = func(string) error { return errors.New("open /Users/me/.terminatr/config.toml: permission denied") }
	a := exportPair(t, ex)
	err := a.call(context.Background(), proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web"}, nil)
	if code(err) != proto.ErrInternal || strings.Contains(err.Error(), "/Users") || strings.Contains(err.Error(), "config.toml") {
		t.Errorf("error crossed as %v", err)
	}
}

// TestWatchCapThroughRelay: a server that exports says watch in its
// hello and answers peer.* over its link; one that doesn't, neither.
func TestWatchCapThroughRelay(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)
	ma := a.start(t, "0.30.0", tc)
	ex := newFakeExport()
	mb := Start(Options{Dir: b.dir, Version: "0.31.0", Logf: t.Logf, RelayTLS: tc, Export: ex,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(mb.Close)
	pa := waitLink(t, ma, "mini", "up", isUp)
	pb := waitLink(t, mb, "studio", "up", isUp)
	if !slices.Contains(pa.Caps, CapWatch) || slices.Contains(pb.Caps, CapWatch) {
		t.Fatalf("caps: a sees %v, b sees %v", pa.Caps, pb.Caps)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ps proto.PeerProjects
	if err := ma.Call(ctx, "mini", proto.MethodPeerProjects, nil, &ps); err != nil || len(ps.Projects) != 1 {
		t.Fatalf("peer.projects through the relay: %+v %v", ps, err)
	}
	if seen := ex.seen(); seen[0] != caller.ForPeer("studio") {
		t.Errorf("b's exporter saw %+v, want its alias for a", seen[0])
	}
	var raw json.RawMessage
	if err := mb.Call(ctx, "studio", proto.MethodPeerProjects, nil, &raw); code(err) != proto.ErrUnknownMethod {
		t.Errorf("a exports nothing, yet answered %s %v", raw, err)
	}
	if err := ma.Call(ctx, "nobody", proto.MethodPeerProjects, nil, nil); err == nil {
		t.Error("called a server that isn't paired")
	}
}
