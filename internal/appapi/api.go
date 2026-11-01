// Package appapi is the Mac app's API (docs/SPEC.md §3.8): what tm app
// bridge lets the app read and do, and the bridge itself. The app runs
// the installed tm as a child, `tm app bridge`, and speaks NDJSON on its
// stdin and stdout; the bridge dials the human socket like any CLI call
// and opens no listener of its own.
//
// The API is a named allowlist of control methods (Methods). Each
// result is copied field by field into this package's types, never
// passed through: a field the server gains stays home until a copy here
// names it, which a reflection test enforces. Every string goes through
// textsafe on the way out. Params are decoded strictly into the app's
// own params types and copied into the server's, so the app can't set
// what a method's server params carry for tm's own commands (Relay).
package appapi

import (
	"encoding/json"
	"reflect"
)

// Version is the app API's version. It goes up when a method, a field or
// a meaning is added or changes; the app reads it, and Capabilities, from
// the bridge's hello, so an older app works with a newer tm.
const Version = 42

// Capabilities of this bridge, in its hello: what kinds of method it
// has (Method.Write, Method.Stream). An app checks a capability, or a
// method by name, before it offers what needs it.
const (
	// CapRead: methods that only read.
	CapRead = "read"
	// CapWrite: methods that act as the human (prompts, answers,
	// approvals, keys, task actions, starting a coordinator),
	// journaled as "human (app)".
	CapWrite = "write"
	// CapStreams: methods that stream events after their result.
	CapStreams = "streams"
)

// Capabilities are this bridge's capabilities.
var Capabilities = []string{CapRead, CapWrite, CapStreams}

// The bridge's own method, answered without the server.
const (
	// MethodCancel ends the stream of request CancelParams.ID: its end
	// line follows. The result is empty.
	MethodCancel = "cancel"
)

// Error codes the bridge answers itself, besides the server's
// (proto.Err*, "human-only", the methods' own).
const (
	// ErrBadRequest: the line isn't a request (no id, no method, not
	// JSON), or reuses the id of a request still open.
	ErrBadRequest = "bad-request"
	// ErrUnknownMethod: the method isn't in the app API, whether the
	// server has it or not.
	ErrUnknownMethod = "unknown-method"
	// ErrBadParams: the params don't decode into the method's app
	// params (an unknown field among them).
	ErrBadParams = "bad-params"
	// ErrServer: the server couldn't be reached, or its connection
	// broke during the call.
	ErrServer = "server"
	// ErrTooMany: too many streams are open.
	ErrTooMany = "too-many-streams"
	// ErrServerMismatch: the running server is older than this tm
	// (hello only); tm server restart switches it.
	ErrServerMismatch = "server-mismatch"
	// ErrLevel: the method needs a higher level than the device's
	// (app API 19, devices only).
	ErrLevel = "device-level"
	// ErrTooBig: the answer, or a stream's event (which ends the
	// stream), is over the bytes a device's link carries in a line
	// (MaxLine), so the bridge didn't send it (app API 37). The message
	// names the method and the size.
	ErrTooBig = "too-big"
)

// Request is a line the app writes: a call of Method with Params,
// answered by Out lines with the same ID (from 1, unique among the
// requests still open). Auth is a device's signature on a write (app
// API 23, sign.go), which a device's bridge needs on every write.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Auth   *Auth           `json:"auth,omitempty"`
}

// CancelParams name the stream MethodCancel ends.
type CancelParams struct {
	ID int64 `json:"id"`
}

// Out is a line the bridge writes. The first is the Hello alone. Then,
// for request ID: Result or Error; for a stream after its Result, an
// Event line (Event, Data) for each event, and End last, with Error
// when the stream broke rather than ended.
type Out struct {
	ID     int64           `json:"id,omitempty"`
	Hello  *Hello          `json:"hello,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	End    bool            `json:"end,omitempty"`
}

// Error is a failed request: Code to match on, Message to show.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Hello is the bridge's first line: the app API it speaks, the tm it
// is and the server it reached. Error, when set, says why it reached
// none; the bridge exits after it. On a device's link (app API 19)
// Level is the device's level, Capabilities and Methods only what it
// allows, and a new Hello line comes whenever the level changes. Sign
// (app API 23) says every write needs Request.Auth: a device's at the
// levels that write.
type Hello struct {
	AppAPI       int      `json:"app_api"`
	TM           string   `json:"tm"`
	Build        string   `json:"build"`
	Level        string   `json:"level,omitempty"`
	Sign         bool     `json:"sign,omitempty"`
	Capabilities []string `json:"capabilities"`
	Methods      []string `json:"methods"`
	Server       *Server  `json:"server,omitempty"`
	Error        *Error   `json:"error,omitempty"`
}

// Server is the server the bridge reached: its version, protocol and
// pid, whether the bridge started it (through the launchd job), and its
// capabilities (proto.CapPaneVT, proto.CapTranscript): a method that
// needs one the server lacks answers unknown-method from it.
type Server struct {
	Version  string   `json:"version"`
	Protocol int      `json:"protocol"`
	PID      int      `json:"pid,omitempty"`
	Started  bool     `json:"started,omitempty"`
	Caps     []string `json:"caps"`
}

// Method is one method of the app API: its name (the control method's,
// or the bridge's own for a stream's input), whether it acts as the
// human, whether it streams, and its app types: Params (nil for none),
// Result, and for a stream its events' names and their Data. Target,
// for a stream's input (pane.input), is the stream method whose open
// stream (params' id) takes it.
type Method struct {
	Name   string
	Write  bool
	Stream bool
	Events []string
	Target string
	Params reflect.Type
	Result reflect.Type
	Data   reflect.Type
	// toServer decodes the app's params and copies them into the
	// server's; result and event copy the server's answer and events,
	// event also the line that acks one (nil for none); toStream
	// decodes a stream's input into its stream's id and the line for
	// the server.
	toServer func(json.RawMessage) (any, error)
	result   func(json.RawMessage) (any, error)
	event    func(json.RawMessage) (name string, data any, keep bool, ack any, err error)
	toStream func(json.RawMessage) (int64, any, error)
	// local answers a method the bridge answers itself, without the
	// server (the guard's, guard.go), from the app's params as toServer
	// decoded them; its error is an *Error.
	local func(b *bridge, params any) (any, error)
	// device: only a device's bridge has it, at the levels that sign
	// (device.nonce).
	device bool
}

// Local says whether the bridge answers m in its own process, never
// through the server (the guard's refusals).
func (m Method) Local() bool { return m.local != nil }

// DeviceOnly says whether only a device's bridge has m (device.nonce).
func (m Method) DeviceOnly() bool { return m.device }
