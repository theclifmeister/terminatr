package proto

// A project's daily numbers (docs/SPEC.md §5.1, History; design
// board-library, steps 3 and 4): done per day and the facts, from tm's
// records, and each repo's releases from its tags, read locally. A
// console reads them again when the board's done count changes, not on a
// timer.
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
	// MethodReleaseTags answers a project's release_tags
	// (ReleaseTagsParams): a ReleaseTags. The human's.
	MethodReleaseTags = "release.tags"
	// MethodReleaseTagsSet changes a project's release_tags
	// (ReleaseTagsSetParams): a ReleaseTags. The human's.
	MethodReleaseTagsSet = "release.tags.set"
)

const (
	// CapHistory says the server answers project.history.
	CapHistory = "history"
	// CapPeerHistory says the server answers peer.history.
	CapPeerHistory = "peer-history"
	// CapReleaseTags says the server answers release.tags and
	// release.tags.set.
	CapReleaseTags = "release-tags"
)

// The ways a project's tags count (ReleaseTags.Mode).
const (
	// ReleaseTagsRule is the default rule: version-shaped tags.
	ReleaseTagsRule = "rule"
	// ReleaseTagsPatterns is the project's own patterns, which replace it.
	ReleaseTagsPatterns = "patterns"
	// ReleaseTagsNone is no releases.
	ReleaseTagsNone = "none"
	// ReleaseTagsFollow, only to set, is what all projects have.
	ReleaseTagsFollow = "follow"
)

// HistoryReleases is the most releases of one repo History lists, its
// newest: HistoryRepo.Count still says how many there are.
const HistoryReleases = 1000

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
	// Releases are the releases of each of the project's repos that has
	// any, in its list's order; their counts are never summed, as one
	// repo's versions say nothing of another's. None in a plain folder,
	// with no tags, when git fails, and from a paired server.
	Releases []HistoryRepo `json:"releases,omitempty"`
	// Pulse is the band at the top of the board, from the days and the
	// releases; none before it appears, and none from a paired server.
	Pulse *HistoryPulse `json:"pulse,omitempty"`
}

// What a Pulse's bars stand for (HistoryPulse.Unit).
const (
	PulseDay   = "day"
	PulseWeek  = "week"
	PulseMonth = "month"
)

// HistoryPulse is the Pulse (design board-library, B5 and B16): tasks done
// and releases per day, then week, then month, decided here so that every
// surface shows the same bars in the same words. It appears at 10 done
// tasks across 7 days.
type HistoryPulse struct {
	// Unit is what a bar stands for: PulseDay, PulseWeek or PulseMonth.
	Unit string `json:"unit"`
	// Span says how long the work has run: "33 days", "5 months".
	Span string `json:"span"`
	// Releases is the count in words, each repo's when the project has
	// several: "128 releases", "terminatr 128 · site 6 releases". "" with
	// none, where the words are left out.
	Releases string `json:"releases,omitempty"`
	// Bars run from the first with work to the one holding today, one for
	// every span, oldest first: a span with no work has Done 0.
	Bars []PulseBar `json:"bars"`
}

// PulseBar is one bar: the days it covers, both inclusive and in the
// answering server's time zone (task.done's from and to read its tasks,
// with no tz), its name on the axis ("5 Oct", "today"), how many tasks
// finished, whether any repo released (a dot), and what it says of itself
// ("Mon 5 Oct: 28 done, 8 releases").
type PulseBar struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Label    string `json:"label"`
	Done     int    `json:"done"`
	Released bool   `json:"released,omitempty"`
	Tip      string `json:"tip"`
}

// HistoryRepo is one repo's releases: tags that count, since the
// project's first record, one for each commit.
type HistoryRepo struct {
	// Repo is the repo's name, with its parent's when two share one.
	Repo string `json:"repo"`
	// Count is how many releases the repo has.
	Count int `json:"count"`
	// Parts count a monorepo's parts on their own, only when a release
	// names a part: "" is the releases that name none.
	Parts []HistoryPart `json:"parts,omitempty"`
	// Releases are the newest HistoryReleases, oldest first.
	Releases []HistoryRelease `json:"releases"`
}

// HistoryPart is how many releases a monorepo's part has.
type HistoryPart struct {
	Part  string `json:"part,omitempty"`
	Count int    `json:"count"`
}

// HistoryRelease is one release: its tag, its monorepo part if any, what
// to show after the part (api/v1.2 is part api, version 1.2; a tag only
// a pattern admits shows whole), and when in UTC.
type HistoryRelease struct {
	Tag     string `json:"tag"`
	Part    string `json:"part,omitempty"`
	Version string `json:"version"`
	At      string `json:"at"`
}

// ReleaseTagsParams are release.tags's: the project.
type ReleaseTagsParams struct {
	Project string `json:"project"`
}

// ReleaseTagsSetParams are release.tags.set's: the project, the Mode
// (ReleaseTagsRule, ReleaseTagsPatterns, ReleaseTagsNone or
// ReleaseTagsFollow) and, for ReleaseTagsPatterns, the patterns.
type ReleaseTagsSetParams struct {
	Project  string   `json:"project"`
	Mode     string   `json:"mode"`
	Patterns []string `json:"patterns,omitempty"`
}

// ReleaseTags is a project's release_tags: how its tags count, whether
// the project sets that itself or follows all projects, and all
// projects' own in words ("version tags").
type ReleaseTags struct {
	Project     string   `json:"project"`
	Mode        string   `json:"mode"`
	Patterns    []string `json:"patterns,omitempty"`
	Own         bool     `json:"own,omitempty"`
	AllProjects string   `json:"all_projects"`
}
