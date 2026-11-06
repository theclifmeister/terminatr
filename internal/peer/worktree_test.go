package peer

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// worktreeExport is pagesExport serving folders: refuse is what each read
// answers, if set, and block holds them.
type worktreeExport struct {
	*pagesExport
	refuse error
	block  chan struct{}
	reads  atomic.Int32
}

func worktreeRead[R any](f *worktreeExport, ctx context.Context, res R) (R, error) {
	f.reads.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
		}
	}
	return res, f.refuse
}

func (f *worktreeExport) WorktreeRepos(ctx context.Context, _ caller.Caller, _ proto.PeerWorktreeParams) (proto.PeerWorktreeRepos, error) {
	return worktreeRead(f, ctx, proto.PeerWorktreeRepos{Repos: []string{"api"}})
}

func (f *worktreeExport) WorktreeList(ctx context.Context, _ caller.Caller, _ proto.PeerWorktreeListParams) (proto.PeerWorktreeList, error) {
	return worktreeRead(f, ctx, proto.PeerWorktreeList{Entries: []proto.PeerFileEntry{}})
}

func (f *worktreeExport) WorktreeRead(ctx context.Context, _ caller.Caller, _ proto.PeerWorktreeReadParams) (proto.PeerWorktreeRead, error) {
	return worktreeRead(f, ctx, proto.PeerWorktreeRead{Name: "a"})
}

func (f *worktreeExport) WorktreeStamp(ctx context.Context, _ caller.Caller, _ proto.PeerWorktreeStampParams) (proto.PeerWorktreeStamp, error) {
	return worktreeRead(f, ctx, proto.PeerWorktreeStamp{Stamp: "s"})
}

func (f *worktreeExport) WorktreeDiff(ctx context.Context, _ caller.Caller, _ proto.PeerWorktreeDiffParams) (proto.PeerWorktreeDiff, error) {
	return worktreeRead(f, ctx, proto.PeerWorktreeDiff{Name: "a"})
}

func newWorktreeExport() *worktreeExport {
	return &worktreeExport{pagesExport: &pagesExport{fakeExport: newFakeExport()}}
}

// worktreePair is a session to a dispatcher whose files switch is on for
// the projects on says, and that dispatcher.
func worktreePair(t *testing.T, ex Exporter, on func(string) bool) (*session, *dispatcher) {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	d := &dispatcher{ex: ex, caller: caller.ForPeer("studio"), worktrees: on}
	sb.methods = d.methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa, d
}

var worktreeCalls = []struct {
	method string
	params any
}{
	{proto.MethodPeerWorktreeRepos, proto.PeerWorktreeParams{Project: "web", Session: "s"}},
	{proto.MethodPeerWorktreeList, proto.PeerWorktreeListParams{Project: "web", Session: "s"}},
	{proto.MethodPeerWorktreeRead, proto.PeerWorktreeReadParams{Project: "web", Session: "s", Path: "a"}},
	{proto.MethodPeerWorktreeStamp, proto.PeerWorktreeStampParams{Project: "web", Session: "s"}},
	{proto.MethodPeerWorktreeDiff, proto.PeerWorktreeDiffParams{Project: "web", Session: "s", Path: "a"}},
}

