package appapi

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// The app's types: what each method's result and events carry, copied
// field by field from the server's (the *Of functions). A field the
// server's records gain stays home until it is copied here; the test
// that lists what stays home fails until it is decided.

// Params of the app's methods.
type (
	// ProjectsWatchParams are projects.watch's: Detail asks for the dashboard's data of each
	// project besides the sidebar's. Lean (since app API 8) asks for the inactive projects
	// but Shown, the one the window shows, as their slug and settings alone; a change in
	// them alone sends no event.
	ProjectsWatchParams struct {
		Detail bool   `json:"detail,omitempty"`
		Lean   bool   `json:"lean,omitempty"`
		Shown  string `json:"shown,omitempty"`
	}
	// SessionIDParams name a session.
	SessionIDParams struct {
		ID string `json:"id"`
	}
	// NoticeParams name a mod's notice.
	NoticeParams struct {
		ID string `json:"id"`
	}
	// PromptParams are session.prompt's: Text for session ID, queued, or Mode "now" (steer
	// into the running turn) or "interrupt".
	PromptParams struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Mode string `json:"mode,omitempty"`
		// Files are refs session.upload answered (app API 15): the
		// server names each after Text, which may then be empty.
		Files []string `json:"files,omitempty"`
	}
	// AnswerParams are session.answer's: question Index (from 0) of session ID's open
	// question gets Answer. Question, when set, is the text that
	// question must still ask, single-select, with Answer one of its
	// options' labels: refused otherwise (a notification's button sets
	// it).
	AnswerParams struct {
		ID       string `json:"id"`
		Index    int    `json:"index"`
		Answer   string `json:"answer"`
		Question string `json:"question,omitempty"`
	}
	// ApproveParams are session.approve's: option Choice (from 1, default 1) of the
	// permission dialog on session ID's screen. Check answers without
	// sending. Escape: the user confirmed, in words naming the
	// command, a command that runs outside the sandbox.
	ApproveParams struct {
		ID     string `json:"id"`
		Choice int    `json:"choice,omitempty"`
		Check  bool   `json:"check,omitempty"`
		Escape bool   `json:"escape,omitempty"`
	}
	// KeysParams are session.keys's: Data written to session ID's terminal as if typed;
	// text typed into a thread's prompt box takes it over.
	KeysParams struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	}
	// TaskAskParams are task.ask's: ask Project's coordinator to "delegate", "accept"
	// or "send-back" (with Note) Task ("T12").
	TaskAskParams struct {
		Project string `json:"project"`
		Task    string `json:"task"`
		Kind    string `json:"kind"`
		Note    string `json:"note,omitempty"`
	}
	// TaskParams are task.accept's and task.delegate's: Project's task
	// ("T12").
	TaskParams struct {
		Project string `json:"project"`
		Task    string `json:"task"`
	}
	// SendBackParams are task.send-back's: Project's task ("T12") and
	// the user's note, what to change (required).
	SendBackParams struct {
		Project string `json:"project"`
		Task    string `json:"task"`
		Note    string `json:"note"`
	}
	// TaskGetParams are task.get's (app API 14): Project's task
	// ("T12").
	TaskGetParams struct {
		Project string `json:"project"`
		Task    string `json:"task"`
	}
	// ProjectParams name a project (questions.open, coordinator.clear).
	ProjectParams struct {
		Project string `json:"project"`
	}
	// TaskAddParams are task.add's: the project, the new task's title
	// (one line) and, optionally, its notes.
	TaskAddParams struct {
		Project string `json:"project"`
		Title   string `json:"title"`
		Notes   string `json:"notes,omitempty"`
	}
	// ProjectOpenParams are project.open's: the project, and the size a
	// coordinator it starts gets (80x24 when unset).
	ProjectOpenParams struct {
		Project string `json:"project"`
		Cols    uint16 `json:"cols,omitempty"`
		Rows    uint16 `json:"rows,omitempty"`
	}
	// LinkActParams are link.act's: Action ("approve", "drop",
	// "accept-file", "decline-file", "cancel-file", "accept",
	// "decline") on ID of Project's links; accepting a link request
	// sets this side to Allow ("both", "send", "receive") with Ask.
	LinkActParams struct {
		Project string `json:"project"`
		Action  string `json:"action"`
		ID      string `json:"id"`
		Allow   string `json:"allow,omitempty"`
		Ask     bool   `json:"ask,omitempty"`
	}
	// ShareAnswerParams are peer.share-answer's: Accept shares Project
	// for request ID; without it the request is declined.
	ShareAnswerParams struct {
		ID      string `json:"id"`
		Accept  bool   `json:"accept,omitempty"`
		Project string `json:"project,omitempty"`
	}
	// ProjectActiveParams are project.active's: the project, and whether
	// to activate it (false: deactivate it).
	ProjectActiveParams struct {
		Project string `json:"project"`
		Active  bool   `json:"active"`
	}
)

// Empty is the result of a method that answers nothing.
type Empty struct{}

// SessionList is session.list's result: the sessions, the server's
// alert count (it goes up on a notification), the mods' notices still
// in the tray, and the server's version and the newer release it found.
// NeedsFirst is [ui] needs_first, which the bridge reads from the
// config as the TUI's sidebar does (since app API 10): the sidebar lists
// the projects that need the user first. ContextHint is [ui]
// context_hint (app API 13; absent for never): the percent of its
// context window from which a coordinator's sidebar row warns and offers
// its save and clear. They are the only settings of the config the app
// API has, read only.
type SessionList struct {
	Sessions    []Session `json:"sessions"`
	Alerts      uint64    `json:"alerts"`
	Notices     []Notice  `json:"notices"`
	Version     string    `json:"version,omitempty"`
	Build       string    `json:"build,omitempty"`
	Latest      string    `json:"latest,omitempty"`
	Upgrade     string    `json:"upgrade,omitempty"`
	NeedsFirst  bool      `json:"needs_first,omitempty"`
	ContextHint int       `json:"context_hint,omitempty"`
}

