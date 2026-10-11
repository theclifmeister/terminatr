package modbus

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The events of mod API v1 (the mods design §5). Within a major, names
// and payload fields are only ever added.
const (
	TaskChanged    = "task.changed"
	ThreadState    = "thread.state"
	ThreadStarted  = "thread.started"
	ThreadResolved = "thread.resolved"
	ReportStored   = "report.stored"
	InboxItem      = "inbox.item"
	PRChanged      = "pr.changed"
	SessionState   = "session.state"
	ClockMinute    = "clock.minute"
	// Published by the console, action and view parts of the API.
	UIClick     = "ui.click"
	UIKey       = "ui.key"
	ActionDone  = "action.done"
	ViewOpened  = "view.opened"
	ViewClosed  = "view.closed"
	CommandRun  = "command.run"
	ModSettings = "mod.settings"
	// ModReady is a mod's first event after every (re)start, with a
	// snapshot (Ready).
	ModReady = "mod.ready"
)

// Grants an event may need ([permissions] in mod.toml).
const (
	GrantProjectRead = "project_read"
	GrantViews       = "views"
)

// Needs is the grant a mod needs to get event name; "" for none.
func Needs(name string) string {
	switch name {
	case TaskChanged, ThreadState, ThreadStarted, ThreadResolved, ReportStored, InboxItem, PRChanged:
		return GrantProjectRead
	case ViewOpened, ViewClosed:
		return GrantViews
	}
	return ""
}

// Task is task.changed: a task's status or steps changed, or it is new
// (From "").
type Task struct {
	Project    string `json:"project"`
	ID         string `json:"id"` // "T12"
	Title      string `json:"title"`
	From       string `json:"from"`
	To         string `json:"to"`
	StepsDone  int    `json:"steps_done"`
	StepsTotal int    `json:"steps_total"`
	Thread     string `json:"thread,omitempty"`
}

// Thread is thread.state: the thread's state as the views show it (its
// session's agent state, else its record's: stopped, resolved), and the
// question it waits on.
type Thread struct {
	Project  string `json:"project"`
	Thread   string `json:"thread"`
	Task     string `json:"task,omitempty"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
	NeedsYou string `json:"needs_you,omitempty"`
}

// ThreadLife is thread.started and thread.resolved.
type ThreadLife struct {
	Project string `json:"project"`
	Thread  string `json:"thread"`
	Task    string `json:"task,omitempty"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
}

// Report is report.stored: a thread's newest report, by its number.
type Report struct {
	Project  string         `json:"project"`
	Thread   string         `json:"thread"`
	Task     string         `json:"task,omitempty"`
	Report   int            `json:"report"`
	Sections ReportSections `json:"sections"`
	PR       string         `json:"pr,omitempty"`
}

// ReportSections are a report's sections.
type ReportSections struct {
	Report   string   `json:"report"`
	Next     []string `json:"next"`
	Check    []string `json:"check,omitempty"`
	Remember []string `json:"remember,omitempty"`
}

// Item is inbox.item: a new item in the coordinator's inbox.
type Item struct {
	Project   string `json:"project"`
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Summary   string `json:"summary"`
	NeedsUser bool   `json:"needs_user"`
}

// PR is pr.changed: what the ticker's poll learnt of a thread's PR.
type PR struct {
	Project  string `json:"project"`
	Thread   string `json:"thread"`
	Number   int    `json:"number"`
	URL      string `json:"url,omitempty"`
	State    string `json:"state"`            // OPEN, CLOSED, MERGED
	Checks   string `json:"checks,omitempty"` // pass, fail, pending, ""
	Review   string `json:"review,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	Behind   bool   `json:"behind,omitempty"`
	Conflict bool   `json:"conflict,omitempty"`
}

// Session is session.state: a session's agent state or context use
// changed, or it exited (State "exited").
type Session struct {
	Session string `json:"session"`
	Project string `json:"project,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Role    string `json:"role"`
	Agent   string `json:"agent,omitempty"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	// Context is the percentage of the context window the latest turn
	// used; -1 when unknown.
	Context int `json:"context"`
}

// Clock is clock.minute, on every minute.
type Clock struct {
	Now time.Time `json:"now"`
}

// Ready is mod.ready's snapshot: each project the mod runs for, as
// $.project.get gives it. Projects is nil for a mod without the
// project_read grant.
type Ready struct {
	Projects []proto.ProjectWatch `json:"projects"`
}

// Target is what a mod's cell, row, action or input is about: a task or
// a thread of a project, or a session.
type Target struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
}

// Input is ui.click and ui.key: the human clicked, or pressed Key on, one
// of the mod's own columns, views or widgets.
type Input struct {
	Column string `json:"column,omitempty"`
	View   string `json:"view,omitempty"`
	Widget string `json:"widget,omitempty"`
	// Row is the id of the row under the widget's cursor, or clicked.
	Row    string  `json:"row,omitempty"`
	Key    string  `json:"key,omitempty"` // ui.key only
	Target *Target `json:"target,omitempty"`
}

// Done is action.done: a human ran a core action bound in the
// mod's row, view or notification, and whether it succeeded.
type Done struct {
	Action string `json:"action"`
	Target Target `json:"target"`
	OK     bool   `json:"ok"`
}

// View is view.opened and view.closed: a view the manifest declares, the
// slot it is shown in (project.tab, dashboard.card, full) and its size in
// cells.
type View struct {
	View    string `json:"view"`
	Slot    string `json:"slot"`
	Project string `json:"project,omitempty"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// Command is command.run: the human ran one of the mod's commands, in
// Project, with Selection the row selected then.
type Command struct {
	Command   string  `json:"command"`
	Project   string  `json:"project,omitempty"`
	Selection *Target `json:"selection,omitempty"`
}
