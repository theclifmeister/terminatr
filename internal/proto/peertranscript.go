package proto

// Remote conversations (docs/SPEC.md §16.14): a paired server's
// coordinator and thread conversations, the items its own app reads
// with transcript.watch, through the link. peer.transcript opens a
// stream; binary link frames carry its events (internal/peer).

// MethodPeerTranscript opens the conversation of a shared project's
// session (PeerTranscriptParams): a PeerTranscriptResult, then the
// stream's events, those of transcript.watch.
const MethodPeerTranscript = "peer.transcript"

// ErrConversationsOff means the server doesn't share conversations with
// the caller: its pane switch for that peer is off, or its conversations
// switch is.
const ErrConversationsOff = "conversations-off"

// ConversationsOff is a transcript stream's closed reason when the
// conversations switch went off (the pane switch going off closes it
// with PaneOff); the other reasons are the pane stream's (PaneExited,
// PaneNotShared, PaneStopping, PaneLink).
const ConversationsOff = "conversations-off"

// The conversations switch's values, per paired server, beside the pane
// switch: on (the default, and when unset) shares the shared projects'
// conversations at the pane switch's level; off shares none.
const (
	ConversationsOn  = "on"
	ConversationsNot = "off"
)

// PeerTranscriptParams are the params of peer.transcript: the session (a
// coordinator's or a thread's of the shared Project) and the stream id
// the caller picked for its frames.
type PeerTranscriptParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Stream  uint32 `json:"stream"`
}

// PeerTranscriptResult is peer.transcript's answer: the session as
// peer.watch shows it, whether the caller may send to it (its pane
// switch is write), the mapping's status and the stream's window in
// items, as transcript.watch's.
type PeerTranscriptResult struct {
	Session PeerSession      `json:"session"`
	Input   bool             `json:"input"`
	Status  TranscriptStatus `json:"status"`
	Window  int              `json:"window"`
}