// Session is one session: its role and place, its agent's state, its
// open question and queue, its context use. A paired server's pane has
// Server (its id is "<alias>/<id>").
type Session struct {
	ID             string    `json:"id"`
	Role           string    `json:"role"`
	Agent          string    `json:"agent,omitempty"`
	Project        string    `json:"project,omitempty"`
	Thread         string    `json:"thread,omitempty"`
	Task           string    `json:"task,omitempty"`
	Title          string    `json:"title,omitempty"`
	Cols           uint16    `json:"cols"`
	Rows           uint16    `json:"rows"`
	Created        time.Time `json:"created"`
	Clients        int       `json:"clients"`
	State          string    `json:"state,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Escape         bool      `json:"escape,omitempty"`
	TodosDone      int       `json:"todos_done,omitempty"`
	TodosTotal     int       `json:"todos_total,omitempty"`
	Current        string    `json:"current,omitempty"`
	Queued         int       `json:"queued_prompts,omitempty"`
	QueuedSince    time.Time `json:"queued_since,omitzero"`
	QueueHeld      string    `json:"queue_held,omitempty"`
	QueueHeldSince time.Time `json:"queue_held_since,omitzero"`
	RemoteControl  bool      `json:"remote_control,omitempty"`
	Model          string    `json:"model,omitempty"`
	Question       *Question `json:"question,omitempty"`
	Context        int64     `json:"context,omitempty"`
	ContextWindow  int64     `json:"context_window,omitempty"`
	Server         string    `json:"server,omitempty"`
	ViewOnly       bool      `json:"view_only,omitempty"`
}

// Question is a question menu open on a session; Escape: only the user
// answers it. Screen (since app API 9): the agent's own menu (Claude's
// AskUserQuestion), which session.answer types on its screen, one
// question at a time in order, a single option or free text.
type Question struct {
	Tool      string         `json:"tool,omitempty"`
	Questions []QuestionItem `json:"questions"`
	Since     time.Time      `json:"since,omitzero"`
	Escape    bool           `json:"escape,omitempty"`
	Screen    bool           `json:"screen,omitempty"`
}

// QuestionItem is one question of a menu, Answered once it has Answer.
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multi_select,omitempty"`
	Options     []QuestionOption `json:"options"`
	Answered    bool             `json:"answered,omitempty"`
	Answer      string           `json:"answer,omitempty"`
}

// QuestionOption is one option of a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Notice is a mod's notification in the tray; Drawn once a console drew
// it (notify.claim).
type Notice struct {
	ID      string         `json:"id"`
	Mod     string         `json:"mod"`
	Title   string         `json:"title"`
	Text    string         `json:"text,omitempty"`
	Level   string         `json:"level"`
	At      time.Time      `json:"at"`
	Buttons []NoticeButton `json:"buttons,omitempty"`
	Drawn   bool           `json:"drawn,omitempty"`
}

// NoticeButton is a notice's button: a core action id on a task
// (Project, Task) or a session's question (Session, Question, Option).
type NoticeButton struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Project  string `json:"project,omitempty"`
	Task     int    `json:"task,omitempty"`
	Session  string `json:"session,omitempty"`
	Question string `json:"question,omitempty"`
	Option   string `json:"option,omitempty"`
}

// Watch is a session's state (session.watch): the session, its
// thread's task and PR, and its project's waiting work. State "exited"
// is the last.
type Watch struct {
	Session   WatchSession `json:"session"`
	Task      *WatchTask   `json:"task,omitempty"`
	PR        string       `json:"pr,omitempty"`
	PRURL     string       `json:"pr_url,omitempty"`
	NeedsYou  int          `json:"needs_you"`
	Inbox     int          `json:"inbox"`
	Queued    int          `json:"queued_prompts"`
	Questions int          `json:"questions,omitempty"`
}

// WatchSession is the session of a Watch.
type WatchSession struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Agent    string `json:"agent,omitempty"`
	Project  string `json:"project,omitempty"`
	Thread   string `json:"thread,omitempty"`
	State    string `json:"state,omitempty"`
	Reason   string `json:"reason,omitempty"`
	NeedsYou string `json:"needs_you,omitempty"`
}

// WatchTask is the task of a Watch.
type WatchTask struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	StepsDone  int    `json:"steps_done"`
	StepsTotal int    `json:"steps_total"`
	Current    string `json:"current,omitempty"`
}

// Projects is projects.watch's result and each event's data: every
// project, in slug order, the archived ones left out.
type Projects struct {
	Projects []Project `json:"projects"`
}

// Project is one project: what the sidebar shows (its tasks that need
// the user, open questions, threads), and with detail what the
// dashboard shows too (goal, repos, board counts, inbox, usage, repos
// behind origin, links). Error says why it doesn't open, or why its
// board doesn't load. Active, Paused and Pinned are the project's
// settings of those names (since app API 4), for the sidebar's order:
// its only settings in the app API, with Shared (since app API 16): it
// is shared with the paired servers, so a share request can't be
// answered with it. Board are the tasks on deck and in the backlog,
// without notes and steps (since app API 6). LinkNeeds (since app API
// 13) are what its links wait on the user for, by the ids link.act
// takes.
type Project struct {
	Slug        string            `json:"slug"`
	Active      bool              `json:"active"`
	Paused      bool              `json:"paused,omitempty"`
	Pinned      bool              `json:"pinned,omitempty"`
	Shared      bool              `json:"shared,omitempty"`
	NeedsYou    []Task            `json:"needs_you"`
	Questions   int               `json:"questions,omitempty"`
	Threads     []Thread          `json:"threads"`
	PRHostFails int               `json:"pr_host_fails,omitempty"`
	Goal        string            `json:"goal,omitempty"`
	Repos       []string          `json:"repos,omitempty"`
	Counts      map[string]int    `json:"counts,omitempty"`
	Error       string            `json:"error,omitempty"`
	Board       []Task            `json:"board,omitempty"`
	Items       []Item            `json:"items,omitempty"`
	Usage       Usage             `json:"usage"`
	TaskUsage   map[string]Usage  `json:"task_usage,omitempty"`
	Checkouts   map[string]string `json:"checkouts,omitempty"`
	Links       []Link            `json:"links,omitempty"`
	LinkNeeds   []LinkNeed        `json:"link_needs,omitempty"`
}

