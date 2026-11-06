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
	// MethodLibraryThumbnail is a library file's thumbnail, a small JPEG
	// made on first view and cached (LibraryThumbnailParams): a
	// LibraryThumbnailResult. A file that makes none (not PNG, JPEG or
	// GIF, over the limits, unreadable) is refused ErrNoThumbnail, and a
	// console shows it as a row. The server's hello lists
	// CapLibraryThumbnails.
	MethodLibraryThumbnail = "library.thumbnail"
)

// CapProjectFiles says the server answers memory.list, memory.read,
// library.list and library.read.
const CapProjectFiles = "project-files"

// CapLibraryThumbnails says the server answers library.thumbnail and
// peer.library-thumbnail.
const CapLibraryThumbnails = "library-thumbnails"

// ErrNoThumbnail refuses a thumbnail: the file has none to show, and a
// console draws a plain row.
const ErrNoThumbnail = "no-thumbnail"

// ThumbnailSide and MaxThumbnail bound a thumbnail: its long side in
// pixels, and its bytes, which in base64 stay under the 1 MiB a device's
// link carries in a line.
const (
	ThumbnailSide = 400
	MaxThumbnail  = 512 << 10
)

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
// library.list's program form names Program instead: the director's own
// files, then its members' (LibraryFile.Project), those on a paired server
// only where that project's pages switch is on (§16.17). It is refused
// while programs are off.
type ProjectFilesParams struct {
	Project string `json:"project,omitempty"`
	Program string `json:"program,omitempty"`
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
// Message (State received). Repo is the folder name of the thread's
// repository in a project with several. A coordinator's or director's own
// file has LibrarySeatID for its Thread. In a program's listing Project is
// the member it is in and Server that member's paired server, both empty
// for the director's own. It carries no path.
type LibraryFile struct {
	Thread     string    `json:"thread,omitempty"`
	Task       string    `json:"task,omitempty"`
	TaskStatus string    `json:"task_status,omitempty"`
	Title      string    `json:"title"`
	Repo       string    `json:"repo,omitempty"`
	State      string    `json:"state"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Time       time.Time `json:"time"`
	Peer       string    `json:"peer,omitempty"`
	Message    string    `json:"message,omitempty"`
	Project    string    `json:"project,omitempty"`
	Server     string    `json:"server,omitempty"`
}

// LibrarySeatID is the library id of a coordinator's or director's own
// files, where a thread's id goes.
const LibrarySeatID = "seat"

// LibraryListResult is a project's library, newest file first. A program's
// lists the director's own, then each member's in the program's order,
// each newest first. Note says what a program's leaves out for now: a
// paired member that can't be reached, a list cut to MaxProgramLibrary.
type LibraryListResult struct {
	Files []LibraryFile `json:"files"`
	Note  string        `json:"note,omitempty"`
}

// MaxProgramLibrary is the most files a program's listing carries, the
// newest kept, so it stays under the 1 MiB a device's link carries in a
// line.
const MaxProgramLibrary = 2000

// LibraryReadParams name a library file by its thread, or the message
// a linked project sent it with (ID), and its name; the chunk read is
// from Offset. Program names the program whose director's own file it is
// (ID LibrarySeatID), in place of Project.
type LibraryReadParams struct {
	Project string `json:"project,omitempty"`
	Program string `json:"program,omitempty"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Offset  int64  `json:"offset,omitempty"`
}

// LibraryThumbnailParams name a library file as LibraryReadParams do.
type LibraryThumbnailParams struct {
	Project string `json:"project,omitempty"`
	Program string `json:"program,omitempty"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

// LibraryThumbnailResult is a JPEG of Width by Height pixels.
type LibraryThumbnailResult struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Data   []byte `json:"data"`
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
