package appapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Bridge limits.
const (
	// MaxLine bounds a request line, in bytes.
	MaxLine = 1 << 20
	// MaxStreams bounds the streams open at once: each holds a
	// connection to the server.
	MaxStreams = 32
	// idleConns is how many control connections the bridge keeps for
	// calls between them.
	idleConns = 4
)

// Options are a bridge's.
type Options struct {
	// Paths are the server's.
	Paths client.Paths
	// Autostart starts the server when none answers, as the CLI does:
	// on macOS through the launchd job (client.StartDetached), never as
	// the bridge's child.
	Autostart bool
}

// Serve is tm app bridge: it reaches the server, writes its Hello to out,
// then answers the requests read from in until in ends. It returns the
// error that kept it from reaching the server, after a Hello saying so,
// or what broke in or out.
func Serve(in io.Reader, out io.Writer, o Options) error {
	b := &bridge{o: o, w: json.NewEncoder(out), open: map[int64]bool{}, streams: map[int64]context.CancelFunc{},
		idle: make(chan *client.Client, idleConns)}
	c, err := client.ConnectAs(o.Paths, o.Autostart, proto.ClientApp)
	hello := &Hello{AppAPI: Version, TM: version.Version, Build: version.BuildID(), Capabilities: Capabilities, Methods: MethodNames()}
	if err != nil {
		hello.Error = connectError(err)
		b.write(Out{Hello: hello})
		return err
	}
	hello.Server = &Server{Version: c.Server.Version, Protocol: c.Server.Protocol, PID: c.Server.PID, Started: c.Started}
	b.idle <- c
	if err := b.write(Out{Hello: hello}); err != nil {
		c.Close()
		return err
	}
	err = b.serve(in)
	b.close()
	return err
}

// connectError is err, which kept the bridge from the server, as the
// hello says it.
func connectError(err error) *Error {
	var me *proto.MismatchError
	if errors.As(err, &me) {
		return &Error{Code: ErrServerMismatch, Message: err.Error()}
	}
	return &Error{Code: ErrServer, Message: err.Error()}
}

type bridge struct {
	o  Options
	wm sync.Mutex
	w  *json.Encoder
	// werr is the first write that failed: the app is gone.
	werr error

	mu sync.Mutex
	// open are the requests not yet answered, streams included;
	// streams cancel each open stream.
	open    map[int64]bool
	streams map[int64]context.CancelFunc
	wg      sync.WaitGroup
	// idle are control connections free for a call.
	idle chan *client.Client
}

// write writes one line; lines never interleave.
func (b *bridge) write(o Out) error {
	b.wm.Lock()
	defer b.wm.Unlock()
	if b.werr == nil {
		b.werr = b.w.Encode(o)
	}
	return b.werr
}

// fail answers request id with an error.
func (b *bridge) fail(id int64, code, format string, args ...any) {
	b.write(Out{ID: id, Error: &Error{Code: code, Message: fmt.Sprintf(format, args...)}})
}

// serve reads the requests until in ends.
func (b *bridge) serve(in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil || req.ID < 1 || req.Method == "" {
			b.fail(req.ID, ErrBadRequest, "a request is one JSON object with an id from 1 and a method")
			continue
		}
		b.handle(req)
		if b.writeErr() != nil {
			return b.writeErr()
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read the app's requests: %w", err)
	}
	return nil
}

func (b *bridge) writeErr() error {
	b.wm.Lock()
	defer b.wm.Unlock()
	return b.werr
}

// handle starts answering req.
func (b *bridge) handle(req Request) {
	if req.Method == MethodCancel {
		var p CancelParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			b.fail(req.ID, ErrBadParams, "cancel: %v", err)
			return
		}
		b.mu.Lock()
		cancel := b.streams[p.ID]
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		b.write(Out{ID: req.ID, Result: json.RawMessage("{}")})
		return
	}
	m, ok := Lookup(req.Method)
	if !ok {
		b.fail(req.ID, ErrUnknownMethod, "%s is not in the app API", req.Method)
		return
	}
	params, err := m.toServer(req.Params)
	if err != nil {
		b.fail(req.ID, ErrBadParams, "%s: %v", req.Method, err)
		return
	}
	b.mu.Lock()
	switch {
	case b.open[req.ID]:
		b.mu.Unlock()
		b.fail(req.ID, ErrBadRequest, "request %d is still open", req.ID)
		return
	case m.Stream && len(b.streams) >= MaxStreams:
		b.mu.Unlock()
		b.fail(req.ID, ErrTooMany, "%d streams are open; cancel one first", MaxStreams)
		return
	}
	b.open[req.ID] = true
	var ctx context.Context
	if m.Stream {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		b.streams[req.ID] = cancel
	}
	b.mu.Unlock()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if m.Stream {
			b.stream(ctx, req.ID, m, params)
		} else {
			b.call(req.ID, m, params)
		}
		b.mu.Lock()
		delete(b.open, req.ID)
		if cancel := b.streams[req.ID]; cancel != nil {
			cancel()
			delete(b.streams, req.ID)
		}
		b.mu.Unlock()
	}()
}

