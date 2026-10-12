// Package modhost is a code mod's host (mods design §3, §4): `tm mod
// host` runs one mod's script in a goja VM, speaks modproto on stdin and
// stdout, and gives the script only the $ object tm injects, whose calls
// all go to the server's broker.
//
// Every entry into the VM is a slice of the event loop: loading the
// script, each event handler, each broker reply and each timer. A slice
// runs under the CPU budget, so the code after an await, which runs in the
// slice that settled the promise, is covered too. A slice over budget is
// interrupted and the host exits with modproto.ExitInterrupted: goja
// leaves its promise job queue stuck when an interrupt lands inside a
// promise job, so every later await in that VM would hang. The supervisor
// restarts it.
package modhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// DefaultBudget is a slice's CPU budget (design §7: a handler over 5 s is
// interrupted).
const DefaultBudget = 5 * time.Second

// Runtime names this host's engine in HelloResult.
const Runtime = "goja"

// maxText caps a log line or an error text sent to the server.
const maxText = 8 << 10

// minDelay is the shortest timer, so a zero-delay setInterval can't spin
// the loop.
const minDelay = time.Millisecond

// Options configure Run.
type Options struct {
	// Name is the script's file name in stack traces.
	Name string
	// Script is the bundle to run (see Load).
	Script string
	// Budget is each slice's CPU budget; zero means DefaultBudget.
	Budget time.Duration
	// In and Out are the protocol pipes: the server's requests in, and
	// everything the host sends out. Nothing else may write to Out.
	In  io.Reader
	Out io.Writer
	// Bind adds the $ API's bindings (the T251 schema) once hello has
	// arrived and before the script runs. Nil leaves only $.on and $.log.
	Bind func(h *Host) error
	// Started is when the process started, for HelloResult.StartMS.
	Started time.Time
}

// Host is one mod's VM and its connection. Everything but Run is for the
// loop goroutine only: bindings call it from inside the VM.
type Host struct {
	opt    Options
	conn   *modproto.Conn
	loop   *loop
	vm     *goja.Runtime
	prog   *goja.Program
	dollar *goja.Object
	parse  goja.Callable // JSON.parse
	strfy  goja.Callable // JSON.stringify
	errCtr goja.Value    // Error

	hello    *modproto.Hello
	ready    bool // hello answered
	handlers map[string][]goja.Callable
	tools    map[string]goja.Callable // $.tool.handle's, by tool id
	codehost map[string]goja.Callable // $.codehost.handle's, by method name
	nextID   int64
	pending  map[int64]pendingCall
	timers   map[int64]*timer
	nextTmr  int64
	// unhandled are promises rejected in this slice with no handler yet.
	unhandled map[*goja.Promise]struct{}
	code      int
	stopped   bool // exit was called: no more slices
}

type pendingCall struct {
	method string
	settle func(json.RawMessage, *modproto.Error) error
}

// Run serves the protocol until shutdown, the end of In, or an interrupt,
// and returns the exit code (modproto.Exit*).
func Run(opt Options) int {
	if opt.Budget <= 0 {
		opt.Budget = DefaultBudget
	}
	if opt.Started.IsZero() {
		opt.Started = time.Now()
	}
	prog, err := goja.Compile(opt.Name, opt.Script, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tm mod host: compile:", err)
		return modproto.ExitLoad
	}
	h := &Host{
		opt:       opt,
		conn:      modproto.NewConn(opt.In, opt.Out),
		loop:      newLoop(),
		vm:        goja.New(),
		prog:      prog,
		handlers:  map[string][]goja.Callable{},
		tools:     map[string]goja.Callable{},
		codehost:  map[string]goja.Callable{},
		pending:   map[int64]pendingCall{},
		timers:    map[int64]*timer{},
		unhandled: map[*goja.Promise]struct{}{},
	}
	h.setupVM()
	go h.read()
	h.loop.run()
	for _, t := range h.timers {
		t.t.Stop()
	}
	return h.code
}

// exit ends the loop after the running job with code.
func (h *Host) exit(code int) {
	h.code, h.stopped = code, true
	h.loop.quit()
}

