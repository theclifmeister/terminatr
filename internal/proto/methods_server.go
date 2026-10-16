package proto

import (
	"time"
)

// Control methods of the server itself (M1).
const (
	MethodPing         = "ping"
	MethodServerStatus = "server.status"
	MethodServerStop   = "server.stop"
	// MethodServerKeychain reports whether the server's sessions can reach
	// the macOS login keychain (docs/SPEC.md §3.1).
	MethodServerKeychain = "server.keychain"
	// MethodServerCodeHost runs tm doctor's code-host checks (gh, az, git
	// origin) in the server's own context (docs/SPEC.md §3.3).
	MethodServerCodeHost = "server.codehost"
)

// MethodSecretTest gets one secret variable of threads from its source
// in the server's own context, as a thread's start does, and says only
// whether that worked: never the value (docs/SPEC.md §8.6, Secret
// environment). The human's.
const MethodSecretTest = "secret.test"

// SecretTestParams name the variable: in Project's env table, all
// projects' ([defaults.env]) when Project is "".
type SecretTestParams struct {
	Project string `json:"project,omitempty"`
	Name    string `json:"name"`
}

// SecretTestResult is whether the source gave a value; Error says why
// not (the source and an exit status, never a program's output).
type SecretTestResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ServerStatus is the result of server.status.
type ServerStatus struct {
	PID      int       `json:"pid"`
	Version  string    `json:"version"`
	Build    string    `json:"build"`
	Protocol int       `json:"protocol"`
	Started  time.Time `json:"started"`
	Sessions int       `json:"sessions"`
	Socket   string    `json:"socket"`
	Home     string    `json:"home"`
	// PreviousShutdown is "clean", "crash" or "" (first start).
	PreviousShutdown string `json:"previous_shutdown,omitempty"`
	// Lost lists sessions of the previous server that were not restored.
	Lost []string `json:"lost,omitempty"`
	// Resumed lists agent sessions of the previous server that were
	// relaunched with their agent session ids (docs/SPEC.md §3.6).
	Resumed []string `json:"resumed,omitempty"`
	// Link is this server's links to its paired servers (docs/SPEC.md
	// §16.7); nil until it has an identity key.
	Link *LinkStatus `json:"link,omitempty"`
}

// MethodPeerReload has the server read its identity key and peers again
// (docs/SPEC.md §16.8): tm peer pair, join, remove and rotate call it
// after changing them. The result is the LinkStatus after the reload
// (null without an identity key). The human's.
const MethodPeerReload = "peer.reload"

// LinkStatus is this server's identity and its links.
type LinkStatus struct {
	// ID is this server's id, 64 hex digits; Short the 8 people read.
	ID    string `json:"id"`
	Short string `json:"short"`
	// Error is why the identity key or the peers can't be read.
	Error string     `json:"error,omitempty"`
	Peers []PeerLink `json:"peers,omitempty"`
}

// Link states.
const (
	LinkConnecting = "connecting" // dialling, or proving keys
	LinkUp         = "up"
	LinkOffline    = "offline" // Reason says why; Retry is the next dial
)

// Why a link is offline.
const (
	LinkReasonRelay   = "relay"   // the transport is down or closed the link
	LinkReasonPeer    = "peer"    // the relay is up; the peer isn't in the room
	LinkReasonRefused = "refused" // the other end's key isn't the paired one, or it refused ours
	LinkReasonVersion = "version" // the peer's link protocol is too old
)

// PeerLink is the link to one paired server.
type PeerLink struct {
	Alias     string `json:"alias"`
	ID        string `json:"id"`
	Transport string `json:"transport"` // "relay relay.example.net"
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	// Since is when the link went into its state.
	Since time.Time `json:"since"`
	// Error is the last failure, for people.
	Error string `json:"error,omitempty"`
	// Retry is the next dial, when offline.
	Retry time.Time `json:"retry,omitzero"`
	// RTT is the last ping's round trip in milliseconds, when up.
	RTT float64 `json:"rtt_ms,omitempty"`
	// The peer's hello, from when the link was last up.
	Version      string   `json:"version,omitempty"`
	LinkProtocol int      `json:"link_protocol,omitempty"`
	Caps         []string `json:"caps,omitempty"`
	// RelayVersion is the relay's tm release, as it said when the link
	// last went through it ("" for a relay older than that).
	RelayVersion string `json:"relay_version,omitempty"`
}

// KeychainStatus is the result of server.keychain.
type KeychainStatus struct {
	// Checked is false where there is no keychain to check (Linux).
	Checked bool `json:"checked"`
	// OK means the server's sessions can reach the login keychain.
	OK bool `json:"ok"`
	// OverSSH means the server's environment is an SSH login's.
	OverSSH bool `json:"over_ssh,omitempty"`
	// Session is launchd's name for the server's session: Aqua for the
	// desktop's, Background or StandardIO for an SSH login's.
	Session string `json:"session,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// CodeHostStatus is the result of server.codehost: the code-host checks
// as the server, and so its sessions, would see them.
type CodeHostStatus struct {
	Checks []CodeHostCheck `json:"checks"`
}

// CodeHostCheck is one line of CodeHostStatus.
type CodeHostCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// ServerStopParams are the params of server.stop.
type ServerStopParams struct {
	// Yes stops the server even while agent sessions run.
	Yes bool `json:"yes,omitempty"`
}

// MethodCallerWho asks the server who the calling process is (M4,
// docs/SPEC.md §11.1): the server walks the peer pid's ancestors to a
// hosted session.
const MethodCallerWho = "caller.who"

// CallerInfo is the result of caller.who. Kind is "human", "coordinator"
// or "thread".
type CallerInfo struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Thread  string `json:"thread,omitempty"`
}
