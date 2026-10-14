package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"
)

// Dialer joins a room on a relay.
type Dialer struct {
	Key  ed25519.PrivateKey
	Kind string   // KindLink or KindPair
	Room [32]byte // LinkRoom(own, peer) for a link
	Peer [32]byte // link: the peer's fingerprint
	TLS  *tls.Config
	// OnWait is called when the relay says the peer isn't there yet.
	OnWait func()
}

// Dial connects to rawURL (wss://host[:port], or ws:// for a relay
// behind a proxy on the same host and in tests), says hello and waits
// in the room until the peer comes or ctx ends. The Conn carries the
// peer's bytes. A wss:// URL ending in #sha256=<pin> takes the relay by
// its key's pin alone (PinnedTLS), else by d.TLS (nil: the system's CAs).
func (d *Dialer) Dial(ctx context.Context, rawURL string) (*Conn, error) {
	var conn *Conn
	err := d.open(ctx, rawURL, func(c net.Conn, host, path string) (err error) {
		conn, err = d.hello(c, host, path)
		return err
	})
	return conn, err
}

// UpdateResult is the relay's answer to an update request.
type UpdateResult struct {
	// Result is UpdateQueued, UpdateOff, UpdateNotLinked, UpdateTooSoon
	// or UpdateFailed.
	Result string
	// Next is when the relay takes another request, after UpdateTooSoon.
	Next time.Time
	// Version is the relay's tm release, "" when it doesn't say.
	Version string
}

// ErrNoUpdates means the relay's tm is older than update requests.
var ErrNoUpdates = errors.New("the relay's tm takes no update requests; update it on its host (sudo tm relay update)")

// RequestUpdate asks the relay at rawURL to update itself to the latest
// official release. The hello is a link's (d.Room and d.Peer, signed
// with d.Key) of kind KindUpdate: the relay takes it only while that
// link is live through it, and at most once an hour.
func (d *Dialer) RequestUpdate(ctx context.Context, rawURL string) (UpdateResult, error) {
	var res UpdateResult
	u := *d
	u.Kind = KindUpdate
	err := u.open(ctx, rawURL, func(c net.Conn, host, path string) error {
		ws, ch, err := u.greet(c, host, path)
		if err != nil {
			return err
		}
		res.Version = ch.TM
		op, msg, err := ws.ReadMessage()
		var ce *CloseError
		if errors.As(err, &ce) && ce.Reason == ErrBadHello {
			return ErrNoUpdates
		}
		if err != nil {
			return err
		}
		r, ok := parseUpdateReply(op, msg)
		if !ok {
			return errors.New("relay: unexpected answer to an update request")
		}
		ws.CloseWith(CloseNormal, "")
		res.Result, res.Next = r.Result, r.Next
		return nil
	})
	return res, err
}

// open connects to the relay at rawURL and runs f on the stream, ctx
// bounding both; the stream is closed when f fails.
func (d *Dialer) open(ctx context.Context, rawURL string, f func(c net.Conn, host, path string) error) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	pin, err := ParsePin(u.Fragment)
	if err != nil {
		return fmt.Errorf("relay %s: %w", u.Host, err)
	}
	if pin != "" && u.Scheme != "wss" {
		return fmt.Errorf("relay: %s: a pin goes with wss:// only", rawURL)
	}
	host := u.Host
	if u.Port() == "" {
		port := "443"
		if u.Scheme == "ws" {
			port = "80"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	path := u.Path
	if path == "" || path == "/" {
		path = "/v1"
	}
	var nd net.Dialer
	c, err := nd.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "wss":
		cfg := d.TLS.Clone()
		if pin != "" {
			cfg = PinnedTLS(d.TLS, u.Host, pin)
		}
		if cfg == nil {
			cfg = &tls.Config{}
		}
		if cfg.ServerName == "" {
			cfg.ServerName = u.Hostname()
		}
		if cfg.MinVersion == 0 {
			cfg.MinVersion = tls.VersionTLS12
		}
		c = tls.Client(c, cfg)
	case "ws":
	default:
		c.Close()
		return fmt.Errorf("relay: %s: wss:// or ws:// only", rawURL)
	}
	// ctx bounds everything up to "mated".
	stop := context.AfterFunc(ctx, func() { c.Close() })
	err = f(c, u.Host, path)
	if !stop() {
		c.Close()
		return ctx.Err()
	}
	if err != nil {
		c.Close()
		return err
	}
	return nil
}

