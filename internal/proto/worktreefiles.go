package proto

import "time"

// A session's folder, read only (docs/SPEC.md §3.3): what the Files view
// of the thread's info panel browses, and the Mac's panel after it. The
// human's. A thread's folder is its worktree, a coordinator's the
// project's first repo at its main checkout; a director, a shell and a
// paired server's session have none ("no-files"). A path is relative to
// that folder, with slashes: no parent, never .git, no link leading out,
// and a git-ignored file or folder only when asked (package filetree).
// The server's hello lists CapWorktreeFiles.
const (
	// MethodFilesList lists a folder, or finds files under it (FilesListParams):
	// a FilesListResult.
	MethodFilesList = "files.list"
	// MethodFilesRead reads a file a chunk at a time (FilesReadParams): a
	// FilesReadResult.
	MethodFilesRead = "files.read"
	// MethodFilesStamp says whether what files.list answered may have
	// changed (FilesStampParams): a FilesStampResult.
	MethodFilesStamp = "files.stamp"
)

// CapWorktreeFiles says the server answers files.list, files.read and
// files.stamp.
const CapWorktreeFiles = "worktree-files"

// Refusals of the files methods, besides ErrFileGone for what isn't
// there any more and ErrBadParams.
const (
	ErrNoFiles     = "no-files"     // the session has no folder here: the message says why
	ErrInvalidPath = "invalid-path" // a parent, an absolute path, .git, an unsafe character
	ErrLinkOut     = "link-out"     // a link leads out of the folder, or into .git
	ErrIgnored     = "ignored"      // git-ignored, and not asked for
	ErrNotFile     = "not-file"     // a folder or a special file where a file was named, or the reverse
)

// Bounds of the files methods: a read gives the first MaxFilesRead bytes
// of a file, LibraryChunk at a time, and says Truncated when the file is
// longer; a folder lists MaxFilesEntries, a search MaxFilesFound; a stamp
// looks at MaxFilesStamped folders.
const (
	MaxFilesRead    = MaxLibraryRead
	MaxFilesEntries = 2000
	MaxFilesFound   = 500
	MaxFilesStamped = 256
	MaxFilesQuery   = 200
)

// Kinds of a FileEntry; a link has the kind of what it leads to, and
// KindOther when that can't be opened.
const (
	FileKindFile  = "file"
	FileKindDir   = "dir"
	FileKindOther = "other"
)

// Marks of a FileEntry: its git status against the thread's base.
const (
	FileModified = "M"
	FileAdded    = "A"
	FileDeleted  = "D"
)

// FilesListParams name a folder of the session's: Path, "" for its top.
// Ignored lists git-ignored entries too, marked. Query or Changed list
// the matching files under Path instead, whole paths, flat: those whose
// path holds Query, ignoring case; those git marks.
type FilesListParams struct {
	Session string `json:"session"`
	Path    string `json:"path,omitempty"`
	Ignored bool   `json:"ignored,omitempty"`
	Query   string `json:"query,omitempty"`
	Changed bool   `json:"changed,omitempty"`
}

// FileEntry is a file or folder. Path names it to files.list and
// files.read. Status is its mark ("" for none, a file deleted still has
// its entry), Changed says a folder holds marked files. Link: it is a
// symbolic link; LinkOut: one that doesn't open.
type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Kind    string    `json:"kind"`
	Size    int64     `json:"size,omitempty"`
	MTime   time.Time `json:"mtime,omitzero"`
	Status  string    `json:"status,omitempty"`
	Changed bool      `json:"changed,omitempty"`
	Ignored bool      `json:"ignored,omitempty"`
	Link    bool      `json:"link,omitempty"`
	LinkOut bool      `json:"link_out,omitempty"`
}

// FilesListResult is a folder's entries, folders first by name, or a
// search's by path. Root is the folder's absolute path. Git: Root is a
// checkout whose marks and ignored files are known; Files, Added and
// Deleted are then its changed files and their lines, the whole folder's
// whatever Path. Truncated: more entries than the bounds, or more changes
// than git was read for.
type FilesListResult struct {
	Root      string      `json:"root"`
	Path      string      `json:"path"`
	Git       bool        `json:"git,omitempty"`
	Files     int         `json:"files,omitempty"`
	Added     int         `json:"added,omitempty"`
	Deleted   int         `json:"deleted,omitempty"`
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// FilesReadParams name a file of the session's, the chunk read is from
// Offset. Ignored allows a git-ignored file.
type FilesReadParams struct {
	Session string `json:"session"`
	Path    string `json:"path"`
	Offset  int64  `json:"offset,omitempty"`
	Ignored bool   `json:"ignored,omitempty"`
}

// FilesReadResult is a file's chunk: Data from Offset (at most
// LibraryChunk bytes) of Size bytes, last modified at MTime. Binary: not
// text, no Data, name and size only. Truncated: Size is over MaxFilesRead,
// and no Data comes past it.
type FilesReadResult struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime,omitzero"`
	Offset    int64     `json:"offset"`
	Binary    bool      `json:"binary,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Data      []byte    `json:"data,omitempty"`
}

// FilesStampParams name the folders a console shows, at most
// MaxFilesStamped; "" is the top.
type FilesStampParams struct {
	Session string   `json:"session"`
	Folders []string `json:"folders,omitempty"`
}

// FilesStampResult is an opaque stamp: while it stays the same, the
// session's git status and those folders' entries did. A file changed
// inside a folder not named, and git-ignored, goes unseen.
type FilesStampResult struct {
	Stamp string `json:"stamp"`
}
