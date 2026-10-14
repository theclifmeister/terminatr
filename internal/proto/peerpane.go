package proto

// Remote panes (docs/SPEC.md §16.12): a paired server's coordinator and
// thread panes, watched and typed into through the link. peer.attach
// opens a stream; binary link frames carry it (internal/peer).

// MethodPeerAttach opens a pane of a shared project (PeerAttachParams):
// a PeerAttachResult, then the stream's frames.
const MethodPeerAttach = "peer.attach"

// ErrPanesOff: the server doesn't share panes with the caller (its pane
// switch for that peer is off).
const ErrPanesOff = "panes-off"

// The pane switch's levels, per paired server: what it may do with this
// server's panes. Off (the default) refuses peer.attach; view watches;
// type watches and types.
const (
	PanesOff  = "off"
	PanesView = "view"
	PanesType = "type"
)

// ValidPanes says whether s is a pane switch level.
func ValidPanes(s string) bool { return s == PanesOff || s == PanesView || s == PanesType }

// PeerAttachParams are the params of peer.attach: the session (a
// coordinator's or a thread's of the shared Project), the stream id the
// caller picked for its frames, and the caller's pane size (informational,
// as in AttachParams).
type PeerAttachParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Stream  uint32 `json:"stream"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

// PeerAttachResult is the answer of peer.attach: whether the stream may
// type (the switch is type), and the session as peer.watch shows it.
type PeerAttachResult struct {
	Input   bool        `json:"input"`
	Session PeerSession `json:"session"`
}

// Why a remote pane's stream ended, in fixed words: the closed frame's
// payload on the link.
const (
	PaneExited    = "exited"     // the session ended
	PaneNotShared = "not-shared" // the project isn't shared any more
	PaneOff       = "panes-off"  // the switch went off
	PaneStopping  = "stopping"   // the server is stopping
	PaneLink      = "link"       // the link dropped (never sent: the reader's own)
)