// read turns the server's lines into loop jobs.
func (h *Host) read() {
	for {
		m, err := h.conn.Read()
		var fe *modproto.FrameError
		switch {
		case errors.As(err, &fe):
			h.log("error", fe.Error())
			continue
		case err != nil:
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, "tm mod host: read:", err)
			}
			h.loop.post(func() { h.exit(modproto.ExitOK) })
			return
		}
		switch {
		case m.IsResponse():
			h.loop.post(func() { h.response(m) })
		case m.IsRequest():
			switch m.Method {
			case modproto.MethodHello, modproto.MethodEvent, modproto.MethodShutdown, modproto.MethodStats, modproto.MethodToolCall:
				h.loop.post(func() { h.request(m) })
			default:
				if modproto.IsCodeHost(m.Method) {
					h.loop.post(func() { h.request(m) })
					break
				}
				_ = h.conn.ReplyError(*m.ID, modproto.CodeMethodNotFound, "unknown method "+m.Method)
			}
		}
		// Notifications from the server: none yet, ignored.
	}
}

func (h *Host) request(m *modproto.Message) {
	id := *m.ID
	switch m.Method {
	case modproto.MethodHello:
		h.onHello(id, m.Params)
	case modproto.MethodEvent:
		if !h.ready {
			_ = h.conn.ReplyError(id, modproto.CodeNotReady, "event before hello")
			return
		}
		var ev modproto.Event
		if err := json.Unmarshal(m.Params, &ev); err != nil || ev.Name == "" {
			_ = h.conn.ReplyError(id, modproto.CodeInvalidParams, "event needs a name")
			return
		}
		h.dispatch(id, ev)
	case modproto.MethodToolCall:
		if !h.ready {
			_ = h.conn.ReplyError(id, modproto.CodeNotReady, "tool.call before hello")
			return
		}
		var call modproto.ToolCall
		if err := json.Unmarshal(m.Params, &call); err != nil || call.Tool == "" {
			_ = h.conn.ReplyError(id, modproto.CodeInvalidParams, "tool.call needs a tool")
			return
		}
		h.callTool(id, call)
	default: // a code host method (read checked it)
		if !h.ready {
			_ = h.conn.ReplyError(id, modproto.CodeNotReady, m.Method+" before hello")
			return
		}
		h.callCodeHost(id, m.Method, m.Params)
	case modproto.MethodShutdown:
		_ = h.conn.Reply(id, struct{}{})
		h.exit(modproto.ExitOK)
	case modproto.MethodStats:
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		_ = h.conn.Reply(id, modproto.Stats{
			HeapInuse: ms.HeapInuse, Sys: ms.Sys, Goroutines: runtime.NumGoroutine(),
			Timers: len(h.timers), Pending: len(h.pending),
		})
	}
}

// onHello installs $ and runs the script.
func (h *Host) onHello(id int64, params json.RawMessage) {
	if h.hello != nil {
		_ = h.conn.ReplyError(id, modproto.CodeNotReady, "hello was already sent")
		return
	}
	var hello modproto.Hello
	if err := json.Unmarshal(params, &hello); err != nil {
		_ = h.conn.ReplyError(id, modproto.CodeInvalidParams, "hello: "+err.Error())
		h.exit(modproto.ExitUsage)
		return
	}
	if hello.Protocol > modproto.Protocol {
		_ = h.conn.ReplyError(id, modproto.CodeInvalidParams, fmt.Sprintf(
			"the server speaks mod host protocol %d; this host speaks %d", hello.Protocol, modproto.Protocol))
		h.exit(modproto.ExitUsage)
		return
	}
	h.hello = &hello
	if h.opt.Bind != nil {
		if err := h.opt.Bind(h); err != nil {
			_ = h.conn.ReplyError(id, modproto.CodeInternal, "bindings: "+err.Error())
			h.exit(modproto.ExitLoad)
			return
		}
	}
	start := time.Now()
	err := h.slice("load", func() error {
		_, err := h.vm.RunProgram(h.prog)
		return err
	})
	if h.stopped { // interrupted
		return
	}
	if err != nil {
		msg, _ := describe(err)
		_ = h.conn.ReplyError(id, modproto.CodeLoad, msg)
		h.exit(modproto.ExitLoad)
		return
	}
	h.ready = true
	_ = h.conn.Reply(id, modproto.HelloResult{
		Protocol: modproto.Protocol,
		Runtime:  Runtime,
		Events:   h.events(),
		LoadMS:   ms(time.Since(start)),
		StartMS:  ms(time.Since(h.opt.Started)),
		PID:      os.Getpid(),
		Tools:    h.toolNames(),
		CodeHost: sortedKeys(h.codehost),
	})
}

