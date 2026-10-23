package proto

import "time"

// MethodPeerList is the server's mirror of its paired servers' shared
// projects (docs/SPEC.md §16.10), for its own consoles: a PeerList. A
// remote project has no folder here, so a console reads it from this
// and never from disk. The human's (a human-only method); a link never reaches it.
const MethodPeerList = "peer.list"

// PeerList is the answer of peer.list: every paired server, by alias,
// with what this server last read of its shared projects. Gen counts
// the mirror's changes: the same Gen is the same answer.
type PeerList struct {
	Gen   uint64       `json:"gen"`
	Peers []PeerMirror `json:"peers"`
}

// PeerMirror is one paired server as the mirror has it: its link now,
// the offset of its clock and its shared projects.
type PeerMirror struct {
	Alias string `json:"alias"`
	// Name is the display name set here (PeerLink.Name).
	Name string `json:"name,omitempty"`
	ID   string `json:"id"`
	// State, Reason and Since are the link's (LinkStatus).
	State  string    `json:"state"`
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
	// Watch: the peer exports (its hello lists the watch capability).
	// A peer that doesn't has no projects here.
	Watch bool `json:"watch,omitempty"`
	// Popup: the peer serves its shared projects' popups (its hello
	// lists the popup capability), while its pane switch for this
	// server is view or type (peer.popup).
	Popup bool `json:"popup,omitempty"`
	// Pane: the peer has remote panes (its hello lists pane-vt), and
	// Panes is its switch for this server as it last said (PanesOff,
	// PanesView, PanesType; docs/SPEC.md §16.12). Version is its tm's
	// release.
	Pane    bool   `json:"pane,omitempty"`
	Panes   string `json:"panes,omitempty"`
	Version string `json:"version,omitempty"`
	// Offset is the peer's clock minus this server's, in milliseconds,
	// as last measured; the peer's times (a session's created, the
	// ticker's) less Offset are this server's. Unset (and 0) until a
	// first answer carried the peer's now.
	Offset int64 `json:"offset_ms,omitempty"`
	// RTT is the link's last ping round trip in milliseconds, while up
	// (PeerLink.RTT); a change of it alone doesn't bump Gen.
	RTT float64 `json:"rtt_ms,omitempty"`
	// Listed: Projects are a peer.projects answer, the last one while
	// the link is down; until the first, a project missing from them
	// may still be shared.
	Listed   bool                `json:"listed,omitempty"`
	Projects []PeerMirrorProject `json:"projects"`
}

// PeerMirrorProject is one shared project of a peer: its line in
// peer.projects, counted again from each snapshot, and the last
// snapshot. Stale: the link went down, or the last read failed, since
// Synced (this server's clock); the snapshot is the last one seen.
type PeerMirrorProject struct {
	PeerProject
	Stale  bool      `json:"stale,omitempty"`
	Synced time.Time `json:"synced,omitzero"`
	// Rev, Watch and Sessions are the last peer.watch answer; none
	// before the first.
	Rev      string        `json:"rev,omitempty"`
	Watch    *ProjectWatch `json:"watch,omitempty"`
	Sessions []PeerSession `json:"sessions,omitempty"`
}
