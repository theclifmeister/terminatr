package proto

import (
	"encoding/json"
	"fmt"
)

// The control protocol's core: requests, responses and their errors.
// The methods are in methods_*.go and tools.go, by domain, each
// method's constant beside its params and result.

// Error codes of control responses. Clients match on these, never on
// messages.
const (
	ErrUnknownMethod  = "unknown-method"
	ErrBadParams      = "bad-params"
	ErrUnknownSession = "unknown-session"
	ErrRefused        = "refused"
	ErrInternal       = "internal"
	// ErrTooBig: the answer is over the bytes its connection carries in
	// a line (a paired server's link, a device's), so it wasn't sent;
	// the message names the method and the size.
	ErrTooBig = "too-big"
)

// Error is a structured failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds an *Error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Request is one control call.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers the Request with the same ID.
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}
