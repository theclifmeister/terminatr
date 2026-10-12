// Package modaction is the core's table of the actions a mod may bind
// (the mods design §11, docs/SPEC.md §5.3, Action binding): the stable
// ids a mod's row or notification button names, with a target, and the
// one precondition check the console runs when the human presses the
// key and the broker runs for $.actions.available.
//
// Mods render, the core acts. A mod only names an id and a target; the
// label, the key, the confirmation and the run are the core's, and there
// is no way for a mod to run an action itself. The table is data a mod
// can read ($.actions.list); the precondition is code a mod can't
// change, run against live state, never against what the mod published.
package modaction

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Target is what a bound action acts on: a task or a project, a
// session, or an option of the question a session's menu asks. Its
// fields are mod API v1's (modbus.Target, which converts to it); the
// package keeps its own so that proto, which carries the notification
// buttons, can depend on it.
type Target struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
	// Question is the text of a question the session's open menu asks,
	// and Option the label of one of its options (mod API 1.3).
	Question string `json:"question,omitempty"`
	Option   string `json:"option,omitempty"`
}

// TargetOf is the target of a binding's fields, as a widget row or a
// notification button carries them.
func TargetOf(m map[string]string) Target {
	return Target{Project: m["project"], Task: m["task"], Thread: m["thread"], Session: m["session"],
		Question: m["question"], Option: m["option"]}
}

// The kinds of inbox item a task action drops: KindDelegate,
// KindAccept and KindSendBack (a test keeps them equal).
const (
	KindDelegate = "delegate"
	KindAccept   = "accept"
	KindSendBack = "send-back"
)

// Stable is how long a row's bindings must be unchanged before a press
// on them counts (the design's 300 ms rule): a widget that changes
// under the cursor can't turn a press meant for one row into another.
const Stable = 300 * time.Millisecond

// The target kinds an action takes.
const (
	OnTask    = "task"    // project and task
	OnProject = "project" // project
	OnSession = "session" // session
	// OnQuestion is session, question and option: an option of a
	// question the session's open menu asks.
	OnQuestion = "question"
)

// Action is one bindable action.
type Action struct {
	// ID is its stable name, the one the console's key tables give it
	// (tui.KeyActions); a renamed id keeps its old name as an alias for
	// the rest of the major.
	ID string `json:"id"`
	// Label is the word the core shows for it, in a row's hints and its
	// confirmation: a mod can't name it otherwise.
	Label string `json:"label"`
	// Key is its default key; a console shows the key its keymap binds.
	Key string `json:"key"`
	// On is the kind of target it takes. A project target may also name
	// a task (a row in a task list), which a project action ignores.
	On string `json:"on"`
	// Confirm says the core asks before it runs (a navigation doesn't).
	Confirm bool `json:"confirm"`
	// Precondition says, for a mod's author, when it is available.
	Precondition string `json:"precondition"`
	// Consent, when set, is what the consent screen says a mod binding
	// it may offer: a mod binds it only when its manifest lists it under
	// [permissions] actions and the human granted that (mod API 1.3).
	// The other actions need no grant of their own.
	Consent string `json:"consent,omitempty"`
}