func (h *Host) toolNames() []string { return sortedKeys(h.tools) }

func sortedKeys(m map[string]goja.Callable) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// callTool runs the handler of one of the mod's agent tools with the
// call's input and, second, {tool, project, role, thread, session}, and
// answers with what it returned once a returned promise has settled: a
// string as it is, anything else as JSON, nothing as "". A handler that
// throws or rejects answers CodeToolFailed with its message, which the
// agent reads as the tool's error.
func (h *Host) callTool(id int64, call modproto.ToolCall) {
	fn, ok := h.tools[call.Tool]
	if !ok {
		_ = h.conn.ReplyError(id, modproto.CodeNoTool, "the mod handles no tool "+call.Tool)
		return
	}
	h.answer(id, "tool:"+call.Tool, modproto.CodeToolFailed, func() (goja.Value, error) {
		input := call.Input
		if len(input) == 0 || string(input) == "null" {
			input = json.RawMessage("{}")
		}
		arg, err := h.fromJSON(input)
		if err != nil {
			return nil, err
		}
		meta := h.vm.NewObject()
		_ = meta.Set("tool", call.Tool)
		_ = meta.Set("project", call.Project)
		_ = meta.Set("role", call.Role)
		_ = meta.Set("thread", call.Thread)
		_ = meta.Set("session", call.Session)
		return fn(goja.Undefined(), arg, meta)
	}, func(v goja.Value) error {
		text, err := h.toolText(v)
		if err != nil {
			return errors.New("its answer is not text or JSON: " + err.Error())
		}
		return h.conn.Reply(id, modproto.ToolResult{Text: text})
	})
}

// callCodeHost runs the handler of code host method m.Method
// ($.codehost.handle) with its params and answers with what it returned,
// as JSON (nothing as null), once a returned promise has settled. A
// handler that throws or rejects answers CodeHandlerFailed with its
// message; the server reads it as a failed call of the host.
func (h *Host) callCodeHost(id int64, method string, params json.RawMessage) {
	name := strings.TrimPrefix(method, modproto.CodeHostPrefix)
	fn, ok := h.codehost[name]
	if !ok {
		_ = h.conn.ReplyError(id, modproto.CodeNoHandler, "the mod handles no code host method "+name)
		return
	}
	h.answer(id, method, modproto.CodeHandlerFailed, func() (goja.Value, error) {
		if len(params) == 0 {
			params = json.RawMessage("{}")
		}
		arg, err := h.fromJSON(params)
		if err != nil {
			return nil, err
		}
		return fn(goja.Undefined(), arg)
	}, func(v goja.Value) error {
		b, err := h.toJSON(v)
		if err != nil {
			return errors.New("its answer is not JSON: " + err.Error())
		}
		if b == nil || (v != nil && goja.IsNull(v)) {
			b = json.RawMessage("null")
		}
		return h.conn.Reply(id, b)
	})
}