// greet upgrades c to a WebSocket, reads the relay's challenge and
// answers it with d's hello.
func (d *Dialer) greet(c net.Conn, host, path string) (*wsConn, challenge, error) {
	var ch challenge
	ws, err := clientHandshake(c, host, path, maxMessage)
	if err != nil {
		return nil, ch, err
	}
	ws.idle = 3 * DefaultConfig().Ping
	op, msg, err := ws.ReadMessage()
	if err != nil {
		return nil, ch, err
	}
	if op != opText || json.Unmarshal(msg, &ch) != nil || ch.T != msgChallenge || len(ch.Nonce) != 32 {
		return nil, ch, errNotHello
	}
	if ch.V != Version {
		return nil, ch, fmt.Errorf("relay: wire version %d, this tm speaks %d", ch.V, Version)
	}
	h := hello{T: msgHello, Kind: d.Kind, Key: d.Key.Public().(ed25519.PublicKey), Room: hex.EncodeToString(d.Room[:])}
	var peer []byte
	if d.Kind != KindPair {
		h.Peer = hex.EncodeToString(d.Peer[:])
		peer = d.Peer[:]
	}
	h.Sig = ed25519.Sign(d.Key, helloSigned(ch.Nonce, d.Kind, d.Room[:], peer))
	b, _ := json.Marshal(h)
	if err := ws.WriteMessage(opText, b); err != nil {
		return nil, ch, err
	}
	return ws, ch, nil
}

func (d *Dialer) hello(c net.Conn, host, path string) (*Conn, error) {
	ws, ch, err := d.greet(c, host, path)
	if err != nil {
		return nil, err
	}
	for {
		op, msg, err := ws.ReadMessage()
		if err != nil {
			return nil, err
		}
		var st status
		if op != opText || json.Unmarshal(msg, &st) != nil {
			return nil, errors.New("relay: unexpected message before mated")
		}
		switch st.T {
		case msgWait:
			if d.OnWait != nil {
				d.OnWait()
			}
		case msgMated:
			max := ch.Max
			if max <= 0 || max > maxMessage {
				max = maxMessage
			}
			ws.idle = 0 // from here the link keeps its own time
			return &Conn{ws: ws, max: max, version: ch.TM}, nil
		}
	}
}

// Conn is the byte stream between two mated clients: a net.Conn whose
// writes go out as binary messages and whose reads are the peer's.
type Conn struct {
	ws      *wsConn
	max     int
	version string

	rmu  sync.Mutex
	rbuf []byte
}

// Read returns the peer's bytes. When the relay closes the room the
// error is a *CloseError (peer-gone, shutdown, …).
func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.rbuf) == 0 {
		op, msg, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		if op != opBinary {
			c.ws.CloseWith(CloseProtocol, ErrProtocol)
			return 0, errProtocol
		}
		c.rbuf = msg
	}
	n := copy(p, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

// Write sends p as one or more binary messages.
func (c *Conn) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(len(p), c.max)
		if err := c.ws.WriteMessage(opBinary, p[:k]); err != nil {
			return n, err
		}
		n += k
		p = p[k:]
	}
	return n, nil
}

// RelayVersion is the relay's tm release, as its challenge said; "" for
// a relay older than that.
func (c *Conn) RelayVersion() string { return c.version }

// Close leaves the room; the relay tells the peer.
func (c *Conn) Close() error {
	c.ws.CloseWith(CloseNormal, "")
	return nil
}

func (c *Conn) LocalAddr() net.Addr                { return c.ws.c.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.ws.c.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.ws.c.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.ws.c.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.ws.c.SetWriteDeadline(t) }