// Task is a task on the board; Ref is its id as the user reads it
// ("T12").
type Task struct {
	ID      int    `json:"id"`
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Notes   string `json:"notes,omitempty"`
	Steps   []Step `json:"steps,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// Step is one step of a task.
type Step struct {
	N    int    `json:"n"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Thread is an unresolved thread: its record, its status (nil before
// it reported any), its PR's checks and number as the ticker last saw
// them, and with detail its report's PR line and its task; since app
// API 14 also its report's Check (how the user checks the work) and
// where its PR stands, PRState: OPEN, CLOSED or MERGED.
type Thread struct {
	Record   ThreadRecord  `json:"record"`
	Status   *ThreadStatus `json:"status,omitempty"`
	Checks   string        `json:"checks,omitempty"`
	PR       int           `json:"pr,omitempty"`
	ReportPR string        `json:"report_pr,omitempty"`
	Task     *Task         `json:"task,omitempty"`
	Check    []string      `json:"check,omitempty"`
	PRState  string        `json:"pr_state,omitempty"`
}

// ThreadRecord is a thread's record.
type ThreadRecord struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Task       string    `json:"task,omitempty"`
	Agent      string    `json:"agent"`
	Model      string    `json:"model,omitempty"`
	Repo       string    `json:"repo,omitempty"`
	Base       string    `json:"base,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Worktree   string    `json:"worktree"`
	State      string    `json:"state"`
	Session    string    `json:"session,omitempty"`
	Created    time.Time `json:"created"`
	LastPrompt time.Time `json:"last_prompt,omitzero"`
	Reports    int       `json:"reports"`
	ReportAt   time.Time `json:"report_at,omitzero"`
	ReportAck  int       `json:"report_acked"`
	Done       bool      `json:"done,omitempty"`
	DoneAt     time.Time `json:"done_at,omitzero"`
	Adopted    bool      `json:"adopted,omitempty"`
	Checkout   bool      `json:"checkout,omitempty"`
	Usage      Usage     `json:"usage"`
}

// ThreadStatus is a thread's STATUS.md: its progress, current item,
// last activity, the question it waits on the user for, and its todos.
type ThreadStatus struct {
	Percent       int       `json:"percent"`
	PercentSource string    `json:"percent_source,omitempty"`
	Current       string    `json:"current,omitempty"`
	StepsDone     int       `json:"steps_done"`
	StepsTotal    int       `json:"steps_total"`
	TodosDone     int       `json:"todos_done"`
	TodosTotal    int       `json:"todos_total"`
	Activity      string    `json:"activity,omitempty"`
	NeedsYou      string    `json:"needs_you,omitempty"`
	SelfPercent   int       `json:"self_percent"`
	SelfAt        time.Time `json:"self_at,omitzero"`
	Done          bool      `json:"done,omitempty"`
	Updated       time.Time `json:"updated"`
	Todos         []Todo    `json:"todos,omitempty"`
}

// Todo is one of an agent's todos.
type Todo struct {
	ID         string `json:"id,omitempty"`
	Text       string `json:"text"`
	ActiveText string `json:"active_text,omitempty"`
	Status     string `json:"status"`
}

// Usage is what turns used: tokens, dollars where known, and a plan's
// share for agents on a subscription.
type Usage struct {
	Turns         int     `json:"turns"`
	Input         int64   `json:"input"`
	Output        int64   `json:"output"`
	CacheRead     int64   `json:"cache_read"`
	CacheCreation int64   `json:"cache_creation"`
	CostUSD       float64 `json:"cost_usd"`
	PlanPct       float64 `json:"plan_pct,omitempty"`
	HasPlan       bool    `json:"has_plan,omitempty"`
	Uncosted      int64   `json:"uncosted,omitempty"`
}

// Item is one of the coordinator's unhandled inbox items.
type Item struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Subject   string    `json:"subject"`
	Created   time.Time `json:"created"`
	Summary   string    `json:"summary"`
	NeedsUser bool      `json:"needs_user"`
	Thread    string    `json:"thread,omitempty"`
}

// Link is a link to another project's coordinator: State in words,
// which way messages go, and its counts.
type Link struct {
	Peer         string    `json:"peer"`
	Shown        string    `json:"shown,omitempty"`
	State        string    `json:"state"`
	HasMine      bool      `json:"has_mine"`
	HasTheirs    bool      `json:"has_theirs"`
	Send         bool      `json:"send"`
	Receive      bool      `json:"receive"`
	Paused       bool      `json:"paused,omitempty"`
	Remote       bool      `json:"remote,omitempty"`
	Unread       int       `json:"unread"`
	Held         int       `json:"held"`
	Queued       int       `json:"queued,omitempty"`
	FilesWaiting int       `json:"files_waiting,omitempty"`
	FilesActive  int       `json:"files_active,omitempty"`
	LastAt       time.Time `json:"last_at,omitzero"`
}

// LinkNeed is one thing a project's link waits on the user for (app
// API 16): Kind "held", a message of the coordinator's waiting for the
// OK (link.act approve or drop); "file", a file offered to the project
// (accept-file or decline-file); "request", a paired server's link
// request (accept with this side's Allow and Ask, or decline). Peer is
// the project at the link's other end ("mini/api" on a paired server),
// Shown it by the server's display name. Title is a held message's
// first line, the coordinator's words; Bytes its size. File and Size
// are the file a held message carries, or the file offered. Allow is a
// request's asking side's entry: its mirror is the answer it expects.
type LinkNeed struct {
	Kind    string    `json:"kind"`
	Peer    string    `json:"peer"`
	Shown   string    `json:"shown,omitempty"`
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Title   string    `json:"title,omitempty"`
	Bytes   int       `json:"bytes,omitempty"`
	File    string    `json:"file,omitempty"`
	Size    int64     `json:"size,omitempty"`
	Allow   string    `json:"allow,omitempty"`
}

// LinkActResult is link.act's: what came of it, in words.
type LinkActResult struct {
	Words string `json:"words"`
}

// ShareRequest is a paired server's request that this server share a
// project with it (app API 16), waiting for the user: For is the asking
// project there ("" for none in particular), Note the asking user's
// line. peer.share-answer answers it: accepting shares one of this
// server's active projects that isn't shared yet.
type ShareRequest struct {
	ID      string    `json:"id"`
	For     string    `json:"for,omitempty"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
}

// ShareAnswer is peer.share-answer's: the request's ID, its State
// ("accepted" or "declined") and the Project it shared.
type ShareAnswer struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Project string `json:"project,omitempty"`
}