// answer runs a handler (call) in a slice of its own, under the budget,
// and answers request id once with reply of what it returned, after a
// returned promise has settled. A handler that throws or rejects, or
// whose answer reply refuses, answers failCode with the message.
func (h *Host) answer(id int64, where string, failCode int, call func() (goja.Value, error), reply func(goja.Value) error) {
	answered := false
	bad := func(msg string) {
		if answered {
			return
		}
		answered = true
		_ = h.conn.ReplyError(id, failCode, clip(msg))
	}
	ok1 := func(v goja.Value) {
		if answered {
			return
		}
		if err := reply(v); err != nil {
			bad(err.Error())
			return
		}
		answered = true
	}
	err := h.slice(where, func() error {
		v, err := call()
		if err != nil {
			return err
		}
		p, isPromise := v.Export().(*goja.Promise)
		if !isPromise {
			ok1(v)
			return nil
		}
		then, _ := goja.AssertFunction(h.vm.ToValue(p).ToObject(h.vm).Get("then"))
		onOK := h.vm.ToValue(func(c goja.FunctionCall) goja.Value { ok1(c.Argument(0)); return goja.Undefined() })
		onBad := h.vm.ToValue(func(c goja.FunctionCall) goja.Value {
			msg, stack := describeValue(c.Argument(0))
			h.failed(where, modproto.KindRejected, msg, stack, 0)
			bad(message(c.Argument(0), msg))
			return goja.Undefined()
		})
		_, err = then(h.vm.ToValue(p), onOK, onBad)
		return err
	})
	if h.stopped {
		return
	}
	if err != nil {
		msg, _ := describe(err)
		var ex *goja.Exception
		if errors.As(err, &ex) {
			msg = message(ex.Value(), msg)
		}
		bad(msg)
	}
}

// message is a thrown Error's message, what the agent reads; else def.
func message(v goja.Value, def string) string {
	if o, ok := v.(*goja.Object); ok {
		if m := o.Get("message"); m != nil && !goja.IsUndefined(m) && m.String() != "" {
			return m.String()
		}
	}
	return def
}

// toolText is a tool handler's answer as the agent's text.
func (h *Host) toolText(v goja.Value) (string, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", nil
	}
	if s, ok := v.Export().(string); ok {
		return clipTool(s), nil
	}
	b, err := h.toJSON(v)
	if err != nil {
		return "", err
	}
	return clipTool(string(b)), nil
}

// maxToolText caps a tool's answer, as the agent gets it.
const maxToolText = 64 << 10

func clipTool(s string) string {
	if len(s) > maxToolText {
		return s[:maxToolText] + "…"
	}
	return s
}

