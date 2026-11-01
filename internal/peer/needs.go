package peer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Paired needs (docs/SPEC.md §16.16): a shared project's needs in full,
// on peer.watch, for a paired server whose needs switch here is view or
// answer. Off, the default, it reads only the masked list every paired
// server reads.

// CapNeeds is the link capability of the needs switch: the server shows
// its needs in full on peer.watch to a paired server whose switch lets
// it, and says that switch on peer.projects and peer.panes.
const CapNeeds = "needs"

// CapNeedPicks says peer.act takes a multi-select's Picks (T900): a
// paired server without it refuses them, so its apps leave such a
// question to the pane there (T904).
const CapNeedPicks = "need-picks"

// NeedsExporter is an Exporter that shows a shared project's needs in
// full (CapNeeds).
type NeedsExporter interface {
	// Needs are the shared project's needs in full: never an escape's
	// menu, dialog or options (PeerNeed.Escape).
	Needs(c caller.Caller, project string) ([]proto.PeerNeed, error)
}

// ActExporter is a NeedsExporter that also acts on a need for the
// paired server's user (peer.act), at a needs switch of answer: the link
// checked the switch and the nonce already.
type ActExporter interface {
	NeedsExporter
	// Act does p for c, the paired server's user (caller.ForPeerHuman),
	// once the need is still the one they saw and p's guard holds:
	// proto.ErrNeedChanged otherwise, proto.ErrEscapeHere for an
	// escape's, and the act's own refusals.
	Act(c caller.Caller, p proto.PeerAct) (proto.PeerActResult, error)
}

// needsMethods are the link's methods of paired needs, by what the
// Exporter does: peer.needs to read them, peer.nonce and peer.act to act.
func (d *dispatcher) needsMethods(m map[string]handler) {
	if _, ok := d.ex.(NeedsExporter); !ok || d.needs == nil {
		return
	}
	m[proto.MethodPeerNeeds] = d.needsRead
	if _, ok := d.ex.(ActExporter); ok {
		m[proto.MethodPeerNonce] = d.nonce
		m[proto.MethodPeerAct] = d.act
	}
}

// errNeedsView refuses an act while the needs switch isn't answer.
var errNeedsView = proto.Errorf(proto.ErrNeedsOff, "this server shows yours its needs, but doesn't take answers from it")

// needsRead answers peer.needs: a shared project's needs now, at view
// or answer.
func (d *dispatcher) needsRead(_ context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerNeedsParams
	if err := json.Unmarshal(raw, &p); err != nil || p.Project == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.needs needs a project")
	}
	if !proto.NeedsShown(d.needs()) {
		return nil, proto.Errorf(proto.ErrNeedsOff, "this server doesn't show its needs to yours")
	}
	ns, err := d.needsOf(p.Project)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		ns = []proto.PeerNeed{}
	}
	return proto.PeerNeedsResult{Needs: ns}, nil
}

// nonce answers peer.nonce, at answer only.
func (d *dispatcher) nonce(context.Context, json.RawMessage) (any, error) {
	if d.needs() != proto.NeedsAnswer {
		return nil, errNeedsView
	}
	return proto.PeerNonce{Nonce: d.nonces.next(time.Now())}, nil
}

// act answers peer.act: at answer only, with a nonce this link gave,
// as the paired server's user (or its phone's), never an escape's.
func (d *dispatcher) act(_ context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerActParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.act: %v", err)
	}
	if d.needs() != proto.NeedsAnswer {
		return nil, errNeedsView
	}
	// The nonce goes first, used up whatever comes of the act: a
	// replayed act fails here, before anything is looked at.
	if !d.nonces.take(p.Nonce, time.Now()) {
		return nil, proto.Errorf(proto.ErrBadNonce, "peer.act: the nonce isn't one this server gave yours, or was used or is older than %s; ask peer.nonce again", proto.PeerNonceLife)
	}
	if p.Project == "" || p.Need == "" || p.Act == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.act needs a project, a need and an act")
	}
	device := textsafe.Line(p.Device, 64)
	if strings.TrimSpace(device) == "" {
		device = ""
	}
	return d.ex.(ActExporter).Act(caller.ForPeerHuman(d.caller.Alias, device), p.PeerAct)
}

