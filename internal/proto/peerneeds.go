package proto

import "time"

// Paired needs (docs/SPEC.md §16.16): what waits on the user of a shared
// project, in full, for a paired server whose needs switch here is view
// or answer. Off, the default, shows that server only what peer.watch
// shows anyone: the needs list with each question's text masked
// (PeerQuestion). View and answer carry the needs themselves on
// peer.watch (PeerWatch.Needs): a question's menu, an approval's dialog
// and its options, a task in review, the coordinator's open questions.
// Answer shows what view shows, and takes the paired server's user's
// acts on them (peer.act, below).

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

// Acting on a paired server's needs (docs/SPEC.md §16.16): at a needs
// switch of answer, the paired server's user answers a question menu,
// approves a dialog's option, accepts a task in review or sends it back,
// or has the coordinator open its questions. The owning server takes
// that server's word that its user did so, and checks each act as it
// checks its own user's, plus: the need is still the one the user saw
// (its id, and the act's guard: the question, the option, the task's
// status), the act carries a nonce the owner gave out (peer.nonce), and
// it is never an escape's.

// The link's methods of paired needs, behind CapNeeds (internal/peer):
// peer.needs reads a shared project's needs at view or answer, peer.nonce
// gives a nonce for the next act and peer.act acts, at answer only.
const (
	MethodPeerNeeds = "peer.needs"
	MethodPeerNonce = "peer.nonce"
	MethodPeerAct   = "peer.act"
)

// MethodPeerNeedAct is the console's way to peer.act (PeerNeedActParams):
// this server asks the paired server for a nonce and sends the act, as
// its user's. The human's only; a paired device at answer level calls it
// too, and the owner journals it with the phone's name.
const MethodPeerNeedAct = "peer.need-act"

// CapNeedAct says the server answers peer.need-act.
const CapNeedAct = "need-act"

// The acts on a paired server's need, by PeerNeed kind: ActAnswer a
// question menu's question (NeedQuestion), ActApprove an approval's
// option (NeedApproval), ActAccept and ActSendBack a task in review
// (NeedReview), ActQuestions the coordinator's open questions (NeedAsk).
const (
	ActAnswer    = "answer"
	ActApprove   = "approve"
	ActAccept    = "accept"
	ActSendBack  = "send-back"
	ActQuestions = "questions"
)

// ActsOf are the acts a need of kind takes; none for a need only its
// server's user acts on (blocked, ci, queue) or an escape.
func ActsOf(n PeerNeed) []string {
	if n.Escape {
		return nil
	}
	switch n.Kind {
	case NeedQuestion:
		if n.Question != nil {
			return []string{ActAnswer}
		}
	case NeedApproval:
		if n.Approval != nil {
			return []string{ActApprove}
		}
	case NeedReview:
		return []string{ActAccept, ActSendBack}
	case NeedAsk:
		return []string{ActQuestions}
	}
	return nil
}

// Refusals of an act on a paired server's need.
const (
	// ErrNeedsOff: the owner's needs switch for this server isn't
	// answer (view shows the needs, nothing more).
	ErrNeedsOff = "needs-off"
	// ErrNeedChanged: the need isn't what the user saw any more (it
	// was answered, its menu or dialog changed, the task moved): look
	// again.
	ErrNeedChanged = "need-changed"
	// ErrBadNonce: the act's nonce isn't one the owner gave this
	// server, or was used, or is too old.
	ErrBadNonce = "bad-nonce"
	// ErrEscapeHere: the need asks to run a command outside the
	// sandbox, which only the owner's user answers, there.
	ErrEscapeHere = "escape-here"
)

// PeerAct is an act on one need of a paired server's shared project:
// what the user there chose, with the guard that binds it to what they
// saw.
type PeerAct struct {
	Project string `json:"project"`
	// Need is the need's id (PeerNeed.ID) and Act what is done to it
	// (ActAnswer …).
	Need string `json:"need"`
	Act  string `json:"act"`
	// ActAnswer: question Index of the menu (from 0), answered with
	// Answer, one of its options' labels; Question is its text, the
	// guard.
	Index    int    `json:"index,omitempty"`
	Answer   string `json:"answer,omitempty"`
	Question string `json:"question,omitempty"`
	// Picks answer a multi-select question in place of a label: its
	// options picked, by index (from 0); Answer is then their labels
	// joined with ", ", as the user saw them, the guard (T900).
	Picks []int `json:"picks,omitempty"`
	// ActApprove: the dialog's option Choice (from 1); Option is its
	// text, the guard.
	Choice int    `json:"choice,omitempty"`
	Option string `json:"option,omitempty"`
	// ActAccept, ActSendBack: Status is the task's status as the user
	// saw it, the guard; Note is send-back's why.
	Status string `json:"status,omitempty"`
	Note   string `json:"note,omitempty"`
}

// PeerActParams are peer.act's params: the act and a nonce peer.nonce
// gave. Device names the phone of the paired server's whose user acted,
// "" for that server's own console: the owner journals it.
type PeerActParams struct {
	PeerAct
	Nonce  []byte `json:"nonce"`
	Device string `json:"device,omitempty"`
}

// PeerActResult is what an act did: Asked, for accept and send-back,
// false when an item already asked the coordinator the same;
// Remaining, for answer, the menu's questions still open; Option, for
// approve, the option sent; Open, for questions, how many the
// coordinator puts to the user.
type PeerActResult struct {
	Act       string `json:"act"`
	Asked     bool   `json:"asked,omitempty"`
	Remaining int    `json:"remaining,omitempty"`
	Option    string `json:"option,omitempty"`
	Open      int    `json:"open,omitempty"`
}

// PeerNonce is peer.nonce's result: good for one act, for
// PeerNonceLife.
type PeerNonce struct {
	Nonce []byte `json:"nonce"`
}

// PeerNonceLife is how long a peer.nonce waits for its act.
const PeerNonceLife = 2 * time.Minute

// PeerNeedsParams are peer.needs' params.
type PeerNeedsParams struct {
	Project string `json:"project"`
}

// PeerNeedsResult is peer.needs' result: the shared project's needs now,
// as PeerWatch.Needs has them.
type PeerNeedsResult struct {
	Needs []PeerNeed `json:"needs"`
}

// PeerNeedActParams are peer.need-act's params: the paired server Peer
// (its alias or name) and the act on its need.
type PeerNeedActParams struct {
	Peer string `json:"peer"`
	PeerAct
}