// PromptResult says how a prompt went in: Via "channel", "queued",
// "steered" or "interrupted"; Why a Mode's prompt went another way;
// Running: queued while the turn runs on.
type PromptResult struct {
	Via     string `json:"via"`
	Why     string `json:"why,omitempty"`
	Running bool   `json:"running,omitempty"`
}

// AnswerResult counts the questions still unanswered; at 0 the answers
// went to the asker.
type AnswerResult struct {
	Remaining int `json:"remaining"`
}

// ApproveResult is the dialog as the screen shows it: its question, the
// chosen option and every option. Escape: it asks to run a command
// outside the sandbox. Sent: the key went (not for Check).
type ApproveResult struct {
	Question string   `json:"question,omitempty"`
	Option   string   `json:"option"`
	Options  []string `json:"options"`
	Escape   bool     `json:"escape,omitempty"`
	Sent     bool     `json:"sent,omitempty"`
}

// TaskAskResult is task.ask's: Asked false when an item already asks the coordinator
// something of the task.
type TaskAskResult struct {
	Asked bool `json:"asked"`
}

// NoticeResult is notify.claim's and notify.dismiss's: OK for the first console to claim a notice, and for a
// dismiss that took it out.
type NoticeResult struct {
	OK bool `json:"ok"`
}

// PeerList is peer.list's result: the paired servers, by alias, with
// what this server last read of the projects each shares with it. Gen
// counts the mirror's changes: the same Gen is the same answer.
type PeerList struct {
	Gen   uint64 `json:"gen"`
	Peers []Peer `json:"peers"`
}

// Peer is a paired server: its alias and display name, its link's State
// (Reason why it is down, Since when), the projects it shares with this
// server, and ShareWaits, its share requests that wait for the user
// (ShareRequests, since app API 16).
// Pane: its tm has remote panes; Panes is its switch for this server
// as it last said ("off", "view", "type"): its sessions' panes open
// through pane.vt ("<alias>/<session>") at view or type, and take keys
// at type. Transcript (app API 12): its tm shares conversations;
// Conversations is its conversations switch for this server as it last
// said ("on", "off", "" unsaid): its sessions' conversations open
// through transcript.watch ("<alias>/<session>", the server's caps say
// transcript-remote) while Panes is view or type and it is on.
type Peer struct {
	Alias         string         `json:"alias"`
	Name          string         `json:"name,omitempty"`
	State         string         `json:"state"`
	Reason        string         `json:"reason,omitempty"`
	Since         time.Time      `json:"since,omitzero"`
	Projects      []PeerProject  `json:"projects"`
	ShareWaits    int            `json:"share_waits,omitempty"`
	ShareRequests []ShareRequest `json:"share_requests,omitempty"`
	Pane          bool           `json:"pane,omitempty"`
	Panes         string         `json:"panes,omitempty"`
	Transcript    bool           `json:"transcript,omitempty"`
	Conversations string         `json:"conversations,omitempty"`
}

// PeerProject is a project a paired server shares: its goal line,
// whether it is paused, its counts (NeedsYou: what waits on that
// server's user), and Stale since Synced while the link is down or the
// last read failed (the counts are the last ones seen).
type PeerProject struct {
	Project  string    `json:"project"`
	Goal     string    `json:"goal,omitempty"`
	Paused   bool      `json:"paused,omitempty"`
	NeedsYou int       `json:"needs_you"`
	Threads  int       `json:"threads"`
	Ready    int       `json:"ready"`
	Stale    bool      `json:"stale,omitempty"`
	Synced   time.Time `json:"synced,omitzero"`
	// Sessions are the project's coordinator and threads there, as the
	// server last read them: their panes are the app's to open, and
	// their conversations under the peer's switches.
	Sessions []PeerSession `json:"sessions"`
	// Watched are its unresolved threads as the server last read them
	// (since app API 10), running or not, in id order: what the TUI's
	// sidebar lists under its coordinator. None before the first read,
	// or when the paired server's tm doesn't export them.
	Watched []PeerThread `json:"watched"`
}

// PeerThread is an unresolved thread of a paired server's project, as
// it lets this server see it: Session is its pane's id here
// ("<alias>/<session>", as PeerSession.ID) while a session runs it,
// State that session's agent state; NeedsYou says it waits on that
// server's user, whose question stays there.
type PeerThread struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Task       string `json:"task,omitempty"`
	StepsDone  int    `json:"steps_done,omitempty"`
	StepsTotal int    `json:"steps_total,omitempty"`
	Session    string `json:"session,omitempty"`
	State      string `json:"state,omitempty"`
	NeedsYou   bool   `json:"needs_you,omitempty"`
	PRChecks   string `json:"pr_checks,omitempty"`
	Done       bool   `json:"done,omitempty"`
}

