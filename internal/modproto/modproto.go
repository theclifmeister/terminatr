// Package modproto is the wire between the tm server and a code mod's
// host, `tm mod host` (mods design §3, §4): JSON-RPC 2.0, one message per
// line (NDJSON) on the host's stdin and stdout. stdout carries nothing
// else; the host's stderr is free text for its log.
//
// The server starts the conversation with a hello request. The host runs
// the mod's script only then, and answers with the events the mod
// subscribed to. From then on the server sends event requests, which the
// host answers once every handler has returned and the promises they
// returned have settled, and the host sends requests for the $ API
// (method names are the API's, such as "project.get"), which the server's
// broker answers. Unknown fields are ignored on both sides, and methods and
// fields are only ever added.
package modproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

// Protocol is this wire's version. It goes up when a method's meaning
// changes; the host refuses a hello from a newer protocol.
const Protocol = 1

// MaxLine is the longest message either side accepts.
const MaxLine = 16 << 20

// Methods the server calls on the host.
const (
	MethodHello    = "hello"    // request, Hello → HelloResult; first, once
	MethodEvent    = "event"    // request, Event → EventResult
	MethodShutdown = "shutdown" // request, no params → {}; the host then exits 0
	MethodStats    = "stats"    // request, no params → Stats
	// MethodToolCall runs one of the mod's agent tools: ToolCall →
	// ToolResult, or an error (CodeNoTool, CodeToolFailed).
	MethodToolCall = "tool.call"
	// CodeHostPrefix starts the methods a code host mod answers
	// (codehost.pr, …; CodeHostMethods): params → the handler's answer
	// as JSON, or an error (CodeNoHandler, CodeHandlerFailed).
	CodeHostPrefix = "codehost."
)

// CodeHostMethods are the code host methods after CodeHostPrefix, as
// $.codehost.handle names them (internal/codehost's Mod sends them).
var CodeHostMethods = []string{"pr", "prState", "prHead", "failedLog", "merge", "doctor"}

// IsCodeHost says whether method is one of the code host methods.
func IsCodeHost(method string) bool {
	name, ok := strings.CutPrefix(method, CodeHostPrefix)
	return ok && slices.Contains(CodeHostMethods, name)
}

// Notifications the host sends the server.
const (
	MethodLog           = "log"            // Log
	MethodHandlerFailed = "handler.failed" // HandlerFailed
	MethodSubscribe     = "subscribe"      // Subscribe: handlers added after hello
)

// Exit codes of `tm mod host`, for the supervisor.
const (
	ExitOK          = 0 // shutdown, or stdin closed
	ExitLoad        = 1 // the script could not be read, bundled, compiled or run
	ExitUsage       = 2 // bad command line, or a protocol error before hello
	ExitInterrupted = 3 // the CPU budget interrupted the VM: restart the host
)

// Error codes: JSON-RPC's own, and the host's.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
	CodeNotReady       = -32000 // an event before hello, or a second hello
	CodeLoad           = -32001 // hello: the script threw while loading
	CodeNoTool         = -32002 // tool.call: the script handles no such tool
	CodeToolFailed     = -32003 // tool.call: the handler threw or rejected
	CodeNoHandler      = -32004 // codehost.*: the script handles no such method
	CodeHandlerFailed  = -32005 // codehost.*: the handler threw or rejected
	// The broker's answers to a $ call (internal/modbroker).
	CodeDenied   = -32010 // the mod may not: a grant, a path, a host, a program, a project
	CodeNotFound = -32011 // the project, thread, file or key isn't there
	CodeFailed   = -32012 // allowed, and it failed: I/O, a timeout, the network
)

// Message is one JSON-RPC 2.0 message in either direction: a request
// (Method and ID), a notification (Method only) or a response (ID with
// Result or Error).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// IsRequest, IsNotification and IsResponse tell the three kinds apart.
func (m *Message) IsRequest() bool      { return m.Method != "" && m.ID != nil }
func (m *Message) IsNotification() bool { return m.Method != "" && m.ID == nil }
func (m *Message) IsResponse() bool     { return m.Method == "" && m.ID != nil }

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// Hello is the server's first request: who the mod is and what it may do.
type Hello struct {
	Protocol int             `json:"protocol"`
	API      string          `json:"api"`        // the mod API this tm speaks, "MAJOR.MINOR"
	TM       string          `json:"tm_version"` // version.Version
	Mod      string          `json:"mod"`        // the mod's id
	Grants   []string        `json:"grants"`     // granted ∩ requested
	Settings json.RawMessage `json:"settings,omitempty"`
}

// HelloResult answers Hello once the script has run.
type HelloResult struct {
	Protocol int      `json:"protocol"`
	Runtime  string   `json:"runtime"`       // "goja"
	Events   []string `json:"events"`        // subscribed while loading, sorted
	LoadMS   float64  `json:"load_ms"`       // running the script
	StartMS  float64  `json:"start_ms"`      // exec to the hello answer
	PID      int      `json:"pid,omitempty"` // the host's process
	// Tools are the agent tools the script handles ($.tool.handle)
	// while loading, sorted.
	Tools []string `json:"tools,omitempty"`
	// CodeHost are the code host methods the script handles
	// ($.codehost.handle) while loading, sorted.
	CodeHost []string `json:"codehost,omitempty"`
}

