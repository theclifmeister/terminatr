package proto

import (
	"strings"
	"time"
)

// Coordinator links across servers (docs/SPEC.md §16.11): a message,
// or a receipt, from a project on a paired server to one here. Each
// server's outbox sends them over the link in sequence order.
const (
	// MethodPeerMessage delivers a PeerMessage: a PeerMessageResult.
	MethodPeerMessage = "peer.message"
)

// PeerMessage kinds.
const (
	// PeerMessageText is a coordinator's (or the user's) message.
	PeerMessageText = "message"
	// PeerMessageRead is the handled receipt: the receiving project's
	// coordinator handled the message's item.
	PeerMessageRead = "read"
	// PeerLinkRequest asks the receiving project's user to link back
	// (docs/SPEC.md §16.11): Allow is the requesting side's entry, ID
	// the request's (R-<fp>-<n>). A server that predates it refuses it
	// as bad-message, as any kind it doesn't know.
	PeerLinkRequest = "link-request"
	// PeerLinkAnswer is that user's answer to request ID: Allow is the
	// answering side's own entry now, "off" when declined.
	PeerLinkAnswer = "link-answer"
	// PeerShareRequest asks the receiving server's user to share a
	// project with the sender (docs/SPEC.md §16.9): ID is the request's
	// (S-<fp>-<n>), For the asking project's slug or "", Text an
	// optional one-line note. From and To are empty: it is the server's,
	// not a project's. An older server refuses it as bad-message.
	PeerShareRequest = "share-request"
	// PeerShareAnswer is that user's answer to request Re: Allow is
	// ShareAccepted or ShareDeclined. Which project was shared travels
	// by the mirror, as any share does.
	PeerShareAnswer = "share-answer"
	// The control half of a file over the link (docs/SPEC.md §16.13):
	// the offer of a file with message Re (ID X-<fp>-<n>, Name, Size,
	// SHA256), the receiving user's answer to it (Allow accepted or
	// declined), the receipt once it is in place or failed for good
	// (Allow received or failed, Code why), and either side's cancel.
	PeerFileOffer  = "file-offer"
	PeerFileAnswer = "file-answer"
	PeerFileDone   = "file-done"
	PeerFileCancel = "file-cancel"
)

// PeerProgramPrefix starts every program kind (the programs design
// §9.2: program-invite, program-answer, program-task, …). They go only
// to a server whose link hello lists the capability "program", in their
// own outbox stream, which is paused while this server's programs are
// off.
const PeerProgramPrefix = "program-"

// Program membership (the programs design §9.1): the director's server
// invites a paired server's project into one of its programs, for the
// features it asks (Features); that server's user answers (Allow
// accepted or declined, Features the ones granted); either side ends it
// (program-leave). From and To are each side's slug: the program's on
// the director's server, the project's on the child's. ID is the
// invite's, P-<the inviting server's fingerprint>-<n>.
const (
	PeerProgramInvite = "program-invite"
	PeerProgramAnswer = "program-answer"
	PeerProgramLeave  = "program-leave"
)

// The program's work over the link (the programs design §9.2), once a
// membership is accepted. From and To are each side's slug, as for the
// membership kinds. ID is a remote message's, M-<the sending server's
// fingerprint>-<n>; an answer's Re is the ID it answers.
//   - program-task: the director files a task (Task without Ref) or
//     changes one (Task.Ref and Task.Edit) on the member's board;
//     program-task-filed answers it: Allow a ProgramFiled… word, Task.Ref
//     the task, Code a refusal.
//   - program-message: the program channel, either way, as a link
//     message (Text, Re, By "director", "coordinator" or "human").
//   - program-status: the member pushes the state of a task the
//     director filed or referenced (Task: Ref, Status, Title, PR, URL,
//     Merged, Changed).
//   - program-start: the director starts a thread for Task.Ref (Agent
//     and Model are hints; Allow ProgramStartDelegate or
//     ProgramStartApproved); program-start-result answers it: Allow
//     ProgramStarted or ProgramFiledRefused, Task.Thread, Code.
//   - program-rename: the director's program was renamed from From to
//     Rename.
const (
	PeerProgramTask        = "program-task"
	PeerProgramTaskFiled   = "program-task-filed"
	PeerProgramMessage     = "program-message"
	PeerProgramStatus      = "program-status"
	PeerProgramStart       = "program-start"
	PeerProgramStartResult = "program-start-result"
	PeerProgramRename      = "program-rename"
)