// PeerSession is a paired server's coordinator or thread, as it lets
// this server see it: ID is the pane's here, "<alias>/<session>";
// Created is on this server's clock; Asking says a question menu is
// open there, whose text stays there.
type PeerSession struct {
	ID            string    `json:"id"`
	Role          string    `json:"role"`
	Agent         string    `json:"agent,omitempty"`
	Project       string    `json:"project"`
	Thread        string    `json:"thread,omitempty"`
	Task          string    `json:"task,omitempty"`
	Created       time.Time `json:"created"`
	State         string    `json:"state,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	TodosDone     int       `json:"todos_done,omitempty"`
	TodosTotal    int       `json:"todos_total,omitempty"`
	Current       string    `json:"current,omitempty"`
	Queued        int       `json:"queued_prompts,omitempty"`
	RemoteControl bool      `json:"remote_control,omitempty"`
	Model         string    `json:"model,omitempty"`
	Asking        bool      `json:"asking,omitempty"`
	Context       int64     `json:"context,omitempty"`
	ContextWindow int64     `json:"context_window,omitempty"`
}

// The copies. Lists are never null.

func sessionListOf(r proto.SessionListResult) SessionList {
	first, hint := uiHints()
	out := SessionList{Sessions: make([]Session, 0, len(r.Sessions)), Notices: make([]Notice, 0, len(r.ModNotices)),
		Alerts: r.Alerts, Version: r.Version, Build: r.Build, Latest: r.Latest, Upgrade: r.Upgrade, NeedsFirst: first, ContextHint: hint}
	for _, s := range r.Sessions {
		out.Sessions = append(out.Sessions, sessionOf(s))
	}
	for _, n := range r.ModNotices {
		out.Notices = append(out.Notices, noticeOf(n))
	}
	return out
}

func sessionOf(s proto.SessionInfo) Session {
	return Session{ID: s.ID, Role: s.Role, Agent: s.Agent, Project: s.Project, Thread: s.Thread, Task: s.Task,
		Title: s.Title, Cols: s.Cols, Rows: s.Rows, Created: s.Created, Clients: s.Clients, State: s.State,
		Reason: s.Reason, Escape: s.Escape, TodosDone: s.TodosDone, TodosTotal: s.TodosTotal, Current: s.Current,
		Queued: s.Queued, QueuedSince: s.QueuedSince, QueueHeld: s.QueueHeld, QueueHeldSince: s.QueueHeldSince,
		RemoteControl: s.RemoteControl, Model: s.Model, Question: questionOf(s.Question), Context: s.Context,
		ContextWindow: s.ContextWindow, Server: s.Server, ViewOnly: s.ViewOnly}
}

func questionOf(q *proto.Question) *Question {
	if q == nil {
		return nil
	}
	out := &Question{Tool: q.Tool, Since: q.Since, Escape: q.Escape, Screen: q.Screen, Questions: make([]QuestionItem, 0, len(q.Questions))}
	for _, it := range q.Questions {
		qi := QuestionItem{Question: it.Question, Header: it.Header, MultiSelect: it.MultiSelect,
			Answered: it.Answered, Answer: it.Answer, Options: make([]QuestionOption, 0, len(it.Options))}
		for _, o := range it.Options {
			qi.Options = append(qi.Options, QuestionOption{Label: o.Label, Description: o.Description})
		}
		out.Questions = append(out.Questions, qi)
	}
	return out
}

func noticeOf(n modnotify.Notice) Notice {
	out := Notice{ID: n.ID, Mod: n.Mod, Title: n.Title, Text: n.Text, Level: n.Level, At: n.At, Drawn: n.Drawn}
	for _, b := range n.Buttons {
		out.Buttons = append(out.Buttons, NoticeButton{ID: b.ID, Label: b.Label, Project: b.Project, Task: b.Task,
			Session: b.Session, Question: b.Question, Option: b.Option})
	}
	return out
}

func watchOf(w proto.Watch) Watch {
	out := Watch{Session: WatchSession{ID: w.Session.ID, Role: w.Session.Role, Agent: w.Session.Agent,
		Project: w.Session.Project, Thread: w.Session.Thread, State: w.Session.State, Reason: w.Session.Reason,
		NeedsYou: w.Session.NeedsYou}, PR: w.PR, PRURL: w.PRURL, NeedsYou: w.NeedsYou, Inbox: w.Inbox,
		Queued: w.Queued, Questions: w.Questions}
	if t := w.Task; t != nil {
		out.Task = &WatchTask{ID: t.ID, Title: t.Title, Status: t.Status, StepsDone: t.StepsDone,
			StepsTotal: t.StepsTotal, Current: t.Current}
	}
	return out
}

func projectsOf(w server.ProjectsWatch) Projects {
	out := Projects{Projects: make([]Project, 0, len(w.Projects))}
	for _, p := range w.Projects {
		out.Projects = append(out.Projects, projectOf(p))
	}
	return out
}

// projectOf is p as the app sees it. Of its settings only Active, Paused
// and Pinned cross; the rest (Safety, Own) and the mods' badges stay
// home: settings and mods are never in the app API. Settings that can't
// be read count as active, as in the TUI.
func projectOf(p server.SideProject) Project {
	out := Project{Slug: p.Slug, Active: true, NeedsYou: make([]Task, 0, len(p.NeedsYou)), Questions: p.Questions,
		Threads: make([]Thread, 0, len(p.Threads)), PRHostFails: p.PRHostFails, Goal: p.Goal, Repos: p.Repos,
		Counts: p.Counts, Error: p.Error, Usage: usageOf(p.Usage), Checkouts: p.Checkouts}
	if s := p.Safety; s != nil {
		out.Active, out.Paused, out.Pinned, out.Shared = s.Active, s.Paused, s.Pinned, s.Share
	}
	for _, t := range p.NeedsYou {
		out.NeedsYou = append(out.NeedsYou, taskOf(t))
	}
	for _, t := range p.Threads {
		out.Threads = append(out.Threads, threadOf(t))
	}
	for _, t := range p.Board {
		out.Board = append(out.Board, taskOf(t))
	}
	for _, it := range p.Items {
		out.Items = append(out.Items, itemOf(it))
	}
	if len(p.TaskUsage) > 0 {
		out.TaskUsage = make(map[string]Usage, len(p.TaskUsage))
		for id, u := range p.TaskUsage {
			out.TaskUsage[fmt.Sprintf("T%d", id)] = usageOf(u)
		}
	}
	shown := map[string]string{}
	for _, l := range p.Links {
		out.Links = append(out.Links, linkOf(l))
		shown[l.Peer] = l.Shown
	}
	for _, n := range p.LinkNeeds {
		out.LinkNeeds = append(out.LinkNeeds, LinkNeed{Kind: n.Kind, Peer: n.Peer, Shown: shown[n.Peer], ID: n.ID, Created: n.Created,
			Title: n.Title, Bytes: n.Bytes, File: n.File, Size: n.Size, Allow: n.Allow})
	}
	return out
}

func taskOf(t server.SideTask) Task {
	out := Task{ID: t.ID, Ref: (&tasks.Task{ID: t.ID}).Ref(), Title: t.Title, Status: string(t.Status), Notes: t.Notes,
		Thread: t.Thread, Owner: t.Owner, Created: t.Created, Updated: t.Updated}
	for _, s := range t.Steps {
		out.Steps = append(out.Steps, Step{N: s.N, Text: s.Text, Done: s.Done})
	}
	return out
}

func threadOf(t server.SideThread) Thread {
	out := Thread{Checks: t.Checks, PR: t.PR, ReportPR: t.ReportPR, Check: t.Check, PRState: t.PRState}
	if r := t.Record; r != nil {
		out.Record = recordOf(*r)
	}
	if st := t.Status; st != nil {
		out.Status = statusOf(*st)
	}
	if t.Task != nil {
		task := taskOf(*t.Task)
		out.Task = &task
	}
	return out
}

// recordOf is r as the app sees it; the agent's own session id and
// whether it was ever prompted (for resume) stay home.
func recordOf(r thread.Record) ThreadRecord {
	return ThreadRecord{ID: r.ID, Title: r.Title, Task: r.Task, Agent: r.Agent, Model: r.Model, Repo: r.Repo,
		Base: r.Base, Branch: r.Branch, Worktree: r.Worktree, State: r.State, Session: r.Session,
		Created: r.Created, LastPrompt: r.LastPrompt, Reports: r.Reports, ReportAt: r.ReportAt,
		ReportAck: r.ReportAck, Done: r.Done, DoneAt: r.DoneAt, Adopted: r.Adopted, Checkout: r.Checkout,
		Usage: usageOf(r.Usage)}
}

func statusOf(s thread.Status) *ThreadStatus {
	out := &ThreadStatus{Percent: s.Percent, PercentSource: s.PercentSource, Current: s.Current,
		StepsDone: s.StepsDone, StepsTotal: s.StepsTotal, TodosDone: s.TodosDone, TodosTotal: s.TodosTotal,
		Activity: s.Activity, NeedsYou: s.NeedsYou, SelfPercent: s.SelfPercent, SelfAt: s.SelfAt, Done: s.Done,
		Updated: s.Updated}
	for _, t := range s.Todos {
		out.Todos = append(out.Todos, todoOf(t))
	}
	return out
}

func todoOf(t agent.Todo) Todo {
	return Todo{ID: t.ID, Text: t.Text, ActiveText: t.ActiveText, Status: string(t.Status)}
}

func usageOf(u thread.Usage) Usage {
	return Usage{Turns: u.Turns, Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
		CacheCreation: u.CacheCreation, CostUSD: u.CostUSD, PlanPct: u.PlanPct, HasPlan: u.HasPlan, Uncosted: u.Uncosted}
}

// itemOf is it as the app sees it: a vault item's place and a link
// item's message stay home.
func itemOf(it project.Item) Item {
	return Item{ID: it.ID, Kind: it.Kind, Subject: it.Subject, Created: it.Created, Summary: it.Summary,
		NeedsUser: it.NeedsUser, Thread: it.Thread}
}

// linkOf is r as the app sees it: each side's entry (settings) and the
// link requests waiting stay home.
func linkOf(r link.Row) Link {
	return Link{Peer: r.Peer, Shown: r.Shown, State: r.Words(), HasMine: r.HasMine, HasTheirs: r.HasTheirs,
		Send: r.Send, Receive: r.Receive, Paused: r.Paused, Remote: r.Remote, Unread: r.Unread, Held: r.Held,
		Queued: r.Queued, FilesWaiting: r.FilesWaiting, FilesActive: r.FilesActive, LastAt: r.LastAt}
}

// peerListOf is l as the app sees it: a peer's id, versions, ping and
// share requests (but their count), and a project's snapshot stay
// home but its threads (Watched). Its coordinator and threads cross as
// their panes' ids, the sessions a remote pane and a remote
// conversation open (remotes.find).
func peerListOf(l proto.PeerList) PeerList {
	out := PeerList{Gen: l.Gen, Peers: make([]Peer, 0, len(l.Peers))}
	for _, p := range l.Peers {
		peer := Peer{Alias: p.Alias, Name: p.Name, State: p.State, Reason: p.Reason, Since: p.Since,
			Projects: make([]PeerProject, 0, len(p.Projects)), ShareWaits: proto.ShareWaits([]proto.PeerMirror{p}),
			Pane: p.Pane, Panes: p.Panes, Transcript: p.Transcript, Conversations: p.Conversations}
		for _, q := range p.Shares {
			if !q.Out && q.Pending() {
				peer.ShareRequests = append(peer.ShareRequests, shareRequestOf(q))
			}
		}
		offset := time.Duration(p.Offset) * time.Millisecond
		for _, pp := range p.Projects {
			pr := PeerProject{Project: pp.Project, Goal: pp.Goal, Paused: pp.Paused, NeedsYou: pp.NeedsYou,
				Threads: pp.Threads, Ready: pp.Ready, Stale: pp.Stale, Synced: pp.Synced, Sessions: []PeerSession{},
				Watched: []PeerThread{}}
			if pp.Watch != nil {
				for _, w := range pp.Watch.Threads {
					pr.Watched = append(pr.Watched, peerThreadOf(p.Alias, w))
				}
			}
			for _, s := range pp.Sessions {
				if s.Role != proto.RoleCoordinator && s.Role != proto.RoleThread {
					continue
				}
				pr.Sessions = append(pr.Sessions, peerSessionOf(p.Alias, offset, s))
			}
			peer.Projects = append(peer.Projects, pr)
		}
		out.Peers = append(out.Peers, peer)
	}
	return out
}

// peerThreadOf is thread w of the paired server alias: of its watch
// row, what the sidebar lists; its reason, reports and PR stay the
// remote dashboard's.
func peerThreadOf(alias string, w proto.WatchThread) PeerThread {
	t := PeerThread{ID: w.ID, Title: w.Title, State: w.State, NeedsYou: w.NeedsYou != "", PRChecks: w.PRChecks, Done: w.Done}
	if w.Session != "" {
		t.Session = alias + "/" + w.Session
	}
	if w.Task != nil {
		t.Task, t.StepsDone, t.StepsTotal = w.Task.ID, w.Task.StepsDone, w.Task.StepsTotal
	}
	return t
}

// peerSessionOf is session s of the paired server alias, whose clock
// is offset ahead of this server's.
func peerSessionOf(alias string, offset time.Duration, s proto.PeerSession) PeerSession {
	return PeerSession{ID: alias + "/" + s.ID, Role: s.Role, Agent: s.Agent, Project: s.Project, Thread: s.Thread,
		Task: s.Task, Created: s.Created.Add(-offset), State: s.State, Reason: s.Reason, TodosDone: s.TodosDone,
		TodosTotal: s.TodosTotal, Current: s.Current, Queued: s.Queued, RemoteControl: s.RemoteControl, Model: s.Model,
		Asking: s.Asking, Context: s.Context, ContextWindow: s.ContextWindow}
}

// shareRequestOf is a paired server's share request that waits for the
// user: what answers it.
func shareRequestOf(q proto.ShareRequest) ShareRequest {
	return ShareRequest{ID: q.ID, For: q.For, Note: q.Note, Created: q.Created}
}

func linkActOf(r proto.LinkActResult) LinkActResult { return LinkActResult{Words: r.Words} }

// shareAnswerOf is the answer as the app sees it: of the request, its
// id, state and the project shared; the rest is peer.list's.
func shareAnswerOf(r proto.ShareRequest) ShareAnswer {
	return ShareAnswer{ID: r.ID, State: r.State, Project: r.Project}
}

func promptResultOf(r proto.SessionPromptResult) PromptResult {
	return PromptResult{Via: r.Via, Why: r.Why, Running: r.Running}
}

func answerResultOf(r proto.SessionAnswerResult) AnswerResult {
	return AnswerResult{Remaining: r.Remaining}
}

func approveResultOf(r proto.SessionApproveResult) ApproveResult {
	return ApproveResult{Question: r.Question, Option: r.Option, Options: append([]string{}, r.Options...),
		Escape: r.Escape, Sent: r.Sent}
}

func taskAskResultOf(r proto.TaskAskResult) TaskAskResult { return TaskAskResult{Asked: r.Asked} }

// TaskAdded is task.add's result: the task ("T12"), and Existing when
// a task that isn't done had its title already, so none was added.
type TaskAdded struct {
	Task     string `json:"task"`
	Existing bool   `json:"existing,omitempty"`
}

func taskAddedOf(r proto.TaskAddResult) TaskAdded {
	return TaskAdded{Task: r.Task, Existing: r.Existing}
}

// ProjectOpen is project.open's result: the project's coordinator
// session, and Started when this call started it.
type ProjectOpen struct {
	Session string `json:"session"`
	Started bool   `json:"started,omitempty"`
}

// CoordinatorClear is coordinator.clear's result: the coordinator
// session asked to save what it learned, cleared at its next idle moment
// after that.
type CoordinatorClear struct {
	Session string `json:"session"`
}

// ProjectActive is project.active's result: whether the setting
// changed, and the sessions it stopped, resumed (or started fresh) and
// dropped, by name (coordinator, a thread id).
type ProjectActive struct {
	Changed bool     `json:"changed"`
	Stopped []string `json:"stopped,omitempty"`
	Resumed []string `json:"resumed,omitempty"`
	Lost    []string `json:"lost,omitempty"`
}

// QuestionsOpen is questions.open's result: the coordinator's session
// prompted to put its questions to the user, how the prompt went in (as
// session.prompt's Via) and how many questions were open.
type QuestionsOpen struct {
	Session string `json:"session"`
	Via     string `json:"via"`
	Open    int    `json:"open"`
}

func questionsOpenOf(r proto.QuestionsOpenResult) QuestionsOpen {
	return QuestionsOpen{Session: r.Session, Via: r.Via, Open: r.Open}
}

func projectOpenOf(r proto.ProjectOpenResult) ProjectOpen {
	return ProjectOpen{Session: r.Session, Started: r.Started}
}

func coordinatorClearOf(r proto.CoordinatorClearResult) CoordinatorClear {
	return CoordinatorClear{Session: r.Session}
}

func projectActiveOf(r proto.ProjectActiveResult) ProjectActive {
	return ProjectActive{Changed: r.Changed, Stopped: r.Stopped, Resumed: r.Resumed, Lost: r.Lost}
}

func noticeResultOf(r proto.NoticeResult) NoticeResult { return NoticeResult{OK: r.OK} }

// PaneParams are pane.vt's: the session whose pane to stream.
type PaneParams struct {
	Session string `json:"session"`
}

// PaneResult is pane.vt's result: the session as it is.
type PaneResult struct {
	Session Session `json:"session"`
}

// PaneEvent is a pane.vt event's data: for pane.repaint a fresh
// emulator of Cols × Rows fed Data; for pane.output Data, the PTY's
// output in order; for pane.resize the pane's new size; for pane.closed
// why it ended (Reason). Data is bytes, base64 in JSON, never cleaned:
// it is VT for an emulator.
type PaneEvent struct {
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
	Data   []byte `json:"data,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// PaneInputParams are pane.input's: into pane.vt stream ID, Input (keys
// already encoded for the pane's modes, base64 in JSON), a Size claim,
// a Repaint request.
type PaneInputParams struct {
	ID      int64     `json:"id"`
	Input   []byte    `json:"input,omitempty"`
	Size    *PaneSize `json:"size,omitempty"`
	Repaint bool      `json:"repaint,omitempty"`
}

// PaneSize is a pane's size in cells.
type PaneSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// TranscriptParams are transcript.watch's: the session whose
// conversation to stream.
type TranscriptParams struct {
	Session string `json:"session"`
}

// TranscriptResult is transcript.watch's result: the session, and
// whether its items follow the transcript (Status).
type TranscriptResult struct {
	Session Session          `json:"session"`
	Status  TranscriptStatus `json:"status"`
}

// TranscriptStatus says whether the items follow the transcript (State
// "mapped", "waiting" or "unmapped": then show the terminal), why not,
// and the agent and version the transcript names.
type TranscriptStatus struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Version string `json:"version,omitempty"`
}

