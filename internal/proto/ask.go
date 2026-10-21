package proto

import "time"

// Open questions (docs/SPEC.md §3.3, Ask): tm's own question on a
// session, which the server keeps there (SessionInfo.Question) until it
// is answered through session.answer or goes: a thread's approved
// command that asks first (tm run).
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
