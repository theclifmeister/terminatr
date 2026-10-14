package peer

import (
	"bufio"
	"context"
	"encoding/binary"
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

// Caps are this tm's link capabilities beside the link core: peer views
// (CapWatch, when the server exports), messages (CapMsg) and panes
// (CapPane) each add one when the server offers them.
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
	// maxFrame bounds one binary frame's payload read; frameChunk is the
	// most this tm puts in one, so a frame holds the writer only briefly.
	maxFrame   = 64 << 10
	frameChunk = 16 << 10
	// maxInFlight bounds the peer's requests being answered at once
	// (each peer.watch waits); more are refused.
	maxInFlight = 32
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

// Binary frames (docs/SPEC.md §16.12) go between two lines once both
// hellos list CapPane: a 0 byte (a line starts with "{"), the stream u32,
// the type u8 and the length u32, big-endian, then the payload.
const frameHeader = 10

// session is a link once TLS is up: hellos, then NDJSON both ways, and
// binary frames for the panes.
type session struct {
	c    net.Conn
	br   *bufio.Reader
	ping time.Duration
	idle time.Duration

	wmu sync.Mutex

	// methods answer the peer's requests beside link.ping: the peer
	// dispatcher's, nil for none. Each runs in its own goroutine, at
	// most maxInFlight at once, until ctx ends with the session.
	methods  map[string]handler
	inFlight chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc

	// panes are the session's pane streams, both ways.
	panes *paneMux

	mu      sync.Mutex
	next    int64
	pending map[int64]chan line
	err     error // set when the session ended
}

// sessionKey finds the session in its ctx: a handler's ctx is the
// session's.
type sessionKey struct{}

func newSession(c net.Conn) *session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{c: c, br: bufio.NewReaderSize(c, 64<<10), ping: pingEvery, idle: idleAfter, pending: map[int64]chan line{},
		inFlight: make(chan struct{}, maxInFlight), cancel: cancel}
	s.ctx = context.WithValue(ctx, sessionKey{}, s)
	s.panes = newPaneMux(s)
	return s
}

// sessionOf is the session whose ctx ctx is; nil for none.
func sessionOf(ctx context.Context) *session {
	s, _ := ctx.Value(sessionKey{}).(*session)
	return s
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

// sendFrame writes one binary frame, payload at most maxFrame.
func (s *session) sendFrame(stream uint32, typ byte, payload []byte) error {
	b := make([]byte, frameHeader, frameHeader+len(payload))
	binary.BigEndian.PutUint32(b[1:], stream)
	b[5] = typ
	binary.BigEndian.PutUint32(b[6:], uint32(len(payload)))
	b = append(b, payload...)
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.c.SetWriteDeadline(time.Now().Add(s.idle))
	_, err := s.c.Write(b)
	return err
}

func (s *session) read(v any) error {
	b, err := s.readLine()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// readLine reads one line, without its newline, of at most maxLine.
func (s *session) readLine() ([]byte, error) {
	var line []byte
	for {
		b, err := s.br.ReadSlice('\n')
		if len(line)+len(b) > maxLine+1 {
			return nil, bufio.ErrTooLong
		}
		switch {
		case err == nil:
			if line == nil {
				return b[:len(b)-1], nil
			}
			line = append(line, b...)
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			line = append(line, b...)
		case errors.Is(err, io.EOF) && len(line)+len(b) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

// readFrame reads a binary frame after its 0 byte.
func (s *session) readFrame() (stream uint32, typ byte, payload []byte, err error) {
	var h [frameHeader]byte
	if _, err := io.ReadFull(s.br, h[:]); err != nil {
		return 0, 0, nil, err
	}
	n := binary.BigEndian.Uint32(h[6:])
	if n > maxFrame {
		return 0, 0, nil, fmt.Errorf("link: a frame of %d bytes, over %d", n, maxFrame)
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(s.br, payload); err != nil {
		return 0, 0, nil, err
	}
	return binary.BigEndian.Uint32(h[1:]), h[5], payload, nil
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
	s.cancel()
	s.panes.end()
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
		if b, err := s.br.Peek(1); err == nil && b[0] == 0 {
			stream, typ, payload, err := s.readFrame()
			if err != nil {
				return err
			}
			s.panes.frame(stream, typ, payload)
			continue
		}
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
		h, ok := s.methods[l.Method]
		switch {
		case l.Method == MethodPing:
			r.Result = json.RawMessage("{}")
		case ok:
			select {
			case s.inFlight <- struct{}{}:
				go s.answer(l, h)
				continue
			default:
				r.Error = proto.Errorf(proto.ErrRefused, "too many requests at once")
			}
		default:
			// A newer peer's method, or one this server doesn't
			// export: a peer checks capabilities first, so this is only
			// a peer that didn't.
			r.Error = proto.Errorf(proto.ErrUnknownMethod, "unknown link method %q", l.Method)
		}
		if err := s.send(r); err != nil {
			return err
		}
	}
}

// answer runs h for the peer's request l and sends its response.
func (s *session) answer(l line, h handler) {
	defer func() { <-s.inFlight }()
	r := line{ID: l.ID}
	v, err := h(s.ctx, l.Params)
	if s.ctx.Err() != nil {
		return
	}
	if err == nil {
		r.Result, err = json.Marshal(v)
	}
	if err != nil {
		r.Result, r.Error = nil, asProtoError(err)
	}
	if s.send(r) != nil {
		s.c.Close()
	}
}
