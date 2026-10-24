package proto

import "time"

// Share requests (docs/SPEC.md §16.9, Asking for a share): this
// server's user asks a paired server's user to share a project with it.
// The request goes as peer.message kind share-request; there it waits
// for that user, who shares a project (in the console) or declines.
const (
	// MethodPeerShareAsk sends a share request (PeerShareAskParams) to a
	// paired server: a ShareRequest. The human's.
	MethodPeerShareAsk = "peer.share-ask"
	// MethodPeerShareAnswer answers one from a paired server
	// (PeerShareAnswerParams): a ShareRequest. Accepting shares Project
	// (share = true, the settings writer's), then says so; declining
	// shares nothing. The human's.
	MethodPeerShareAnswer = "peer.share-answer"
)

// Share request states: an outgoing one is queued, then waiting once
// that server took it; an incoming one waits from its arrival.
const (
	ShareQueued      = "queued"
	ShareWaiting     = "waiting"
	ShareIsAccepted  = "accepted"
	ShareIsDeclined  = "declined"
	ShareExpired     = "expired"
	ShareUndelivered = "undelivered"
)

// ShareRequest is a share request as each side keeps it (the link
// store), the newest per paired server and direction.
type ShareRequest struct {
	ID string `json:"id"`
	// Out: this server asked; else Peer did.
	Out bool `json:"out"`
	// Peer is the paired server's alias.
	Peer string `json:"peer"`
	// For is the asking project's slug (on the asking server), "" for
	// none in particular; Note the asking user's line, data only.
	For     string    `json:"for,omitempty"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
	State   string    `json:"state"`
	// Project is the project shared on accepting, this server's (an
	// incoming request's only).
	Project  string    `json:"project,omitempty"`
	Answered time.Time `json:"answered,omitzero"`
	// Code is why an outgoing request wasn't delivered, a fixed word.
	Code string `json:"code,omitempty"`
	// Summary is the request in fixed words, as its item says it.
	Summary string `json:"summary,omitempty"`
}

// Pending reports a request that waits for its answer.
func (r ShareRequest) Pending() bool { return r.State == ShareQueued || r.State == ShareWaiting }

// PeerShareAskParams asks the paired server Alias's user to share a
// project, for this server's project For ("" for none), with an
// optional one-line Note.
type PeerShareAskParams struct {
	Alias string `json:"alias"`
	For   string `json:"for,omitempty"`
	Note  string `json:"note,omitempty"`
}

// PeerShareAnswerParams answers request ID: Accept shares Project,
// else it is declined.
type PeerShareAnswerParams struct {
	ID      string `json:"id"`
	Accept  bool   `json:"accept,omitempty"`
	Project string `json:"project,omitempty"`
}
