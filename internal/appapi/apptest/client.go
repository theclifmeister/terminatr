// Package apptest is a Go client of the Mac app's bridge (tm app bridge,
// docs/SPEC.md §3.8): the fake app the bridge's tests drive, speaking
// the app API's NDJSON as the app does.
package apptest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/theclifmeister/terminatr/internal/appapi"
)

// Client is one app's side of a bridge.
type Client struct {
	w  io.Writer
	wm sync.Mutex
	// Hello is the bridge's hello.
	Hello appapi.Hello
	// Hellos are the hellos after the first: a device's, whose level
	// changed (the last 8 kept).
	Hellos chan appapi.Hello
	// Sign, when set, signs every write as a device does (app API 23):
	// a nonce from device.nonce first, then Sign's signature of the
	// signed message in the request's auth.
	Sign func(msg []byte) ([]byte, error)

	mu   sync.Mutex
	next int64
	// waiting are the open requests' lines, by id.
	waiting map[int64]chan appapi.Out
	// err is why reading the bridge stopped.
	err  error
	done chan struct{}
}

// New reads the bridge's hello from r and starts reading its lines;
// requests go to w. A hello with an error is returned with it.
func New(r io.Reader, w io.Writer) (*Client, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("the bridge's hello: %w", err)
	}
	var first appapi.Out
	if err := json.Unmarshal(line, &first); err != nil || first.Hello == nil {
		return nil, fmt.Errorf("the bridge's first line isn't a hello: %q", line)
	}
	c := &Client{w: w, Hello: *first.Hello, Hellos: make(chan appapi.Hello, 8), waiting: map[int64]chan appapi.Out{}, done: make(chan struct{})}
	if c.Hello.Error != nil {
		return c, c.Hello.Error
	}
	go c.read(br)
	return c, nil
}

// read routes the bridge's lines to their requests until it ends.
func (c *Client) read(br *bufio.Reader) {
	defer close(c.done)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			c.mu.Lock()
			c.err = err
			for _, ch := range c.waiting {
				close(ch)
			}
			c.waiting = map[int64]chan appapi.Out{}
			c.mu.Unlock()
			return
		}
		var o appapi.Out
		if err := json.Unmarshal(line, &o); err != nil {
			continue
		}
		if o.ID == 0 && o.Hello != nil {
			select {
			case c.Hellos <- *o.Hello:
			default:
			}
			continue
		}
		c.mu.Lock()
		ch := c.waiting[o.ID]
		c.mu.Unlock()
		if ch != nil {
			ch <- o
		}
	}
}

// Send writes request method with params under a new id, whose lines
// come on the channel returned.
func (c *Client) Send(method string, params any) (int64, chan appapi.Out, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return 0, nil, err
		}
		raw = b
	}
	var auth *appapi.Auth
	if m, ok := appapi.Lookup(method); ok && m.Write && c.Sign != nil {
		var n appapi.DeviceNonce
		if err := c.Call(appapi.MethodDeviceNonce, nil, &n); err != nil {
			return 0, nil, err
		}
		sig, err := c.Sign(appapi.SignedMessage(method, n.Nonce, raw))
		if err != nil {
			return 0, nil, err
		}
		auth = &appapi.Auth{Nonce: n.Nonce, Sig: sig}
	}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return 0, nil, c.err
	}
	c.next++
	id := c.next
	ch := make(chan appapi.Out, 64)
	c.waiting[id] = ch
	c.mu.Unlock()
	return id, ch, c.WriteLine(appapi.Request{ID: id, Method: method, Params: raw, Auth: auth})
}

// Raw writes req as it is, under a new id, and decodes its answer: a
// request the client wouldn't make (a test's forged auth).
func (c *Client) Raw(req appapi.Request) error {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.next++
	req.ID = c.next
	ch := make(chan appapi.Out, 64)
	c.waiting[req.ID] = ch
	c.mu.Unlock()
	defer c.forget(req.ID)
	if err := c.WriteLine(req); err != nil {
		return err
	}
	o, ok := <-ch
	if !ok {
		return c.readErr()
	}
	return decode(o, nil)
}

// Signer is Client.Sign for the write key k.
func Signer(k *ecdsa.PrivateKey) func([]byte) ([]byte, error) {
	return func(msg []byte) ([]byte, error) {
		h := sha256.Sum256(msg)
		return ecdsa.SignASN1(rand.Reader, k, h[:])
	}
}

// WriteLine writes v as one line to the bridge.
func (c *Client) WriteLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wm.Lock()
	defer c.wm.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.waiting, id)
	c.mu.Unlock()
}

// Call calls method and decodes its result into result (nil to drop
// it). A failure the bridge answers is an *appapi.Error.
func (c *Client) Call(method string, params, result any) error {
	id, ch, err := c.Send(method, params)
	if err != nil {
		return err
	}
	defer c.forget(id)
	o, ok := <-ch
	if !ok {
		return c.readErr()
	}
	return decode(o, result)
}

func (c *Client) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		return io.ErrUnexpectedEOF
	}
	return c.err
}

func decode(o appapi.Out, result any) error {
	if o.Error != nil {
		return o.Error
	}
	if result != nil {
		return json.Unmarshal(o.Result, result)
	}
	return nil
}

// Stream is an open stream.
type Stream struct {
	c  *Client
	id int64
	ch chan appapi.Out
}

// Stream opens stream method and decodes its result into result.
func (c *Client) Stream(method string, params, result any) (*Stream, error) {
	id, ch, err := c.Send(method, params)
	if err != nil {
		return nil, err
	}
	o, ok := <-ch
	if !ok {
		c.forget(id)
		return nil, c.readErr()
	}
	if err := decode(o, result); err != nil {
		c.forget(id)
		return nil, err
	}
	return &Stream{c: c, id: id, ch: ch}, nil
}

// ID is the stream's request id, which cancel and pane.input name.
func (s *Stream) ID() int64 { return s.id }

// ErrEnd is Next's error once the stream ended without a fault.
var ErrEnd = errors.New("the stream ended")

// Next decodes the stream's next event's data into data, and names the
// event. At the end it returns ErrEnd, or the end's error.
func (s *Stream) Next(data any) (string, error) {
	o, ok := <-s.ch
	if !ok {
		return "", s.c.readErr()
	}
	if o.End {
		s.c.forget(s.id)
		if o.Error != nil {
			return "", o.Error
		}
		return "", ErrEnd
	}
	if o.Error != nil {
		return "", o.Error
	}
	return o.Event, json.Unmarshal(o.Data, data)
}

// Cancel asks the bridge to end the stream; Next then returns ErrEnd.
func (s *Stream) Cancel() error {
	return s.c.Call(appapi.MethodCancel, appapi.CancelParams{ID: s.id}, nil)
}

// Bridge is a tm app bridge running as a child, as the app runs it.
type Bridge struct {
	*Client
	Cmd   *exec.Cmd
	stdin io.WriteCloser
}

// Run starts tm (bin) app bridge with env and reads its hello.
func Run(bin string, env []string) (*Bridge, error) {
	cmd := exec.Command(bin, "app", "bridge")
	cmd.Env = env
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c, err := New(out, in)
	if err != nil {
		in.Close()
		cmd.Wait()
		return nil, err
	}
	return &Bridge{Client: c, Cmd: cmd, stdin: in}, nil
}

// Close closes the bridge's stdin, which ends it, and waits for it.
func (b *Bridge) Close() error {
	b.stdin.Close()
	<-b.done
	return b.Cmd.Wait()
}
