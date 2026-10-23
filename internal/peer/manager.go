package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
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
	// Export is what paired servers may read of this one (peer.projects,
	// peer.watch); nil exports nothing and leaves out CapWatch.
	Export Exporter
	// Messages takes paired servers' messages (peer.message); nil takes
	// none and leaves out CapMsg.
	Messages Messages
	// Panes serves this server's panes to paired servers whose switch
	// allows it (peer.attach); nil serves none and leaves out CapPane.
	Panes PaneHost
	// Files serves the files this server offered paired servers
	// (peer.file.fetch); nil serves none and leaves out CapFile.
	Files FileHost
	// noPaneLevel plays tm v0.27.0 (tests): no CapPaneLevel, no
	// peer.panes.
	noPaneLevel bool
}

// Manager keeps a link to every paired server. Nothing local waits on
// it: each link is its own goroutine, and Status only reads.
type Manager struct {
	opts Options
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu      sync.Mutex
	id      *Identity
	err     error
	links   map[ID]*running
	onPanes func(id ID, level string)
	// fileSlots bounds the file streams served over all links.
	fileSlots chan struct{}
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
	m := &Manager{opts: opts, links: map[ID]*running{}, fileSlots: make(chan struct{}, maxFileStreams)}
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
		} else {
			if p.PanesLevel() != r.l.panesLevel() {
				r.l.setPanes(p.PanesLevel())
			}
			if p.Name != r.l.status().Name {
				r.l.setName(p.Name)
			}
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
		peer:      p,
		id:        id,
		cert:      cert,
		t:         RelayTransport(p.Relay, id, p.ID(), m.opts.RelayTLS),
		hello:     Hello{LinkProtocol: LinkProtocol, Version: m.opts.Version, Caps: m.caps()},
		logf:      m.opts.Logf,
		backoff:   m.opts.Backoff,
		fileSlots: m.fileSlots,
	}
	d := &dispatcher{ex: m.opts.Export, msgs: m.opts.Messages, panes: m.opts.Panes, files: m.opts.Files, caller: caller.ForPeer(p.Alias),
		peer: p.ID(), level: l.panesLevel, panesHere: func(level string) { m.panesHere(p.ID(), level) }}
	if m.opts.noPaneLevel {
		d.panesHere = nil
	}
	l.methods = d.methods()
	l.st = proto.PeerLink{Alias: p.Alias, Name: p.Name, ID: p.ID().String(), Transport: l.t.String(), State: proto.LinkConnecting}
	ctx, stop := context.WithCancel(m.ctx)
	r := &running{l: l, stop: stop}
	m.links[p.ID()] = r
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		l.run(ctx)
	}()
}

// caps are the capabilities this server's hello lists.
func (m *Manager) caps() []string {
	caps := slices.Clone(Caps)
	if m.opts.Export != nil {
		caps = append(caps, CapWatch)
		if _, ok := m.opts.Export.(PopupExporter); ok {
			caps = append(caps, CapPopup)
		}
	}
	if m.opts.Messages != nil {
		caps = append(caps, CapMsg)
	}
	if m.opts.Panes != nil {
		caps = append(caps, CapPane)
	}
	if m.opts.Files != nil {
		caps = append(caps, CapFile)
	}
	if m.opts.noPaneLevel {
		return caps
	}
	return append(caps, CapPaneLevel)
}

// OnPanes sets f to hear a paired server's word that its pane switch
// for this server changed (peer.panes): the mirror's. f mustn't block.
func (m *Manager) OnPanes(f func(id ID, level string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPanes = f
}

func (m *Manager) panesHere(id ID, level string) {
	m.mu.Lock()
	f := m.onPanes
	m.mu.Unlock()
	if f != nil {
		f(id, level)
	}
}

// Call sends the paired server alias (or the one named so) a request on its link and waits
// for the answer (peer.projects, peer.watch where its hello lists
// CapWatch).
func (m *Manager) Call(ctx context.Context, alias, method string, params, result any) error {
	if m == nil {
		return errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return fmt.Errorf("no paired server %q", alias)
	}
	return l.call(ctx, method, params, result)
}

// OpenPane opens a stream on the pane p.Session of the paired server
// alias (docs/SPEC.md §16.12), its frames to sink: ErrNoPanes when that
// server's tm has none, else peer.attach's error (proto.ErrPanesOff …).
func (m *Manager) OpenPane(ctx context.Context, alias string, p proto.PeerAttachParams, sink PaneSink) (*PaneConn, error) {
	if m == nil {
		return nil, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return nil, fmt.Errorf("no paired server %q", alias)
	}
	return l.openPane(ctx, p, sink)
}

// link is the link to the paired server alias, or the one named so;
// nil for none.
func (m *Manager) link(alias string) *link {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.links {
		if r.l.peer.Alias == alias {
			return r.l
		}
	}
	for _, r := range m.links {
		if r.l.status().Name == alias {
			return r.l
		}
	}
	return nil
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
