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
)

// needsExport is a fakeExport that shows "web"'s needs in full: a
// question menu, an escape's dialog the exporter (wrongly) sends whole,
// and a task in review.
type needsExport struct {
	*fakeExport
	mu    sync.Mutex
	asked int
}

func (n *needsExport) Needs(c caller.Caller, slug string) ([]proto.PeerNeed, error) {
	n.mu.Lock()
	n.asked++
	n.mu.Unlock()
	if slug != "web" {
		return nil, proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
	}
	menu := &proto.Question{Questions: []proto.QuestionItem{{Question: "Which cache?", Options: []proto.QuestionOption{{Label: "Redis"}, {Label: "None"}}}}}
	return []proto.PeerNeed{
		{ID: "question:s-1:a", Kind: proto.NeedQuestion, Session: "s-1", Thread: "t-0001", Question: menu},
		{ID: "approval:s-2", Kind: proto.NeedApproval, Session: "s-2", Escape: true,
			Approval: &proto.PeerApproval{Question: "Run rm -rf / outside the sandbox?", Options: []string{"Yes"}}},
		{ID: "review:T12:1", Kind: proto.NeedReview, Task: "T12", Title: "Cache layer", Status: "review"},
	}, nil
}

func (n *needsExport) times() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.asked
}

// TestPeerWatchNeeds: peer.watch carries the needs in full only while
// the switch is view or answer, never an escape's dialog, and
// peer.projects says the switch.
func TestPeerWatchNeeds(t *testing.T) {
	ex := &needsExport{fakeExport: newFakeExport()}
	var mu sync.Mutex
	level := proto.NeedsOff
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	sb.methods = (&dispatcher{ex: ex, caller: caller.ForPeer("studio"), needs: func() string { mu.Lock(); defer mu.Unlock(); return level }}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	ctx := context.Background()

	var w proto.PeerWatch
	if err := sa.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web"}, &w); err != nil || w.Needs != nil || ex.times() != 0 {
		t.Fatalf("off: %+v %v, asked %d", w.Needs, err, ex.times())
	}
	var ps proto.PeerProjects
	if err := sa.call(ctx, proto.MethodPeerProjects, nil, &ps); err != nil || ps.Needs != proto.NeedsOff {
		t.Fatalf("peer.projects at off: %q %v", ps.Needs, err)
	}
	for _, l := range []string{proto.NeedsView, proto.NeedsAnswer} {
		mu.Lock()
		level = l
		mu.Unlock()
		var full proto.PeerWatch
		if err := sa.call(ctx, proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web", Rev: w.Rev}, &full); err != nil || len(full.Needs) != 3 {
			t.Fatalf("%s: %+v %v", l, full.Needs, err)
		}
		if full.Rev == w.Rev {
			t.Errorf("%s: the rev didn't change with the needs", l)
		}
		if q := full.Needs[0].Question; q == nil || q.Questions[0].Question != "Which cache?" {
			t.Errorf("%s: the question's text didn't cross: %+v", l, full.Needs[0])
		}
		if e := full.Needs[1]; !e.Escape || e.Approval != nil || e.Question != nil {
			t.Errorf("%s: an escape's dialog crossed: %+v", l, e)
		}
		if r := full.Needs[2]; r.Kind != proto.NeedReview || r.Task != "T12" {
			t.Errorf("%s: review %+v", l, r)
		}
		if err := sa.call(ctx, proto.MethodPeerProjects, nil, &ps); err != nil || ps.Needs != l {
			t.Errorf("peer.projects at %s: %q %v", l, ps.Needs, err)
		}
	}
}

