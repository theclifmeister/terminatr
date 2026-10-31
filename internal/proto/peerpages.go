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

// A shared project's designs (T888), under the same project pages
// switch: each design's record and its build, read-only. A design's page
// is a link to a third server (an https URL, a claude.ai artifact as a
// rule), and stays home: a shared link to such a page is a key to it,
// and this server can't vouch for what is there. Page says only that
// the design has one. Its design task's open questions stay home too:
// they are its user's to answer, and cross only as needs (§16.15),
// under the needs switch.
const (
	// MethodPeerPagesDesigns lists a shared project's designs
	// (PeerPagesParams): a PeerDesigns.
	MethodPeerPagesDesigns = "peer.pages.designs"
	// MethodPeerDesignList is peer.pages.designs of the paired server
	// Alias (PeerPagesReadParams): a PeerDesigns.
	MethodPeerDesignList = "peer.design-list"
)

// CapPeerDesigns says the server answers peer.design-list.
const CapPeerDesigns = "peer-designs"

// The most a designs answer carries: designs, build tasks per design,
// a design's title in runes; an answer is cut to MaxDesignAnswer runes
// and a design keeps at most MaxDesignAnswers of them. The far server
// keeps its answer under PeerPagesBudget bytes of text, dropping done
// and archived designs first, and says Truncated.
const (
	PeerDesignsMax     = 200
	PeerDesignBuild    = 100
	PeerDesignTitle    = 200
	PeerDesignAnswer   = 300
	PeerDesignAnswers  = 200
	PeerDesignAfterMax = 20
)

// PeerDesigns are a shared project's designs as a paired server may read
// them, by name; Truncated: some didn't fit (done and archived ones go
// first).
type PeerDesigns struct {
	Designs   []PeerDesign `json:"designs"`
	Truncated bool         `json:"truncated,omitempty"`
}

// PeerDesign is one design: its name, title, status from its build, its
// design task and that task's status, Page (it has a page, which stays
// home), how many build tasks are done, the user's decided answers, its
// build in after order, and whether it is archived or a thread of its
// build is running a turn.
type PeerDesign struct {
	Name       string           `json:"name"`
	Title      string           `json:"title"`
	Status     string           `json:"status"`
	Task       string           `json:"task,omitempty"`
	TaskStatus string           `json:"task_status,omitempty"`
	Page       bool             `json:"page,omitempty"`
	TasksDone  int              `json:"tasks_done"`
	Answers    []string         `json:"answers"`
	Build      []PeerDesignTask `json:"build"`
	Archived   bool             `json:"archived,omitempty"`
	Working    bool             `json:"working,omitempty"`
}

// PeerDesignTask is a build task: its id, title, status, the tasks it
// comes after, whether it is archived and whether a thread of it is
// running a turn.
type PeerDesignTask struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	After    []string `json:"after,omitempty"`
	Archived bool     `json:"archived,omitempty"`
	Working  bool     `json:"working,omitempty"`
}
