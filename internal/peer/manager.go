package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// Options configure a Manager.
type Options struct {
	// Dir is the peer state directory (DirIn).
	Dir string
	// Version is this tm's release, for the hello.
	Version string
	// Logf logs link changes; nil logs nothing.
	Logf func(format string, a ...any)
	// RelayTLS is the outer TLS to relays: nil trusts the system's CAs
	// (a test's relay has its own).
	RelayTLS *tls.Config
	// Backoff replaces the wait between dials (tests).
	Backoff func(n int) time.Duration
}

// Manager keeps a link to every paired server. Nothing local waits on
// it: each link is its own goroutine, and Status only reads.
type Manager struct {
	opts Options
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu    sync.Mutex
	id    *Identity
	err   error
	links map[ID]*running
}

type running struct {
	l    *link
	stop context.CancelFunc
}

// Start loads the identity key and the peers in opts.Dir and dials each
// peer. A server with no identity key has no links until Reload finds
// one.
func Start(opts Options) *Manager {
	if opts.Backoff == nil {
		opts.Backoff = backoff
	}
	m := &Manager{opts: opts, links: map[ID]*running{}}
	m.ctx, m.stop = context.WithCancel(context.Background())
	m.Reload()
	return m
}

// Reload reads the identity key and the peers again: a new peer gets a
// link, a removed one loses it, a changed one dials again.
func (m *Manager) Reload() error {
	id, err := LoadIdentity(m.opts.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		id, err = nil, nil
	}
	var peers []Peer
	if err == nil && id != nil {
		peers, err = LoadPeers(m.opts.Dir)
	}
	var cert tls.Certificate
	if err == nil && id != nil {
		cert, err = certificate(id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return nil
	}
	m.err = err
	if err != nil {
		m.logf("peers: %v", err)
		// Keep the links as they are: a file being rewritten, or broken
		// by hand, shouldn't drop them.
		return err
	}
	if id == nil || m.id == nil || id.ID != m.id.ID {
		// A new key: every link proves it again.
		for k := range m.links {
			m.drop(k)
		}
	}
	m.id = id
	want := map[ID]Peer{}
	for _, p := range peers {
		if p.ID() == id.ID {
			m.logf("peer %s: the server's own key; not dialled", p.Alias)
			continue
		}
		want[p.ID()] = p
	}
	for k, r := range m.links {
		if p, ok := want[k]; !ok || p.Alias != r.l.peer.Alias || p.Relay != r.l.peer.Relay {
			m.drop(k)
		}
	}
	for k, p := range want {
		if _, ok := m.links[k]; !ok {
			m.add(id, cert, p)
		}
	}
	return nil
}

func (m *Manager) add(id *Identity, cert tls.Certificate, p Peer) {
	l := &link{
		peer:    p,
		id:      id,
		cert:    cert,
		t:       RelayTransport(p.Relay, id, p.ID(), m.opts.RelayTLS),
		hello:   Hello{LinkProtocol: LinkProtocol, Version: m.opts.Version, Caps: Caps},
		logf:    m.opts.Logf,
		backoff: m.opts.Backoff,
	}
	l.st = proto.PeerLink{Alias: p.Alias, ID: p.ID().String(), Transport: l.t.String(), State: proto.LinkConnecting}
	ctx, stop := context.WithCancel(m.ctx)
	r := &running{l: l, stop: stop}
	m.links[p.ID()] = r
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		l.run(ctx)
	}()
}

func (m *Manager) drop(k ID) {
	r := m.links[k]
	delete(m.links, k)
	r.stop()
}

func (m *Manager) logf(format string, a ...any) {
	if m.opts.Logf != nil {
		m.opts.Logf(format, a...)
	}
}

// Status is the identity and every link, by alias; nil without an
// identity key.
func (m *Manager) Status() *proto.LinkStatus {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.id == nil && m.err == nil {
		return nil
	}
	st := &proto.LinkStatus{}
	if m.id != nil {
		st.ID, st.Short = m.id.ID.String(), m.id.ID.Short()
	}
	if m.err != nil {
		st.Error = m.err.Error()
	}
	for _, r := range m.links {
		st.Peers = append(st.Peers, r.l.status())
	}
	slices.SortFunc(st.Peers, func(a, b proto.PeerLink) int { return strings.Compare(a.Alias, b.Alias) })
	return st
}

// Close ends every link and waits for them.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stop()
	m.mu.Unlock()
	m.wg.Wait()
}