func (h *Host) events() []string {
	names := make([]string, 0, len(h.handlers))
	for n := range h.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// eventRun tracks one event until its handlers and their promises are
// done, then answers it.
type eventRun struct {
	id       int64
	res      modproto.EventResult
	pending  int
	dispatch bool // still calling handlers
	answered bool
}

func (h *Host) dispatch(id int64, ev modproto.Event) {
	r := &eventRun{id: id, dispatch: true}
	// A copy: a handler that subscribes more doesn't run for this event.
	fns := append([]goja.Callable(nil), h.handlers[ev.Name]...)
	r.res.Handlers = len(fns)
	if len(fns) > 0 {
		var arg, meta goja.Value
		err := h.slice(ev.Name, func() (err error) {
			if arg, err = h.fromJSON(ev.Data); err != nil {
				return err
			}
			m := h.vm.NewObject()
			_ = m.Set("name", ev.Name)
			_ = m.Set("project", ev.Project)
			_ = m.Set("seq", ev.Seq)
			_ = m.Set("time", ev.Time.Format(time.RFC3339Nano))
			_ = m.Set("dropped", ev.Dropped)
			meta = m
			return nil
		})
		if h.stopped {
			return
		}
		if err != nil {
			r.res.Failed = len(fns)
			fns = nil
		}
		for _, fn := range fns {
			err := h.slice(ev.Name, func() error {
				v, err := fn(goja.Undefined(), arg, meta)
				if err != nil {
					return err
				}
				h.await(ev.Name, r, v)
				return nil
			})
			if h.stopped {
				return
			}
			if err != nil {
				r.res.Failed++
			}
		}
	}
	r.dispatch = false
	h.settle(r)
}

// await counts a handler's returned promise into r. In a slice.
func (h *Host) await(where string, r *eventRun, v goja.Value) {
	p, ok := v.Export().(*goja.Promise)
	if !ok {
		return
	}
	done := func(failed goja.Value) {
		r.pending--
		if failed != nil {
			r.res.Failed++
			msg, stack := describeValue(failed)
			h.failed(where, modproto.KindRejected, msg, stack, 0)
		}
		h.settle(r)
	}
	then, _ := goja.AssertFunction(h.vm.ToValue(p).ToObject(h.vm).Get("then"))
	ok1 := h.vm.ToValue(func(goja.FunctionCall) goja.Value { done(nil); return goja.Undefined() })
	bad := h.vm.ToValue(func(c goja.FunctionCall) goja.Value { done(c.Argument(0)); return goja.Undefined() })
	// Attaching handlers marks the promise handled, so it's reported here
	// and not again as unhandled.
	if _, err := then(h.vm.ToValue(p), ok1, bad); err != nil {
		panic(err)
	}
	r.pending++
}

func (h *Host) settle(r *eventRun) {
	if r.dispatch || r.pending > 0 || r.answered || h.stopped {
		return
	}
	r.answered = true
	_ = h.conn.Reply(r.id, r.res)
}

// response settles the promise of one of the host's calls.
func (h *Host) response(m *modproto.Message) {
	pc, ok := h.pending[*m.ID]
	if !ok {
		h.log("warn", fmt.Sprintf("a reply to unknown call %d", *m.ID))
		return
	}
	delete(h.pending, *m.ID)
	h.slice("reply:"+pc.method, func() error { return pc.settle(m.Result, m.Error) })
}

// slice runs f, one job's entry into the VM, under the CPU budget, and
// reports what went wrong. After an interrupt it ends the host.
func (h *Host) slice(where string, f func() error) (err error) {
	start := time.Now()
	fired := make(chan struct{})
	t := time.AfterFunc(h.opt.Budget, func() {
		h.vm.Interrupt(fmt.Sprintf("over the CPU budget of %s", h.opt.Budget))
		close(fired)
	})
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		err = f()
	}()
	interrupted := false
	if !t.Stop() {
		<-fired
		interrupted = true
	}
	var ie *goja.InterruptedError
	if interrupted || errors.As(err, &ie) {
		msg := fmt.Sprintf("over the CPU budget of %s", h.opt.Budget)
		if err != nil {
			msg, _ = describe(err)
		}
		h.failed(where, modproto.KindInterrupted, msg, "", time.Since(start))
		h.exit(modproto.ExitInterrupted)
		return err
	}
	h.vm.ClearInterrupt()
	if err != nil {
		msg, stack := describe(err)
		h.failed(where, modproto.KindException, msg, stack, time.Since(start))
	}
	for p := range h.unhandled {
		msg, stack := describeValue(p.Result())
		h.failed(where, modproto.KindRejected, msg, stack, time.Since(start))
		delete(h.unhandled, p)
	}
	return err
}

func (h *Host) failed(where, kind, msg, stack string, after time.Duration) {
	_ = h.conn.Notify(modproto.MethodHandlerFailed, modproto.HandlerFailed{
		Where: where, Kind: kind, Error: clip(msg), Stack: clip(stack), AfterMS: ms(after),
	})
}

func (h *Host) log(level, text string) {
	_ = h.conn.Notify(modproto.MethodLog, modproto.Log{Level: level, Text: clip(text)})
}

// describe turns an error out of the VM into its message and stack.
func describe(err error) (msg, stack string) {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return describeValue(ex.Value())
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		return fmt.Sprint(ie.Value()), ""
	}
	return err.Error(), ""
}

func describeValue(v goja.Value) (msg, stack string) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return fmt.Sprint(v), ""
	}
	if ie, ok := v.Export().(*goja.InterruptedError); ok {
		return fmt.Sprint(ie.Value()), ""
	}
	if o, ok := v.(*goja.Object); ok {
		if s := o.Get("stack"); s != nil && !goja.IsUndefined(s) {
			stack = s.String()
		}
	}
	return firstLine(v.String()), stack
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func clip(s string) string {
	if len(s) > maxText {
		return s[:maxText] + "…"
	}
	return s
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
