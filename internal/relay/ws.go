package relay

// The WebSocket subset the relay speaks (RFC 6455): one subprotocol, no
// extensions, messages up to a fixed size. Both ends live here, so tm
// needs no WebSocket module (docs/SPEC.md §16).

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Subprotocol is the Sec-WebSocket-Protocol a relay client asks for. A
// later, incompatible wire gets a new one.
const Subprotocol = "tm-relay.1"

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Opcodes.
const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

// Close codes. The reason is one of the Err* words.
const (
	CloseNormal     = 1000
	CloseGoingAway  = 1001
	CloseProtocol   = 1002
	CloseTooBig     = 1009
	CloseBadHello   = 4400
	CloseForbidden  = 4403
	CloseTimeout    = 4408
	CloseConflict   = 4409
	ClosePeerGone   = 4410
	CloseRateLimit  = 4429
	closeNoStatus   = 1005
	maxControlBytes = 125
)

// CloseError is a close frame the other end sent.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("closed: %s (%d)", e.Reason, e.Code)
	}
	return fmt.Sprintf("closed (%d)", e.Code)
}

var errProtocol = errors.New("websocket protocol error")

// wsConn is one WebSocket connection. Reads are for one goroutine;
// writes may come from several (the copy loop and the pinger).
type wsConn struct {
	c      net.Conn
	br     *bufio.Reader
	client bool          // mask what we send
	max    int           // largest message accepted
	idle   time.Duration // read deadline per frame; 0 none

	wmu    sync.Mutex
	closed bool // a close frame was sent
	wbuf   []byte
}

func newWS(c net.Conn, br *bufio.Reader, client bool, max int) *wsConn {
	if br == nil {
		br = bufio.NewReaderSize(c, 4096)
	}
	return &wsConn{c: c, br: br, client: client, max: max}
}

// acceptKey is Sec-WebSocket-Accept for a Sec-WebSocket-Key.
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// upgrade answers a WebSocket handshake for Subprotocol and hijacks the
// connection. On a bad request it writes the HTTP error itself.
func upgrade(w http.ResponseWriter, r *http.Request, max int) (*wsConn, error) {
	fail := func(code int, msg string) (*wsConn, error) {
		http.Error(w, msg, code)
		return nil, errors.New(msg)
	}
	if r.Method != http.MethodGet {
		return fail(http.StatusMethodNotAllowed, "GET only")
	}
	if !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") {
		return fail(http.StatusUpgradeRequired, "a WebSocket upgrade is required")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return fail(http.StatusUpgradeRequired, "WebSocket version 13 only")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if k, err := base64.StdEncoding.DecodeString(key); err != nil || len(k) != 16 {
		return fail(http.StatusBadRequest, "bad Sec-WebSocket-Key")
	}
	if !headerHas(r.Header, "Sec-WebSocket-Protocol", Subprotocol) {
		return fail(http.StatusBadRequest, "subprotocol "+Subprotocol+" required")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return fail(http.StatusInternalServerError, "can't hijack the connection")
	}
	c, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey(key) + "\r\n" +
		"Sec-WebSocket-Protocol: " + Subprotocol + "\r\n\r\n"
	// The HTTP server's deadlines stay on a hijacked connection: from
	// here the WebSocket sets its own.
	c.SetReadDeadline(time.Time{})
	c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte(resp)); err != nil {
		c.Close()
		return nil, err
	}
	c.SetWriteDeadline(time.Time{})
	return newWS(c, brw.Reader, false, max), nil
}

// clientHandshake sends the upgrade request for url's host and path over
// c and checks the answer.
func clientHandshake(c net.Conn, host, path string, max int) (*wsConn, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	req := "GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Protocol: " + Subprotocol + "\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(c, 4096)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, &HTTPError{Status: resp.StatusCode}
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) ||
		resp.Header.Get("Sec-WebSocket-Protocol") != Subprotocol {
		return nil, errors.New("relay: bad WebSocket handshake answer")
	}
	return newWS(c, br, true, max), nil
}

// HTTPError is a relay that refused the upgrade, e.g. 429 for too many
// connections from one address.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("relay refused: HTTP %d %s", e.Status, http.StatusText(e.Status))
}

