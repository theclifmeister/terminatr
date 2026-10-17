package proto

import "time"

// MethodLogList lists the logs on the server's host (docs/SPEC.md §11.1):
// its own, the service's, the consoles' and each mod's, for the , popup's
// Logs tab. The human's: an agent never reads them through the server.
const MethodLogList = "log.list"

// The logs' ids. A mod's is LogModPrefix and its id.
const (
	LogServer    = "server"
	LogService   = "service"
	LogConsole   = "console"
	LogModPrefix = "mod:"
)

// LogInfo is one log: what log.read takes (ID), its file and how big it
// is. Missing is set for a log that has no file yet.
type LogInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mod     time.Time `json:"mod,omitzero"`
	Missing bool      `json:"missing,omitempty"`
}

// LogListResult is the answer of log.list.
type LogListResult struct {
	Logs []LogInfo `json:"logs"`
}

// MethodLogRead reads a chunk of a log by its id, never a path: the end
// (the tail), the chunk before an offset (scrolling up), or what follows
// one (following it). The human's.
const MethodLogRead = "log.read"

// LogReadMax is the largest chunk log.read returns, and LogReadDefault
// the one it returns when Max is 0.
const (
	LogReadMax     = 256 << 10
	LogReadDefault = 64 << 10
)

// LogReadParams are the params of log.read. With After set it reads what
// follows After; else the chunk that ends at Before (0: the end).
type LogReadParams struct {
	ID     string `json:"id"`
	Before int64  `json:"before,omitempty"`
	After  *int64 `json:"after,omitempty"`
	Max    int    `json:"max,omitempty"`
}

// LogReadResult is the answer of log.read: Text is the file's bytes
// [Start, End), whole lines only, of a file Size long. Reset says the
// file is shorter than After (it was rotated or cut), and Text is then
// its end, as a read of the tail.
type LogReadResult struct {
	Text  string `json:"text"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Size  int64  `json:"size"`
	Reset bool   `json:"reset,omitempty"`
}