// TestPeerWorktreeGate: each method answers only while the switch is on for
// its project, read at each request, files-off in the same words otherwise
// and the exporter not asked; peer.projects says it per project; an exporter
// without folders has no such methods at all, whatever the switch.
func TestPeerWorktreeGate(t *testing.T) {
	ex := newWorktreeExport()
	var on atomic.Value
	on.Store("")
	a, d := worktreePair(t, ex, func(p string) bool { return p == on.Load() })
	ctx := context.Background()
	for _, c := range worktreeCalls {
		d.worktreeLimit = worktreeRate{}
		err := a.call(ctx, c.method, c.params, nil)
		if code(err) != proto.ErrFilesOff || !strings.Contains(err.Error(), proto.PeerWorktreeRefusal(proto.ErrFilesOff).Message) {
			t.Errorf("%s off: %v", c.method, err)
		}
	}
	if n := ex.reads.Load(); n != 0 {
		t.Errorf("off, the exporter was asked %d times", n)
	}
	on.Store("web")
	for _, c := range worktreeCalls {
		d.worktreeLimit = worktreeRate{}
		if err := a.call(ctx, c.method, c.params, nil); err != nil {
			t.Errorf("%s on: %v", c.method, err)
		}
	}
	d.worktreeLimit = worktreeRate{}
	if err := a.call(ctx, proto.MethodPeerWorktreeList, proto.PeerWorktreeListParams{Project: "api"}, nil); code(err) != proto.ErrFilesOff {
		t.Errorf("on for another project: %v", err)
	}
	if err := a.call(ctx, proto.MethodPeerWorktreeList, proto.PeerWorktreeListParams{}, nil); code(err) != proto.ErrFilesOff {
		t.Errorf("no project: %v", err)
	}
	var ps proto.PeerProjects
	if err := a.call(ctx, proto.MethodPeerProjects, nil, &ps); err != nil || !ps.Projects[0].Files || ps.Projects[1].Files {
		t.Errorf("peer.projects: %+v %v", ps, err)
	}

	for name, plain := range map[string]*dispatcher{
		"no exporter of them": {ex: newFakeExport(), caller: caller.ForPeer("studio"), worktrees: func(string) bool { return true }},
		"no switch":           {ex: ex, caller: caller.ForPeer("studio")},
	} {
		for _, c := range worktreeCalls {
			if _, ok := plain.methods()[c.method]; ok {
				t.Errorf("%s: %s is there", name, c.method)
			}
		}
	}
}

// TestPeerWorktreeWords: whatever the exporter's error says, only its code's
// fixed words cross, and an error without a code is internal.
func TestPeerWorktreeWords(t *testing.T) {
	ex := newWorktreeExport()
	a, d := worktreePair(t, ex, func(string) bool { return true })
	ctx := context.Background()
	for _, tc := range []struct {
		err  error
		code string
	}{
		{proto.Errorf(proto.ErrNoFiles, "/Users/me/SECRET has gone"), proto.ErrNoFiles},
		{proto.Errorf(proto.ErrIgnored, "/Users/me/SECRET"), proto.ErrIgnored},
		{proto.Errorf("no-such-code", "/Users/me/SECRET"), proto.ErrInternal},
		{errors.New("/Users/me/SECRET"), proto.ErrInternal},
	} {
		ex.refuse = tc.err
		d.worktreeLimit = worktreeRate{}
		err := a.call(ctx, proto.MethodPeerWorktreeRead, proto.PeerWorktreeReadParams{Project: "web", Session: "s", Path: "a"}, nil)
		if code(err) != tc.code || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%v crossed as %v", tc.err, err)
		}
	}
	for c := range map[string]bool{proto.ErrFilesOff: true, proto.ErrWithheld: true, proto.ErrRateLimited: true, proto.ErrFileGone: true} {
		if pe := proto.PeerWorktreeRefusal(c); pe.Code != c || pe.Message == "" {
			t.Errorf("%s: %+v", c, pe)
		}
	}
}

// TestPeerWorktreeRate: at most so many requests a second and so many running
// at once, the rest rate-limited; time gives the places back.
func TestPeerWorktreeRate(t *testing.T) {
	var r worktreeRate
	now := time.Unix(1000, 0)
	var done []func()
	take := func() bool {
		f, ok := r.take(now)
		if ok {
			done = append(done, f)
		}
		return ok
	}
	for range proto.PeerWorktreeRunning {
		if !take() {
			t.Fatal("refused under the running limit")
		}
	}
	if take() {
		t.Fatal("over the running limit")
	}
	done[0]()
	if !take() {
		t.Fatal("a place given back wasn't taken")
	}
	for _, f := range done[1:] {
		f()
	}
	// That was three; the fourth is the last of this second.
	if !take() {
		t.Fatal("the fourth of a second was refused")
	}
	done[len(done)-1]()
	if f, ok := r.take(now); ok {
		f()
		t.Fatal("a fifth in the same second")
	}
	now = now.Add(time.Second / proto.PeerWorktreeRate)
	if !take() {
		t.Fatal("time gave nothing back")
	}
	done[len(done)-1]()
	now = now.Add(time.Hour)
	for i := range proto.PeerWorktreeRate {
		f, ok := r.take(now)
		if !ok {
			t.Fatalf("after a rest, %d refused", i)
		}
		f()
	}
	if f, ok := r.take(now); ok {
		f()
		t.Fatal("a rest saved up more than a second's")
	}
}

