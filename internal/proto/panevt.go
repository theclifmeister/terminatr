package proto

// Panes for local clients of any build (docs/SPEC.md §3.3, Pane VT): a
// console that isn't tm's own build (the macOS app, through tm app
// bridge) can't read an attach's libghostty snapshot. pane.vt streams a
// pane as remote panes do (§16.12): a repaint in plain VT, which any
// emulator reads, then the PTY's output and resizes, in order, as
// NDJSON on the control connection. The client acks what it took, so a
// slow one never makes the server hold more than PaneVTWindow of
// output, and types keys, which raise a thread's takeover as a
// console's do. The server's hello lists CapPaneVT.

// MethodPaneVT opens a pane (PaneVTParams), the human's only: a
// PaneVTResult, then PaneVTEvent lines; the client sends PaneVTInput
// lines.
const MethodPaneVT = "pane.vt"

// The events of a pane.vt stream.
const (
	// EventPaneRepaint starts the pane again: a fresh emulator of Cols
	// × Rows, fed Data; output events that follow continue it.
	EventPaneRepaint = "pane.repaint"
	// EventPaneOutput is PTY output (or the rest of a repaint), in order.
	EventPaneOutput = "pane.output"
	// EventPaneResize: the pane is Cols × Rows from here on.
	EventPaneResize = "pane.resize"
	// EventPaneClosed ends the stream, saying why in a fixed word
	// (PaneExited, PaneStopping).
	EventPaneClosed = "pane.closed"
)

// PaneVTWindow is how many bytes of a pane.vt stream's data (repaints
// and output) the server sends past the client's acks.
const PaneVTWindow = 256 << 10

// PaneVTParams are the params of pane.vt: this server's session, and the
// client's id for takeovers, told once per console as for an attach
// (AttachParams.Console; "" is the client's process).
type PaneVTParams struct {
	Session string `json:"session"`
	Console string `json:"console,omitempty"`
}

// PaneVTResult is pane.vt's answer: the session, and the window the
// client's acks open.
type PaneVTResult struct {
	Session SessionInfo `json:"session"`
	Window  int         `json:"window"`
}

// PaneVTEvent is one line of a pane.vt stream after the answer. Data is
// bytes, base64 in JSON.
type PaneVTEvent struct {
	Event  string `json:"event"`
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
	Data   []byte `json:"data,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// PaneVTInput is one line a pane.vt client sends; each field set does
// its part. Input is keys for the pane, already encoded for its modes;
// Ack the data bytes taken since the last ack; Size claims the pane's
// size, as typing in a console does; Repaint asks for a fresh repaint;
// Detach ends the stream.
type PaneVTInput struct {
	Input   []byte    `json:"input,omitempty"`
	Ack     int       `json:"ack,omitempty"`
	Size    *PaneSize `json:"size,omitempty"`
	Repaint bool      `json:"repaint,omitempty"`
	Detach  bool      `json:"detach,omitempty"`
}

// PaneSize is a pane's size in cells.
type PaneSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}
