// Package modapi is the mod API v1 (mods design §5) as one schema: the
// events a code mod can subscribe to and the methods of the $ object tm
// injects. Both the host's $ bindings (Bind) and the TypeScript
// declarations mod authors import (TS, checked in as tm.d.ts) are made
// from it, so the two can't drift.
//
// A method's name is its path under $ and its JSON-RPC method on the wire
// (modproto): $.project.get(slug) sends "project.get" with params
// {"slug": …}, positional arguments named by Params. The server's broker
// answers it and checks the grant; the host checks only that required
// arguments are there. Within a major, events, methods, params and fields
// are only ever added.
package modapi

import (
	"reflect"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Grants a method may need ([permissions] in mod.toml).
const (
	GrantProjectRead = modbus.GrantProjectRead
	GrantViews       = modbus.GrantViews
	GrantNotify      = "notify"
	GrantInbox       = "inbox"
	GrantNet         = "net"
	GrantCommands    = "commands"
	GrantFiles       = "files"
)

// Event is one event of the API.
type Event struct {
	Name string
	// Data is the payload's type.
	Data  reflect.Type
	Needs string // the grant; "" for none
	Doc   string
}

// Method is one method of $.
type Method struct {
	// Name is the path under $ and the wire method, "project.get".
	Name   string
	Params []Param
	// Result is the type the promise resolves to; nil for none.
	Result reflect.Type
	Needs  string // the grant the broker checks; "" for none
	// Local methods are answered in the host from hello and return
	// their result, not a promise.
	Local bool
	Doc   string
}

// Param is one positional argument, sent as a field of the params object.
type Param struct {
	Name     string
	Type     reflect.Type
	Optional bool
}

func typ[T any]() reflect.Type { return reflect.TypeFor[T]() }

// Types of the API that exist only for it.

// Value is any JSON value a mod keeps ($.store) or is given (settings).
type Value any

// Settings are the mod's [settings], as the user set them over the
// manifest's defaults.
type Settings map[string]Value

// Role is a theme role (internal/tui theme.go) that colours a span.
type Role string

// Roles are the theme roles a span may name (widget.Roles: the mods
// design §6, Spans).
var Roles = widget.Roles

// Icon is an icon name, drawn from the console's icon set.
type Icon string

// Icons are the icon names a span may name (widget.Icons).
var Icons = widget.Icons

// Level is a toast's or notification's level.
type Level string

// Levels are the levels.
var Levels = []string{"info", "warn", "error"}

// Span is a run of text in one theme role, or an icon: what a cell,
// status segment or badge is made of (widget.Span).
type Span struct {
	Text string `json:"text,omitempty"`
	Role Role   `json:"role,omitempty"`
	Icon Icon   `json:"icon,omitempty"`
}

// Widget is a view's widget tree (the widget set: text, list, table, kv,
// progress, steps, bars, keys), drawn by terminatr.
type Widget map[string]Value

// ViewOptions are $.ui.view.set's options: the project the tree is for
// (a project tab's, as view.opened names it; none: every project), for a
// details.section the task or thread of that project it is for (as
// view.opened names it; none: the project's own row), the count a
// project tab shows beside its title, the lines a dashboard card or a
// section asks for (at most 20; a sidebar.section at most 12, 4 when it
// asks for none; a sidebar.project.section at most 4, 2 when none), and
// a sidebar section's badge on its header, folded or not (mod API 1.7:
// at most 4 spans in 12 cells, an icon counting 2). A sidebar section
// takes no task, thread or count.
type ViewOptions struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Count   *int   `json:"count,omitempty"`
	Height  int    `json:"height,omitempty"`
	Badge   []Span `json:"badge,omitempty"`
}

// Project is one project the mod runs for.
type Project struct {
	Slug string `json:"slug"`
}

// MemoryFile is one of a project's memory files: its path in the project
// folder (CONTEXT.md, MEMORY.md or memory/<name>.md), its title (a
// note's first heading) and its size.
type MemoryFile struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Bytes int64  `json:"bytes"`
}

// Task is one task of a project's board.
type Task struct {
	ID         string `json:"id"` // "T12"
	Title      string `json:"title"`
	Status     string `json:"status"`
	StepsDone  int    `json:"steps_done"`
	StepsTotal int    `json:"steps_total"`
	Thread     string `json:"thread,omitempty"`
	// Mod API 1.2: the steps themselves, the date the task last changed
	// (YYYY-MM-DD), and the kind of the item that asks the coordinator
	// about it (delegate, accept or send-back; none while nothing does).
	Steps   []TaskStep `json:"steps"`
	Updated string     `json:"updated,omitempty"`
	Asked   string     `json:"asked,omitempty"`
}

