package proto

import "time"

// A shared project's folders for a paired server: a thread's worktree or a
// coordinator's repo, read only, while this server's files switch for that
// server is on for the project. The session names the folder, never the
// caller, whose one say is which repo of the project's list, by its name.
// Every record is copied field by field from this server's own
// (internal/server), so a field added there stays home until it is added
// here. Absolute paths, git-ignored files, the contents of what a listing
// marks Withheld and the bytes of what isn't text never cross, and a
// refusal crosses in fixed words (PeerWorktreeRefusal).

// Link methods: a paired server reads them over the link.
const (
	// MethodPeerWorktreeRepos names what a session's Repo may name
	// (PeerWorktreeParams): a PeerWorktreeRepos.
	MethodPeerWorktreeRepos = "peer.worktree.repos"
	// MethodPeerWorktreeList lists a folder, or finds files under it
	// (PeerWorktreeListParams): a PeerWorktreeList.
	MethodPeerWorktreeList = "peer.worktree.list"
	// MethodPeerWorktreeRead reads a text file a chunk at a time
	// (PeerWorktreeReadParams): a PeerWorktreeRead.
	MethodPeerWorktreeRead = "peer.worktree.read"
	// MethodPeerWorktreeStamp says whether what list answered may have
	// changed (PeerWorktreeStampParams): a PeerWorktreeStamp.
	MethodPeerWorktreeStamp = "peer.worktree.stamp"
	// MethodPeerWorktreeDiff says what changed in a file since the base
	// (PeerWorktreeDiffParams): a PeerWorktreeDiff.
	MethodPeerWorktreeDiff = "peer.worktree.diff"
)

// Refusals only these methods give, besides the files methods' own
// (ErrNoFiles, ErrInvalidPath, ErrLinkOut, ErrIgnored, ErrNotFile,
// ErrFileGone) and ErrNotShared, ErrUnknownSession, ErrBadParams.
const (
	ErrFilesOff    = "files-off"    // the files switch is off for the project, or there is no such project
	ErrWithheld    = "withheld"     // its listing shows it, and no more
	ErrRateLimited = "rate-limited" // over the rate: nothing is wrong, ask later
)

// Bounds of the link's reads: a file's first PeerWorktreeMaxRead bytes,
// PeerLibraryChunk at a time; a stamp of PeerWorktreeStamped folders; hunks,
// and a listing, of at most PeerWorktreeMaxHunks bytes as JSON writes them.
// The owning server answers a peer PeerWorktreeRate requests a second,
// PeerWorktreeRunning at once.
const (
	PeerWorktreeMaxRead  = 1 << 20
	PeerWorktreeStamped  = 64
	PeerWorktreeMaxHunks = 640 << 10
	PeerWorktreeRate     = 4
	PeerWorktreeRunning  = 2
)

// peerWorktreeWords are the sentences a refusal crosses in: never the
// words of the error behind it, which can hold a path.
var peerWorktreeWords = map[string]string{
	ErrFilesOff:       "this server doesn't share that project's files with yours",
	ErrNotShared:      "this server doesn't share that project",
	ErrUnknownSession: "no such session in that project",
	ErrNoFiles:        "that session has no folder to read",
	ErrBadParams:      "the request isn't one this method takes",
	ErrInvalidPath:    "that isn't a path in the folder",
	ErrLinkOut:        "that link leads out of the folder",
	ErrIgnored:        "git-ignored files stay on this server",
	ErrWithheld:       "that name may hold secrets, so it stays on this server",
	ErrFileGone:       "that is no longer there",
	ErrNotFile:        "that isn't the kind of file asked for",
	ErrRateLimited:    "too many requests just now",
	ErrInternal:       "the peer couldn't answer",
}

// PeerWorktreeRefusal is the refusal code means, in its fixed words: a code
// without words is ErrInternal.
func PeerWorktreeRefusal(code string) *Error {
	words, ok := peerWorktreeWords[code]
	if !ok {
		code, words = ErrInternal, peerWorktreeWords[ErrInternal]
	}
	return &Error{Code: code, Message: words}
}

