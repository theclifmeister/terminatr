package proto

import "time"

// A shared project's pages for a paired server (docs/SPEC.md §16.17): its
// board, its overview and its library, read-only, while this server's
// project pages switch for that server is on for the project (tm peer
// pages ALIAS PROJECT on). The far server answers only for a project it
// shares with the pages switch on; otherwise ErrPagesOff. Its memory
// never crosses, nor do a task's notes, its repositories' paths or the
// files a linked project brought. Every record is copied field by field
// from the far server's own (internal/server), so a field added there
// stays home until it is added here.

// Link methods: a paired server reads them over the link.
const (
	// MethodPeerPages reads a shared project's board and overview
	// (PeerPagesParams): a PeerPages.
	MethodPeerPages = "peer.pages"
	// MethodPeerPagesLibrary lists its library (PeerPagesParams): a
	// PeerLibrary, its threads' files only.
	MethodPeerPagesLibrary = "peer.pages.library"
	// MethodPeerPagesLibraryRead reads one of those files a chunk at a
	// time (PeerLibraryReadParams): a LibraryReadResult of at most
	// PeerLibraryChunk bytes. A file peer.pages.library doesn't list is
	// refused.
	MethodPeerPagesLibraryRead = "peer.pages.library.read"
)

// Control methods: a console's way to a paired server's pages, through
// this server's link. The human's; each answers the far server's word
// cleaned.
const (
	// MethodPeerPagesRead is peer.pages of the paired server Alias
	// (PeerPagesReadParams): a PeerPages.
	MethodPeerPagesRead = "peer.pages-read"
	// MethodPeerLibraryList is peer.pages.library of the paired server
	// Alias (PeerPagesReadParams): a PeerLibrary.
	MethodPeerLibraryList = "peer.library-list"
	// MethodPeerLibraryRead is peer.pages.library.read of the paired
	// server Alias (PeerLibraryReadReadParams): a LibraryReadResult.
	MethodPeerLibraryRead = "peer.library-read"
)

// CapPeerPages says the server answers peer.pages-read,
// peer.library-list and peer.library-read, and peer.list's projects say
// whether their pages are shared.
const CapPeerPages = "peer-pages"

// ErrPagesOff refuses a page of a shared project whose pages switch is
// off for the caller.
const ErrPagesOff = "pages-off"

// ErrNoPages is a paired server whose tm serves no pages (its hello
// doesn't list pages): the console says "not shared" all the same.
const ErrNoPages = "no-pages"

// The most a pages answer carries; the near server cuts what is over
// (internal/peer). The far server keeps its answer under PeerPagesBudget
// bytes of text, dropping done tasks first, then the rest, and says
// Truncated.
const (
	PeerPagesMaxTasks  = 500
	PeerPagesMaxSteps  = 50
	PeerPagesMaxFiles  = 500
	PeerPagesMaxBody   = 32 << 10
	PeerPagesTitle     = 200 // a task's title, in runes
	PeerPagesStep      = 300 // a step's text, in runes
	PeerPagesBudget    = 640 << 10
	PeerLibraryChunk   = 512 << 10
	PeerLibraryMaxRead = MaxLibraryRead
)

// PeerPagesParams name the shared project.
type PeerPagesParams struct {
	Project string `json:"project"`
}

// PeerPagesReadParams are a console's: the paired server's alias and its
// project's slug.
type PeerPagesReadParams struct {
	Alias   string `json:"alias"`
	Project string `json:"project"`
}

// PeerPages are a shared project's board and overview as a paired server
// may read them: Tasks, in board order, every one not archived (done
// ones last when the answer is cut: Truncated), and the Overview. Now is
// the far server's clock as it answered.
type PeerPages struct {
	Project   string         `json:"project"`
	Overview  PeerOverview   `json:"overview"`
	Tasks     []PeerPageTask `json:"tasks"`
	Truncated bool           `json:"truncated,omitempty"`
	Now       time.Time      `json:"now,omitzero"`
}

// PeerOverview is a project's overview: its goal, PROJECT.md's body
// (its standing instructions, at most PeerPagesMaxBody bytes) and how
// many repositories it has (never their paths).
type PeerOverview struct {
	Goal  string `json:"goal,omitempty"`
	Body  string `json:"body,omitempty"`
	Repos int    `json:"repos"`
}

// PeerPageTask is a task of the board: its id, title, status, thread,
// its steps (their words and whether done) and when it was made and
// last changed. Its notes, owner, repo, PR and reports stay home.
type PeerPageTask struct {
	ID      int        `json:"id"`
	Title   string     `json:"title"`
	Status  string     `json:"status"`
	Thread  string     `json:"thread,omitempty"`
	Steps   []PeerStep `json:"steps,omitempty"`
	Created string     `json:"created,omitempty"`
	Updated string     `json:"updated,omitempty"`
}

// PeerStep is one step of a task.
type PeerStep struct {
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

// PeerLibrary is a shared project's library as a paired server may read
// it: the files its threads' reports attached, newest first. Files a
// linked project brought stay home.
type PeerLibrary struct {
	Files []PeerLibraryFile `json:"files"`
}

// PeerLibraryFile is one of them: the thread that attached it (with its
// task, title and state), its name, size and time.
type PeerLibraryFile struct {
	Thread string    `json:"thread"`
	Task   string    `json:"task,omitempty"`
	Title  string    `json:"title"`
	State  string    `json:"state"`
	Name   string    `json:"name"`
	Size   int64     `json:"size"`
	Time   time.Time `json:"time"`
}

// PeerLibraryReadParams name a file of peer.pages.library by its
// thread and name; the chunk read is from Offset.
type PeerLibraryReadParams struct {
	Project string `json:"project"`
	Thread  string `json:"thread"`
	Name    string `json:"name"`
	Offset  int64  `json:"offset,omitempty"`
}

// PeerLibraryReadReadParams are a console's: the paired server's alias
// and peer.pages.library.read's params.
type PeerLibraryReadReadParams struct {
	Alias   string `json:"alias"`
	Project string `json:"project"`
	Thread  string `json:"thread"`
	Name    string `json:"name"`
	Offset  int64  `json:"offset,omitempty"`
}
