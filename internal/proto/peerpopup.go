package proto

import "time"

// The project popup of a paired server's project (docs/SPEC.md §16.9,
// §4 Remote dashboard): what the near server's console shows in its
// popup, read-only, beside what peer.watch already carries (the inbox,
// the threads, NEEDS YOU). The far server answers only for a project it
// shares, and only to a peer whose pane switch there is view or type;
// off is ErrPanesOff. Every record here is copied field by field from
// the far server's own (internal/server), so a field added there stays
// home until it is added here.

// MethodPeerPopup reads a shared project's popup (PeerPopupParams): a
// PeerPopup.
const MethodPeerPopup = "peer.popup"

// MethodPeerPopupRead is the console's way to it (PeerPopupReadParams):
// this server asks the paired server Alias, and answers its PeerPopup
// cleaned. The human's.
const MethodPeerPopupRead = "peer.popup-read"

// PeerPopupParams are the params of peer.popup.
type PeerPopupParams struct {
	Project string `json:"project"`
}

// PeerPopupReadParams are the params of peer.popup-read: the paired
// server's alias and its project's slug.
type PeerPopupReadParams struct {
	Alias   string `json:"alias"`
	Project string `json:"project"`
}

// The most a popup answer carries of each list; the near server cuts
// what is over (internal/peer).
const (
	PeerPopupMaxTasks    = 500
	PeerPopupMaxLinks    = 50
	PeerPopupMaxMessages = 50 // a link's newest
	PeerPopupMaxModels   = 50
)

// PeerPopup is a shared project's popup as a paired server may read it:
// its goal, how many repositories (never their paths), its board, its
// settings that say how it runs (no secrets, no paths, no settings
// file), and its links with the asking server's projects with the
// headers of the messages that crossed (never their text). Nothing of
// its memory crosses. Now is the far server's clock as it answered.
type PeerPopup struct {
	Project  string          `json:"project"`
	Goal     string          `json:"goal,omitempty"`
	Now      time.Time       `json:"now,omitzero"`
	Repos    int             `json:"repos"`
	Tasks    []PeerTask      `json:"tasks"`
	Settings PeerSettings    `json:"settings"`
	Links    []PeerPopupLink `json:"links"`
}

// PeerTask is a task of the board: its id, title and status, its
// thread and how many of its steps are done. Its notes, owner, repo and
// steps' words stay home.
type PeerTask struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Thread     string `json:"thread,omitempty"`
	StepsDone  int    `json:"steps_done,omitempty"`
	StepsTotal int    `json:"steps_total,omitempty"`
	Created    string `json:"created,omitempty"`
	Updated    string `json:"updated,omitempty"`
}

// PeerPopupLink is the far project's link with a project of the asking
// server (Project is that project's slug there, on the near server), as
// the far side has it: which way messages go from there, and the
// headers of its messages that crossed (PeerMessageStates), newest
// last; Last is the newest of those. A message held for the far user's
// OK, queued over the rate, dropped or undelivered never crossed and
// stays home, its count too.
type PeerPopupLink struct {
	Project  string            `json:"project"`
	Send     bool              `json:"send,omitempty"`
	Receive  bool              `json:"receive,omitempty"`
	Paused   bool              `json:"paused,omitempty"`
	Unread   int               `json:"unread,omitempty"`
	Last     string            `json:"last,omitempty"`
	LastAt   time.Time         `json:"last_at,omitzero"`
	Messages []PeerLinkMessage `json:"messages"`
}

// PeerMessageStates are the states of a message whose header crosses:
// it reached the other server (link.StateSent, link.StateRead).
var PeerMessageStates = []string{"sent", "read"}

// PeerLinkMessage is a message's header: no text, no attachments' names.
type PeerLinkMessage struct {
	ID      string    `json:"id"`
	Out     bool      `json:"out,omitempty"`
	Created time.Time `json:"created"`
	Re      string    `json:"re,omitempty"`
	Bytes   int       `json:"bytes"`
	State   string    `json:"state"`
	By      string    `json:"by,omitempty"`
}

// PeerSettings are the project's settings a paired server may read: how
// its coordinator and threads run. Each field is config.Safety's of the
// same name, its JSON the setting's key. Its branch name template, the
// guard's rules left out and allowed calls, the cap across all projects,
// and its archived and pinned marks stay home.
type PeerSettings struct {
	StartThreads             string   `json:"start_threads"`
	Yolo                     bool     `json:"yolo"`
	CoordinatorApproves      bool     `json:"coordinator_approves"`
	ParallelThreads          int      `json:"parallel_threads"`
	AutoClose                string   `json:"auto_close"`
	AutoCloseDays            int      `json:"auto_close_days"`
	PRFollowup               bool     `json:"pr_followup"`
	PRPollSeconds            int      `json:"pr_poll_seconds"`
	CompleteTasks            string   `json:"complete_tasks"`
	CoordinatorRemoteControl bool     `json:"coordinator_remote_control"`
	AutoClear                bool     `json:"auto_clear"`
	CoordinatorMerges        bool     `json:"coordinator_merges"`
	FastForwardCheckout      bool     `json:"fast_forward_checkout"`
	Protection               string   `json:"protection"`
	CoordinatorAccess        bool     `json:"coordinator_access"`
	ThreadAgent              string   `json:"thread_agent"`
	CoordinatorAgent         string   `json:"coordinator_agent"`
	Models                   []string `json:"models,omitempty"`
	CoordinatorModel         string   `json:"coordinator_model,omitempty"`
	Active                   bool     `json:"active"`
	Paused                   bool     `json:"paused"`
	Share                    bool     `json:"share"`
	Merge                    string   `json:"merge"`
	MergeMethod              string   `json:"merge_method"`
	Guard                    bool     `json:"guard"`
	GuardMode                string   `json:"guard_mode"`
	ArchiveTasksDays         int      `json:"archive_tasks_days"`
	ArchiveThreadsDays       int      `json:"archive_threads_days"`
	ArchiveInboxDays         int      `json:"archive_inbox_days"`
	ArchiveJournalDays       int      `json:"archive_journal_days"`
	LinkBurst                int      `json:"link_burst"`
	LinkPerHour              int      `json:"link_per_hour"`
	LinkMaxKB                int      `json:"link_max_kb"`
}
