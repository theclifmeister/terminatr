package peer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// uploadExport counts what reaches it past the link's gate.
type uploadExport struct {
	*fakeExport
	mu      sync.Mutex
	chunks  [][]byte
	asked   atomic.Int32
	dropped atomic.Int32
	answer  func(p proto.PeerSessionUploadParams, got int64) proto.PeerSessionUpload
	paths   []string
	gone    context.Context
}

func (f *uploadExport) SessionUpload(gone context.Context, _ caller.Caller, p proto.PeerSessionUploadParams) (proto.PeerSessionUpload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone = gone
	f.chunks = append(f.chunks, p.Data)
	got := p.Offset + int64(len(p.Data))
	if f.answer != nil {
		return f.answer(p, got), nil
	}
	return proto.PeerSessionUpload{Upload: "ab12", Received: got}, nil
}

func (f *uploadExport) SessionFiles(_ caller.Caller, p proto.PeerSessionFilesParams) (proto.PeerSessionFiles, error) {
	f.asked.Add(1)
	return proto.PeerSessionFiles{Paths: f.paths}, nil
}

func (f *uploadExport) DropUploads(caller.Caller) { f.dropped.Add(1) }

func uploadPair(t *testing.T, ex Exporter, on func() bool) *session {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	d := &dispatcher{ex: ex, caller: caller.ForPeer("studio"), uploads: on}
	sb.methods = d.methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa
}

// TestPeerUploadGate: only at the write level, read at each request; off says
// so, forgets what the peer has in memory and asks the exporter nothing; a
// chunk over the link's bound is refused; and the methods aren't there for an
// exporter without them or a server without the switch.
func TestPeerUploadGate(t *testing.T) {
	ex := &uploadExport{fakeExport: newFakeExport(), paths: []string{"/p/a.png"}}
	var on atomic.Bool
	a := uploadPair(t, ex, on.Load)
	ctx := context.Background()
	chunk := proto.PeerSessionUploadParams{Project: "web", Session: "s", Name: "a.png", Size: 1, Data: []byte("x")}
	files := proto.PeerSessionFilesParams{Project: "web", Session: "s", Files: []string{"a.png"}}

	if err := a.call(ctx, proto.MethodPeerSessionUpload, chunk, nil); code(err) != proto.ErrPanesOff {
		t.Errorf("off, a chunk: %v", err)
	}
	if err := a.call(ctx, proto.MethodPeerSessionFiles, files, nil); code(err) != proto.ErrPanesOff {
		t.Errorf("off, files: %v", err)
	}
	if len(ex.chunks) != 0 || ex.asked.Load() != 0 {
		t.Errorf("the exporter was asked while off: %v %d", ex.chunks, ex.asked.Load())
	}
	if ex.dropped.Load() != 2 {
		t.Errorf("off dropped %d times", ex.dropped.Load())
	}

	on.Store(true)
	var r proto.PeerSessionUpload
	if err := a.call(ctx, proto.MethodPeerSessionUpload, chunk, &r); err != nil || r.Upload != "ab12" {
		t.Errorf("on, a chunk: %+v %v", r, err)
	}
	if ex.gone == nil || ex.gone.Err() != nil {
		t.Errorf("gone: %v", ex.gone)
	}
	var fs proto.PeerSessionFiles
	if err := a.call(ctx, proto.MethodPeerSessionFiles, files, &fs); err != nil || len(fs.Paths) != 1 {
		t.Errorf("on, files: %+v %v", fs, err)
	}
	big := chunk
	big.Data = make([]byte, proto.PeerUploadChunk+1)
	if err := a.call(ctx, proto.MethodPeerSessionUpload, big, nil); code(err) != proto.ErrBadParams {
		t.Errorf("a chunk over the bound: %v", err)
	}
	// Off again takes hold at the next request.
	on.Store(false)
	if err := a.call(ctx, proto.MethodPeerSessionUpload, chunk, nil); code(err) != proto.ErrPanesOff {
		t.Errorf("off again: %v", err)
	}

	for _, s := range []*session{uploadPair(t, newFakeExport(), on.Load), uploadPair(t, &uploadExport{fakeExport: newFakeExport()}, nil)} {
		if err := s.call(ctx, proto.MethodPeerSessionUpload, chunk, nil); code(err) != proto.ErrUnknownMethod {
			t.Errorf("no method: %v", err)
		}
	}
	if m := (&dispatcher{ex: ex, uploads: on.Load}).methods(); m[proto.MethodPeerSessionUpload] == nil || m[proto.MethodPeerSessionFiles] == nil {
		t.Errorf("methods: %v", m)
	}
}

