package proto

import "time"

// Coordinator links across servers (docs/SPEC.md §16.11): a message,
// or a receipt, from a project on a paired server to one here. Each
// server's outbox sends them over the link in sequence order.
const (
	// MethodPeerMessage delivers a PeerMessage: a PeerMessageResult.
	MethodPeerMessage = "peer.message"
)

// PeerMessage kinds.
const (
	// PeerMessageText is a coordinator's (or the user's) message.
	PeerMessageText = "message"
	// PeerMessageRead is the handled receipt: the receiving project's
	// coordinator handled the message's item.
	PeerMessageRead = "read"
	// PeerLinkRequest asks the receiving project's user to link back
	// (docs/SPEC.md §16.11): Allow is the requesting side's entry, ID
	// the request's (R-<fp>-<n>). A server that predates it refuses it
	// as bad-message, as any kind it doesn't know.
	PeerLinkRequest = "link-request"
	// PeerLinkAnswer is that user's answer to request ID: Allow is the
	// answering side's own entry now, "off" when declined.
	PeerLinkAnswer = "link-answer"
	// The control half of a file over the link (docs/SPEC.md §16.13):
	// the offer of a file with message Re (ID X-<fp>-<n>, Name, Size,
	// SHA256), the receiving user's answer to it (Allow accepted or
	// declined), the receipt once it is in place or failed for good
	// (Allow received or failed, Code why), and either side's cancel.
	PeerFileOffer  = "file-offer"
	PeerFileAnswer = "file-answer"
	PeerFileDone   = "file-done"
	PeerFileCancel = "file-cancel"
)

// The words of a file-answer's and a file-done's Allow.
const (
	FileAccepted = "accepted"
	FileDeclined = "declined"
	FileReceived = "received"
	FileFailed   = "failed"
)

// PeerMessage is one entry of a server's outbox to a paired server.
// Epoch and Seq order and dedupe the entries of one sending server: the
// receiver takes each Seq of an Epoch once. From and To are the slugs of
// the sending and receiving projects, each on its own server; the
// receiver knows the sending server by its link, never by the message.
type PeerMessage struct {
	Epoch   string    `json:"epoch"`
	Seq     int       `json:"seq"`
	Kind    string    `json:"kind"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	ID      string    `json:"id"`
	Created time.Time `json:"created,omitzero"`
	Re      string    `json:"re,omitempty"`
	By      string    `json:"by,omitempty"` // "coordinator" or "human"
	Text    string    `json:"text,omitempty"`
	Allow   string    `json:"allow,omitempty"` // link-request, link-answer, file-answer, file-done
	// A file-offer's file: its cleaned base name, size in bytes and
	// SHA-256 in hex; Code is a file-done's failure, a fixed word.
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Code   string `json:"code,omitempty"`
}

// MethodPeerFileFetch is the data half of a file over the link (docs/SPEC.md
// §16.13): the receiving server asks for an offered file's bytes from
// Offset, on a stream id it picked; the answer is the file's size and
// SHA-256, then binary frames carry the bytes. Only when both hellos list
// the file capability.
const MethodPeerFileFetch = "peer.file.fetch"

// PeerFileFetchParams asks for transfer ID's bytes from Offset.
type PeerFileFetchParams struct {
	ID     string `json:"id"`
	Stream uint32 `json:"stream"`
	Offset int64  `json:"offset"`
}

// PeerFileFetchResult is the file a fetch opened.
type PeerFileFetchResult struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Refusals of peer.file.fetch, and the words a file stream's abort frame
// says.
const (
	ErrUnknownFile = "unknown-file" // no such offer to that server, or cancelled
	ErrFileBusy    = "busy"         // that link serves its streams already
	ErrFileGone    = "gone"         // the sender's copy was deleted
	ErrNotLinked   = "not-linked"   // the link doesn't send there now
	ErrLinkPaused  = "link-paused"

	FileCancelled  = "cancelled"
	FileBadContent = "bad-content"
	FileTooBig     = "too-big"
	FileStalled    = "stalled"
	FileNoSpace    = "no-space"
	FileStopping   = "shutting-down"
)

// PeerMessageResult statuses.
const (
	PeerDelivered = "ok"        // taken: the delivered receipt
	PeerDuplicate = "duplicate" // taken before
	PeerRetry     = "retry"     // not now (Code says why); the outbox tries again
	PeerRefused   = "refused"   // never (Code says why); the sender hears of it
)

// PeerMessageResult answers a PeerMessage. Code is one of a fixed set
// of words (link.RefusalWords); the sender shows nothing else of it.
type PeerMessageResult struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
	// RetryAfter is, with PeerRetry, how many seconds the outbox waits
	// before it tries that server again; 0 is its next poll.
	RetryAfter int `json:"retry_after,omitempty"`
}
