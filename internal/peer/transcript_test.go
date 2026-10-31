package peer

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// fakeConversation is a conversation on the far end: events the test
// queues, acks it records.
type fakeConversation struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []proto.TranscriptEvent
	acked  int
	closed bool
}

func newFakeConversation() *fakeConversation {
	f := &fakeConversation{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

func (f *fakeConversation) push(ev proto.TranscriptEvent) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.cond.Broadcast()
	f.mu.Unlock()
}

func (f *fakeConversation) Run(send func(proto.TranscriptEvent) error) {
	for {
		f.mu.Lock()
		for len(f.events) == 0 && !f.closed {
			f.cond.Wait()
		}
		if f.closed {
			f.mu.Unlock()
			return
		}
		ev := f.events[0]
		f.events = f.events[1:]
		f.mu.Unlock()
		if send(ev) != nil || ev.Event == proto.EventTranscriptClosed {
			return
		}
	}
}

func (f *fakeConversation) Ack(n int) { f.mu.Lock(); f.acked += n; f.mu.Unlock() }
func (f *fakeConversation) Close() {
	f.mu.Lock()
	f.closed = true
	f.cond.Broadcast()
	f.mu.Unlock()
}
func (f *fakeConversation) isClosed() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

// fakeTranscripts serves one conversation for project web, session s-1.
type fakeTranscripts struct {
	mu   sync.Mutex
	srcs []*fakeConversation
	seen []caller.Caller
}

func (h *fakeTranscripts) OpenTranscript(c caller.Caller, project, session string) (TranscriptSource, proto.PeerTranscriptResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, c)
	if project != "web" {
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", project)
	}
	if session != "s-1" {
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrUnknownSession, "no session %s", session)
	}
	src := newFakeConversation()
	h.srcs = append(h.srcs, src)
	return src, proto.PeerTranscriptResult{Session: proto.PeerSession{ID: session, Role: proto.RoleThread, Project: project, Thread: "t-0004"},
		Status: proto.TranscriptStatus{State: proto.TranscriptMapped, Agent: "claude"}, Window: 500, Input: true}, nil
}

func (h *fakeTranscripts) count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.srcs) }

func (h *fakeTranscripts) last() *fakeConversation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.srcs[len(h.srcs)-1]
}

// trSink records what a near end's stream gets.
type trSink struct {
	mu     sync.Mutex
	events []proto.TranscriptEvent
}

func (s *trSink) Event(ev proto.TranscriptEvent) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *trSink) got() []proto.TranscriptEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]proto.TranscriptEvent(nil), s.events...)
}

// closed is the closed event's reason, "" before one.
func (s *trSink) closed() string {
	for _, ev := range s.got() {
		if ev.Event == proto.EventTranscriptClosed {
			return ev.Reason
		}
	}
	return ""
}

// trSwitches are the far end's two switches for its peer studio.
type trSwitches struct {
	mu    sync.Mutex
	level string
	on    bool
}

func (w *trSwitches) set(level string, on bool) {
	w.mu.Lock()
	w.level, w.on = level, on
	w.mu.Unlock()
}

// trPair is two sessions over loopback TCP, b serving h's conversations
// to a's calls with the switches w.
func trPair(t *testing.T, h TranscriptHost, w *trSwitches) (a, b *session) {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	sb.methods = (&dispatcher{transcripts: h, caller: caller.ForPeer("studio"),
		level:  func() string { w.mu.Lock(); defer w.mu.Unlock(); return w.level },
		convOn: func() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.on }}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa, sb
}

var trWeb = proto.PeerTranscriptParams{Project: "web", Session: "s-1"}