// TestSessionUploadCap: a hello that lists every capability keeps them all
// here; an older tm, which keeps 16, loses only this one, and it is why the
// list raised its bound.
func TestSessionUploadCap(t *testing.T) {
	var with Manager
	with.opts.Export, with.opts.Panes = &uploadExport{fakeExport: newFakeExport()}, &fakeHost{}
	caps := with.caps()
	if !slices.Contains(caps, CapSessionUpload) {
		t.Fatalf("no capability: %v", caps)
	}
	if n := len(caps); n > maxCaps {
		t.Errorf("%d caps, over the %d a hello keeps", n, maxCaps)
	}
	if caps[len(caps)-1] != CapSessionUpload {
		t.Errorf("it isn't last, so an older tm would cut another: %v", caps)
	}
	var noPanes Manager
	noPanes.opts.Export = &uploadExport{fakeExport: newFakeExport()}
	if slices.Contains(noPanes.caps(), CapSessionUpload) {
		t.Error("a server without panes says it takes files")
	}
	var old Manager
	old.opts.Export, old.opts.Panes = newFakeExport(), &fakeHost{}
	if slices.Contains(old.caps(), CapSessionUpload) {
		t.Error("an exporter without them says it takes files")
	}
}

// startUploadManagers links a Manager to the exporter through an in-process
// relay and answers the near one.
func startUploadManagers(t *testing.T, ex Exporter, level string) (*Manager, string) {
	t.Helper()
	_, _, url, tc := startRelay(t)
	near, far := filepath.Join(t.TempDir(), "near"), filepath.Join(t.TempDir(), "far")
	for _, d := range []string{near, far} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ida, _, err := EnsureIdentity(near)
	if err != nil {
		t.Fatal(err)
	}
	idb, _, err := EnsureIdentity(far)
	if err != nil {
		t.Fatal(err)
	}
	pub := func(i *Identity) ed25519.PublicKey { return i.Key.Public().(ed25519.PublicKey) }
	if err := SavePeers(near, []Peer{{Alias: "mini", Key: pub(idb), Relay: url}}); err != nil {
		t.Fatal(err)
	}
	if err := SavePeers(far, []Peer{{Alias: "studio", Key: pub(ida), Relay: url, Panes: level}}); err != nil {
		t.Fatal(err)
	}
	start := func(dir string, ex Exporter, p PaneHost) *Manager {
		m := Start(Options{Dir: dir, Version: "test", Logf: t.Logf, RelayTLS: tc, Export: ex, Panes: p,
			Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
		t.Cleanup(m.Close)
		return m
	}
	n := start(near, nil, nil)
	start(far, ex, &fakeHost{})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := n.Status(); st != nil && len(st.Peers) == 1 && st.Peers[0].State == proto.LinkUp {
			return n, far
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the link never came up: %+v", n.Status())
	return nil, ""
}

// TestManagerSessionUpload: through a relay, a chunk of the app's goes in
// chunks the link's bound takes, in order, each carrying the id the first
// answered; a switch change on the far side holds at the next chunk, from its
// file; a hostile answer is refused; and paths are cleaned on arrival.
func TestManagerSessionUpload(t *testing.T) {
	old := uploadRate
	uploadRate = 64 << 20
	t.Cleanup(func() { uploadRate = old })
	ex := &uploadExport{fakeExport: newFakeExport()}
	m, far := startUploadManagers(t, ex, proto.PanesType)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data := bytes.Repeat([]byte("abcdefgh"), (proto.PeerUploadChunk*2+100)/8)
	r, err := m.SessionUpload(ctx, "mini", proto.PeerSessionUploadParams{Project: "web", Session: "s", Name: "a.txt", Size: int64(len(data)), Data: data})
	if err != nil || r.Received != int64(len(data)) || r.Upload != "ab12" {
		t.Fatalf("upload: %+v %v", r, err)
	}
	ex.mu.Lock()
	var got []byte
	for _, c := range ex.chunks {
		if len(c) > proto.PeerUploadChunk {
			t.Errorf("a chunk of %d bytes", len(c))
		}
		got = append(got, c...)
	}
	n := len(ex.chunks)
	ex.mu.Unlock()
	if n != 3 || !bytes.Equal(got, data) {
		t.Errorf("%d chunks, bytes the same: %v", n, bytes.Equal(got, data))
	}

	// Each hostile answer.
	for name, a := range map[string]func(p proto.PeerSessionUploadParams, got int64) proto.PeerSessionUpload{
		"a bad id": func(_ proto.PeerSessionUploadParams, got int64) proto.PeerSessionUpload {
			return proto.PeerSessionUpload{Upload: "../x", Received: got}
		},
		"received wrong": func(_ proto.PeerSessionUploadParams, got int64) proto.PeerSessionUpload {
			return proto.PeerSessionUpload{Upload: "ab", Received: got + 1}
		},
		"a ref too early": func(_ proto.PeerSessionUploadParams, got int64) proto.PeerSessionUpload {
			return proto.PeerSessionUpload{Upload: "ab", Received: got, Ref: "x.png"}
		},
	} {
		ex.mu.Lock()
		ex.answer = a
		ex.mu.Unlock()
		if _, err := m.SessionUpload(ctx, "mini", proto.PeerSessionUploadParams{Project: "web", Session: "s", Name: "a.txt", Size: int64(len(data)), Data: data}); code(err) != proto.ErrRefused {
			t.Errorf("%s: %v", name, err)
		}
	}
	ex.mu.Lock()
	ex.answer = nil
	ex.mu.Unlock()

	// Paths on arrival.
	for _, tc := range []struct {
		paths []string
		ok    bool
	}{
		{[]string{"/p/uploads/a.png"}, true},
		{[]string{`C:\p\uploads\a.png`}, true},
		{[]string{"/p/my dir/uploads/a.png"}, true},
		{[]string{}, false},
		{[]string{"/p/a.png", "/p/a.png"}, false},
		{[]string{"/p/other.png"}, false},
		{[]string{"a.png"}, false},
		{[]string{"/p/\x1b[2Ja.png"}, false},
		{[]string{"/p\nrm -rf/a.png"}, false},
		{[]string{"/" + strings.Repeat("x", maxUploadPath) + "/a.png"}, false},
	} {
		ex.paths = tc.paths
		got, err := m.SessionFiles(ctx, "mini", proto.PeerSessionFilesParams{Project: "web", Session: "s", Files: []string{"a.png"}})
		if tc.ok && (err != nil || !slices.Equal(got, tc.paths)) {
			t.Errorf("%q: %v %v", tc.paths, got, err)
		}
		if !tc.ok && code(err) != proto.ErrRefused {
			t.Errorf("%q: %v %v", tc.paths, got, err)
		}
	}

	// The far side's file, at the next chunk.
	ps, err := LoadPeers(far)
	if err != nil {
		t.Fatal(err)
	}
	ps[0].Panes = proto.PanesView
	if err := SavePeers(far, ps); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SessionUpload(ctx, "mini", proto.PeerSessionUploadParams{Project: "web", Session: "s", Name: "a.txt", Size: 1, Data: []byte("x")}); code(err) != proto.ErrPanesOff {
		t.Errorf("view: %v", err)
	}
	if ex.dropped.Load() == 0 {
		t.Error("view left its bytes held")
	}

	if _, err := m.SessionUpload(ctx, "nobody", proto.PeerSessionUploadParams{}); code(err) != proto.ErrRefused {
		t.Errorf("no such peer: %v", err)
	}
}

// TestManagerSessionUploadOlderTm: a tm that doesn't say session-upload isn't
// sent a chunk.
func TestManagerSessionUploadOlderTm(t *testing.T) {
	m, _ := startUploadManagers(t, newFakeExport(), proto.PanesType)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := m.SessionUpload(ctx, "mini", proto.PeerSessionUploadParams{Project: "web", Session: "s", Name: "a.txt", Size: 1, Data: []byte("x")}); !errors.Is(err, ErrNoUploads) {
		t.Errorf("upload: %v", err)
	}
	if _, err := m.SessionFiles(ctx, "mini", proto.PeerSessionFilesParams{Project: "web", Session: "s", Files: []string{"a.png"}}); !errors.Is(err, ErrNoUploads) {
		t.Errorf("files: %v", err)
	}
}

// TestUploadPace: the pace holds bytes to the rate, slows when answers come
// late, and recovers.
func TestUploadPace(t *testing.T) {
	old := uploadRate
	uploadRate = 1 << 20
	t.Cleanup(func() { uploadRate = old })
	var u uploadPace
	ctx := context.Background()
	at := time.Now()
	for range 5 {
		if err := u.wait(ctx, 32<<10); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(at); d < 100*time.Millisecond {
		t.Errorf("5 × 32 KiB at 1 MiB/s took %v", d)
	}
	u.took(time.Second, time.Millisecond)
	if u.rate != (1<<20)/2 {
		t.Errorf("slow: %v", u.rate)
	}
	for range 100 {
		u.took(time.Second, time.Millisecond)
	}
	if u.rate != fileMinRate {
		t.Errorf("floor: %v", u.rate)
	}
	for range 200 {
		u.took(time.Millisecond, time.Millisecond)
	}
	if u.rate != uploadRate {
		t.Errorf("recovers: %v", u.rate)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	u.next = time.Now().Add(time.Hour)
	if err := u.wait(cctx, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
