package proto

import "time"

// Paired needs (docs/SPEC.md §16.16): what waits on the user of a shared
// project, in full, for a paired server whose needs switch here is view
// or answer. Off, the default, shows that server only what peer.watch
// shows anyone: the needs list with each question's text masked
// (PeerQuestion). View and answer carry the needs themselves on
// peer.watch (PeerWatch.Needs): a question's menu, an approval's dialog
// and its options, a task in review, the coordinator's open questions.
// Answer is the level the acts on them need (phase 2); until then it
// shows what view shows.

// The needs switch's levels, per paired server: what it may see (and
// later do) of this server's needs. These are the wire's and
// peers.json's values.
const (
	NeedsOff    = "off"
	NeedsView   = "view"
	NeedsAnswer = "answer"
)

// ParseNeeds reads a needs level as a user, a file or a peer gives it.
func ParseNeeds(s string) (string, bool) {
	switch s {
	case NeedsOff, NeedsView, NeedsAnswer:
		return s, true
	}
	return "", false
}

// NeedsShown says whether level lets a paired server see the needs in
// full: view or answer.
func NeedsShown(level string) bool { return level == NeedsView || level == NeedsAnswer }

// The kinds of a PeerNeed.
const (
	// NeedQuestion: a menu open on a session, tm's own question (tm
	// run's) or the agent's (Question.Screen); or a thread's question
	// in words only (tm status --needs-you, Text).
	NeedQuestion = "question"
	// NeedApproval: a permission dialog on a session's screen.
	NeedApproval = "approval"
	// NeedAsk: one of the coordinator's open questions (tm ask).
	NeedAsk = "ask"
	// NeedReview: a task in review, to accept or send back.
	NeedReview = "review"
	// NeedBlocked: a blocked task with no question from its thread.
	NeedBlocked = "blocked"
	// NeedCI: a task whose thread's PR fails its checks or conflicts.
	NeedCI = "ci"
	// NeedQueue: a session's prompts held while its agent idles.
	NeedQueue = "queue"
)

// PeerNeed is one need of a shared project as a paired server at needs
// view or answer sees it. ID names it while it lasts: a new menu on the
// same session is a new id, so an act meant for one never reaches
// another. The rest is what the user there reads to decide, and what an
// act will carry back as its guard (the question's text, the dialog's
// option, the task's status).
type PeerNeed struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Session, Role, Thread and Task say whose need it is: a session's
	// id and role (coordinator, thread), its thread, and the task
	// ("T12").
	Session string `json:"session,omitempty"`
	Role    string `json:"role,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Task    string `json:"task,omitempty"`
	// Title is the task's title, else the thread's.
	Title string `json:"title,omitempty"`
	// Status is the task's status (review, blocked, in-progress …).
	Status string `json:"status,omitempty"`
	// Text is what the need says in words: a thread's question (tm
	// status --needs-you), a held queue's note.
	Text string `json:"text,omitempty"`
	// Question is the menu a NeedQuestion opens, or the one question a
	// NeedAsk is: every question with its options, answered ones marked.
	Question *Question `json:"question,omitempty"`
	// Approval is a NeedApproval's dialog as the screen shows it.
	Approval *PeerApproval `json:"approval,omitempty"`
	// Escape: the question or dialog asks to run a command outside the
	// sandbox, which only the user answers, on the owning server. It is
	// listed so the far user knows, never with its menu, dialog or
	// options: nothing about it can be answered from there.
	Escape bool `json:"escape,omitempty"`
	// PR, PRURL, PRNumber and Mergeable are the thread's pull request
	// (WatchNeed's); Asked is the inbox item that already asks the
	// coordinator about the task.
	PR        string `json:"pr,omitempty"`
	PRURL     string `json:"pr_url,omitempty"`
	PRNumber  int    `json:"pr_number,omitempty"`
	Mergeable bool   `json:"mergeable,omitempty"`
	Asked     string `json:"asked,omitempty"`
	// Reports counts the thread's reports, the latest one a review's.
	Reports int `json:"reports,omitempty"`
	// Since is when it began: the menu opened, the question was asked.
	Since time.Time `json:"since,omitzero"`
}

// PeerApproval is a permission dialog on a session's screen: its
// question, its options in order (numbered from 1) and how long each
// allows ("once", "session", "persistent").
type PeerApproval struct {
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options"`
	Scopes   []string `json:"scopes,omitempty"`
}
