package proto

import "time"

// A project's memory and library, read as the project popup's Memory and
// Library tabs read them (docs/SPEC.md §4, §7.2, §7.6), for a console
// that isn't on this machine's files (the Mac app, a paired device). The
// human's; each only reads, and never answers a path outside the project:
// a memory file by its path in the project folder (CONTEXT.md, MEMORY.md,
// memory/<name>.md, only those memory.list lists), a library file by its
// thread (or the message a linked project sent it with) and its name.
// The server's hello lists CapProjectFiles.
const (
	// MethodMemoryList lists a project's memory files (ProjectFilesParams):
	// a MemoryListResult.
	MethodMemoryList = "memory.list"
	// MethodMemoryRead reads one (MemoryReadParams): a MemoryReadResult.
	// A path memory.list doesn't list is refused ("not-memory"), a file
	// over MaxMemoryRead too.
	MethodMemoryRead = "memory.read"
	// MethodLibraryList lists a project's library (ProjectFilesParams): a
	// LibraryListResult.
	MethodLibraryList = "library.list"
	// MethodLibraryRead reads a library file a chunk at a time
	// (LibraryReadParams): a LibraryReadResult. The thread library's
	// codes refuse a bad name or thread ("invalid-file", "unknown-thread",
	// "unknown-file"), a file over MaxLibraryRead is refused ("too-large").
	MethodLibraryRead = "library.read"
)

// CapProjectFiles says the server answers memory.list, memory.read,
// library.list and library.read.
const CapProjectFiles = "project-files"

// Bounds of the project files: a memory file of at most MaxMemoryRead
// bytes (project.MaxMemoryRead), a library file of at most
// MaxLibraryRead, read LibraryChunk bytes at a time: in base64 a chunk
// stays under the 1 MiB a device's link carries in a line, as
// SentFileChunk does.
const (
	MaxMemoryRead  = 1 << 20
	MaxLibraryRead = 25 << 20
	LibraryChunk   = 512 << 10
)

// ProjectFilesParams name the project of memory.list and library.list.
type ProjectFilesParams struct {
	Project string `json:"project"`
}

// MemoryFile is one of a project's memory files: its path in the
// project folder, its title (a note's first heading), its size and, for
// a note with front matter, what it touches.
type MemoryFile struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Bytes   int64    `json:"bytes"`
	Touches []string `json:"touches,omitempty"`
}

// MemoryListResult are a project's memory files: CONTEXT.md and
// MEMORY.md when they are there, then the notes by title.
type MemoryListResult struct {
	Files []MemoryFile `json:"files"`
}

// MemoryReadParams name a memory file by its path, as memory.list gave
// it.
type MemoryReadParams struct {
	Project string `json:"project"`
	Path    string `json:"path"`
}

// MemoryReadResult is a memory file's text.
type MemoryReadResult struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// LibraryFile is a file of a project's library: Thread's report
// attached it (with the thread's Task, Title and State: running,
// stopped, resolved or archived), or linked project Peer sent it with
// Message (State received). It carries no path.
type LibraryFile struct {
	Thread  string    `json:"thread,omitempty"`
	Task    string    `json:"task,omitempty"`
	Title   string    `json:"title"`
	State   string    `json:"state"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Time    time.Time `json:"time"`
	Peer    string    `json:"peer,omitempty"`
	Message string    `json:"message,omitempty"`
}

// LibraryListResult is a project's library, newest file first.
type LibraryListResult struct {
	Files []LibraryFile `json:"files"`
}

// LibraryReadParams name a library file by its thread, or the message
// a linked project sent it with (ID), and its name; the chunk read is
// from Offset.
type LibraryReadParams struct {
	Project string `json:"project"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Offset  int64  `json:"offset,omitempty"`
}

// LibraryReadResult is a library file's chunk: Data from Offset (at
// most LibraryChunk bytes) of Size bytes, last modified at MTime.
type LibraryReadResult struct {
	Name   string    `json:"name"`
	Size   int64     `json:"size"`
	MTime  time.Time `json:"mtime,omitzero"`
	Offset int64     `json:"offset"`
	Data   []byte    `json:"data,omitempty"`
}