// conn is a control connection for a call: an idle one, or a new one
// (the server started again if it stopped, as Options.Autostart says).
func (b *bridge) conn() (*client.Client, error) {
	select {
	case c := <-b.idle:
		return c, nil
	default:
		return client.ConnectAs(b.o.Paths, b.o.Autostart, proto.ClientApp)
	}
}

// release keeps c for the next call, unless it broke or enough wait.
func (b *bridge) release(c *client.Client, broken bool) {
	if !broken {
		select {
		case b.idle <- c:
			return
		default:
		}
	}
	c.Close()
}

// call answers request id of unary method m. A call is never retried:
// a write may have reached the server before its connection broke.
func (b *bridge) call(id int64, m Method, params any) {
	c, err := b.conn()
	if err != nil {
		b.fail(id, ErrServer, "%v", err)
		return
	}
	var raw json.RawMessage
	err = c.Call(m.Name, params, &raw)
	var perr *proto.Error
	b.release(c, err != nil && !errors.As(err, &perr))
	if err != nil {
		b.serverError(id, err)
		return
	}
	res, err := m.result(raw)
	if err != nil {
		b.fail(id, ErrServer, "%s: the server's answer: %v", m.Name, err)
		return
	}
	b.answer(id, res)
}

// serverError answers request id with the server's error, its code
// kept, or ErrServer for a broken connection.
func (b *bridge) serverError(id int64, err error) {
	var perr *proto.Error
	if errors.As(err, &perr) {
		b.fail(id, perr.Code, "%s", perr.Message)
		return
	}
	b.fail(id, ErrServer, "%v", err)
}

// answer answers request id with v, made safe.
func (b *bridge) answer(id int64, v any) {
	raw, err := json.Marshal(safe(v))
	if err != nil {
		b.fail(id, ErrServer, "%v", err)
		return
	}
	b.write(Out{ID: id, Result: raw})
}

// stream answers request id of stream method m: on a connection of its
// own, the result, then each event, until the server ends it or ctx
// (MethodCancel, the bridge ending) does; then the end line.
func (b *bridge) stream(ctx context.Context, id int64, m Method, params any) {
	c, err := client.ConnectAs(b.o.Paths, b.o.Autostart, proto.ClientApp)
	if err != nil {
		b.fail(id, ErrServer, "%v", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	var raw json.RawMessage
	if err := c.Call(m.Name, params, &raw); err != nil {
		b.serverError(id, err)
		return
	}
	res, err := m.result(raw)
	if err != nil {
		b.fail(id, ErrServer, "%s: the server's answer: %v", m.Name, err)
		return
	}
	b.answer(id, res)
	end := Out{ID: id, End: true}
	for {
		var line json.RawMessage
		if err := c.ReadLine(&line); err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				end.Error = &Error{Code: ErrServer, Message: fmt.Sprintf("the stream broke: %v", err)}
			}
			break
		}
		data, keep, err := m.event(line)
		if err != nil {
			end.Error = &Error{Code: ErrServer, Message: fmt.Sprintf("%s: the server's event: %v", m.Name, err)}
			break
		}
		if !keep {
			continue
		}
		raw, err := json.Marshal(safe(data))
		if err != nil {
			end.Error = &Error{Code: ErrServer, Message: err.Error()}
			break
		}
		if b.write(Out{ID: id, Event: m.Event, Data: raw}) != nil {
			return
		}
	}
	b.write(end)
}

// close ends the streams, waits for the calls and closes the idle
// connections.
func (b *bridge) close() {
	b.mu.Lock()
	for _, cancel := range b.streams {
		cancel()
	}
	b.mu.Unlock()
	b.wg.Wait()
	for {
		select {
		case c := <-b.idle:
			c.Close()
		default:
			return
		}
	}
}