// The words of a program-task-filed's and a program-start-result's
// Allow.
const (
	ProgramFiled    = "created"  // filed on the board
	ProgramExisting = "existing" // an open task of that title was there
	ProgramEdited   = "edited"   // the change was made
	ProgramProposed = "proposed" // program_tasks = "propose": an item the coordinator proposes
	ProgramRefused  = "refused"  // Code says why
	ProgramStarted  = "started"  // the thread started
)

// The words of a program-start's Allow: why the director may start it.
const (
	ProgramStartDelegate = "delegate" // the program's authority is delegate
	ProgramStartApproved = "approved" // the director's user said so (--approved-by-user)
)

// ProgramTask is the task part of the program work kinds; the
// receiver cleans and caps every string.
type ProgramTask struct {
	// Ref is the member's task, "T7": the one changed, filed, pushed or
	// started.
	Ref string `json:"ref,omitempty"`
	// A new task (program-task without Ref): Key names it in the route,
	// After are refs into the member's own board ("T3") and the
	// director's server's member tasks ("acme-api/T12", kept there as
	// text), Umbrella the program's task it is part of ("acme/T4").
	Key      string   `json:"key,omitempty"`
	Title    string   `json:"title,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Steps    []string `json:"steps,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Status   string   `json:"status,omitempty"`
	Repo     string   `json:"repo,omitempty"`
	Design   string   `json:"design,omitempty"`
	After    []string `json:"after,omitempty"`
	Umbrella string   `json:"umbrella,omitempty"`
	// Edit is a program-task's change of task Ref.
	Edit *ProgramTaskEdit `json:"edit,omitempty"`
	// A program-status: the thread's PR, its https URL, whether it
	// merged, and when the task last changed.
	PR      int       `json:"pr,omitempty"`
	URL     string    `json:"url,omitempty"`
	Merged  bool      `json:"merged,omitempty"`
	Changed time.Time `json:"changed,omitzero"`
	// A program-start's hints, and its result's thread.
	Agent  string `json:"agent,omitempty"`
	Model  string `json:"model,omitempty"`
	Thread string `json:"thread,omitempty"`
}

// ProgramTaskEdit is a director's change of a member's task (the
// programs design §5.2); nil leaves a field alone. After replaces the
// refs, an empty list clears them.
type ProgramTaskEdit struct {
	Priority *string   `json:"priority,omitempty"`
	Design   *string   `json:"design,omitempty"`
	Title    *string   `json:"title,omitempty"`
	Repo     *string   `json:"repo,omitempty"`
	Status   *string   `json:"status,omitempty"`
	After    *[]string `json:"after,omitempty"`
	Note     string    `json:"note,omitempty"`
}

// The words of a program-answer's Allow.
const (
	ProgramAccepted = "accepted"
	ProgramDeclined = "declined"
)

// The features a program invite asks for and an answer grants.
const (
	ProgramFeatureTasks    = "tasks"    // the director files tasks (program_tasks)
	ProgramFeatureMessages = "messages" // the program channel
	ProgramFeatureStatus   = "status"   // status pushes (program_status)
	ProgramFeatureStarts   = "starts"   // the director starts threads (program_starts)
)

// ProgramFeatures are the features, in order.
var ProgramFeatures = []string{ProgramFeatureTasks, ProgramFeatureMessages, ProgramFeatureStatus, ProgramFeatureStarts}