// TestTranscriptStream: a stream opens with a reset larger than a frame,
// carries items in order, takes acks back, and detaching closes the
// source.
func TestTranscriptStream(t *testing.T) {
	h := &fakeTranscripts{}
	w := &trSwitches{level: proto.PanesType, on: true}
	a, _ := trPair(t, h, w)
	sk := &trSink{}
	c, err := a.transcripts.open(context.Background(), trWeb, sk)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Result.Input || c.Result.Session.Thread != "t-0004" || c.Result.Status.State != proto.TranscriptMapped || c.Result.Window != 500 {
		t.Fatalf("peer.transcript: %+v", c.Result)
	}
	src := h.last()
	var reset []proto.TranscriptItem
	for i := range 20 {
		reset = append(reset, proto.TranscriptItem{Kind: proto.ItemText, Text: strings.Repeat("x", 4000) + string(rune('a'+i))})
	}
	src.push(proto.TranscriptEvent{Event: proto.EventTranscriptReset, Items: reset, Skipped: 3})
	src.push(proto.TranscriptEvent{Event: proto.EventTranscriptItems, Items: []proto.TranscriptItem{{Kind: proto.ItemTool, Tool: "Bash", Text: "ls", Call: "toolu_1"}}})
	testwait.Eventually(t, "the reset and the items", func() bool { return len(sk.got()) == 2 })
	got := sk.got()
	if got[0].Event != proto.EventTranscriptReset || len(got[0].Items) != 20 || got[0].Skipped != 3 || got[0].Items[19].Text != reset[19].Text {
		t.Errorf("reset: %s, %d items, skipped %d", got[0].Event, len(got[0].Items), got[0].Skipped)
	}
	if it := got[1].Items[0]; it.Tool != "Bash" || it.Text != "ls" || it.Call != "toolu_1" {
		t.Errorf("items: %+v", got[1])
	}
	c.Ack(21)
	testwait.Eventually(t, "the ack", func() bool { src.mu.Lock(); defer src.mu.Unlock(); return src.acked == 21 })
	c.Close()
	testwait.Eventually(t, "the source closed", src.isClosed)
	if h.seen[0] != caller.ForPeer("studio") {
		t.Errorf("the host saw %+v", h.seen[0])
	}
}

// TestTranscriptGate: the far end serves a conversation only while its
// pane switch is view or write and its conversations switch is on; view
// opens without input; the host's refusals (a project not shared, an
// unknown session) cross as they are.
func TestTranscriptGate(t *testing.T) {
	h := &fakeTranscripts{}
	w := &trSwitches{}
	a, _ := trPair(t, h, w)
	ctx := context.Background()
	for _, sw := range []struct {
		level string
		on    bool
	}{{proto.PanesOff, true}, {proto.PanesOff, false}, {proto.PanesView, false}, {proto.PanesType, false}} {
		w.set(sw.level, sw.on)
		if _, err := a.transcripts.open(ctx, trWeb, &trSink{}); code(err) != proto.ErrConversationsOff {
			t.Errorf("panes %s, conversations %v: %v, want conversations-off", sw.level, sw.on, err)
		}
	}
	if h.count() != 0 {
		t.Fatalf("the host opened %d conversations behind the switches", h.count())
	}
	w.set(proto.PanesView, true)
	c, err := a.transcripts.open(ctx, trWeb, &trSink{})
	if err != nil || c.Result.Input {
		t.Fatalf("view: %+v %v", c, err)
	}
	c.Close()
	w.set(proto.PanesType, true)
	if c, err = a.transcripts.open(ctx, trWeb, &trSink{}); err != nil || !c.Result.Input {
		t.Fatalf("write: %+v %v", c, err)
	}
	c.Close()
	for _, p := range []proto.PeerTranscriptParams{{Project: "api", Session: "s-1"}, {Project: "web", Session: "s-9"}} {
		_, err := a.transcripts.open(ctx, p, &trSink{})
		if want := map[string]string{"api": proto.ErrNotShared, "web": proto.ErrUnknownSession}[p.Project]; code(err) != want {
			t.Errorf("%+v: %v, want %s", p, err, want)
		}
	}
}

// TestTranscriptSwitchOff: the switch going off ends an open stream with
// its reason, and closes the source; the pane switch going off ends it
// as panes-off; a dropped link ends it as link.
func TestTranscriptSwitchOff(t *testing.T) {
	for _, reason := range []string{proto.ConversationsOff, proto.PaneOff} {
		h := &fakeTranscripts{}
		w := &trSwitches{level: proto.PanesView, on: true}
		a, b := trPair(t, h, w)
		sk := &trSink{}
		if _, err := a.transcripts.open(context.Background(), trWeb, sk); err != nil {
			t.Fatal(err)
		}
		src := h.last()
		src.push(proto.TranscriptEvent{Event: proto.EventTranscriptReset})
		testwait.Eventually(t, "the reset", func() bool { return len(sk.got()) == 1 })
		b.transcripts.closeServed(reason)
		testwait.Eventually(t, "the stream closed", func() bool { return sk.closed() != "" })
		if got := sk.closed(); got != reason {
			t.Errorf("closed %q, want %q", got, reason)
		}
		testwait.Eventually(t, "the source closed", src.isClosed)
	}

	h := &fakeTranscripts{}
	a, b := trPair(t, h, &trSwitches{level: proto.PanesView, on: true})
	sk := &trSink{}
	if _, err := a.transcripts.open(context.Background(), trWeb, sk); err != nil {
		t.Fatal(err)
	}
	b.c.Close()
	testwait.Eventually(t, "the stream closed", func() bool { return sk.closed() == proto.PaneLink })
}