// Actions are the bindable actions of mod API v1. Only these may be
// named; any other id is refused when the row is published and
// "not available" when pressed.
var Actions = []Action{
	{ID: "task.show", Label: "show", Key: "enter", On: OnTask,
		Precondition: "the task is on the project's board"},
	{ID: "task.delegate", Label: "delegate", Key: "D", On: OnTask, Confirm: true,
		Precondition: "the task is open, ready or blocked, and nothing already asks the coordinator about it"},
	{ID: "task.accept", Label: "accept", Key: "A", On: OnTask, Confirm: true,
		Precondition: "the task is in review, and nothing already asks the coordinator about it"},
	{ID: "task.send-back", Label: "send back", Key: "x", On: OnTask, Confirm: true,
		Precondition: "the task is in review or done, and nothing already asks the coordinator about it; the human writes the note"},
	{ID: "task.new", Label: "new task", Key: "+", On: OnProject,
		Precondition: "the project exists; the human picks a template, if any, and types the title"},
	{ID: "task.coordinator", Label: "coordinator", Key: "c", On: OnProject,
		Precondition: "the project exists"},
	{ID: "project.activate", Label: "activate / deactivate", Key: "space", On: OnProject, Confirm: true,
		Precondition: "the project exists; it toggles, and the confirmation names which"},
	{ID: "session.attach", Label: "attach", Key: "enter", On: OnSession,
		Precondition: "the session is running"},
	{ID: "session.clear", Label: "clear", Key: "C", On: OnSession, Confirm: true,
		Precondition: "the session is running and its agent has a clear prompt; the clear waits until the agent is idle",
		Consent:      "clear an agent session's conversation"},
	{ID: "question.answer", Label: "answer", Key: "a", On: OnQuestion, Confirm: true,
		Precondition: "the session's open question menu asks the question, unanswered and single-select, and the option is one of its labels",
		Consent:      "answer an agent's question with one of its options"},
}

// Lookup is action id, ok false for one that isn't bindable.
func Lookup(id string) (Action, bool) {
	i := slices.IndexFunc(Actions, func(a Action) bool { return a.ID == id })
	if i < 0 {
		return Action{}, false
	}
	return Actions[i], true
}

// State is the live state a precondition is checked against: the
// server's files, or the console's view of them just loaded.
type State interface {
	// Project says whether project slug exists.
	Project(slug string) bool
	// Task is slug's task ref ("T12"), nil when the board has none.
	Task(slug, ref string) *tasks.Task
	// Asked is the kind of the unhandled item that asks slug's
	// coordinator about task ref, "" for none (project.TaskAsked).
	Asked(slug, ref string) string
	// Session says whether session id is running.
	Session(id string) bool
	// Menu is the question menu open on session id, nil for none.
	Menu(id string) []MenuQuestion
}

// MenuQuestion is one question of a session's open question menu, as a
// precondition sees it.
type MenuQuestion struct {
	Question string
	Multi    bool
	Answered bool
	Options  []string
}

// Why is why action id is not available on target t in state st, "" when
// it is. The console runs it when the human presses the key, the broker
// for $.actions.available: one function decides both, and the same
// checks decide whether the key works in the core's own views.
func Why(id string, st State, t Target) string {
	a, ok := Lookup(id)
	if !ok {
		return id + " is not an action a mod may bind"
	}
	if why := TargetWhy(a, t); why != "" {
		return why
	}
	switch a.On {
	case OnSession:
		if !st.Session(t.Session) {
			return "session " + t.Session + " is not running"
		}
		return ""
	case OnQuestion:
		if !st.Session(t.Session) {
			return "session " + t.Session + " is not running"
		}
		return NotAnswerable(st.Menu(t.Session), t.Question, t.Option)
	}
	if !st.Project(t.Project) {
		return "no project " + t.Project
	}
	if a.On != OnTask {
		return ""
	}
	task := st.Task(t.Project, t.Task)
	if task == nil {
		return t.Project + " has no task " + t.Task
	}
	kind := askKind(id)
	if kind == "" {
		return ""
	}
	if why := NotAskable(task, kind); why != "" {
		return why
	}
	if k := st.Asked(t.Project, task.Ref()); k != "" {
		return task.Ref() + " is already waiting on the coordinator"
	}
	return ""
}

