package proto

import "time"

// A session's conversation (docs/SPEC.md §3.3, Transcript): the agent's
// transcript, the file its manifest's [jsonl_tail] follows, read as
// typed items through the manifest's [transcript] table, for a client
// that shows a conversation rather than a terminal (the macOS app).
// transcript.watch is the human's only, bounded and never exported to
// paired servers. The server's hello lists CapTranscript.

// MethodTranscriptWatch watches a session's conversation
// (TranscriptWatchParams): a TranscriptWatchResult, then TranscriptEvent
// lines; the client acks the items it took with TranscriptInput lines.
const MethodTranscriptWatch = "transcript.watch"

// The events of a transcript.watch stream.
const (
	// EventTranscriptReset starts the conversation again: Items are the
	// last of it (at most TranscriptBacklog), Skipped how many earlier
	// ones were left out. The first event, and the one after the agent
	// moved to another transcript (/clear).
	EventTranscriptReset = "transcript.reset"
	// EventTranscriptItems are items the agent added.
	EventTranscriptItems = "transcript.items"
	// EventTranscriptStatus: the mapping's status changed.
	EventTranscriptStatus = "transcript.status"
	// EventTranscriptClosed ends the stream: the session ended
	// (PaneExited) or the server is stopping (PaneStopping).
	EventTranscriptClosed = "transcript.closed"
)

// The states of a TranscriptStatus.
const (
	// TranscriptMapped: the items follow the transcript.
	TranscriptMapped = "mapped"
	// TranscriptWaiting: the agent hasn't named its transcript yet.
	TranscriptWaiting = "waiting"
	// TranscriptUnmapped: tm can't read this agent's transcript (no
	// [transcript] table, a version newer than the table was checked
	// on, or lines it doesn't know): the client shows the terminal.
	TranscriptUnmapped = "unmapped"
)

// Bounds of a transcript.watch stream: a reset carries at most
// TranscriptBacklog items, the server sends at most TranscriptWindow
// items past the client's acks, an item's text is cut at
// TranscriptItemText bytes, and a one-line summary (a tool call's
// argument, a result's first line) at TranscriptSummary characters.
const (
	TranscriptBacklog  = 200
	TranscriptWindow   = 500
	TranscriptItemText = 8 << 10
	TranscriptSummary  = 200
	TranscriptTodos    = 100
)

// TranscriptWatchParams are the params of transcript.watch.
type TranscriptWatchParams struct {
	Session string `json:"session"`
}

// TranscriptWatchResult is transcript.watch's answer.
type TranscriptWatchResult struct {
	Session SessionInfo      `json:"session"`
	Status  TranscriptStatus `json:"status"`
	Window  int              `json:"window"`
}

// TranscriptStatus says whether the items follow the transcript; Reason
// says why not, for people. Version is the agent's version as the
// transcript gives it.
type TranscriptStatus struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Version string `json:"version,omitempty"`
}

// TranscriptEvent is one line of a transcript.watch stream after the
// answer.
type TranscriptEvent struct {
	Event   string            `json:"event"`
	Items   []TranscriptItem  `json:"items,omitempty"`
	Skipped int               `json:"skipped,omitempty"`
	Status  *TranscriptStatus `json:"status,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

// The kinds of a TranscriptItem.
const (
	ItemPrompt = "prompt" // the user's prompt (or tm's, pasted for them)
	ItemText   = "text"   // the agent's text
	ItemTool   = "tool"   // a tool call, folded: Tool and its main argument
	ItemResult = "result" // a tool call's result, summed up
	ItemNote   = "note"   // a mark in the conversation (interrupted)
	ItemTodos  = "todos"  // the agent's todo list as it is now
)

// TranscriptItem is one item of a conversation. Every string is clean
// text (textsafe) and bounded: Text at TranscriptItemText bytes
// (Truncated says it was cut), a summary at TranscriptSummary
// characters on one line.
type TranscriptItem struct {
	Kind string `json:"kind"`
	// Text is the prompt's, the agent's or the note's text; a tool
	// call's main argument (Bash's command, Read's path); a result's
	// first line.
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// Tool is a call's tool; Call the call's id, which its result names.
	Tool string `json:"tool,omitempty"`
	Call string `json:"call,omitempty"`
	// Error and Lines are a result's: it failed; its output's lines.
	Error bool `json:"error,omitempty"`
	Lines int  `json:"lines,omitempty"`
	// Todos is a todos item's list.
	Todos []TranscriptTodo `json:"todos,omitempty"`
	// At is when the agent wrote the item's line (its manifest's
	// [transcript] time_field); zero when the line doesn't say.
	At time.Time `json:"at,omitzero"`
	// From is who sent a prompt, as far as the server knows: FromTM
	// (a thread's kickoff, a "[tm]" prompt) or FromCoordinator (sent
	// through session.prompt by the project's coordinator); empty for
	// the user's own.
	From string `json:"from,omitempty"`
}

// The senders of a prompt (TranscriptItem.From).
const (
	FromTM          = "tm"
	FromCoordinator = "coordinator"
)

// TranscriptTodo is one entry of a todos item: its text and status
// (pending, in_progress, completed).
type TranscriptTodo struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// TranscriptInput is a line a transcript.watch client sends: Ack counts
// the items taken since the last ack; Detach ends the stream.
type TranscriptInput struct {
	Ack    int  `json:"ack,omitempty"`
	Detach bool `json:"detach,omitempty"`
}
