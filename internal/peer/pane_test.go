package peer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// fakeSource is a pane on the far end: batches the test queues, keys
// and sizes it records.
type fakeSource struct {
	mu      sync.Mutex
	cond    *sync.Cond
	batches [][]byte
	closed  bool
	resyncs int
	input   []byte
	sizes   [][2]uint16
	asked   int
}

func newFakeSource() *fakeSource {
	f := &fakeSource{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

func (f *fakeSource) push(typ proto.FrameType, p []byte) {
	f.mu.Lock()
	f.batches = append(f.batches, proto.AppendFrame(nil, typ, p))
	f.cond.Broadcast()
	f.mu.Unlock()
}

func (f *fakeSource) Next() ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.batches) == 0 && !f.closed {
		f.cond.Wait()
	}
	if len(f.batches) == 0 {
		return nil, false
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, true
}

func (f *fakeSource) Resyncs() int { f.mu.Lock(); defer f.mu.Unlock(); return f.resyncs }
func (f *fakeSource) Resync()      { f.mu.Lock(); f.asked++; f.mu.Unlock() }
func (f *fakeSource) Input(p []byte) error {
	f.mu.Lock()
	f.input = append(f.input, p...)
	f.mu.Unlock()
	return nil
}
func (f *fakeSource) Resize(c, r uint16) error {
	f.mu.Lock()
	f.sizes = append(f.sizes, [2]uint16{c, r})
	f.mu.Unlock()
	return nil
}
func (f *fakeSource) Close() {
	f.mu.Lock()
	f.closed = true
	f.cond.Broadcast()
	f.mu.Unlock()
}

func (f *fakeSource) typed() string { f.mu.Lock(); defer f.mu.Unlock(); return string(f.input) }

func (f *fakeSource) isClosed() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

// fakeHost serves one source for project web, session s-1.
type fakeHost struct {
	mu   sync.Mutex
	srcs []*fakeSource
	seen []caller.Caller
}

func (h *fakeHost) OpenPane(c caller.Caller, project, session string) (PaneSource, proto.PeerSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, c)
	if project != "web" {
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", project)
	}
	if session != "s-1" {
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrUnknownSession, "no session %s", session)
	}
	src := newFakeSource()
	h.srcs = append(h.srcs, src)
	return src, proto.PeerSession{ID: session, Role: proto.RoleThread, Project: project, Thread: "t-0004"}, nil
}

func (h *fakeHost) last() *fakeSource {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.srcs[len(h.srcs)-1]
}

// sink records what a near end's stream gets.
type sink struct {
	mu      sync.Mutex
	size    [2]uint16
	screen  bytes.Buffer // the last repaint and the output after it
	repaint int
	resizes [][2]uint16
	closed  string
}

func (s *sink) Repaint(c, r uint16, vt []byte) {
	s.mu.Lock()
	s.size = [2]uint16{c, r}
	s.screen.Reset()
	s.screen.Write(vt)
	s.repaint++
	s.mu.Unlock()
}
func (s *sink) Output(p []byte) { s.mu.Lock(); s.screen.Write(p); s.mu.Unlock() }
func (s *sink) Resize(c, r uint16) {
	s.mu.Lock()
	s.resizes = append(s.resizes, [2]uint16{c, r})
	s.mu.Unlock()
}
func (s *sink) Closed(reason string) { s.mu.Lock(); s.closed = reason; s.mu.Unlock() }

func (s *sink) text() string   { s.mu.Lock(); defer s.mu.Unlock(); return s.screen.String() }
func (s *sink) reason() string { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }

// panePair is two sessions over loopback TCP, b serving h's panes with
// the switch *level (for its peer studio) to a's calls.
func panePair(t *testing.T, h PaneHost, level *string) (a, b *session) {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	sb.methods = (&dispatcher{panes: h, caller: caller.ForPeer("studio"),
		level: func() string { mu.Lock(); defer mu.Unlock(); return *level }}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa, sb
}

func repaintFrame(cols, rows uint16, vt string) []byte {
	return append(proto.Size(cols, rows), vt...)
}

// TestPaneStream: a stream opens with a repaint, larger than a frame, and
// carries output and resizes; keys, a size and a repaint request go the
// other way while the switch is type; detaching closes the source.
func TestPaneStream(t *testing.T) {
	h := &fakeHost{}
	level := proto.PanesType
	a, _ := panePair(t, h, &level)
	ctx := context.Background()
	sk := &sink{}
	c, err := a.panes.open(ctx, proto.PeerAttachParams{Project: "web", Session: "s-1"}, sk)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Result.Input || c.Result.Session.Thread != "t-0004" {
		t.Fatalf("peer.attach: %+v", c.Result)
	}
	src := h.last()
	big := strings.Repeat("0123456789abcdef", 3000) // 48 000 bytes: four frames
	src.push(proto.FrameRepaint, repaintFrame(120, 40, big))
	src.push(proto.FrameOutput, []byte("more"))
	src.push(proto.FrameResize, proto.Size(100, 30))
	testwait.Eventually(t, "the repaint and output", func() bool { return sk.text() == big+"more" })
	testwait.Eventually(t, "the resize", func() bool { sk.mu.Lock(); defer sk.mu.Unlock(); return len(sk.resizes) == 1 })
	if sk.size != [2]uint16{120, 40} || sk.resizes[0] != [2]uint16{100, 30} {
		t.Errorf("size %v, resizes %v", sk.size, sk.resizes)
	}

	c.Input([]byte("y\r"))
	c.Size(90, 20)
	c.Repaint()
	testwait.Eventually(t, "keys, size and repaint request", func() bool {
		src.mu.Lock()
		defer src.mu.Unlock()
		return string(src.input) == "y\r" && len(src.sizes) == 1 && src.asked == 1
	})
	if src.sizes[0] != [2]uint16{90, 20} {
		t.Errorf("size %v", src.sizes[0])
	}
	c.Close()
	testwait.Eventually(t, "the source closed", src.isClosed)
	if h.seen[0] != caller.ForPeer("studio") {
		t.Errorf("the host saw %+v", h.seen[0])
	}
}

// TestPaneSwitch: off refuses peer.attach; view opens without input and
// drops keys; refusals of the host cross as they are.
func TestPaneSwitch(t *testing.T) {
	h := &fakeHost{}
	level := proto.PanesOff
	a, _ := panePair(t, h, &level)
	ctx := context.Background()
	if _, err := a.panes.open(ctx, proto.PeerAttachParams{Project: "web", Session: "s-1"}, &sink{}); code(err) != proto.ErrPanesOff {
		t.Fatalf("switch off: %v, want panes-off", err)
	}
	level = proto.PanesView
	c, err := a.panes.open(ctx, proto.PeerAttachParams{Project: "web", Session: "s-1"}, &sink{})
	if err != nil || c.Result.Input {
		t.Fatalf("switch view: %+v %v", c, err)
	}
	src := h.last()
	c.Input([]byte("rm -rf /\r"))
	c.Size(10, 10)
	c.Repaint()
	testwait.Eventually(t, "the repaint request", func() bool { src.mu.Lock(); defer src.mu.Unlock(); return src.asked == 1 })
	if src.input != nil || src.sizes != nil {
		t.Errorf("view typed %q, sized %v", src.input, src.sizes)
	}
	for _, p := range []proto.PeerAttachParams{{Project: "api", Session: "s-1"}, {Project: "web", Session: "s-9"}} {
		_, err := a.panes.open(ctx, p, &sink{})
		if want := map[string]string{"api": proto.ErrNotShared, "web": proto.ErrUnknownSession}[p.Project]; code(err) != want {
			t.Errorf("%+v: %v, want %s", p, err, want)
		}
	}
}

// TestPaneClosed: the far end's closed frame reaches the sink in fixed
// words, as does the switch going off, and the link dropping.
func TestPaneClosed(t *testing.T) {
	h := &fakeHost{}
	level := proto.PanesType
	a, b := panePair(t, h, &level)
	ctx := context.Background()
	open := func() (*sink, *fakeSource) {
		sk := &sink{}
		if _, err := a.panes.open(ctx, proto.PeerAttachParams{Project: "web", Session: "s-1"}, sk); err != nil {
			t.Fatal(err)
		}
		return sk, h.last()
	}
	sk, src := open()
	src.push(proto.FrameClosed, []byte("session exited: exit status 1 /home/me/secret"))
	testwait.Eventually(t, "exited", func() bool { return sk.reason() == proto.PaneExited })

	sk, src = open()
	b.panes.closeServed(proto.PaneOff)
	testwait.Eventually(t, "panes-off", func() bool { return sk.reason() == proto.PaneOff })
	testwait.Eventually(t, "the source closed", src.isClosed)

	sk, src = open()
	a.c.Close()
	testwait.Eventually(t, "link", func() bool { return sk.reason() == proto.PaneLink })
	testwait.Eventually(t, "the source closed with the link", src.isClosed)
}

// TestPaneWindow: the far end stops at the window until the near end
// acks, then sends the rest, in order.
func TestPaneWindow(t *testing.T) {
	oldW, oldA := paneWindow, paneAckEvery
	paneWindow, paneAckEvery = 64<<10, 1<<30 // the near end never acks
	t.Cleanup(func() { paneWindow, paneAckEvery = oldW, oldA })
	h := &fakeHost{}
	level := proto.PanesType
	a, _ := panePair(t, h, &level)
	sk := &sink{}
	c, err := a.panes.open(context.Background(), proto.PeerAttachParams{Project: "web", Session: "s-1"}, sk)
	if err != nil {
		t.Fatal(err)
	}
	src := h.last()
	var all bytes.Buffer
	for i := range 20000 {
		all.WriteString(strings.Repeat(string(rune('a'+i%26)), 10))
	}
	src.push(proto.FrameRepaint, repaintFrame(80, 24, ""))
	src.push(proto.FrameOutput, all.Bytes())
	testwait.Eventually(t, "the window's worth", func() bool { return len(sk.text()) >= paneWindow })
	time.Sleep(100 * time.Millisecond)
	if n := len(sk.text()); n > paneWindow+frameChunk {
		t.Fatalf("%d bytes arrived without an ack, window %d", n, paneWindow)
	}
	// Acks open the window again.
	c.mu.Lock()
	c.unacked = 0
	c.mu.Unlock()
	go func() {
		for len(sk.text()) < all.Len() {
			c.s.sendFrame(c.id, frameAck, []byte{0, 1, 0, 0})
			time.Sleep(2 * time.Millisecond)
		}
	}()
	testwait.Eventually(t, "everything", func() bool { return len(sk.text()) == all.Len() })
	if sk.text() != all.String() {
		t.Error("output arrived out of order")
	}
}

// TestPaneThroughRelay: a server with panes says pane-vt in its hello and
// serves its panes while its switch for the peer allows it; changing the
// switch in peers.json acts on the open stream at Reload; a server
// without panes is ErrNoPanes.
func TestPaneThroughRelay(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)
	if ok, err := SetPanes(b.dir, "studio", proto.PanesType); !ok || err != nil {
		t.Fatalf("SetPanes: %v %v", ok, err)
	}
	ma := a.start(t, "0.30.0", tc)
	h := &fakeHost{}
	mb := Start(Options{Dir: b.dir, Version: "0.31.0", Logf: t.Logf, RelayTLS: tc, Panes: h,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(mb.Close)
	pa := waitLink(t, ma, "mini", "up", isUp)
	waitLink(t, mb, "studio", "up", isUp)
	if !slices.Contains(pa.Caps, CapPane) {
		t.Fatalf("a sees caps %v", pa.Caps)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sk := &sink{}
	c, err := ma.OpenPane(ctx, "mini", proto.PeerAttachParams{Project: "web", Session: "s-1"}, sk)
	if err != nil || !c.Result.Input {
		t.Fatalf("OpenPane: %+v %v", c, err)
	}
	h.last().push(proto.FrameRepaint, repaintFrame(80, 24, "hello"))
	testwait.Eventually(t, "the repaint", func() bool { return sk.text() == "hello" })
	var ps proto.PeerProjects
	if err := ma.Call(ctx, "mini", proto.MethodPeerProjects, nil, &ps); code(err) != proto.ErrUnknownMethod {
		// b exports no projects; peer.projects isn't there.
		t.Logf("peer.projects: %v", err)
	}

	if _, err := SetPanes(b.dir, "studio", proto.PanesOff); err != nil {
		t.Fatal(err)
	}
	mb.Reload()
	testwait.Eventually(t, "panes-off", func() bool { return sk.reason() == proto.PaneOff })
	if _, err := ma.OpenPane(ctx, "mini", proto.PeerAttachParams{Project: "web", Session: "s-1"}, &sink{}); code(err) != proto.ErrPanesOff {
		t.Errorf("after off: %v", err)
	}
	waitLink(t, ma, "mini", "still up", isUp)

	if _, err := mb.OpenPane(ctx, "studio", proto.PeerAttachParams{Project: "web", Session: "s-1"}, &sink{}); !errors.Is(err, ErrNoPanes) {
		t.Errorf("a has no panes, yet: %v", err)
	}
}

// TestSetPanes: the switch is kept in peers.json, off when unset, and
// pairing again keeps it. write is kept as "type", which tm v0.27.0
// reads, and a peers.json that says write reads as type.
func TestSetPanes(t *testing.T) {
	a, b := newServer(t), newServer(t)
	b.pair(t, "studio", a, "wss://relay.example.net")
	ps, _ := LoadPeers(b.dir)
	if ps[0].PanesLevel() != proto.PanesOff {
		t.Fatalf("default %q", ps[0].PanesLevel())
	}
	if _, err := SetPanes(b.dir, "studio", "talk"); err == nil {
		t.Error("took a level that isn't one")
	}
	if ok, err := SetPanes(b.dir, "studio", proto.PanesWrite); !ok || err != nil {
		t.Fatalf("write: %v %v", ok, err)
	}
	if ps, _ = LoadPeers(b.dir); ps[0].Panes != proto.PanesType {
		t.Fatalf("write kept as %q", ps[0].Panes)
	}
	if p := (Peer{Panes: proto.PanesWrite}); p.PanesLevel() != proto.PanesType {
		t.Errorf("a peers.json write reads as %q", p.PanesLevel())
	}
	if ok, _ := SetPanes(b.dir, "nobody", proto.PanesView); ok {
		t.Error("set the switch of a peer that isn't paired")
	}
	SetPanes(b.dir, "studio", proto.PanesView)
	if _, err := Pin(b.dir, pub(a.id.Key), "wss://relay.example.net", "studio", ""); err != nil {
		t.Fatal(err)
	}
	ps, _ = LoadPeers(b.dir)
	if len(ps) != 1 || ps[0].Panes != proto.PanesView {
		t.Fatalf("after pairing again: %+v", ps)
	}
}

// TestPaneRaiseTold: the far server tells the near one at once when its
// pane switch changes (peer.panes), and a stream opened at view types
// once raised to write; a near tm without peer.panes (v0.27.0) is never
// sent it, and its stream types too once raised (docs/SPEC.md §16.12).
func TestPaneRaiseTold(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "v0.27.0"}[old], func(t *testing.T) {
			_, _, url, tc := startRelay(t)
			a, b := newServer(t), newServer(t)
			a.pair(t, "mini", b, url)
			b.pair(t, "studio", a, url)
			SetPanes(b.dir, "studio", proto.PanesView)
			var mu sync.Mutex
			var told []string
			var farLog []string
			ma := Start(Options{Dir: a.dir, Version: "0.27.0", Logf: t.Logf, RelayTLS: tc, noPaneLevel: old,
				Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
			t.Cleanup(ma.Close)
			ma.OnPanes(func(_ ID, level string) { mu.Lock(); told = append(told, level); mu.Unlock() })
			h := &fakeHost{}
			mb := Start(Options{Dir: b.dir, Version: "0.28.0", RelayTLS: tc, Panes: h,
				Logf:    func(f string, a ...any) { mu.Lock(); farLog = append(farLog, fmt.Sprintf(f, a...)); mu.Unlock() },
				Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
			t.Cleanup(mb.Close)
			pb := waitLink(t, mb, "studio", "up", isUp)
			waitLink(t, ma, "mini", "up", isUp)
			if slices.Contains(pb.Caps, CapPaneLevel) == old {
				t.Fatalf("b sees caps %v", pb.Caps)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := ma.OpenPane(ctx, "mini", proto.PeerAttachParams{Project: "web", Session: "s-1"}, &sink{})
			if err != nil || c.Result.Input {
				t.Fatalf("OpenPane at view: %+v %v", c, err)
			}
			c.Input([]byte("at view"))

			SetPanes(b.dir, "studio", proto.PanesWrite)
			mb.Reload()
			if !old {
				testwait.Eventually(t, "the raise told", func() bool {
					mu.Lock()
					defer mu.Unlock()
					return slices.Equal(told, []string{proto.PanesType})
				})
			}
			// The far end checks each key: the open stream types now.
			testwait.Eventually(t, "keys after the raise", func() bool {
				c.Input([]byte("!"))
				return strings.Contains(h.last().typed(), "!")
			})
			if in := h.last().typed(); strings.Contains(in, "at view") {
				t.Errorf("a key at view went through: %q", in)
			}

			SetPanes(b.dir, "studio", proto.PanesView)
			mb.Reload()
			if !old {
				testwait.Eventually(t, "the lowering told", func() bool {
					mu.Lock()
					defer mu.Unlock()
					return slices.Equal(told, []string{proto.PanesType, proto.PanesView})
				})
			}
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			if old && len(told) > 0 {
				t.Errorf("an old tm heard %v", told)
			}
			for _, l := range farLog {
				if strings.Contains(l, "peer.panes") {
					t.Errorf("far logged %q", l)
				}
			}
		})
	}
}
