package device

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io/fs"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Level is a device's level while its link runs: the app API reads it
// for every request and hears when it changes, so a change acts at once
// on the open streams too.
type Level struct {
	mu      sync.Mutex
	v       string
	changed chan struct{}
}

// NewLevel is a level at v.
func NewLevel(v string) *Level { return &Level{v: v, changed: make(chan struct{})} }

// Get is the level now, and a channel closed when it changes.
func (l *Level) Get() (string, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.v, l.changed
}

// Set changes the level.
func (l *Level) Set(v string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v == l.v {
		return
	}
	l.v = v
	close(l.changed)
	l.changed = make(chan struct{})
}

// Serve runs the app API for device d over conn (past its TLS) until
// conn or ctx ends: the server's (internal/appapi, through cli, which
// imports the server).
type Serve func(ctx context.Context, conn net.Conn, d Device, level *Level) error

// Options configure a Manager.
type Options struct {
	// Dir is the device state directory (DirIn); PeerDir the peer one,
	// whose identity key the links prove.
	Dir, PeerDir string
	// Serve serves each link once its TLS is up; nil serves nothing.
	Serve Serve
	// Logf logs link changes; nil logs nothing.
	Logf func(format string, a ...any)
	// RelayTLS is the outer TLS to relays: nil trusts the system's CAs.
	RelayTLS *tls.Config
	// Backoff replaces the wait between dials (tests).
	Backoff func(n int) time.Duration
}

// Manager keeps a link to every paired device: a connection waiting in
// its room on the relay, which the device mates when it opens.
type Manager struct {
	opts Options
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu    sync.Mutex
	id    *peer.Identity
	err   error
	links map[peer.ID]*link
}

type link struct {
	d      Device
	level  *Level
	cancel context.CancelFunc

	mu sync.Mutex
	st proto.DeviceLink
	// waitLogged says the wait for the device is logged since the link
	// was last up (run's goroutine only).
	waitLogged bool
}

// Start reads the identity key and the devices and dials each device's
// room. No identity key, or no devices, is no links until Reload.
func Start(opts Options) *Manager {
	if opts.Backoff == nil {
		opts.Backoff = backoff
	}
	m := &Manager{opts: opts, links: map[peer.ID]*link{}}
	m.ctx, m.stop = context.WithCancel(context.Background())
	m.Reload()
	return m
}

// backoff is peer links' (1 s doubling to 60 s, with jitter).
func backoff(n int) time.Duration {
	d := 60 * time.Second
	if n < 6 {
		d = min(d, time.Second<<n)
	}
	return d/2 + rand.N(d/2+1)
}

// Reload reads the identity key and the devices again: a new device gets
// a link; a removed one, or one whose key, name, relay or write key
// changed, loses its link at once (a changed one dials again); a
// changed level reaches the open link.
func (m *Manager) Reload() error {
	if m == nil {
		return nil
	}
	id, err := peer.LoadIdentity(m.opts.PeerDir)
	if errors.Is(err, fs.ErrNotExist) {
		id, err = nil, nil
	}
	var devices []Device
	if err == nil && id != nil {
		devices, err = Load(m.opts.Dir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return nil
	}
	m.err = err
	if err != nil {
		m.logf("devices: %v", err)
		return err
	}
	if id == nil || m.id == nil || id.ID != m.id.ID {
		for k := range m.links {
			m.drop(k)
		}
	}
	m.id = id
	want := map[peer.ID]Device{}
	for _, d := range devices {
		want[d.ID()] = d
	}
	for k, l := range m.links {
		d, ok := want[k]
		if !ok || d.Name != l.d.Name || d.Relay != l.d.Relay || !bytes.Equal(d.WriteKey, l.d.WriteKey) {
			m.drop(k)
			continue
		}
		l.d.Level = d.Level
		l.level.Set(d.Effective())
		l.mu.Lock()
		l.st.Level, l.st.Effective = d.Level, d.Effective()
		l.mu.Unlock()
	}
	for k, d := range want {
		if _, ok := m.links[k]; !ok {
			m.add(id, d)
		}
	}
	return nil
}

func (m *Manager) add(id *peer.Identity, d Device) {
	ctx, cancel := context.WithCancel(m.ctx)
	l := &link{d: d, level: NewLevel(d.Effective()), cancel: cancel,
		st: proto.DeviceLink{Name: d.Name, ID: d.ID().String(), Level: d.Level, Effective: d.Effective(), State: proto.LinkConnecting, Since: time.Now()}}
	m.links[d.ID()] = l
	t := peer.RelayTransport(d.Relay, id, d.ID(), m.opts.RelayTLS)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, id, t, l)
	}()
}