// TestPeerWorktreeRateOverLink: requests still running hold their places, and
// the link says rate-limited, the off answer never asked of the file.
func TestPeerWorktreeRateOverLink(t *testing.T) {
	ex := newWorktreeExport()
	ex.block = make(chan struct{})
	a, _ := worktreePair(t, ex, func(string) bool { return true })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errs := make(chan error, proto.PeerWorktreeRunning)
	for range proto.PeerWorktreeRunning {
		go func() {
			errs <- a.call(ctx, proto.MethodPeerWorktreeStamp, proto.PeerWorktreeStampParams{Project: "web", Session: "s"}, nil)
		}()
	}
	for ex.reads.Load() < proto.PeerWorktreeRunning {
		if ctx.Err() != nil {
			t.Fatal("they never ran")
		}
		time.Sleep(time.Millisecond)
	}
	if err := a.call(ctx, proto.MethodPeerWorktreeStamp, proto.PeerWorktreeStampParams{Project: "web", Session: "s"}, nil); code(err) != proto.ErrRateLimited {
		t.Errorf("a third at once: %v", err)
	}
	close(ex.block)
	for range proto.PeerWorktreeRunning {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// TestWorktreeCap: a server whose exporter serves folders says so in its
// hello, one whose exporter doesn't, not, and the hello still fits.
func TestWorktreeCap(t *testing.T) {
	with := &Manager{opts: Options{Export: newWorktreeExport()}}
	without := &Manager{opts: Options{Export: newFakeExport()}}
	if !slices.Contains(with.caps(), CapWorktree) || slices.Contains(without.caps(), CapWorktree) {
		t.Errorf("caps: %v and %v", with.caps(), without.caps())
	}
	if n := len(with.caps()); n > maxCaps {
		t.Errorf("%d caps, over the %d a hello keeps", n, maxCaps)
	}
}

// TestFilesOnReadsTheFile: the switch is the file's now, with no reload, by
// the peer's key: another key, an entry gone, a file that can't be read are
// off, and so is what the human didn't ask for. Pairing again keeps it; a list
// with something not a slug isn't loaded.
func TestFilesOnReadsTheFile(t *testing.T) {
	dir := hometest.PrivateDir(t)
	key, other := make(ed25519.PublicKey, ed25519.PublicKeySize), make(ed25519.PublicKey, ed25519.PublicKeySize)
	other[0] = 1
	save := func(files ...string) {
		t.Helper()
		if err := SavePeers(dir, []Peer{{Alias: "mini", Key: key, Relay: "wss://r.example", Files: files}}); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manager{opts: Options{Dir: dir}, grants: loadGrants(dir, nil)}
	id := IDOf(key)
	if m.filesOn(id, "web") {
		t.Error("no file: on")
	}
	allow := func() {
		t.Helper()
		if _, err := m.grants.set(proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchAllow}); err != nil {
			t.Fatal(err)
		}
	}
	save("web")
	if m.filesOn(id, "web") {
		t.Error("on in the file alone, with no word from the human")
	}
	allow()
	if !m.filesOn(id, "web") || m.filesOn(id, "api") || m.filesOn(id, "") {
		t.Error("on for web alone: wrong")
	}
	if m.filesOn(IDOf(other), "web") {
		t.Error("another key's: on")
	}
	save()
	if m.filesOn(id, "web") {
		t.Error("off in the file, still on")
	}
	save("web")
	if m.filesOn(id, "web") {
		t.Error("back on in the file alone")
	}
	allow()
	if _, err := Pin(dir, key, "wss://r.example", "mini", ""); err != nil {
		t.Fatal(err)
	}
	if !m.filesOn(id, "web") {
		t.Error("pairing again with the same key dropped it")
	}
	if err := os.WriteFile(filepath.Join(dir, PeersFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m.filesOn(id, "web") {
		t.Error("an unreadable file: on")
	}
	if err := SavePeers(dir, []Peer{{Alias: "mini", Key: key, Relay: "wss://r.example", Files: []string{"../x"}}}); err == nil {
		t.Error("a files list naming no project was saved")
	}
}