// IsProgramKind says whether kind is a program kind.
func IsProgramKind(kind string) bool { return strings.HasPrefix(kind, PeerProgramPrefix) }

// The words of a share-answer's Allow.
const (
	ShareAccepted = "accepted"
	ShareDeclined = "declined"
)

// The words of a file-answer's and a file-done's Allow.
const (
	FileAccepted = "accepted"
	FileDeclined = "declined"
	FileReceived = "received"
	FileFailed   = "failed"
)

// PeerMessage is one entry of a server's outbox to a paired server.
// Epoch and Seq order and dedupe the entries of one sending server: the
// receiver takes each Seq of an Epoch once. From and To are the slugs of
// the sending and receiving projects, each on its own server; the
// receiver knows the sending server by its link, never by the message.
type PeerMessage struct {
	Epoch   string    `json:"epoch"`
	Seq     int       `json:"seq"`
	Kind    string    `json:"kind"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	ID      string    `json:"id"`
	Created time.Time `json:"created,omitzero"`
	Re      string    `json:"re,omitempty"`
	By      string    `json:"by,omitempty"` // "coordinator" or "human"
	Text    string    `json:"text,omitempty"`
	Allow   string    `json:"allow,omitempty"` // link-request, link-answer, share-answer, file-answer, file-done
	// For is a share-request's asking project, "" for none in particular.
	For string `json:"for,omitempty"`
	// A file-offer's file: its cleaned base name, size in bytes and
	// SHA-256 in hex; Code is a file-done's failure, a fixed word.
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Code   string `json:"code,omitempty"`
	// Features are a program-invite's asked features and a
	// program-answer's granted ones (ProgramFeatures).
	Features []string `json:"features,omitempty"`
	// Task is the task part of the program work kinds; Rename a
	// program-rename's new slug.
	Task   *ProgramTask `json:"task,omitempty"`
	Rename string       `json:"rename,omitempty"`
}

// MethodPeerFileFetch is the data half of a file over the link (docs/SPEC.md
// §16.13): the receiving server asks for an offered file's bytes from
// Offset, on a stream id it picked; the answer is the file's size and
// SHA-256, then binary frames carry the bytes. Only when both hellos list
// the file capability.
const MethodPeerFileFetch = "peer.file.fetch"

// PeerFileFetchParams asks for transfer ID's bytes from Offset.
type PeerFileFetchParams struct {
	ID     string `json:"id"`
	Stream uint32 `json:"stream"`
	Offset int64  `json:"offset"`
}

// PeerFileFetchResult is the file a fetch opened.
type PeerFileFetchResult struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Refusals of peer.file.fetch, and the words a file stream's abort frame
// says.
const (
	ErrUnknownFile = "unknown-file" // no such offer to that server, or cancelled
	ErrFileBusy    = "busy"         // that link serves its streams already
	ErrFileGone    = "gone"         // the sender's copy was deleted
	ErrNotLinked   = "not-linked"   // the link doesn't send there now
	ErrLinkPaused  = "link-paused"

	FileCancelled  = "cancelled"
	FileBadContent = "bad-content"
	FileTooBig     = "too-big"
	FileStalled    = "stalled"
	FileNoSpace    = "no-space"
	FileStopping   = "shutting-down"
)

// PeerMessageResult statuses.
const (
	PeerDelivered = "ok"        // taken: the delivered receipt
	PeerDuplicate = "duplicate" // taken before
	PeerRetry     = "retry"     // not now (Code says why); the outbox tries again
	PeerRefused   = "refused"   // never (Code says why); the sender hears of it
)

// PeerMessageResult answers a PeerMessage. Code is one of a fixed set
// of words (link.RefusalWords); the sender shows nothing else of it.
type PeerMessageResult struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
	// RetryAfter is, with PeerRetry, how many seconds the outbox waits
	// before it tries that server again; 0 is its next poll.
	RetryAfter int `json:"retry_after,omitempty"`
}
