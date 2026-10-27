package proto

import "time"

// Open questions (docs/SPEC.md §3.3, Ask): tm's own question on a
// session, which the server keeps there (SessionInfo.Question) until it
// is answered through session.answer or goes: a thread's approved
// command that asks first (tm run). The agent's own question menu
// (Question.Screen) shows there too while the agent is blocked on it.
const (
	// MethodSessionAnswer answers one question of a session's open one
	// (SessionAnswerParams) and returns SessionAnswerResult.
	MethodSessionAnswer = "session.answer"
)

// ErrNoQuestion means the session has no open question (none asked, or
// it is gone).
const ErrNoQuestion = "no-question"

// Question is an open question menu: one or more questions, each
// answered on its own.
type Question struct {
	// Tool is the agent's tool that opened the menu (AskUserQuestion).
	Tool      string         `json:"tool,omitempty"`
	Questions []QuestionItem `json:"questions"`
	// Since is when the menu opened (set by the server).
	Since time.Time `json:"since,omitzero"`
	// Escape: only the user answers it, never the coordinator or an
	// agent (tm's own question before an approved command that asks
	// first runs outside the sandbox).
	Escape bool `json:"escape,omitempty"`
	// Screen: the agent's own menu (its [answer] tool's call, Claude's
	// AskUserQuestion), open on its screen: session.answer types the
	// answer there, one question at a time in order, a single option or
	// free text (a multi-select's options are picked in its pane).
	Screen bool `json:"screen,omitempty"`
}

// QuestionItem is one question of a menu. Answered says whether
// session.answer already gave it its answer (Answer).
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Options     []QuestionOption `json:"options"`
	Answered    bool             `json:"answered,omitempty"`
	Answer      string           `json:"answer,omitempty"`
}

// QuestionOption is one option of a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// SessionAnswerParams are the params of session.answer: question Index
// (from 0) of the session's open question gets Answer, an option's
// label, labels joined with ", " for a multi-select, or free text.
type SessionAnswerParams struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Answer string `json:"answer"`
	// Question, when set, is the text question Index must still ask,
	// unanswered and single-select, and Answer must be one of its
	// options' labels; refused otherwise. A console's question.answer
	// binding sets it, so a menu replaced after the human confirmed gets
	// no answer meant for another (docs/SPEC.md §5.3, Action binding).
	Question string `json:"question,omitempty"`
	// Relay is the role whose answer this is, when tm's own commands
	// send it for an agent (the coordinator's tm thread answer):
	// "coordinator" never answers while the session is on an escape's
	// prompt (SessionKeysParams.Relay).
	Relay string `json:"relay,omitempty"`
}

// SessionAnswerResult says how many questions still wait for an answer;
// at 0 the answers went to the question's asker.
type SessionAnswerResult struct {
	Remaining int `json:"remaining"`
}

// MethodSessionApprove answers the permission prompt open on an agent's
// screen with one of its numbered options (SessionApproveParams), once
// the server has checked what tm thread approve checks: the agent's
// state is blocked on a permission, the screen shows the dialog and has
// the option, which allows once only (or for the session, from the
// user in the Mac app, with Session). An escape's prompt (a command
// outside the sandbox) is the user's alone, and needs Escape. The
// server journals the answer as thread.approve. The human's; tm's own
// command sends it for the coordinator with Relay.
const MethodSessionApprove = "session.approve"

// Errors of session.approve, besides refused and human-only.
const (
	// ErrNotPermission: the agent isn't blocked on a permission prompt.
	ErrNotPermission = "not-permission"
	// ErrNoDialog: the screen shows no permission dialog, or none with
	// that option.
	ErrNoDialog = "no-dialog"
	// ErrAlwaysAllow: the option allows more than once ("don't ask
	// again"), which only the user picks, in the agent's pane.
	ErrAlwaysAllow = "always-allow"
	// ErrEscapeUnconfirmed: the prompt asks to run a command outside
	// the sandbox and the call didn't say the user confirmed that.
	ErrEscapeUnconfirmed = "escape-unconfirmed"
	// ErrDeviceEscapeOff: a paired device asked to answer a prompt that
	// runs a command outside the sandbox, which devices never answer
	// (docs/SPEC.md §16.15).
	ErrDeviceEscapeOff = "device-escape-off"
	// ErrSessionUnconfirmed: the option allows for the rest of the
	// session and the call didn't say the user confirmed that.
	ErrSessionUnconfirmed = "session-unconfirmed"
)

// SessionApproveParams are the params of session.approve: option
// Choice (from 1, default 1: allow once) of the dialog on session ID's
// screen.
type SessionApproveParams struct {
	ID     string `json:"id"`
	Choice int    `json:"choice,omitempty"`
	// Check runs every check and answers what would be chosen, without
	// sending a key.
	Check bool `json:"check,omitempty"`
	// Escape says the user confirmed, in words naming the command, that
	// it runs outside the sandbox: an escape's prompt is refused
	// without it.
	Escape bool `json:"escape,omitempty"`
	// Session says the user confirmed, in words naming the tool or
	// command, that the agent may run it for the rest of its session
	// without asking: an option that allows for the session is refused
	// without it, and from anyone but the user in the Mac app.
	Session bool `json:"session,omitempty"`
	// Relay is the role whose answer this is, when tm's own commands
	// send it for an agent (the coordinator's tm thread approve):
	// "coordinator" is refused on an escape's prompt, and unless the
	// project's coordinator_approves is on.
	Relay string `json:"relay,omitempty"`
}

// SessionApproveResult is the dialog as the screen shows it: its
// question, the chosen option and every option, in order. Escape: the
// prompt asks to run a command outside the sandbox. Scopes: how long
// each option allows, "once", "session" or "persistent" (agent.Scope).
// Sent: the key went (not for Check).
type SessionApproveResult struct {
	Question string   `json:"question,omitempty"`
	Option   string   `json:"option"`
	Options  []string `json:"options"`
	Scopes   []string `json:"scopes,omitempty"`
	Escape   bool     `json:"escape,omitempty"`
	Sent     bool     `json:"sent,omitempty"`
}
