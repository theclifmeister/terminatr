package proto

// A project's daily numbers (docs/SPEC.md §5.1, History; design
// board-library, step 3): done per day and the facts, from tm's records
// only, never git. A console reads them again when the board's done
// count changes, not on a timer.
const (
	// MethodProjectHistory answers a project's numbers (HistoryParams):
	// a History. The human's.
	MethodProjectHistory = "project.history"
	// MethodPeerPagesHistory answers a shared project's numbers to a
	// paired server under the pages switch (PeerPagesParams): a History.
	MethodPeerPagesHistory = "peer.pages.history"
	// MethodPeerHistory is peer.pages.history of the paired server Alias
	// (PeerPagesReadParams): a History.
	MethodPeerHistory = "peer.history"
)

const (
	// CapHistory says the server answers project.history.
	CapHistory = "history"
	// CapPeerHistory says the server answers peer.history.
	CapPeerHistory = "peer-history"
)

// PeerHistoryDays is the most days a paired server's answer carries, the
// newest, about ten years of daily work: History.Truncated says some
// were left out.
const PeerHistoryDays = 3660

// TypicalFrom is how many timed tasks a typical time needs (design
// board-library, B16): with fewer, History has none.
const TypicalFrom = 10

// HistoryParams are the params of project.history: the project.
type HistoryParams struct {
	Project string `json:"project"`
}

// HistoryDay is one day with work: the answering server's local day
// (2026-11-06) and how many tasks finished on it.
type HistoryDay struct {
	Day  string `json:"day"`
	Done int    `json:"done"`
}

// History is a project's numbers. Days are the days with work, oldest
// first, in the answering server's time zone; a done task with only a
// day on record counts on that day.
type History struct {
	// FirstRecord is the day of the project's first record: its oldest
	// journal line or task. "" when it has none.
	FirstRecord string `json:"first_record,omitempty"`
	// Done, Open and Dropped count the tasks on the board and in the
	// archive; Open is every task neither done nor dropped.
	Done    int `json:"done"`
	Open    int `json:"open"`
	Dropped int `json:"dropped"`
	// Days are the days with work.
	Days []HistoryDay `json:"days"`
	// BestDay is the day most tasks finished, the first of equals; none
	// before the first finish.
	BestDay *HistoryDay `json:"best_day,omitempty"`
	// Timed counts the done tasks the journal gives both a start and a
	// finish time for.
	Timed int `json:"timed"`
	// TypicalSeconds is the median time from start to done, Typical the
	// same in words ("34 minutes", "5 hours", "2 days"): both only from
	// TypicalFrom timed tasks.
	TypicalSeconds int64  `json:"typical_seconds,omitempty"`
	Typical        string `json:"typical,omitempty"`
	// Truncated says a paired server's answer left out the oldest days
	// (PeerHistoryDays).
	Truncated bool `json:"truncated,omitempty"`
}
