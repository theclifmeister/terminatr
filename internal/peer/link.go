package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// Backoff between dials: 1 s doubling to 60 s, with jitter. A link that
// came up starts again from 1 s.
const (
	backoffMin = time.Second
	backoffMax = 60 * time.Second
)

// backoff is the wait before dial number n+1 after n failures in a row:
// between half and all of min(backoffMax, backoffMin·2ⁿ).
func backoff(n int) time.Duration {
	d := backoffMax
	if n < 6 {
		d = min(backoffMax, backoffMin<<n)
	}
	return d/2 + rand.N(d/2+1)
}

// link keeps one peer's link up: dial, TLS, hello, serve, and again.
type link struct {
	peer  Peer
	id    *Identity
	cert  tls.Certificate
	t     Transport
	hello Hello
	// methods answer the peer's requests (the peer dispatcher's).
	methods map[string]handler
	logf    func(string, ...any)
	// backoff is the wait after n failures (a test shortens it).
	backoff func(n int) time.Duration

	mu sync.Mutex
	st proto.PeerLink
	// logged is the last offline line logged: a link that keeps failing
	// the same way logs it once, not every minute.
	logged string
	// sess is the live session while up.
	sess *session
}

// panesLevel is this server's pane switch for the peer now.
func (l *link) panesLevel() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peer.PanesLevel()
}

// setPanes changes the pane switch for the peer: at once for its open
// streams, which off closes; view stops their keys at the next one, and
// type lets them type. A peer that takes peer.panes hears of it now, so
// its open panes follow (docs/SPEC.md §16.12).
func (l *link) setPanes(level string) {
	l.mu.Lock()
	l.peer.Panes = level
	level = l.peer.PanesLevel()
	s, tell := l.sess, slices.Contains(l.st.Caps, CapPaneLevel)
	l.mu.Unlock()
	if s == nil {
		return
	}
	if level == proto.PanesOff {
		s.panes.closeServed(proto.PaneOff)
	}
	if tell {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), helloTimeout)
			defer cancel()
			if err := s.call(ctx, proto.MethodPeerPanes, proto.PeerPanesParams{Panes: level}, nil); err != nil && l.logf != nil {
				l.logf("peer %s: peer.panes: %v", l.peer.Alias, err)
			}
		}()
	}
}

// openPane opens a stream on the peer's pane p.Session (docs/SPEC.md
// §16.12), frames to sink.
func (l *link) openPane(ctx context.Context, p proto.PeerAttachParams, sink PaneSink) (*PaneConn, error) {
	l.mu.Lock()
	s, caps := l.sess, l.st.Caps
	l.mu.Unlock()
	if s == nil {
		return nil, errNotUp
	}
	if !slices.Contains(caps, CapPane) {
		return nil, ErrNoPanes
	}
	return s.panes.open(ctx, p, sink)
}

// ErrNoPanes: the peer's tm has no remote panes (its hello doesn't list
// CapPane).
var ErrNoPanes = errors.New("the paired server's tm has no remote panes")

func (l *link) status() proto.PeerLink {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.st
	st.Caps = append([]string(nil), st.Caps...)
	return st
}

// set moves the link to state for reason, logging a change.
func (l *link) set(state, reason string, err error, retry time.Time) {
	var line string
	switch {
	case state == proto.LinkOffline && err != nil:
		line = fmt.Sprintf("offline (%s): %v", reason, err)
	case state == proto.LinkOffline:
		line = fmt.Sprintf("offline (%s)", reason)
	}
	l.mu.Lock()
	changed := l.st.State != state || l.st.Reason != reason
	if changed {
		l.st.Since = time.Now()
	}
	l.st.State, l.st.Reason, l.st.Retry = state, reason, retry
	if err != nil {
		l.st.Error = err.Error()
	} else if state == proto.LinkUp {
		l.st.Error = ""
	}
	if state != proto.LinkUp {
		l.st.RTT = 0
	}
	version, lp := l.st.Version, l.st.LinkProtocol
	repeat := line != "" && line == l.logged
	switch state {
	case proto.LinkUp:
		l.logged = ""
	case proto.LinkOffline:
		l.logged = line
	}
	l.mu.Unlock()
	if !changed || repeat || l.logf == nil {
		return
	}
	switch state {
	case proto.LinkUp:
		l.logf("peer %s: up (tm %s, link protocol %d)", l.peer.Alias, version, lp)
	case proto.LinkOffline:
		l.logf("peer %s: %s", l.peer.Alias, line)
	}
}

