package appapi

import (
	"errors"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The guard's refusals (app API 15): the one exception to settings and
// access staying out of the app API (docs/SPEC.md §3.8). The guard's log
// is the human's, so the bridge reads and answers it itself, in its own
// process as the console does, never through the server, and only when
// the caller is the human (bridge.human). An answer settles one refusal
// as the console's Guard refusals do (guardlog.Answer): allowed here,
// for all projects, or kept refusing.
const (
	// MethodGuardRefusals lists the refusals waiting on an answer.
	MethodGuardRefusals = "guard.refusals"
	// MethodGuardDecide answers one (GuardDecideParams).
	MethodGuardDecide = "guard.decide"
)

// The guard methods' error codes.
const (
	// ErrHumanOnly: the bridge's caller isn't the human, as the server
	// told it.
	ErrHumanOnly = "human-only"
	// ErrGone: the refusal is no longer waiting (answered elsewhere).
	ErrGone = "gone"
	// ErrGuardRefused: the answer can't be given (the floor's refusal,
	// a pattern a credentials allowance can't name) or written.
	ErrGuardRefused = "refused"
)

type (
	// GuardDecideParams are guard.decide's: the refusal's id and the
	// answer, "here" (allow for its project), "all" (for all
	// projects; the app confirms it first) or "keep" (keep refusing).
	GuardDecideParams struct {
		ID  string `json:"id"`
		How string `json:"how"`
	}
)

// GuardRefusals is guard.refusals' result: every project's refusals
// waiting on an answer, the latest refused first.
type GuardRefusals struct {
	Refusals []GuardRefusal `json:"refusals"`
}

// GuardRefusal is one refused call: by whom (its thread, or its
// session's role), under which rule, with which tool, the journal's
// Summary and the whole Subject (the command line, or the file when
// Path) an allowance names. Floor: it is never allowed, only kept.
type GuardRefusal struct {
	ID      string    `json:"id"`
	Project string    `json:"project"`
	Thread  string    `json:"thread,omitempty"`
	Role    string    `json:"role,omitempty"`
	Rule    string    `json:"rule"`
	Tool    string    `json:"tool"`
	Summary string    `json:"summary,omitempty"`
	Subject string    `json:"subject"`
	Path    bool      `json:"path,omitempty"`
	Floor   bool      `json:"floor,omitempty"`
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Count   int       `json:"count"`
}

// GuardDecided is guard.decide's result: what to tell the user.
type GuardDecided struct {
	Message string `json:"message"`
}

func guardRefusalOf(e guardlog.Entry) GuardRefusal {
	return GuardRefusal{ID: e.ID, Project: e.Project, Thread: e.Thread, Role: e.Role, Rule: e.Rule, Tool: e.Tool,
		Summary: e.Summary, Subject: e.Subject, Path: e.Path, Floor: e.Floor, First: e.First, Last: e.Last, Count: e.Count}
}

// guardRefusals answers guard.refusals from the log.
func guardRefusals(b *bridge, _ struct{}) (GuardRefusals, error) {
	if !b.human() {
		return GuardRefusals{}, humanOnly()
	}
	list, err := guardlog.List(b.o.Paths.Home, "")
	out := GuardRefusals{Refusals: []GuardRefusal{}}
	for _, e := range list {
		out.Refusals = append(out.Refusals, guardRefusalOf(e))
	}
	if err != nil {
		return out, &Error{Code: ErrGuardRefused, Message: "the guard's log can't be read: " + err.Error()}
	}
	return out, nil
}

// guardDecide answers guard.decide: the refusal is the log's own entry
// by id, never what the app says of it, and the thread's running session
// is told when it is allowed.
func guardDecide(b *bridge, p GuardDecideParams) (GuardDecided, error) {
	if !b.human() {
		return GuardDecided{}, humanOnly()
	}
	switch p.How {
	case guardlog.AllowHere, guardlog.AllowAll, guardlog.Keep:
	default:
		return GuardDecided{}, &Error{Code: ErrBadParams, Message: `guard.decide: how is "here", "all" or "keep"`}
	}
	list, err := guardlog.List(b.o.Paths.Home, "")
	if err != nil {
		return GuardDecided{}, &Error{Code: ErrGuardRefused, Message: "the guard's log can't be read: " + err.Error()}
	}
	var e *guardlog.Entry
	for i := range list {
		if list[i].ID == p.ID {
			e = &list[i]
		}
	}
	if e == nil {
		return GuardDecided{}, &Error{Code: ErrGone, Message: guardlog.ErrGone.Error()}
	}
	who := caller.Caller{Kind: caller.Human, Via: caller.ViaApp}
	msg, err := guardlog.Answer(who, b.o.Paths.Home, *e, p.How, func(text string) {
		if id := b.guardSession(*e); id != "" {
			var res proto.SessionPromptResult
			b.serverCall(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: id, Text: text}, &res)
		}
	})
	switch {
	case errors.Is(err, guardlog.ErrGone):
		return GuardDecided{}, &Error{Code: ErrGone, Message: err.Error()}
	case err != nil:
		return GuardDecided{}, &Error{Code: ErrGuardRefused, Message: err.Error()}
	}
	return GuardDecided{Message: msg}, nil
}

// guardSession is the session to tell about refusal e: its thread's
// running session, or the one refused it, while the server has it.
func (b *bridge) guardSession(e guardlog.Entry) string {
	var res proto.SessionListResult
	if b.serverCall(proto.MethodSessionList, nil, &res) != nil {
		return ""
	}
	for _, s := range res.Sessions {
		if e.Thread != "" && s.Project == e.Project && s.Thread == e.Thread && s.Role == proto.RoleThread {
			return s.ID
		}
	}
	for _, s := range res.Sessions {
		if s.ID == e.Session {
			return s.ID
		}
	}
	return ""
}

func humanOnly() *Error {
	return &Error{Code: ErrHumanOnly, Message: "the guard's refusals are the user's: only they answer them"}
}
