package proto

import "time"

// A program's rolled-up Needs you (the programs design §7.1): what waits
// on the user in each of its member projects, built as a paired server's
// view of a shared project is (PeerNeed), plus the members' link needs
// and a failing PR host, ranked across the members in one queue.

// MethodProgramNeeds reads a program's rolled-up Needs you
// (ProgramNeedsParams): the human's for any program, a director's for
// its own; refused programs-off while programs are off.
const MethodProgramNeeds = "program.needs"

// The kinds of a ProgramNeed besides PeerNeed's (NeedQuestion …).
const (
	// NeedLinkHeld: a message of the member's that waits for the
	// user's OK over a link (link approve, drop).
	NeedLinkHeld = "link-held"
	// NeedLinkFile: a file offered to the member over a link
	// (accept-file, decline-file).
	NeedLinkFile = "link-file"
	// NeedLinkRequest: a paired server asks to link to the member
	// (accept, decline).
	NeedLinkRequest = "link-request"
	// NeedHost: the member's PR host fails its polls (PR states go
	// stale until it answers again); the user fixes its sign-in.
	NeedHost = "host"
)

// ProgramNeed is one row of a program's Needs you: a need of member
// Project (on paired server Server, "" for this one) as PeerNeed has it,
// with what the program's queue ranks and offers. (Server, Project, ID)
// names it; ID is the member's own need id, which its acts take
// (peer.act's on a paired server, the project's own methods here).
type ProgramNeed struct {
	PeerNeed
	Project string `json:"project"`
	Server  string `json:"server,omitempty"`
	// Priority is the need's task's (must, should, could), "" for none
	// or no task.
	Priority string `json:"priority,omitempty"`
	// Peer is a link need's other project, Link its id there (M-, X-
	// or a link request's), which link.act takes.
	Peer string `json:"peer,omitempty"`
	Link string `json:"link,omitempty"`
	// Acts are the human acts the consoles offer on the row: the need's
	// own (ActsOf: answer, approve, accept, send-back, questions; a link
	// need's link.act actions), then those of the needs folded into it
	// (Also), each once.
	Acts []string `json:"acts,omitempty"`
	// Also are the member's other needs that are this one too, folded
	// into the row (need-level dedupe): a coordinator's open question
	// about the task in review, kept whole so its own act still runs.
	Also []PeerNeed `json:"also,omitempty"`
	// Similar are other members' open coordinator questions that ask
	// what this one asks (§7.1: alike words and the same options), as
	// Q7@acme-web: a hint that the director may merge them (tm program
	// ask merge); nothing merges them by itself.
	Similar []string `json:"similar,omitempty"`
	// Program: the row is one of the program's own questions (Kind
	// NeedAsk, Project the program's slug), which the user answers
	// once for the member questions it covers, Merged (acme-api/Q3);
	// those have no rows of their own while it is open.
	Program bool     `json:"program,omitempty"`
	Merged  []string `json:"merged,omitempty"`
}

// MethodProgramNeedsOpen asks the consoles to open a program's needs
// dialog for the user over its director's pane (tm program needs
// --open): the director opens it, never fills it (§7.3). Its params are
// ProgramNeedsParams, the answer ProgramNeedsOpenResult; refused
// not-running without a director session, and as program.needs is.
const MethodProgramNeedsOpen = "program.needs.open"

// ProgramNeedsOpenResult says what program.needs.open did: the
// director's session stamped (SessionInfo.NeedsOpen) and how many
// needs wait; with none it stamps nothing.
type ProgramNeedsOpenResult struct {
	Session string `json:"session,omitempty"`
	Waiting int    `json:"waiting"`
}

// ProgramNeedsParams are program.needs' params.
type ProgramNeedsParams struct {
	Program string `json:"program"`
}

// ProgramNeedsResult is a program's Needs you now: its needs ranked
// (escape and approval first, then a thread's question menu, review,
// the coordinator's question, a red PR, link needs, held prompts and
// blocked; then by the task's priority, the oldest first, and the
// members' order), and every member's count.
type ProgramNeedsResult struct {
	Program string        `json:"program"`
	Needs   []ProgramNeed `json:"needs"`
	Members []NeedMember  `json:"members"`
	// Quiet counts the members with nothing waiting: "9 other
	// projects: nothing waiting".
	Quiet int `json:"quiet,omitempty"`
}

// NeedMember is one member of a program as its Needs you counts it, in
// the program's order: Needs its rows in the queue. A paired member's
// needs aren't in the queue yet: Count is what that server's mirror
// says waits there ("3 waiting on mini"), Synced when it said so. Error
// says why a member's needs don't read (a project that doesn't open).
type NeedMember struct {
	Project string    `json:"project"`
	Server  string    `json:"server,omitempty"`
	Needs   int       `json:"needs"`
	Count   int       `json:"count,omitempty"`
	Synced  time.Time `json:"synced,omitzero"`
	Error   string    `json:"error,omitempty"`
}