// TranscriptEvent is a transcript.watch event's data: for
// transcript.reset the conversation's last Items (Skipped earlier ones
// left out), for transcript.items the new ones, for transcript.status
// the new Status, for transcript.closed why it ended (Reason).
type TranscriptEvent struct {
	Items   []TranscriptItem  `json:"items,omitempty"`
	Skipped int               `json:"skipped,omitempty"`
	Status  *TranscriptStatus `json:"status,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

// TranscriptItem is one item of a conversation: Kind "prompt", "text",
// "tool", "result", "note" or "todos". At is when the agent wrote it,
// From who sent a prompt: "tm", "coordinator", or empty for the user
// (since app API 6).
type TranscriptItem struct {
	Kind      string           `json:"kind"`
	Text      string           `json:"text,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
	Tool      string           `json:"tool,omitempty"`
	Call      string           `json:"call,omitempty"`
	Error     bool             `json:"error,omitempty"`
	Lines     int              `json:"lines,omitempty"`
	Todos     []TranscriptTodo `json:"todos,omitempty"`
	At        time.Time        `json:"at,omitzero"`
	From      string           `json:"from,omitempty"`
	Files     []TranscriptFile `json:"files,omitempty"`
	Display   string           `json:"display,omitempty"`
}

// TranscriptFile is a file an item puts in front of the user (since app
// API 11): a prompt's pasted image, a file a call sends (Claude's
// SendUserFile; the item's Text is its caption, Display "render" or
// "attach"). Ref names it to session.file; Path is a sent file's path
// (empty for a pasted image); Type the media type session.file sends it
// as (empty: it doesn't send its bytes).
type TranscriptFile struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Type string `json:"type,omitempty"`
}