// TaskStep is one step of a task (mod API 1.2).
type TaskStep struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// InboxRaise is the item $.inbox.raise gives the coordinator: data only,
// with the mod's id in front.
type InboxRaise struct {
	Subject string `json:"subject"`
	Summary string `json:"summary"`
}

// Notification is an OS notification a console draws, marked as the
// mod's. Its buttons are core action ids: pressing one opens the core's
// own confirmation; the mod never acts.
type Notification struct {
	Title   string         `json:"title"`
	Text    string         `json:"text"`
	Level   Level          `json:"level,omitempty"`
	Actions []NotifyButton `json:"actions,omitempty"`
}

// NotifyButton is a notification's button: a core action on a target.
type NotifyButton struct {
	ID     string        `json:"id"`
	Target modbus.Target `json:"target"`
}

// Action is a core action a mod's row may name: its label, default key
// and when it is available are the core's (modaction). On is the target
// it takes (task, project or session); Confirm says the core asks first.
type Action struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Key          string `json:"key,omitempty"`
	On           string `json:"on"`
	Confirm      bool   `json:"confirm"`
	Precondition string `json:"precondition,omitempty"`
	Consent      string `json:"consent,omitempty"`
}

// FetchOptions are $.http.fetch's options.
type FetchOptions struct {
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// FetchResponse is $.http.fetch's answer.
type FetchResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// RunOptions are $.process.run's options.
type RunOptions struct {
	Cwd       string `json:"cwd,omitempty"`
	Stdin     string `json:"stdin,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// RunResult is $.process.run's answer.
type RunResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// Events are the API's events, which $.on subscribes to.
var Events = []Event{
	{Name: modbus.ModReady, Data: typ[modbus.Ready](), Doc: "First after every (re)start: a snapshot of the projects the mod runs for (none without project_read), not a replay."},
	{Name: modbus.TaskChanged, Data: typ[modbus.Task](), Needs: GrantProjectRead, Doc: "A task's status or steps changed, or it is new (from \"\")."},
	{Name: modbus.TasksChanged, Data: typ[modbus.Tasks](), Needs: GrantProjectRead, Doc: "Anything $.tasks.list shows of a project changed: a title, a step, a status, a task added or gone, an ask about a task came or was handled (mod API 1.4). List the tasks again."},
	{Name: modbus.ThreadState, Data: typ[modbus.Thread](), Needs: GrantProjectRead, Doc: "A thread's state changed, or the question it waits on."},
	{Name: modbus.ThreadStarted, Data: typ[modbus.ThreadLife](), Needs: GrantProjectRead, Doc: "A thread started."},
	{Name: modbus.ThreadResolved, Data: typ[modbus.ThreadLife](), Needs: GrantProjectRead, Doc: "A thread was resolved."},
	{Name: modbus.ReportStored, Data: typ[modbus.Report](), Needs: GrantProjectRead, Doc: "A thread stored a report."},
	{Name: modbus.InboxItem, Data: typ[modbus.Item](), Needs: GrantProjectRead, Doc: "A new item in the coordinator's inbox."},
	{Name: modbus.PRChanged, Data: typ[modbus.PR](), Needs: GrantProjectRead, Doc: "The ticker learnt something new of a thread's PR."},
	{Name: modbus.SessionState, Data: typ[modbus.Session](), Doc: "A session's agent state or context use changed, or it exited."},
	{Name: modbus.SessionQuestion, Data: typ[modbus.Question](), Needs: GrantProjectRead, Doc: "The question a thread's or coordinator's open question menu asks changed (mod API 1.3): its first unanswered question, none once it is answered or gone. Its options can be offered as question.answer bindings."},
	{Name: modbus.ClockMinute, Data: typ[modbus.Clock](), Doc: "Every minute."},
	{Name: modbus.UIClick, Data: typ[modbus.Input](), Doc: "The human clicked one of the mod's columns, views or widgets, or pressed enter on (or clicked) the header of a sidebar section whose manifest names a companion view (opens, mod API 1.7): target.project is the section's project, and the mod may then open its companion with $.ui.view.open."},
	{Name: modbus.UIKey, Data: typ[modbus.Input](), Doc: "The human pressed a key on one of the mod's views or widgets."},
	{Name: modbus.UICursor, Data: typ[modbus.Input](), Doc: "The row under the cursor of a view's focused list (widget) is now row (\"\": none), moved by a key, a click, the filter or a publish; sent when the view shows, and again on each change. To follow the selection."},
	{Name: modbus.ActionDone, Data: typ[modbus.Done](), Doc: "A human ran a core action bound in the mod's row, view or notification."},
	{Name: modbus.ViewOpened, Data: typ[modbus.View](), Needs: GrantViews, Doc: "One of the mod's views is shown: publish it with $.ui.view.set. A details.section names the task or thread the details panel shows: publish for that one, and clear it on view.closed. A project.tab opened to show a task picked elsewhere names it (mod API 1.4): list its row, and the console puts the cursor on the row that binds an action on it. A sidebar.section is for the project the sidebar's tree is for; a sidebar.project.section opens once for each project whose rows show (mod API 1.7): publish for each, or one tree for all, and a null tree for a project shows no section under it."},
	{Name: modbus.ViewClosed, Data: typ[modbus.View](), Needs: GrantViews, Doc: "One of the mod's views is no longer shown."},
	{Name: modbus.CommandRun, Data: typ[modbus.Command](), Doc: "The human ran one of the commands in the manifest's [provides].commands: by its key (on the dashboard, or prefix+key in a session: the selection is then that session), or ≡ menu entry (then the mod may open a view), or with tm mod run."},
	{Name: modbus.ModSettings, Data: typ[Settings](), Doc: "The user changed the mod's settings: these are the new ones."},
}

// Methods are $'s methods, besides $.on, $.log, $.tool.handle and
// $.codehost.handle, which the host builds in.
var Methods = []Method{
	// Read.
	{Name: "projects.list", Result: typ[[]Project](), Needs: GrantProjectRead,
		Doc: "The projects the mod runs for."},
	{Name: "project.get", Params: []Param{{Name: "slug", Type: typ[string]()}}, Result: typ[proto.ProjectWatch](), Needs: GrantProjectRead,
		Doc: "A project as its dashboard shows it."},
	{Name: "tasks.list", Params: []Param{{Name: "slug", Type: typ[string]()}}, Result: typ[[]Task](), Needs: GrantProjectRead,
		Doc: "A project's tasks, in board order."},
	{Name: "thread.get", Params: []Param{{Name: "project", Type: typ[string]()}, {Name: "thread", Type: typ[string]()}}, Result: typ[*proto.WatchThread](), Needs: GrantProjectRead,
		Doc: "A thread of a project; null when there is none."},
	{Name: "report.get", Params: []Param{{Name: "project", Type: typ[string]()}, {Name: "thread", Type: typ[string]()}}, Result: typ[*modbus.Report](), Needs: GrantProjectRead,
		Doc: "A thread's latest report; null when it has none."},
	{Name: "memory.list", Params: []Param{{Name: "slug", Type: typ[string]()}}, Result: typ[[]MemoryFile](), Needs: GrantProjectRead,
		Doc: "A project's memory files: CONTEXT.md, MEMORY.md and the notes in memory/, by title."},
	{Name: "memory.read", Params: []Param{{Name: "slug", Type: typ[string]()}, {Name: "path", Type: typ[string]()}}, Result: typ[string](), Needs: GrantProjectRead,
		Doc: "The text of one of a project's memory files, by the path memory.list gives (1 MB at most)."},
	// Own state.
	{Name: "store.get", Params: []Param{{Name: "key", Type: typ[string]()}}, Result: typ[Value](),
		Doc: "A value the mod stored; null when there is none."},
	{Name: "store.set", Params: []Param{{Name: "key", Type: typ[string]()}, {Name: "value", Type: typ[Value]()}},
		Doc: "Stores a JSON value under key (1 MB in all)."},
	{Name: "store.del", Params: []Param{{Name: "key", Type: typ[string]()}},
		Doc: "Removes key from the store."},
	{Name: "fs.read", Params: []Param{{Name: "path", Type: typ[string]()}}, Result: typ[string](),
		Doc: "Reads a text file in the mod's own data dir, or under a root the files grant lists."},
	{Name: "fs.write", Params: []Param{{Name: "path", Type: typ[string]()}, {Name: "text", Type: typ[string]()}},
		Doc: "Writes a text file in the mod's own data dir, or under a root the files grant lets it write."},
	{Name: "settings", Result: typ[Settings](), Local: true,
		Doc: "The mod's settings."},
	{Name: "api.has", Params: []Param{{Name: "name", Type: typ[string]()}}, Result: typ[bool](), Local: true,
		Doc: "Whether this tm has a method (\"ui.view.set\") or event (\"view.opened\"): feature-test instead of pinning a minor."},
	// Annotations.
	{Name: "annotate", Params: []Param{{Name: "target", Type: typ[modbus.Target]()}, {Name: "key", Type: typ[string]()}, {Name: "spans", Type: typ[*[]Span]()}}, Needs: GrantProjectRead,
		Doc: "Shows spans as badges by a task or thread (target {project, task} or {project, thread}); null removes them."},
	// UI.
	{Name: "ui.cell.set", Params: []Param{{Name: "column", Type: typ[string]()}, {Name: "target", Type: typ[modbus.Target]()}, {Name: "spans", Type: typ[*[]Span]()}},
		Doc: "Publishes the mod's cell in one of its columns for a target; null clears it."},
	{Name: "ui.status.set", Params: []Param{{Name: "spans", Type: typ[*[]Span]()}},
		Doc: "Publishes the mod's status-bar segment; null clears it."},
	{Name: "ui.toast", Params: []Param{{Name: "text", Type: typ[string]()}, {Name: "level", Type: typ[Level](), Optional: true}},
		Doc: "Shows a short message on the consoles."},
	{Name: "ui.view.set", Params: []Param{{Name: "view", Type: typ[string]()}, {Name: "widget", Type: typ[*Widget]()}, {Name: "options", Type: typ[ViewOptions](), Optional: true}}, Needs: GrantViews,
		Doc: "Publishes the widget tree of a view the manifest declares, for one project (options.project) or all, a details.section's also for one task or thread of it (options.task, options.thread), a sidebar section's with a badge (options.badge); null clears it."},
	{Name: "ui.view.open", Params: []Param{{Name: "view", Type: typ[string]()}}, Needs: GrantViews,
		Doc: "Asks the console to show a view (not a panel section, which shows with its panel); only after a human input (a key, a click or a command)."},
	{Name: "ui.notify", Params: []Param{{Name: "notification", Type: typ[Notification]()}}, Needs: GrantNotify,
		Doc: "Sends an OS notification through a console."},
	// Actions.
	{Name: "actions.list", Result: typ[[]Action](),
		Doc: "The core actions a row may name. There is no run: a bound action runs only from a human key, click or notification button."},
	{Name: "actions.available", Params: []Param{{Name: "id", Type: typ[string]()}, {Name: "target", Type: typ[modbus.Target]()}}, Result: typ[bool](),
		Doc: "Whether action id's precondition holds for target now, to grey a row out."},
	// Act.
	{Name: "inbox.raise", Params: []Param{{Name: "project", Type: typ[string]()}, {Name: "item", Type: typ[InboxRaise]()}}, Needs: GrantInbox,
		Doc: "Raises an item of kind mod for the project's coordinator: data only, with the mod's id in front."},
	// Outside.
	{Name: "http.fetch", Params: []Param{{Name: "url", Type: typ[string]()}, {Name: "options", Type: typ[FetchOptions](), Optional: true}}, Result: typ[FetchResponse](), Needs: GrantNet,
		Doc: "HTTPS to a host the net grant lists; redirects only within the list."},
	{Name: "process.run", Params: []Param{{Name: "argv", Type: typ[[]string]()}, {Name: "options", Type: typ[RunOptions](), Optional: true}}, Result: typ[RunResult](), Needs: GrantCommands,
		Doc: "Runs a program the commands grant lists, with a scrubbed env, no shell and a timeout."},
}

// Has says whether name is a method of $ (on, log, tool.handle and
// codehost.handle included) or an event.
func Has(name string) bool {
	if name == "on" || name == "log" || name == "tool.handle" || name == "codehost.handle" {
		return true
	}
	for _, m := range Methods {
		if m.Name == name {
			return true
		}
	}
	for _, e := range Events {
		if e.Name == name {
			return true
		}
	}
	return false
}

// Lookup returns the method named name.
func Lookup(name string) (Method, bool) {
	for _, m := range Methods {
		if m.Name == name {
			return m, true
		}
	}
	return Method{}, false
}
