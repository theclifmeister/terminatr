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
	// TranscriptEventBytes bounds the items of one event or page in
	// JSON (T892): a device's link carries at most 1 MiB in a line, and
	// 200 items of up to 8 KiB each don't fit. The items appended go in
	// as many items events as they need; a reset or a page carries the
	// last items that fit, its Before reading the earlier ones.
	TranscriptEventBytes = 512 << 10
)

// TranscriptWatchParams are the params of transcript.watch. Tail, when
// set (the server's caps say CapTranscriptTail), is how many of the last
// items a reset carries, at most TranscriptBacklog; transcript.page reads
// the earlier ones. A paired server's conversation takes no tail yet.
type TranscriptWatchParams struct {
	Session string `json:"session"`
	Tail    int    `json:"tail,omitempty"`
}

// CapTranscriptTail says transcript.watch takes a tail and its resets a
// Before cursor, and the server answers transcript.page.
const CapTranscriptTail = "transcript-tail"

// MethodTranscriptPage reads the items of a session's conversation
// before a cursor (TranscriptPageParams): a TranscriptPageResult. The
// human's only, as transcript.watch; not for a paired server's
// conversation.
const MethodTranscriptPage = "transcript.page"

// TranscriptPageParams name the session, the cursor (a reset's or a
// page's Before) and how many items to read, at most TranscriptBacklog
// (0 is that).
type TranscriptPageParams struct {
	Session string `json:"session"`
	Before  string `json:"before"`
	Limit   int    `json:"limit,omitempty"`
}

// TranscriptPageResult are the items just before the cursor, oldest
// first, and the cursor for the ones before them ("" when there are
// none). The cursor names a place in the transcript file: after the
// file is replaced (a reset), it reads the new one's.
type TranscriptPageResult struct {
	Items  []TranscriptItem `json:"items"`
	Before string           `json:"before,omitempty"`
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
	Event   string           `json:"event"`
	Items   []TranscriptItem `json:"items,omitempty"`
	Skipped int              `json:"skipped,omitempty"`
	// Before, on a reset with earlier items than it carries, is the
	// cursor transcript.page reads them from (CapTranscriptTail).
	Before string            `json:"before,omitempty"`
	Status *TranscriptStatus `json:"status,omitempty"`
	Reason string            `json:"reason,omitempty"`
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
	// Files are what the item puts in front of the user: a prompt's
	// pasted images, the files a call sends (Claude's SendUserFile),
	// shown as its Display says ("render" inline, "attach" as a card
	// without a preview; empty is render). session.file serves each by
	// its Ref. Text is then the call's caption.
	Files   []TranscriptFile `json:"files,omitempty"`
	Display string           `json:"display,omitempty"`
	// From is who sent a prompt, as far as the server knows: FromTM
	// (a thread's kickoff, a "[tm]" prompt) or FromCoordinator (sent
	// through session.prompt by the project's coordinator); empty for
	// the user's own.
	From string `json:"from,omitempty"`
}

// TranscriptFile is a file an item puts in front of the user: Ref names
// it to session.file, Name is its name (a sent file's base name, "Image
// #1"), Path a sent file's path as the call gives it (empty for a pasted
// image), Type the media type session.file sends it as (empty: it
// doesn't send that file's bytes).
type TranscriptFile struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Type string `json:"type,omitempty"`
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

// MethodSessionFile reads a file a session's conversation puts in front
// of the user (SessionFileParams): a SessionFileResult. The human's
// only, and only a file the session's transcript names at the item's
// Ref: a call of its files tool's path, symlinks evaluated, a regular
// file of a type tm sends (agent.SentFileType) and at most MaxSentFile
// bytes; or an image pasted into a prompt. ErrFileGone when the file,
// or the line naming it, is no longer there. The server's hello lists
// CapSessionFile.
const MethodSessionFile = "session.file"

// CapSessionFile says the server answers session.file.
const CapSessionFile = "session-file"

// Bounds of session.file: a file of at most MaxSentFile bytes, read
// SentFileChunk bytes at a time. A chunk goes in base64, a third
// larger, so its answer stays well under the 1 MiB a device's link
// carries in a line (T892: a pasted screenshot's 1 MiB chunk dropped the
// phone's link). Clients read on from Offset plus the bytes they got.
const (
	MaxSentFile   = 25 << 20
	SentFileChunk = 512 << 10
)

// SessionFileParams name a file of session's conversation by its Ref
// (TranscriptFile.Ref), and the chunk of it to read from Offset; Stat
// asks only for its name, type and size.
type SessionFileParams struct {
	Session string `json:"session"`
	Ref     string `json:"ref"`
	Offset  int64  `json:"offset,omitempty"`
	Stat    bool   `json:"stat,omitempty"`
}

// SessionFileResult is a file's chunk: Data from Offset (at most
// SentFileChunk bytes; empty for a stat) of Size bytes, last modified at
// MTime (a pasted image's is zero). Type is the media type; Sendable
// says whether its bytes may be read (a stat answers a file of another
// type, or a larger one, without them).
type SessionFileResult struct {
	Name     string    `json:"name"`
	Type     string    `json:"type,omitempty"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime,omitzero"`
	Sendable bool      `json:"sendable"`
	Offset   int64     `json:"offset"`
	Data     []byte    `json:"data,omitempty"`
}
