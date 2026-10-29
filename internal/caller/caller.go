// Package caller says who is calling tm: the human, a project's
// coordinator, one of its threads (docs/SPEC.md §11.1), or a user mod
// through the server's broker (§5.3).
//
// The server decides this from the peer pid of the socket connection.
// Until the server serves project calls, FromEnv reads the session
// environment the server gives every hosted process (§3.4). That check is
// soft, as the spec says plainly: an agent can unset the variables. The
// agent's own permission rules and sandbox are the second layer (§5.2).
package caller

import "os"

// Kind is the class of caller.
type Kind string

const (
	Human       Kind = "human"
	Coordinator Kind = "coordinator"
	Thread      Kind = "thread"
	// Ticker is the server's own ticker acting on the human's settings,
	// e.g. resolving a thread after its PR merged (docs/SPEC.md §9). It
	// has a human's rights and is journaled as "ticker".
	Ticker Kind = "ticker"
	// Mod is a user mod calling through the server's broker (the mods
	// design §7). It never comes from the environment: the broker names
	// it. It has fewer rights than any agent, and is journaled as
	// "mod:<id>".
	Mod Kind = "mod"
	// Peer is a paired server reading through a link (docs/SPEC.md
	// §16.9). It never comes from the environment or the socket: the
	// link's dispatcher names it, and it reaches only the peer.* methods.
	// It has no rights beyond reading shared projects, and is journaled
	// as "peer:<alias>".
	Peer Kind = "peer"
)

// Caller is one tm invocation's identity.
type Caller struct {
	Kind    Kind
	Project string // the agent's project slug; empty for the human
	Thread  string // the thread id, e.g. "t-0003", for Kind == Thread
	Mod     string // the mod's id, e.g. "auto-labeller", for Kind == Mod
	Alias   string // the paired server's alias, e.g. "mini", for Kind == Peer
	// Session is the session the server's pid walk placed the caller in
	// (any role, a shell's too), which session-addressed calls are scoped
	// to (docs/SPEC.md §11.1). The environment never sets it.
	Session string
	// Via is the console a human's call came through, when its hello
	// says so: ViaApp for the Mac app's bridge (tm app bridge,
	// docs/SPEC.md §3.8). The rights are the human's; only the journal
	// tells them apart.
	Via string
	// Device is the paired device a human's call came from, through its
	// link's app API on the server (docs/SPEC.md §16.15): journaled as
	// "human (iphone: <name>)". Its rights are its level's, which the
	// device's app API enforces; the server refuses it escape approvals.
	Device string
	// Peer is the paired server a human's act came through (peer.act,
	// docs/SPEC.md §16.16): its user, at a needs switch of answer here,
	// answered one of this server's needs. Journaled as "human (peer:
	// <alias>)", or "human (peer: <alias> › iphone: <name>)" when a
	// phone of that server's acted through it. It reaches only the five
	// answer acts, which peer.act calls itself, and never an escape.
	Peer string
}

// ViaApp is Via for the Mac app's calls, journaled as "human (app)".
const ViaApp = "app"

// ForDevice is the caller for the paired device name's calls.
func ForDevice(name string) Caller { return Caller{Kind: Human, Via: "iphone: " + name, Device: name} }

// ForPeerHuman is the caller for an act of the paired server alias's
// user (peer.act): device names the phone of that server's that acted
// through it, "" for its own console. Unlike ForPeer, it is the human's:
// peer.act lets it reach the answer acts only.
func ForPeerHuman(alias, device string) Caller {
	via := "peer: " + alias
	if device != "" {
		via += " › iphone: " + device
	}
	return Caller{Kind: Human, Via: via, Peer: alias}
}

// Remote says whether the caller is the human acting from elsewhere: a
// paired device, or through a paired server. Neither answers an
// escape's prompt, which is the user's at this computer alone.
func (c Caller) Remote() bool { return c.Device != "" || c.Peer != "" }

// ForMod is the caller for mod id's brokered calls.
func ForMod(id string) Caller { return Caller{Kind: Mod, Mod: id} }

// ForPeer is the caller for the paired server alias's link requests.
func ForPeer(alias string) Caller { return Caller{Kind: Peer, Alias: alias} }

// IsAgent reports whether the caller is not the human: a coordinator or
// thread agent, a mod or a paired server. Every human-only check refuses
// them all.
func (c Caller) IsAgent() bool {
	return c.Kind == Coordinator || c.Kind == Thread || c.Kind == Mod || c.Kind == Peer
}

// String is the name used in the journal: "human", "human (app)",
// "coordinator", the thread id or "mod:<id>".
func (c Caller) String() string {
	switch {
	case c.Kind == Human && c.Via != "":
		return "human (" + c.Via + ")"
	case c.Kind == Thread && c.Thread != "":
		return c.Thread
	case c.Kind == Mod:
		return "mod:" + c.Mod
	case c.Kind == Peer:
		return "peer:" + c.Alias
	}
	return string(c.Kind)
}

// Environment variables set by the server on hosted sessions (§3.4).
// TERMINATR_ROLE is the session's role: coordinator, thread or shell.
const (
	EnvSession = "TERMINATR_SESSION"
	EnvRole    = "TERMINATR_ROLE"
	EnvProject = "TERMINATR_PROJECT"
	EnvThread  = "TERMINATR_THREAD"
)

// FromEnv derives the caller from the environment. Outside a hosted
// session, or in a shell session, the caller is the human.
func FromEnv() Caller { return FromLookup(os.Getenv) }

// FromLookup is FromEnv with an injectable getenv, for tests.
func FromLookup(getenv func(string) string) Caller {
	if getenv(EnvSession) == "" {
		return Caller{Kind: Human}
	}
	switch getenv(EnvRole) {
	case string(Coordinator):
		return Caller{Kind: Coordinator, Project: getenv(EnvProject)}
	case string(Thread):
		return Caller{Kind: Thread, Project: getenv(EnvProject), Thread: getenv(EnvThread)}
	case "":
		// No role but a thread id: err on the side of the narrower rights.
		if getenv(EnvThread) != "" {
			return Caller{Kind: Thread, Project: getenv(EnvProject), Thread: getenv(EnvThread)}
		}
	}
	return Caller{Kind: Human}
}

// rank orders kinds by how much they may do: the narrower, the higher.
func rank(k Kind) int {
	switch k {
	case Peer:
		return 4
	case Mod:
		return 3
	case Thread:
		return 2
	case Coordinator:
		return 1
	}
	return 0
}

// Narrower returns whichever of a and b has fewer rights; b wins a tie.
// The environment's view (a, §3.4) and the server's (b, from the peer
// pid, §11.1) are combined this way, so neither unsetting the variables
// nor a process that left its session's tree widens an agent's rights.
func Narrower(a, b Caller) Caller {
	if rank(b.Kind) >= rank(a.Kind) {
		return b
	}
	return a
}
