package proto

// Approved commands (T306, docs/SPEC.md §5.2): a thread asks tm to run
// one of the commands the human approved for its project outside the
// sandbox, in its worktree (tm run -- make test). command.run is a
// stream: the server answers CommandRunResult once it has checked the
// command (and, for one that asks first, the user said yes), then sends
// CommandRunEvent lines, the command's output as it comes and one
// run.exit last. Only a thread, by the process tree, never the human:
// what runs is the thread's.
const (
	// MethodCommandRun runs an approved command (CommandRunParams).
	MethodCommandRun = "command.run"
	// EventRunOutput carries a piece of the command's output.
	EventRunOutput = "run.output"
	// EventRunExit ends the stream: the command's exit code, or why it
	// didn't run to its end.
	EventRunExit = "run.exit"
)

// CommandRunParams are the params of command.run: the words to run,
// as they are (no shell), or List for the thread's approved commands
// only, run nothing.
type CommandRunParams struct {
	Argv []string `json:"argv,omitempty"`
	List bool     `json:"list,omitempty"`
}

// ApprovedCommand is one command a thread may ask tm to run: its words
// (a last * takes more arguments), and whether the user is asked first
// (an excluded command that asks first, for a Codex thread).
type ApprovedCommand struct {
	Command string `json:"command"`
	Ask     bool   `json:"ask,omitempty"`
	// Source is the project's slug or AccessFromAll.
	Source string `json:"source,omitempty"`
}

// CommandRunResult answers command.run: with List the commands; else
// the approved command the argv matched and the folder it runs in.
type CommandRunResult struct {
	Commands []ApprovedCommand `json:"commands,omitempty"`
	Command  string            `json:"command,omitempty"`
	Cwd      string            `json:"cwd,omitempty"`
}

// CommandRunEvent is one line of a command.run stream.
type CommandRunEvent struct {
	Event string `json:"event"` // EventRunOutput or EventRunExit
	// Stream is "stdout" or "stderr", Data what it wrote (run.output).
	Stream string `json:"stream,omitempty"`
	Data   []byte `json:"data,omitempty"`
	// Code is the exit code, Error why it ended otherwise (run.exit).
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}
