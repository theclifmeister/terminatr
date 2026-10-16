package modproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrClosed says the host's stdout ended: the host exited or is exiting.
// The supervisor reads its exit code; the event bus calls this mod gone.
var ErrClosed = errors.New("mod host closed")

// Client is the server's side of one host: it sends hello, events and
// shutdown, and hands the host's requests and notifications to callbacks.
type Client struct {
	conn *Conn
	// OnRequest answers a host request (a $ API call) with Reply or
	// ReplyError on Conn. Each runs in its own goroutine, or on the read
	// loop when inline.
	onRequest func(*Message)
	inline    bool
	// OnNotify gets the host's notifications, in order, on the read loop:
	// it must not block.
	onNotify func(*Message)

	mu     sync.Mutex
	next   int64
	waits  map[int64]chan *Message
	closed bool
	done   chan struct{}
}

// NewClient reads the host's messages from r until it ends and writes to
// w. Either callback may be nil.
func NewClient(r io.Reader, w io.Writer, onRequest, onNotify func(*Message)) *Client {
	return newClient(r, w, onRequest, onNotify, false)
}

// NewInlineClient is NewClient answering the host's requests on the read
// loop: one at a time, in the order the host made them (tm mod test
// records them so).
func NewInlineClient(r io.Reader, w io.Writer, onRequest, onNotify func(*Message)) *Client {
	return newClient(r, w, onRequest, onNotify, true)
}

func newClient(r io.Reader, w io.Writer, onRequest, onNotify func(*Message), inline bool) *Client {
	c := &Client{conn: NewConn(r, w), onRequest: onRequest, onNotify: onNotify, inline: inline,
		waits: map[int64]chan *Message{}, done: make(chan struct{})}
	go c.read()
	return c
}

// Conn is the connection, for replies to the host's requests.
func (c *Client) Conn() *Conn { return c.conn }

// Done is closed once the host's stdout has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) read() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.waits = nil
		c.mu.Unlock()
		close(c.done)
	}()
	for {
		m, err := c.conn.Read()
		var fe *FrameError
		if errors.As(err, &fe) {
			continue
		}
		if err != nil {
			return
		}
		switch {
		case m.IsResponse():
			c.mu.Lock()
			ch := c.waits[*m.ID]
			delete(c.waits, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m // buffered; a reply to a call that gave up is dropped
			}
		case m.IsRequest():
			if c.onRequest == nil {
				_ = c.conn.ReplyError(*m.ID, CodeMethodNotFound, "no broker")
				continue
			}
			if c.inline {
				c.onRequest(m)
			} else {
				go c.onRequest(m)
			}
		case m.IsNotification():
			if c.onNotify != nil {
				c.onNotify(m)
			}
		}
	}
}

// Call sends a request and waits for its answer, ctx's end, or the host's.
// After ctx ends the answer, when it comes, is dropped.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ch := make(chan *Message, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.next++
	id := c.next
	c.waits[id] = ch
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.waits, id)
		c.mu.Unlock()
	}
	if err := c.conn.Request(id, method, params); err != nil {
		forget()
		return nil, fmt.Errorf("%w: %v", ErrClosed, err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	case <-c.done:
		// A host that answers, then exits (shutdown) closes done just
		// after the answer is in ch: select may pick either, so the
		// answer counts when it is there.
		select {
		case m := <-ch:
			if m.Error != nil {
				return nil, m.Error
			}
			return m.Result, nil
		default:
		}
		return nil, ErrClosed
	}
}

// Hello sends hello; the host runs the script and answers.
func (c *Client) Hello(ctx context.Context, h Hello) (HelloResult, error) {
	var r HelloResult
	if h.Protocol == 0 {
		h.Protocol = Protocol
	}
	raw, err := c.Call(ctx, MethodHello, h)
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	return r, err
}

// Event gives the mod one event and returns when its handlers are done,
// when ctx ends (the event bus's 5 s limit), or when the host is gone
// (ErrClosed). A cancelled event's handlers may still be running in the
// host; their answer is dropped, and the next event can be sent at once.
func (c *Client) Event(ctx context.Context, ev Event) (EventResult, error) {
	var r EventResult
	raw, err := c.Call(ctx, MethodEvent, ev)
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	return r, err
}

// Shutdown asks the host to exit 0.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.Call(ctx, MethodShutdown, nil)
	return err
}

// ToolCall runs one of the mod's agent tools and returns the text its
// handler gave, or its error (an *Error with CodeNoTool or
// CodeToolFailed), ctx's end or ErrClosed.
func (c *Client) ToolCall(ctx context.Context, call ToolCall) (ToolResult, error) {
	var r ToolResult
	raw, err := c.Call(ctx, MethodToolCall, call)
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	return r, err
}

// CodeHost runs one of the code host methods (IsCodeHost) in the mod and
// returns its handler's answer as JSON, or its error (an *Error with
// CodeNoHandler or CodeHandlerFailed), ctx's end or ErrClosed.
func (c *Client) CodeHost(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !IsCodeHost(method) {
		return nil, fmt.Errorf("%s is not a code host method", method)
	}
	return c.Call(ctx, method, params)
}
