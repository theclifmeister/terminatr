package peer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// LinkProtocol is the version of what two servers say over a link,
// apart from tm's control protocol: the two may run different releases.
// Methods and fields are only ever added, and a capability names each
// feature, so a newer server talks to an older one and leaves out what
// the older one lacks.
const LinkProtocol = 1

// MinLinkProtocol is the oldest link protocol this tm talks to.
const MinLinkProtocol = 1

// Caps are this tm's link capabilities. The link core has none: peer
// views, panes and messages each add one when they're built.
var Caps = []string{}

// Hello is the first line each end sends inside the TLS.
type Hello struct {
	LinkProtocol int      `json:"link_protocol"`
	Version      string   `json:"version"` // the tm release
	Caps         []string `json:"caps"`
}

// Has says whether the peer's hello lists the capability c.
func (h Hello) Has(c string) bool { return slices.Contains(h.Caps, c) }

// Link methods, NDJSON requests either end may send once the hellos are
// through.
const (
	// MethodPing answers nothing: it keeps the link's time and measures
	// its round trip.
	MethodPing = "link.ping"
)

// Link timing.
const (
	helloTimeout = 10 * time.Second
	// pingEvery is how often a link pings; a link that hears nothing for
	// idleAfter is dead, whatever the transport says.
	pingEvery = 30 * time.Second
	idleAfter = 90 * time.Second
	// maxLine bounds one NDJSON line.
	maxLine = 1 << 20
)

// versionError: the peer's link protocol is older than this tm talks.
type versionError struct{ peer Hello }

func (e *versionError) Error() string {
	return fmt.Sprintf("the peer runs tm %s, link protocol %d; this tm needs %d or later", e.peer.Version, e.peer.LinkProtocol, MinLinkProtocol)
}

// line is one NDJSON message: a request when Method is set, else the
// response to the request with its ID.
type line struct {
	ID     int64           `json:"id"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *proto.Error    `json:"error,omitempty"`
}

// session is a link once TLS is up: hellos, then NDJSON both ways.
type session struct {
	c    net.Conn
	sc   *bufio.Scanner
	ping time.Duration
	idle time.Duration

	wmu sync.Mutex

	mu      sync.Mutex
	next    int64
	pending map[int64]chan line
	err     error // set when the session ended
}

func newSession(c net.Conn) *session {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	return &session{c: c, sc: sc, ping: pingEvery, idle: idleAfter, pending: map[int64]chan line{}}
}

func (s *session) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.c.SetWriteDeadline(time.Now().Add(s.idle))
	_, err = s.c.Write(append(b, '\n'))
	return err
}

func (s *session) read(v any) error {
	if !s.sc.Scan() {
		if err := s.sc.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	return json.Unmarshal(s.sc.Bytes(), v)
}

// hello sends own and reads the peer's, within helloTimeout.
func (s *session) hello(own Hello) (Hello, error) {
	var peer Hello
	if err := s.send(own); err != nil {
		return peer, err
	}
	s.c.SetReadDeadline(time.Now().Add(helloTimeout))
	if err := s.read(&peer); err != nil {
		return peer, fmt.Errorf("link hello: %w", err)
	}
	if peer.LinkProtocol < MinLinkProtocol {
		return peer, &versionError{peer}
	}
	return peer, nil
}

// call sends a request and waits for its response.
func (s *session) call(ctx context.Context, method string, params, result any) error {
	l := line{Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		l.Params = b
	}
	ch := make(chan line, 1)
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return s.err
	}
	s.next++
	l.ID = s.next
	s.pending[l.ID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, l.ID)
		s.mu.Unlock()
	}()
	if err := s.send(l); err != nil {
		return err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return s.err
		}
		if r.Error != nil {
			return r.Error
		}
		if result != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serve reads the peer's lines until the link ends, answers its
// requests, and pings every s.ping, reporting each round trip to rtt.
// Any line keeps the link alive; s.idle without one ends it.
func (s *session) serve(rtt func(time.Duration)) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(s.ping)
		defer t.Stop()
		for {
			t0 := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), s.idle)
			err := s.call(ctx, MethodPing, nil, nil)
			cancel()
			if err != nil {
				s.c.Close()
				return
			}
			rtt(time.Since(t0))
			select {
			case <-t.C:
			case <-done:
				return
			}
		}
	}()
	err := s.loop()
	s.mu.Lock()
	s.err = err
	for id, ch := range s.pending {
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()
	return err
}

func (s *session) loop() error {
	for {
		s.c.SetReadDeadline(time.Now().Add(s.idle))
		var l line
		if err := s.read(&l); err != nil {
			var se *json.SyntaxError
			if errors.As(err, &se) {
				return fmt.Errorf("link: the peer sent a line that isn't JSON: %w", err)
			}
			return err
		}
		if l.Method == "" {
			s.mu.Lock()
			ch := s.pending[l.ID]
			s.mu.Unlock()
			if ch != nil {
				ch <- l
			}
			continue
		}
		r := line{ID: l.ID}
		switch l.Method {
		case MethodPing:
			r.Result = json.RawMessage("{}")
		default:
			// A newer peer's method: it checks capabilities first, so
			// this is only a peer that didn't.
			r.Error = proto.Errorf(proto.ErrUnknownMethod, "unknown link method %q", l.Method)
		}
		if err := s.send(r); err != nil {
			return err
		}
	}
}