// SessionFileParams are session.file's (app API 11): the file of
// session's conversation its Ref names, the chunk from Offset; Stat
// asks for its name, type and size only.
type SessionFileParams struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Offset  int64  `json:"offset,omitempty"`
	Stat    bool   `json:"stat,omitempty"`
}

// SessionFile is a chunk of a file: Data from Offset (at most 1 MiB;
// none for a stat) of Size bytes, last modified at MTime. Sendable says
// whether its bytes may be read: a type tm sends, at most 25 MiB. A file
// no longer there answers the error "gone".
type SessionFile struct {
	Name     string    `json:"name"`
	Type     string    `json:"type,omitempty"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime,omitzero"`
	Sendable bool      `json:"sendable"`
	Offset   int64     `json:"offset"`
	Data     []byte    `json:"data,omitempty"`
}

// UploadParams are session.upload's (app API 15): a chunk of a file
// for session's prompt, at most 512 KiB of Data. The first (no Upload,
// Offset 0) names the file (Name, whose extension gives its type) and
// its Size; each next one gives the Upload id the first answered and
// its Offset, the bytes taken so far. The app sends bytes, never a path.
type UploadParams struct {
	Session string `json:"session"`
	Upload  string `json:"upload,omitempty"`
	Name    string `json:"name,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// Upload is where an upload stands: Received bytes of Upload; once all
// are in, Ref for session.prompt's Files and the stored file's Name,
// Type and Size.
type Upload struct {
	Upload   string `json:"upload"`
	Received int64  `json:"received"`
	Ref      string `json:"ref,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

func uploadOf(r proto.SessionUploadResult) Upload {
	return Upload{Upload: r.Upload, Received: r.Received, Ref: r.Ref, Name: r.Name, Type: r.Type, Size: r.Size}
}

func sessionFileOf(r proto.SessionFileResult) SessionFile {
	return SessionFile{Name: r.Name, Type: r.Type, Size: r.Size, MTime: r.MTime, Sendable: r.Sendable, Offset: r.Offset, Data: r.Data}
}

// TranscriptTodo is one entry of a todos item.
type TranscriptTodo struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// paneResultOf is r as the app sees it; the window is the bridge's,
// which acks for the app.
func paneResultOf(r proto.PaneVTResult) PaneResult { return PaneResult{Session: sessionOf(r.Session)} }

func paneEventOf(ev proto.PaneVTEvent) PaneEvent {
	return PaneEvent{Cols: ev.Cols, Rows: ev.Rows, Data: ev.Data, Reason: ev.Reason}
}

// transcriptResultOf is r as the app sees it; the window is the
// bridge's.
func transcriptResultOf(r proto.TranscriptWatchResult) TranscriptResult {
	return TranscriptResult{Session: sessionOf(r.Session), Status: transcriptStatusOf(r.Status)}
}

func transcriptStatusOf(s proto.TranscriptStatus) TranscriptStatus {
	return TranscriptStatus{State: s.State, Reason: s.Reason, Agent: s.Agent, Version: s.Version}
}

func transcriptEventOf(ev proto.TranscriptEvent) TranscriptEvent {
	out := TranscriptEvent{Skipped: ev.Skipped, Reason: ev.Reason}
	for _, it := range ev.Items {
		ti := TranscriptItem{Kind: it.Kind, Text: it.Text, Truncated: it.Truncated, Tool: it.Tool, Call: it.Call,
			Error: it.Error, Lines: it.Lines, At: it.At, From: it.From, Display: it.Display}
		for _, t := range it.Todos {
			ti.Todos = append(ti.Todos, TranscriptTodo{Text: t.Text, Status: t.Status})
		}
		for _, f := range it.Files {
			ti.Files = append(ti.Files, TranscriptFile{Ref: f.Ref, Name: f.Name, Path: f.Path, Type: f.Type})
		}
		out.Items = append(out.Items, ti)
	}
	if ev.Status != nil {
		st := transcriptStatusOf(*ev.Status)
		out.Status = &st
	}
	return out
}