// TestNeedsSwitchThroughRelay: a server whose exporter shows needs says
// needs in its hello; its switch for a peer comes from peers.json, a
// change reaches the open link on Reload and the peer hears it on
// peer.panes, and the mirror there carries the needs and the switch.
func TestNeedsSwitchThroughRelay(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)
	if ok, err := SetNeeds(b.dir, "studio", proto.NeedsView); !ok || err != nil {
		t.Fatalf("SetNeeds: %v %v", ok, err)
	}
	ma := a.start(t, "0.61.0", tc)
	ex := &needsExport{fakeExport: newFakeExport()}
	mb := Start(Options{Dir: b.dir, Version: "0.61.0", Logf: t.Logf, RelayTLS: tc, Export: ex,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(mb.Close)
	pa := waitLink(t, ma, "mini", "up", isUp)
	waitLink(t, mb, "studio", "up", isUp)
	if !slices.Contains(pa.Caps, CapNeeds) {
		t.Fatalf("caps %v, want needs", pa.Caps)
	}
	mir := StartMirror(ma, MirrorOptions{})
	t.Cleanup(mir.Close)
	wait := func(what string, ok func(proto.PeerMirror) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		var last proto.PeerList
		for time.Now().Before(deadline) {
			if last = mir.List(); len(last.Peers) == 1 && ok(last.Peers[0]) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("mirror never %s: %+v", what, last)
	}
	wait("showed the needs", func(p proto.PeerMirror) bool {
		return p.NeedsCap && p.Needs == proto.NeedsView && len(p.Projects) == 1 && len(p.Projects[0].Needs) == 3
	})
	if _, err := SetNeeds(b.dir, "studio", proto.NeedsOff); err != nil {
		t.Fatal(err)
	}
	mb.Reload()
	wait("dropped the needs", func(p proto.PeerMirror) bool {
		return p.Needs == proto.NeedsOff && len(p.Projects) == 1 && p.Projects[0].Needs == nil
	})
}

// TestSetNeeds: the switch is kept in peers.json, off when unset, a
// word that isn't a level is refused, and pairing again keeps it.
func TestSetNeeds(t *testing.T) {
	a, b := newServer(t), newServer(t)
	b.pair(t, "studio", a, "wss://relay.example.net")
	ps, _ := LoadPeers(b.dir)
	if ps[0].NeedsLevel() != proto.NeedsOff {
		t.Fatalf("default %q", ps[0].NeedsLevel())
	}
	if _, err := SetNeeds(b.dir, "studio", "write"); err == nil {
		t.Error("took a level that isn't one")
	}
	if ok, _ := SetNeeds(b.dir, "nobody", proto.NeedsView); ok {
		t.Error("set the switch of a peer that isn't paired")
	}
	if ok, err := SetNeeds(b.dir, "studio", proto.NeedsAnswer); !ok || err != nil {
		t.Fatalf("answer: %v %v", ok, err)
	}
	if _, err := Pin(b.dir, pub(a.id.Key), "wss://relay.example.net", "studio", ""); err != nil {
		t.Fatal(err)
	}
	if ps, _ = LoadPeers(b.dir); len(ps) != 1 || ps[0].Needs != proto.NeedsAnswer {
		t.Fatalf("after pairing again: %+v", ps)
	}
	SetNeeds(b.dir, "studio", proto.NeedsOff)
	if ps, _ = LoadPeers(b.dir); ps[0].Needs != "" {
		t.Errorf("off kept as %q", ps[0].Needs)
	}
	ps[0].Needs = "all"
	if err := SavePeers(b.dir, ps); err == nil {
		t.Error("saved a needs switch that isn't a level")
	}
}

// TestCleanNeeds: a peer's needs are cleaned where they arrive: a need
// without an id or of a kind tm doesn't have is dropped, ids are
// checked, a row's words go on one line, and an escape keeps no menu or
// dialog whatever the peer sent.
func TestCleanNeeds(t *testing.T) {
	w := cleanWatch(proto.PeerWatch{Needs: []proto.PeerNeed{
		{ID: "", Kind: proto.NeedReview},
		{ID: "x:1", Kind: "merge"},
		{ID: "question:s-1:a", Kind: proto.NeedQuestion, Session: "s 1", Thread: "../t", Title: "two\nlines\x1b[31m",
			Question: &proto.Question{Questions: []proto.QuestionItem{{Question: "Which?", Options: []proto.QuestionOption{{Label: "a\nb"}}}}}},
		{ID: "approval:s-2", Kind: proto.NeedApproval, Escape: true, Approval: &proto.PeerApproval{Options: []string{"Yes"}},
			Question: &proto.Question{}},
	}})
	if len(w.Needs) != 2 {
		t.Fatalf("kept %+v", w.Needs)
	}
	q := w.Needs[0]
	if q.Session != "" || q.Thread != "" || strings.ContainsAny(q.Title, "\n\x1b") || strings.Contains(q.Question.Questions[0].Options[0].Label, "\n") {
		t.Errorf("not cleaned: %+v %+v", q, q.Question)
	}
	if e := w.Needs[1]; e.Approval != nil || e.Question != nil {
		t.Errorf("an escape kept its dialog: %+v", e)
	}
}