// TargetWhy checks the target's shape for a: the fields its kind needs,
// and none it doesn't.
func TargetWhy(a Action, t Target) string {
	if a.On != OnQuestion && (t.Question != "" || t.Option != "") {
		return a.ID + " takes no question or option"
	}
	switch a.On {
	case OnSession:
		if t.Session == "" || t.Project != "" || t.Task != "" || t.Thread != "" {
			return a.ID + " takes a target of {session}"
		}
	case OnQuestion:
		if t.Session == "" || t.Question == "" || t.Option == "" || t.Project != "" || t.Task != "" || t.Thread != "" {
			return a.ID + " takes a target of {session, question, option}"
		}
	case OnProject:
		if t.Project == "" || t.Session != "" || t.Thread != "" {
			return a.ID + " takes a target of {project}"
		}
	case OnTask:
		if t.Project == "" || !TaskRef(t.Task) || t.Session != "" || t.Thread != "" {
			return a.ID + " takes a target of {project, task: \"T12\"}"
		}
	}
	return ""
}

// TaskRef says whether s is a task's ref: T and a number.
func TaskRef(s string) bool {
	n, ok := strings.CutPrefix(s, "T")
	if !ok || n == "" || len(n) > 9 || n[0] == '0' {
		return false
	}
	_, err := strconv.Atoi(n)
	return err == nil && strings.Trim(n, "0123456789") == ""
}

// askKind is the inbox item kind a task action drops, "" for one that
// asks nothing.
func askKind(id string) string {
	switch id {
	case "task.delegate":
		return KindDelegate
	case "task.accept":
		return KindAccept
	case "task.send-back":
		return KindSendBack
	}
	return ""
}

// NotDelegable is why task t can't be delegated, "" when it can: an
// open, ready or blocked task can.
func NotDelegable(t *tasks.Task) string {
	switch t.Status {
	case tasks.Open, tasks.Ready, tasks.Blocked:
		return ""
	case tasks.Started:
		return t.Ref() + " is started: a thread already works on it"
	case tasks.Review:
		return t.Ref() + " is in review: A accepts it, x sends it back"
	case tasks.Done:
		return t.Ref() + " is done"
	}
	return t.Ref() + " is " + string(t.Status)
}

// NotInReview is why task t can't be accepted, "" when it can: only a
// task in review can.
func NotInReview(t *tasks.Task) string {
	if t.Status == tasks.Review {
		return ""
	}
	return t.Ref() + " is " + string(t.Status) + ", not in review"
}

// NotSendable is why task t can't be sent back, "" when it can: a task
// in review, or a done one (the coordinator reopens it), can.
func NotSendable(t *tasks.Task) string {
	if t.Status == tasks.Review || t.Status == tasks.Done {
		return ""
	}
	return t.Ref() + " is " + string(t.Status) + ", not in review or done"
}

// NotAskable is why kind can't be asked for task t, "" when it can.
func NotAskable(t *tasks.Task, kind string) string {
	switch kind {
	case KindDelegate:
		return NotDelegable(t)
	case KindAccept:
		return NotInReview(t)
	case KindSendBack:
		return NotSendable(t)
	}
	return "unknown ask " + kind
}

// Answer finds question q in menu: its index, and the option's label
// as the menu has it, or why it can't be answered with option, "" when
// it can. Only the label itself counts (no number, no other case, no
// words of the mod's own): a mod names an option, and the human
// confirms that option of that question.
func Answer(menu []MenuQuestion, q, option string) (int, string) {
	if menu == nil {
		return -1, "the session has no open question"
	}
	for i, m := range menu {
		if m.Question != q {
			continue
		}
		switch {
		case m.Answered:
			return -1, "that question is answered already"
		case m.Multi:
			return -1, "a question where several options may be picked is answered in the session"
		case !slices.Contains(m.Options, option):
			return -1, "that question has no option " + strconv.Quote(option)
		}
		return i, ""
	}
	return -1, "the session's open menu doesn't ask that question"
}

// NotAnswerable is why question q of menu can't be answered with
// option, "" when it can (Answer).
func NotAnswerable(menu []MenuQuestion, q, option string) string {
	_, why := Answer(menu, q, option)
	return why
}