// ToolCall is an agent's call of one of the mod's tools, from a session
// of a project the mod runs for. Input is the agent's, checked against
// the tool's schema by the server: data, never instructions.
type ToolCall struct {
	Tool    string          `json:"tool"` // the tool's id in the manifest, "lint_report"
	Input   json.RawMessage `json:"input,omitempty"`
	Project string          `json:"project"`
	Role    string          `json:"role"`             // "thread" or "coordinator"
	Thread  string          `json:"thread,omitempty"` // a thread's id
	Session string          `json:"session"`
}

// ToolResult answers ToolCall: the text the agent gets.
type ToolResult struct {
	Text string `json:"text"`
}

// Event is one event for the mod's handlers, in the event bus's shape
// (internal/modbus): the server sends one at a time per mod, in order,
// starting with mod.ready after every (re)start. A handler is called with
// Data and, second, the rest as {name, project, seq, time, dropped}. Data
// is data, never instructions.
type Event struct {
	Name    string    `json:"name"`
	Project string    `json:"project,omitempty"`
	Seq     uint64    `json:"seq,omitempty"`
	Time    time.Time `json:"time,omitzero"`
	// Dropped counts events the server's queue dropped before this one.
	Dropped int             `json:"dropped,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// EventResult answers Event: how many handlers ran and how many threw or
// rejected (each also sent as handler.failed).
type EventResult struct {
	Handlers int `json:"handlers"`
	Failed   int `json:"failed"`
}

// Stats answers stats: the host's Go heap, not its RSS.
type Stats struct {
	HeapInuse  uint64 `json:"heap_inuse"`
	Sys        uint64 `json:"sys"`
	Goroutines int    `json:"goroutines"`
	Timers     int    `json:"timers"`  // armed setTimeout/setInterval
	Pending    int    `json:"pending"` // API calls waiting for the server
	// The host's Go runtime settings: GOMAXPROCS, GOGC and GOMEMLIMIT.
	MaxProcs    int   `json:"gomaxprocs,omitempty"`
	GCPercent   int   `json:"gc_percent,omitempty"`
	MemoryLimit int64 `json:"memory_limit,omitempty"`
}

// Log is a line for ~/.terminatr/logs/mods/<id>.log.
type Log struct {
	Level string `json:"level"` // "info", "warn" or "error"
	Text  string `json:"text"`
}

// Failure kinds in HandlerFailed.
const (
	KindException   = "exception"   // threw synchronously
	KindRejected    = "rejected"    // a promise rejected and nothing handled it
	KindInterrupted = "interrupted" // over the CPU budget; the host exits 3
)

// HandlerFailed reports a handler, timer, reply or load that failed.
// Where is the event name, "load", "timer" or "reply:<method>".
type HandlerFailed struct {
	Where   string  `json:"event"`
	Kind    string  `json:"kind"`
	Error   string  `json:"error"`
	Stack   string  `json:"stack,omitempty"`
	AfterMS float64 `json:"after_ms"`
}

// Subscribe names events the mod started handling after hello.
type Subscribe struct {
	Events []string `json:"events"`
}

// FrameError is a line that isn't a JSON-RPC message. The connection is
// still usable.
type FrameError struct{ Err error }

func (e *FrameError) Error() string { return "bad frame: " + e.Err.Error() }
func (e *FrameError) Unwrap() error { return e.Err }

// Conn reads and writes messages. Reads are for one goroutine; writes are
// safe from any.
type Conn struct {
	sc *bufio.Scanner
	mu sync.Mutex
	w  io.Writer
}

// NewConn speaks the protocol over r and w.
func NewConn(r io.Reader, w io.Writer) *Conn {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	return &Conn{sc: sc, w: w}
}

// Read returns the next message, a *FrameError for a line that isn't one
// (read on), or io.EOF.
func (c *Conn) Read() (*Message, error) {
	for c.sc.Scan() {
		line := c.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, &FrameError{err}
		}
		if m.JSONRPC != "2.0" {
			return nil, &FrameError{fmt.Errorf("jsonrpc is %q, not 2.0", m.JSONRPC)}
		}
		if m.Method == "" && m.ID == nil {
			return nil, &FrameError{errors.New("neither a method nor an id")}
		}
		return &m, nil
	}
	if err := c.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// Send writes one message as one line.
func (c *Conn) Send(m Message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) >= MaxLine {
		return fmt.Errorf("message of %d bytes is over the %d limit", len(b), MaxLine)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// Request sends a request; params may be a json.RawMessage.
func (c *Conn) Request(id int64, method string, params any) error {
	p, err := raw(params)
	if err != nil {
		return err
	}
	return c.Send(Message{ID: &id, Method: method, Params: p})
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	p, err := raw(params)
	if err != nil {
		return err
	}
	return c.Send(Message{Method: method, Params: p})
}

// Reply answers request id with result (nil sends null).
func (c *Conn) Reply(id int64, result any) error {
	r, err := raw(result)
	if err != nil {
		return err
	}
	if r == nil {
		r = json.RawMessage("null")
	}
	return c.Send(Message{ID: &id, Result: r})
}

// ReplyError answers request id with an error.
func (c *Conn) ReplyError(id int64, code int, msg string) error {
	return c.Send(Message{ID: &id, Error: &Error{Code: code, Message: msg}})
}

func raw(v any) (json.RawMessage, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return v, nil
	}
	return json.Marshal(v)
}