// nonceMax bounds the nonces a link gave out and that wait for their
// act: a new one drops the oldest.
const nonceMax = 16

// nonces are the nonces a link gave its paired server, each good once.
type nonces struct {
	mu    sync.Mutex
	given map[string]time.Time
}

// next is a fresh nonce.
func (ns *nonces) next(now time.Time) []byte {
	b := make([]byte, 32)
	rand.Read(b)
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.given == nil {
		ns.given = map[string]time.Time{}
	}
	ns.sweep(now)
	for len(ns.given) >= nonceMax {
		var oldest string
		var at time.Time
		for k, t := range ns.given {
			if oldest == "" || t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(ns.given, oldest)
	}
	ns.given[string(b)] = now
	return b
}

// take uses nonce up: whether it was given and is still good.
func (ns *nonces) take(nonce []byte, now time.Time) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.sweep(now)
	_, ok := ns.given[string(nonce)]
	delete(ns.given, string(nonce))
	return ok && len(nonce) > 0
}

func (ns *nonces) sweep(now time.Time) {
	for k, t := range ns.given {
		if now.Sub(t) > proto.PeerNonceLife {
			delete(ns.given, k)
		}
	}
}

// needsLevel is this server's needs switch for the peer now.
func (l *link) needsLevel() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peer.NeedsLevel()
}

// setNeeds changes the needs switch for the peer: its next peer.watch
// answer follows (the watches read again within watchPoll), and a peer
// that takes peer.panes hears of it now.
func (l *link) setNeeds(level string) {
	l.mu.Lock()
	l.peer.Needs = level
	s := l.sess
	l.mu.Unlock()
	if s != nil {
		l.tellSwitches(s)
	}
}

// needsOf are project's needs for the peer at its switch now: none
// below view.
func (d *dispatcher) needsOf(project string) ([]proto.PeerNeed, error) {
	nx, ok := d.ex.(NeedsExporter)
	if !ok || d.needs == nil || !proto.NeedsShown(d.needs()) {
		return nil, nil
	}
	ns, err := nx.Needs(d.caller, project)
	if err != nil {
		return nil, err
	}
	for i := range ns {
		if ns[i].Escape {
			// Never what an escape asks: only the user there answers it.
			ns[i].Question, ns[i].Approval = nil, nil
		}
	}
	return ns, nil
}

// actWait bounds an act across the link: the console waits for it, and
// an answer types its keys there.
const actWait = 30 * time.Second

// ErrNoActs means the paired server's tm takes no acts on its needs
// (its hello doesn't list CapNeeds).
var ErrNoActs = proto.Errorf(proto.ErrUnknownMethod, "the paired server's tm takes no answers to its needs")

// Act sends act p on a need of the paired server alias (or the one
// named so), as this server's user, or its phone device's: a nonce from
// peer.nonce, then peer.act with it. Its refusals come back with their
// codes (proto.ErrNeedsOff, proto.ErrNeedChanged, proto.ErrEscapeHere …).
func (m *Manager) Act(ctx context.Context, alias string, p proto.PeerAct, device string) (proto.PeerActResult, error) {
	if m == nil {
		return proto.PeerActResult{}, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return proto.PeerActResult{}, proto.Errorf(proto.ErrRefused, "no paired server %q", alias)
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapNeeds) {
		return proto.PeerActResult{}, ErrNoActs
	}
	ctx, cancel := context.WithTimeout(ctx, actWait)
	defer cancel()
	var n proto.PeerNonce
	if err := l.call(ctx, proto.MethodPeerNonce, struct{}{}, &n); err != nil {
		return proto.PeerActResult{}, err
	}
	var res proto.PeerActResult
	if err := l.call(ctx, proto.MethodPeerAct, proto.PeerActParams{PeerAct: p, Nonce: n.Nonce, Device: device}, &res); err != nil {
		return proto.PeerActResult{}, err
	}
	res.Act = p.Act
	res.Option = textsafe.Line(res.Option, 300)
	res.Remaining, res.Open = max(res.Remaining, 0), max(res.Open, 0)
	return res, nil
}