// PeerWorktreeParams name a shared project and one of its sessions, its
// coordinator or a thread.
type PeerWorktreeParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
}

// PeerWorktreeRepos are the names a coordinator's Repo may be, in the
// project's order, the first being the default. A thread has none.
type PeerWorktreeRepos struct {
	Repos []string `json:"repos"`
}

// PeerWorktreeListParams name a folder of the session's: Path, "" for its
// top. Query or Changed list the matching files under it instead, whole
// paths, flat, as FilesListParams. There is no way to ask for ignored files.
type PeerWorktreeListParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path,omitempty"`
	Query   string `json:"query,omitempty"`
	Changed bool   `json:"changed,omitempty"`
}

// PeerWorktreeList is FilesListResult as a paired server may see it, without
// the folder's absolute path.
type PeerWorktreeList struct {
	Path      string          `json:"path"`
	Git       bool            `json:"git,omitempty"`
	Files     int             `json:"files,omitempty"`
	Added     int             `json:"added,omitempty"`
	Deleted   int             `json:"deleted,omitempty"`
	Entries   []PeerFileEntry `json:"entries"`
	Truncated bool            `json:"truncated,omitempty"`
}

// PeerFileEntry is FileEntry as a paired server may see it. Withheld: it
// shows by name alone, and read and diff refuse it.
type PeerFileEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Size     int64     `json:"size,omitempty"`
	MTime    time.Time `json:"mtime,omitzero"`
	Status   string    `json:"status,omitempty"`
	Changed  bool      `json:"changed,omitempty"`
	Link     bool      `json:"link,omitempty"`
	LinkOut  bool      `json:"link_out,omitempty"`
	Withheld bool      `json:"withheld,omitempty"`
}

// PeerWorktreeReadParams name a file of the session's; the chunk read is
// from Offset. There is no way to ask for an ignored file or for the bytes
// of one that isn't text.
type PeerWorktreeReadParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path"`
	Offset  int64  `json:"offset,omitempty"`
}

// PeerWorktreeRead is a file's chunk: Data from Offset, of Size bytes.
// Binary: not text, name and size only. Truncated: Size is over
// PeerWorktreeMaxRead, and no Data comes past it.
type PeerWorktreeRead struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime,omitzero"`
	Offset    int64     `json:"offset"`
	Binary    bool      `json:"binary,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Data      []byte    `json:"data,omitempty"`
}

// PeerWorktreeStampParams name the folders a console shows, at most
// PeerWorktreeStamped; "" is the top.
type PeerWorktreeStampParams struct {
	Project string   `json:"project"`
	Session string   `json:"session"`
	Repo    string   `json:"repo,omitempty"`
	Folders []string `json:"folders,omitempty"`
}

// PeerWorktreeStamp is an opaque stamp, as FilesStampResult. An ignored or
// withheld folder counts as one that doesn't answer.
type PeerWorktreeStamp struct {
	Stamp string `json:"stamp"`
}

// PeerWorktreeDiffParams name a file of the session's, a deleted one too.
type PeerWorktreeDiffParams struct {
	Project string `json:"project"`
	Session string `json:"session"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path"`
}

// PeerWorktreeDiff is FilesDiffResult as a paired server may see it. A file
// whose path or From is withheld is refused, not answered.
type PeerWorktreeDiff struct {
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Status    string         `json:"status,omitempty"`
	From      string         `json:"from,omitempty"`
	Base      string         `json:"base,omitempty"`
	Size      int64          `json:"size,omitempty"`
	MTime     time.Time      `json:"mtime,omitzero"`
	Added     int            `json:"added,omitempty"`
	Deleted   int            `json:"deleted,omitempty"`
	Binary    bool           `json:"binary,omitempty"`
	TooBig    bool           `json:"too_big,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	Hunks     []PeerFileHunk `json:"hunks,omitempty"`
}

// PeerFileHunk is FilesHunk as a paired server may see it.
type PeerFileHunk struct {
	Old     int      `json:"old"`
	New     int      `json:"new"`
	Added   int      `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}
