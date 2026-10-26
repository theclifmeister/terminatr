package proto

// Control methods of projects and threads (M6).
const (
	// MethodCLIRun runs a project command (tm task, thread, report,
	// status, done, …) inside the server for an agent caller: the server
	// tells the caller from the peer pid (docs/SPEC.md §11.1) and writes
	// the project folder with its own permissions, which a sandboxed
	// thread lacks (§5.2).
	MethodCLIRun = "cli.run"
)

// MethodProjectRename renames a project's slug (tm project rename,
// docs/SPEC.md §5.1): the server checks no thread of it runs, stops its
// coordinator, renames it between two ticker sweeps, and starts the
// coordinator again under the new slug. The human's.
const MethodProjectRename = "project.rename"

// MethodProjectActive activates or deactivates a project (tm project
// activate|deactivate, the sidebar's space, docs/SPEC.md §5.1): the
// server writes the setting, then stops the project's coordinator and
// threads, keeping their records dormant to resume, or resumes its
// dormant ones. The human's.
const MethodProjectActive = "project.active"

// ProjectActiveParams are the params of project.active.
type ProjectActiveParams struct {
	Project string `json:"project"`
	Active  bool   `json:"active"`
}

// ProjectActiveResult says what project.active did: whether the setting
// changed, and the sessions it stopped, resumed (or started fresh) and
// dropped, by name (coordinator, a thread id).
type ProjectActiveResult struct {
	Changed bool     `json:"changed"`
	Stopped []string `json:"stopped,omitempty"`
	Resumed []string `json:"resumed,omitempty"`
	Lost    []string `json:"lost,omitempty"`
}

// ProjectRenameParams are the params of project.rename. The slug is a
// project's only name.
type ProjectRenameParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ProjectRenameResult is what the rename did: the project's folder and
// worktrees folder now ("" for none), how many thread records changed,
// what didn't go through, and the coordinator session started again
// ("" when none was running).
type ProjectRenameResult struct {
	Dir         string   `json:"dir"`
	Worktrees   string   `json:"worktrees,omitempty"`
	Threads     int      `json:"threads"`
	Notes       []string `json:"notes,omitempty"`
	Coordinator string   `json:"coordinator,omitempty"`
}

// CLIRunParams are the params of cli.run.
type CLIRunParams struct {
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	Stdin []byte   `json:"stdin,omitempty"`
}

// CLIRunResult is the command's output and exit code.
type CLIRunResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

// MethodTaskAsk asks a project's coordinator to act on a task, as the
// dashboard's D, A and X do (docs/SPEC.md §4, Asking the coordinator):
// an inbox item the coordinator takes as the user's word, checked
// against the live board and journaled. The human's.
const MethodTaskAsk = "task.ask"

// TaskAskParams are the params of task.ask: project's task ("T12"),
// what to ask (project.KindDelegate, KindAccept or KindSendBack) and,
// for a send-back, the user's note.
type TaskAskParams struct {
	Project string `json:"project"`
	Task    string `json:"task"`
	Kind    string `json:"kind"`
	Note    string `json:"note,omitempty"`
}

// TaskAskResult says whether the item was dropped: false when one
// already asks the coordinator something of the task.
type TaskAskResult struct {
	Asked bool `json:"asked"`
}

// The user's task actions by name (since protocol 20): task.ask's
// kinds with the checks tm task accept and send-back apply
// (project.AskTask), so a refusal says why in its code: "not-accept",
// "not-send-back" or "not-delegate" for the task's status on the live
// board, "invalid-note", "already-asked" when an ask of another kind
// waits on the coordinator. Params TaskActionParams, result
// TaskAskResult (false when the same ask waits already). The human's,
// journaled as the caller.
const (
	MethodTaskAccept   = "task.accept"
	MethodTaskSendBack = "task.send-back"
	MethodTaskDelegate = "task.delegate"
)

// TaskActionParams are the params of task.accept, task.send-back and
// task.delegate: project's task ("T12") and, for a send-back, the
// user's note (required).
type TaskActionParams struct {
	Project string `json:"project"`
	Task    string `json:"task"`
	Note    string `json:"note,omitempty"`
}

// MethodProjectOpen opens a project's coordinator, as tm project open
// and the dashboard's project rows do (since protocol 20): the running
// one, else one started with the project's coordinator agent, model and
// remote control settings; an inactive project is refused
// ("project-inactive"), as opening never activates. The human's; a
// start is journaled as the caller (coordinator.start).
const MethodProjectOpen = "project.open"

// ProjectOpenParams are the params of project.open: the project, and
// the size of the pane a started coordinator gets (80x24 when unset).
type ProjectOpenParams struct {
	Project string `json:"project"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

// ProjectOpenResult is the coordinator's session, and whether this call
// started it.
type ProjectOpenResult struct {
	Session string `json:"session"`
	Started bool   `json:"started,omitempty"`
}