// switchingTranscripts turns the switch off while a conversation opens.
type switchingTranscripts struct {
	fakeTranscripts
	off func()
}

func (h *switchingTranscripts) OpenTranscript(c caller.Caller, project, session string) (TranscriptSource, proto.PeerTranscriptResult, error) {
	src, res, err := h.fakeTranscripts.OpenTranscript(c, project, session)
	h.off()
	return src, res, err
}

// TestTranscriptSwitchOffWhileOpening: a conversation whose switch goes
// off while it opens isn't served.
func TestTranscriptSwitchOffWhileOpening(t *testing.T) {
	h := &switchingTranscripts{}
	w := &trSwitches{level: proto.PanesView, on: true}
	a, b := trPair(t, h, w)
	h.off = func() {
		w.set(proto.PanesView, false)
		b.transcripts.closeServed(proto.ConversationsOff)
	}
	if _, err := a.transcripts.open(context.Background(), trWeb, &trSink{}); code(err) != proto.ErrConversationsOff {
		t.Fatalf("%v, want conversations-off", err)
	}
	testwait.Eventually(t, "the source closed", h.last().isClosed)
}

// TestTranscriptClean: what a far server's conversation says is cleaned
// as it arrives: escapes go, a summary is one bounded line, a kind or an
// event this tm doesn't know is dropped, a closed reason is a fixed
// word.
func TestTranscriptClean(t *testing.T) {
	const osc52 = "\x1b]52;c;cm0gLXJmIH4=\x07"
	ev, ok := cleanTranscriptEvent(proto.TranscriptEvent{Event: proto.EventTranscriptItems, Items: []proto.TranscriptItem{
		{Kind: proto.ItemText, Text: "hi" + osc52 + "\nthere"},
		{Kind: proto.ItemTool, Tool: "Bash" + osc52, Text: "ls\n" + strings.Repeat("y", 500), Call: "bad id with spaces"},
		{Kind: "exec", Text: "run me"},
		{Kind: proto.ItemTodos, Todos: []proto.TranscriptTodo{{Text: "a\x1b[2J", Status: "in_progress"}}},
		{Kind: proto.ItemPrompt, Text: "go", From: "root"},
	}, Status: &proto.TranscriptStatus{State: "mapped", Reason: "x" + osc52}})
	if !ok || len(ev.Items) != 4 {
		t.Fatalf("%v %+v", ok, ev)
	}
	if ev.Items[0].Text != "hi]52;c;cm0gLXJmIH4=\nthere" {
		t.Errorf("text %q", ev.Items[0].Text)
	}
	if it := ev.Items[1]; strings.ContainsAny(it.Tool+it.Text, "\x1b\n") || len([]rune(it.Text)) > proto.TranscriptSummary || it.Call != "" {
		t.Errorf("tool %+v", it)
	}
	if ev.Items[2].Todos[0].Text != "a[2J" || ev.Items[3].From != "" || strings.Contains(ev.Status.Reason, "\x1b") {
		t.Errorf("todos %+v, from %q, status %+v", ev.Items[2].Todos, ev.Items[3].From, ev.Status)
	}
	many := make([]string, proto.TranscriptPicked+3)
	for i := range many {
		many[i] = "opt"
	}
	ev, _ = cleanTranscriptEvent(proto.TranscriptEvent{Event: proto.EventTranscriptItems, Items: []proto.TranscriptItem{
		{Kind: proto.ItemResult, Answers: make([]proto.TranscriptAnswer, proto.TranscriptAnswers+2)},
		{Kind: proto.ItemResult, Answers: []proto.TranscriptAnswer{{Header: "H" + osc52, Question: "Q\nnext" + strings.Repeat("q", 2000),
			Picked: append([]string{"", "a\x1b[2J"}, many...), Typed: "t" + osc52, Notes: "n\x1b[1m"}}},
		{Kind: proto.ItemText, Text: "x", Answers: []proto.TranscriptAnswer{{Question: "not a result's"}}},
	}})
	if len(ev.Items) != 3 || len(ev.Items[0].Answers) != proto.TranscriptAnswers || ev.Items[2].Answers != nil {
		t.Fatalf("answers %+v", ev.Items)
	}
	a := ev.Items[1].Answers[0]
	if strings.ContainsAny(a.Header+a.Question+a.Typed+a.Notes+strings.Join(a.Picked, ""), "\x1b\n") ||
		len([]rune(a.Question)) > proto.TranscriptAnswerText || len(a.Picked) != proto.TranscriptPicked-1 || a.Picked[0] != "a[2J" {
		t.Errorf("answer %+v", a)
	}
	if _, ok := cleanTranscriptEvent(proto.TranscriptEvent{Event: "transcript.exec"}); ok {
		t.Error("an unknown event passed")
	}
	if ev, _ := cleanTranscriptEvent(proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: "pwned" + osc52}); ev.Reason != proto.PaneExited {
		t.Errorf("closed %q", ev.Reason)
	}
}

