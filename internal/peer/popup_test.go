package peer

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// popupExport is fakeExport serving web's popup.
type popupExport struct {
	*fakeExport
	pp proto.PeerPopup
}

func (f *popupExport) Popup(c caller.Caller, slug string) (proto.PeerPopup, error) {
	f.mu.Lock()
	f.callers = append(f.callers, c)
	f.mu.Unlock()
	if slug != "web" {
		return proto.PeerPopup{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
	}
	return f.pp, nil
}

// popupPair is exportPair with this server's pane switch for the caller
// at *level.
func popupPair(t *testing.T, ex Exporter, level *string) *session {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	var mu sync.Mutex
	sb.methods = (&dispatcher{ex: ex, caller: caller.ForPeer("studio"), level: func() string {
		mu.Lock()
		defer mu.Unlock()
		return *level
	}}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return sa
}

// TestPeerPopupGate: peer.popup answers a shared project while the pane
// switch for the caller is view or type, and refuses with panes-off
// while it is off; an unshared project is not-shared; an exporter
// without popups has no such method at all.
func TestPeerPopupGate(t *testing.T) {
	ex := &popupExport{fakeExport: newFakeExport(), pp: proto.PeerPopup{Project: "web", Goal: "the shop"}}
	level := proto.PanesOff
	a := popupPair(t, ex, &level)
	ctx := context.Background()
	var pp proto.PeerPopup
	if err := a.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{Project: "web"}, &pp); code(err) != proto.ErrPanesOff {
		t.Fatalf("off: %+v %v", pp, err)
	}
	for _, l := range []string{proto.PanesView, proto.PanesType} {
		level = l
		pp = proto.PeerPopup{}
		if err := a.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{Project: "web"}, &pp); err != nil || pp.Goal != "the shop" || pp.Now.IsZero() {
			t.Errorf("%s: %+v %v", l, pp, err)
		}
	}
	if err := a.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{Project: "secret"}, nil); code(err) != proto.ErrNotShared {
		t.Errorf("unshared: %v", err)
	}
	if err := a.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("no project: %v", err)
	}
	for _, c := range ex.seen() {
		if c != caller.ForPeer("studio") {
			t.Errorf("the exporter was called as %+v", c)
		}
	}
	plain := popupPair(t, newFakeExport(), &level)
	if err := plain.call(ctx, proto.MethodPeerPopup, proto.PeerPopupParams{Project: "web"}, nil); code(err) != proto.ErrUnknownMethod {
		t.Errorf("an exporter without popups answered: %v", err)
	}
}

// TestPopupCap: a server whose exporter serves popups says popup in its
// hello, one whose exporter doesn't, not.
func TestPopupCap(t *testing.T) {
	with := &Manager{opts: Options{Export: &popupExport{fakeExport: newFakeExport()}}}
	without := &Manager{opts: Options{Export: newFakeExport()}}
	if !slices.Contains(with.caps(), CapPopup) || slices.Contains(without.caps(), CapPopup) {
		t.Errorf("caps: %v and %v", with.caps(), without.caps())
	}
}

// TestPopupIsCleaned: a peer's popup arrives with no escape or bidi
// override in any string, its rows' words on one line, ids checked
// against their patterns, and each list cut to its most.
func TestPopupIsCleaned(t *testing.T) {
	in := proto.PeerPopup{
		Project: "web", Goal: "ship‮" + osc52 + "\nnow", Repos: -3,
		Tasks: []proto.PeerTask{
			{ID: 1, Title: "fix\x1b[2J it\nnow", Status: "ready", Thread: "t-0001", StepsDone: 9, StepsTotal: 2},
			{ID: 0, Title: "no id"},
			{ID: 2, Title: "bad thread", Status: "re view", Thread: "../t-1"},
		},
		Settings: proto.PeerSettings{ThreadAgent: "claude" + osc52, Models: []string{"opus\nx", osc52}},
		Links: []proto.PeerPopupLink{
			{Project: "api", Held: -1, Last: "M-9", Messages: []proto.PeerLinkMessage{
				{ID: "M-1", State: "sent", By: "coordinator"}, {ID: "../M-2", State: "sent"}, {ID: "M-3", State: "x\x1b[31m", Re: "nope nope"}}},
			{Project: "../etc"},
		},
		Memory: []string{"Build" + osc52 + "\nnotes", "", "\x1b[2J"},
	}
	for i := range proto.PeerPopupMaxTasks + 10 {
		in.Tasks = append(in.Tasks, proto.PeerTask{ID: 10 + i, Title: "t"})
	}
	for i := range proto.PeerPopupMaxMessages + 5 {
		in.Links[0].Messages = append(in.Links[0].Messages, proto.PeerLinkMessage{ID: fmt.Sprintf("M-%d", 100+i), State: "read"})
	}
	p := cleanPopup(in)
	if hasEscape(t, p) {
		t.Fatalf("an escape crossed: %+v", p)
	}
	if strings.Contains(p.Goal, "\n") || p.Repos != 0 {
		t.Errorf("goal %q repos %d", p.Goal, p.Repos)
	}
	if len(p.Tasks) != proto.PeerPopupMaxTasks || p.Tasks[0].ID != 1 || p.Tasks[1].ID != 2 {
		t.Fatalf("tasks: %d, first %+v", len(p.Tasks), p.Tasks[:2])
	}
	if t0, t1 := p.Tasks[0], p.Tasks[1]; strings.Contains(t0.Title, "\n") || t0.StepsDone != 2 || t0.Thread != "t-0001" || t1.Thread != "" || t1.Status != "" {
		t.Errorf("tasks cleaned to %+v, %+v", t0, t1)
	}
	if len(p.Links) != 1 || p.Links[0].Held != 0 || p.Links[0].Last != "M-9" {
		t.Fatalf("links: %+v", p.Links)
	}
	ms := p.Links[0].Messages
	if len(ms) != proto.PeerPopupMaxMessages || ms[len(ms)-1].ID != fmt.Sprintf("M-%d", 100+proto.PeerPopupMaxMessages+4) {
		t.Errorf("messages: %d, last %+v", len(ms), ms[len(ms)-1])
	}
	if len(p.Memory) != 2 || strings.Contains(p.Memory[0], "\n") {
		t.Errorf("memory: %q", p.Memory)
	}
	if strings.Contains(p.Settings.Models[0], "\n") {
		t.Errorf("models: %q", p.Settings.Models)
	}
}