// readFrame reads one frame header and payload.
func (w *wsConn) readFrame() (fin bool, op byte, payload []byte, err error) {
	if w.idle > 0 {
		w.c.SetReadDeadline(time.Now().Add(w.idle))
	}
	var h [2]byte
	if _, err = io.ReadFull(w.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, errProtocol // no extensions
	}
	op = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	if masked == w.client {
		return false, 0, nil, errProtocol // clients mask, servers don't
	}
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(w.br, b[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(w.br, b[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if op >= opClose && (n > maxControlBytes || !fin) {
		return false, 0, nil, errProtocol
	}
	if n > uint64(w.max) {
		return false, 0, nil, errTooBig
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(w.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(w.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return
}

var errTooBig = errors.New("websocket message too big")

// ReadMessage returns the next text or binary message, reassembled. Pings
// are answered here; a close frame is answered and returned as a
// *CloseError.
func (w *wsConn) ReadMessage() (op byte, msg []byte, err error) {
	defer func() {
		switch {
		case errors.Is(err, errProtocol):
			w.CloseWith(CloseProtocol, "protocol")
		case errors.Is(err, errTooBig):
			w.CloseWith(CloseTooBig, "too-big")
		}
	}()
	for {
		fin, fop, payload, err := w.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch fop {
		case opPing:
			w.write(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			ce := &CloseError{Code: closeNoStatus}
			if len(payload) >= 2 {
				ce.Code = int(binary.BigEndian.Uint16(payload))
				ce.Reason = string(payload[2:])
			}
			w.CloseWith(CloseNormal, "")
			return 0, nil, ce
		case opText, opBinary:
			if op != 0 {
				return 0, nil, errProtocol // a new message inside a fragmented one
			}
			op, msg = fop, payload
		case opCont:
			if op == 0 {
				return 0, nil, errProtocol
			}
			if len(msg)+len(payload) > w.max {
				return 0, nil, errTooBig
			}
			msg = append(msg, payload...)
		default:
			return 0, nil, errProtocol
		}
		if fin {
			return op, msg, nil
		}
	}
}

// wbufKeep is the largest write buffer a connection keeps between
// writes; a bigger frame goes out without one (server) or in a buffer
// of its own (client, which masks a copy).
const wbufKeep = 4 << 10

// write sends one unfragmented frame.
func (w *wsConn) write(op byte, payload []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if w.closed {
		return net.ErrClosed
	}
	if op == opClose {
		w.closed = true
	}
	var hdr [14]byte
	h := append(hdr[:0], 0x80|op)
	var maskBit byte
	if w.client {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		h = append(h, maskBit|byte(n))
	case n <= 0xFFFF:
		h = append(h, maskBit|126, byte(n>>8), byte(n))
	default:
		h = append(h, maskBit|127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	var mask [4]byte
	if w.client {
		if _, err := rand.Read(mask[:]); err != nil {
			return err
		}
		h = append(h, mask[:]...)
	}
	w.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	size := len(h) + len(payload)
	if !w.client && size > wbufKeep {
		bufs := net.Buffers{h, payload}
		_, err := bufs.WriteTo(w.c)
		return err
	}
	var b []byte
	if size <= wbufKeep {
		if w.wbuf == nil {
			w.wbuf = make([]byte, 0, wbufKeep)
		}
		b = w.wbuf[:0]
	} else {
		b = make([]byte, 0, size)
	}
	b = append(b, h...)
	b = append(b, payload...)
	if w.client {
		for i := len(h); i < len(b); i++ {
			b[i] ^= mask[(i-len(h))&3]
		}
	}
	_, err := w.c.Write(b)
	return err
}

// WriteMessage sends a text or binary message.
func (w *wsConn) WriteMessage(op byte, msg []byte) error {
	if len(msg) > w.max {
		return errTooBig
	}
	return w.write(op, msg)
}

// Ping sends a ping.
func (w *wsConn) Ping() error { return w.write(opPing, nil) }

// CloseWith sends a close frame (once) and shuts the connection a little
// later, so the frame has a chance to arrive.
func (w *wsConn) CloseWith(code int, reason string) {
	p := binary.BigEndian.AppendUint16(nil, uint16(code))
	if len(reason) > maxControlBytes-2 {
		reason = reason[:maxControlBytes-2]
	}
	if w.write(opClose, append(p, reason...)) == nil {
		time.AfterFunc(closeGrace, func() { w.c.Close() })
		return
	}
	w.c.Close()
}

// Close shuts the connection at once.
func (w *wsConn) Close() error { return w.c.Close() }