// run keeps the link until ctx ends.
func (l *link) run(ctx context.Context) {
	fails := 0
	for {
		l.set(proto.LinkConnecting, "", nil, time.Time{})
		up, err := l.once(ctx)
		if ctx.Err() != nil {
			return
		}
		if up {
			fails = 0
		}
		reason := reasonOf(err)
		wait := l.backoff(fails)
		if isRoomTimeout(err) {
			// The relay's room waited its hour: no failure, dial again.
			wait = l.backoff(0)
		} else {
			fails++
		}
		l.set(proto.LinkOffline, reason, err, time.Now().Add(wait))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// once dials, proves keys, says hello and serves until the link ends.
// up says it got as far as the hellos.
func (l *link) once(ctx context.Context) (up bool, err error) {
	c, err := l.t.Dial(ctx, func() {
		l.set(proto.LinkOffline, proto.LinkReasonPeer, nil, time.Time{})
	})
	if err != nil {
		return false, err
	}
	// ctx ends the link at any point from here.
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	l.set(proto.LinkConnecting, "", nil, time.Time{})
	cfg := tlsConfig(l.cert, l.peer.Key)
	var tc *tls.Conn
	if tlsClient(l.id.ID, l.peer.ID()) {
		tc = tls.Client(c, cfg)
	} else {
		tc = tls.Server(c, cfg)
	}
	hctx, cancel := context.WithTimeout(ctx, helloTimeout)
	err = tc.HandshakeContext(hctx)
	cancel()
	if err != nil {
		return false, handshakeErr(err)
	}
	s := newSession(tc)
	s.methods = l.methods
	peer, err := s.hello(l.hello)
	l.mu.Lock()
	l.st.Version, l.st.LinkProtocol, l.st.Caps = peer.Version, peer.LinkProtocol, peer.Caps
	l.mu.Unlock()
	if err != nil {
		s.cancel()
		return false, err
	}
	l.mu.Lock()
	l.sess = s
	l.mu.Unlock()
	l.set(proto.LinkUp, "", nil, time.Time{})
	err = s.serve(func(d time.Duration) {
		l.mu.Lock()
		if l.sess == s {
			l.st.RTT = float64(d.Microseconds()) / 1000
		}
		l.mu.Unlock()
	})
	l.mu.Lock()
	l.sess = nil
	l.mu.Unlock()
	return true, err
}

// errNotUp: a call on a link that isn't up.
var errNotUp = errors.New("the link isn't up")

// call sends the peer a request over the live session.
func (l *link) call(ctx context.Context, method string, params, result any) error {
	l.mu.Lock()
	s := l.sess
	l.mu.Unlock()
	if s == nil {
		return errNotUp
	}
	return s.call(ctx, method, params, result)
}

// refusedError: the TLS handshake failed on the keys, not on the
// stream under it.
type refusedError struct{ err error }

func (e *refusedError) Error() string { return "the keys don't match: " + e.err.Error() }
func (e *refusedError) Unwrap() error { return e.err }

// handshakeErr marks a TLS handshake failure that isn't the stream's:
// the other end's key isn't the pinned one, it refused ours, or it
// isn't a tm link at all.
func handshakeErr(err error) error {
	var ce *relay.CloseError
	var ne net.Error
	if errors.As(err, &ce) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && !isRemoteAlert(err)) {
		return err
	}
	return &refusedError{err}
}

// isRemoteAlert: the other end sent a TLS alert, as crypto/tls reports
// it. Once TLS 1.3 is up, that is the server refusing the client's key.
func isRemoteAlert(err error) bool {
	var oe *net.OpError
	return errors.As(err, &oe) && oe.Op == "remote error"
}

// reasonOf says why a link failed with err.
func reasonOf(err error) string {
	var ce *relay.CloseError
	var re *refusedError
	var ve *versionError
	switch {
	case errors.As(err, &ce):
		if ce.Reason == relay.ErrPeerGone || ce.Reason == relay.ErrTimeout {
			return proto.LinkReasonPeer
		}
		return proto.LinkReasonRelay
	case errors.As(err, &re), errors.Is(err, errNotPinned), isRemoteAlert(err):
		return proto.LinkReasonRefused
	case errors.As(err, &ve):
		return proto.LinkReasonVersion
	}
	return proto.LinkReasonRelay
}

// isRoomTimeout: the relay closed a link room that waited its time out
// before the peer came.
func isRoomTimeout(err error) bool {
	var ce *relay.CloseError
	return errors.As(err, &ce) && ce.Reason == relay.ErrTimeout
}