// drop ends k's link: its stream closes at once.
func (m *Manager) drop(k peer.ID) {
	l := m.links[k]
	delete(m.links, k)
	l.cancel()
}

func (m *Manager) logf(format string, a ...any) {
	if m.opts.Logf != nil {
		m.opts.Logf(format, a...)
	}
}

// helloTimeout bounds the TLS handshake once the device is there.
const helloTimeout = 10 * time.Second

// run keeps l's link until ctx ends.
func (m *Manager) run(ctx context.Context, id *peer.Identity, t peer.Transport, l *link) {
	fails := 0
	for {
		l.set(proto.LinkConnecting, "", nil)
		up, err := m.once(ctx, id, t, l)
		if ctx.Err() != nil {
			return
		}
		if up {
			fails = 0
		}
		wait := m.opts.Backoff(fails)
		var ce *relay.CloseError
		reason := proto.LinkReasonRelay
		switch {
		case errors.As(err, &ce) && ce.Reason == relay.ErrTimeout:
			// the room waited its time out: no failure, wait again
			wait, reason, err = m.opts.Backoff(0), proto.LinkReasonPeer, nil
		case up:
			reason = proto.LinkReasonPeer
			fails++
		default:
			fails++
		}
		l.set(proto.LinkOffline, reason, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// once waits for the device, proves both keys and serves the link until
// it ends; up says the TLS came up.
func (m *Manager) once(ctx context.Context, id *peer.Identity, t peer.Transport, l *link) (bool, error) {
	c, err := t.Dial(ctx, func() {
		l.set(proto.LinkOffline, proto.LinkReasonPeer, nil)
		if !l.waitLogged {
			l.waitLogged = true
			m.logf("device %s: waiting for it in link room %s on %s", l.d.Name, roomPrefix(id.ID, l.d.ID()), t)
		}
	})
	if err != nil {
		return false, err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	l.set(proto.LinkConnecting, "", nil)
	cfg, client, err := peer.LinkTLS(id, l.d.Key)
	if err != nil {
		return false, err
	}
	var tc *tls.Conn
	if client {
		tc = tls.Client(c, cfg)
	} else {
		tc = tls.Server(c, cfg)
	}
	hctx, cancel := context.WithTimeout(ctx, helloTimeout)
	err = tc.HandshakeContext(hctx)
	cancel()
	if err != nil {
		l.set(proto.LinkOffline, proto.LinkReasonRefused, err)
		m.logf("device %s: its TLS failed: %v", l.d.Name, err)
		return false, err
	}
	l.set(proto.LinkUp, "", nil)
	l.waitLogged = false
	m.logf("device %s: up", l.d.Name)
	if m.opts.Serve == nil {
		<-ctx.Done()
		return true, nil
	}
	err = m.opts.Serve(ctx, tc, l.d, l.level)
	m.logf("device %s: closed", l.d.Name)
	return true, err
}

// roomPrefix is the link room's id as the relay logs it: its first four
// bytes in hex.
func roomPrefix(own, device peer.ID) string {
	r := relay.LinkRoom(own, device)
	return hex.EncodeToString(r[:4])
}

// maxErrorRunes bounds the error a link keeps and shows.
const maxErrorRunes = 200

func (l *link) set(state, reason string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.st.State != state || l.st.Reason != reason {
		l.st.Since = time.Now()
	}
	l.st.State, l.st.Reason = state, reason
	switch {
	case err != nil:
		l.st.Error = textsafe.Line(err.Error(), maxErrorRunes)
	case state == proto.LinkUp:
		l.st.Error = ""
	}
}

// Status is every device's link, by name.
func (m *Manager) Status() []proto.DeviceLink {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []proto.DeviceLink
	for _, l := range m.links {
		l.mu.Lock()
		out = append(out, l.st)
		l.mu.Unlock()
	}
	slices.SortFunc(out, func(a, b proto.DeviceLink) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
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

// Dial is the device's side of its link, as the phone runs it: it meets
// the server whose key is serverKey in their link room on relayURL and
// proves both keys. ctx bounds the wait for the server.
func Dial(ctx context.Context, key *peer.Identity, serverKey ed25519.PublicKey, relayURL string, relayTLS *tls.Config) (net.Conn, error) {
	c, err := peer.RelayTransport(relayURL, key, peer.IDOf(serverKey), relayTLS).Dial(ctx, nil)
	if err != nil {
		return nil, err
	}
	cfg, client, err := peer.LinkTLS(key, serverKey)
	if err != nil {
		c.Close()
		return nil, err
	}
	var tc *tls.Conn
	if client {
		tc = tls.Client(c, cfg)
	} else {
		tc = tls.Server(c, cfg)
	}
	if err := tc.HandshakeContext(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return tc, nil
}