// TestTranscriptEventTooBig: an event past maxTranscriptEvent ends the
// stream rather than filling the near end.
func TestTranscriptEventTooBig(t *testing.T) {
	h := &fakeTranscripts{}
	a, b := trPair(t, h, &trSwitches{level: proto.PanesView, on: true})
	sk := &trSink{}
	c, err := a.transcripts.open(context.Background(), trWeb, sk)
	if err != nil {
		t.Fatal(err)
	}
	piece := make([]byte, frameChunk)
	for range maxTranscriptEvent/frameChunk + 1 {
		if b.sendFrame(c.id, frameTrPart, piece) != nil {
			break
		}
	}
	testwait.Eventually(t, "the stream closed", func() bool { return sk.closed() != "" })
	testwait.Eventually(t, "the source closed", h.last().isClosed)
}

// TestSetConversations: the switch is on by default, off is kept in
// peers.json and through pairing again, and a word that isn't on or off
// doesn't load.
func TestSetConversations(t *testing.T) {
	a, b := newServer(t), newServer(t)
	b.pair(t, "studio", a, "wss://relay.example.net")
	ps, _ := LoadPeers(b.dir)
	if !ps[0].ConversationsOn() || ps[0].Conversations != "" {
		t.Fatalf("default %+v", ps[0])
	}
	if ok, err := SetConversations(b.dir, "studio", false); !ok || err != nil {
		t.Fatalf("off: %v %v", ok, err)
	}
	if ok, _ := SetConversations(b.dir, "nobody", false); ok {
		t.Error("set the switch of a peer that isn't paired")
	}
	if _, err := Pin(b.dir, pub(a.id.Key), "wss://relay.example.net", "studio", ""); err != nil {
		t.Fatal(err)
	}
	if ps, _ = LoadPeers(b.dir); len(ps) != 1 || ps[0].ConversationsOn() || ps[0].Conversations != proto.ConversationsNot {
		t.Fatalf("after pairing again: %+v", ps)
	}
	SetConversations(b.dir, "studio", true)
	if ps, _ = LoadPeers(b.dir); ps[0].Conversations != "" {
		t.Fatalf("on: %+v", ps[0])
	}
	bad := ps[0]
	bad.Conversations = "maybe"
	if err := SavePeers(b.dir, []Peer{bad}); err == nil {
		t.Error("saved a switch that is neither on nor off")
	}
}

// TestConversationsTold: the far server says its conversations switch
// in peer.projects and, when it changes, in peer.panes, so the near
// server's mirror has it; a link without a transcript host says neither.
func TestConversationsTold(t *testing.T) {
	var got []proto.PeerPanesParams
	var mu sync.Mutex
	d := &dispatcher{transcripts: &fakeTranscripts{}, panes: &fakeHost{}, ex: &fakeExport{},
		level: func() string { return proto.PanesView }, convOn: func() bool { return false },
		panesHere: func(p proto.PeerPanesParams) { mu.Lock(); got = append(got, p); mu.Unlock() }}
	res, err := d.projects(context.Background(), nil)
	if err != nil || res.(proto.PeerProjects).Conversations != proto.ConversationsNot || res.(proto.PeerProjects).Panes != proto.PanesView {
		t.Fatalf("peer.projects: %+v %v", res, err)
	}
	d.transcripts = nil
	if res, _ := d.projects(context.Background(), nil); res.(proto.PeerProjects).Conversations != "" {
		t.Fatalf("without a transcript host: %+v", res)
	}
	for _, raw := range []string{`{"panes":"view","conversations":"off"}`, `{"panes":"type","conversations":"pwned"}`, `{"panes":"view"}`} {
		if _, err := d.panesChanged(context.Background(), []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	want := []proto.PeerPanesParams{{Panes: "view", Conversations: "off"}, {Panes: "type"}, {Panes: "view"}}
	if !slices.Equal(got, want) {
		t.Fatalf("told %+v, want %+v", got, want)
	}
}
