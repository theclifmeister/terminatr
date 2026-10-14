package proto

import "time"

// Coordinator links across servers (docs/SPEC.md §16.10): a message,
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
}

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
}
