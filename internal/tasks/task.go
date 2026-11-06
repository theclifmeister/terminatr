package tasks

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Status is one of the seven task statuses (§6.1).
type Status string

const (
	Open    Status = "open"    // captured
	Ready   Status = "ready"   // on deck
	Started Status = "started" // in motion
	Blocked Status = "blocked" // waiting on something
	Review  Status = "review"  // finished, waiting for the human
	Done    Status = "done"
	Dropped Status = "dropped" // the user decided not to do it
)

// Statuses lists every status in board order.
var Statuses = []Status{Blocked, Review, Started, Ready, Open, Done, Dropped}

// Closed reports whether s ends a task: done or dropped. Only the
// human sets either (§6.4); only done counts as shipped.
func (s Status) Closed() bool { return s == Done || s == Dropped }

// ParseStatus accepts a status name, or tsk's "start" alias.
func ParseStatus(s string) (Status, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "start" {
		return Started, true
	}
	for _, st := range Statuses {
		if string(st) == s {
			return st, true
		}
	}
	return "", false
}

// Group is a board section, derived only from status.
type Group string

const (
	NeedsYou Group = "Needs you"
	InMotion Group = "In motion"
	OnDeck   Group = "On deck"
	Backlog  Group = "Backlog"
	DoneG    Group = "Done"
	DroppedG Group = "Dropped"
)

// Groups lists the board sections in order.
var Groups = []Group{NeedsYou, InMotion, OnDeck, Backlog, DoneG, DroppedG}

// GroupOf returns the board section of a status.
func GroupOf(s Status) Group {
	switch s {
	case Blocked, Review:
		return NeedsYou
	case Started:
		return InMotion
	case Open:
		return Backlog
	case Done:
		return DoneG
	case Dropped:
		return DroppedG
	}
	return OnDeck
}

// rank orders tasks inside the board: by group, then by id.
func rank(s Status) int {
	switch s {
	case Blocked, Review:
		return 0
	case Started:
		return 1
	case Ready:
		return 2
	case Open:
		return 3
	case Done:
		return 4
	}
	return 5
}

// Priority ranks a task inside its board group: must, should or could;
// "" is none, which sorts last.
type Priority string

const (
	Must   Priority = "must"
	Should Priority = "should"
	Could  Priority = "could"
	// NoPriority is a task without one.
	NoPriority Priority = ""
)

// Priorities lists the levels, highest first.
var Priorities = []Priority{Must, Should, Could}

// ParsePriority accepts must, should or could, and none (or "") for
// none.
func ParsePriority(s string) (Priority, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" {
		return NoPriority, true
	}
	for _, p := range Priorities {
		if string(p) == s {
			return p, true
		}
	}
	return "", false
}

// Rank orders priorities: must 0, should 1, could 2, none 3.
func (p Priority) Rank() int {
	if i := slices.Index(Priorities, p); i >= 0 {
		return i
	}
	return len(Priorities)
}

// String is the level's name, or "none".
func (p Priority) String() string {
	if p == NoPriority {
		return "none"
	}
	return string(p)
}

func checkPriority(s string) (Priority, error) {
	p, ok := ParsePriority(s)
	if !ok {
		return "", refuse("invalid-priority", fmt.Sprintf("unknown priority %q (must, should, could or none)", s))
	}
	return p, nil
}

// Step is one checklist item. N is its 1-based position.
type Step struct {
	N    int    `json:"n"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Task is one task (§6.1). Dates are YYYY-MM-DD.
type Task struct {
	ID       int
	Title    string
	Status   Status
	Priority Priority // must, should, could or none ("")
	Notes    string
	Steps    []Step
	Thread   string
	Owner    string
	Repo     string // the repo its thread works in, by name or path, or NoRepo
	Design   string // the design it builds, by name (designs/<name>.md), or ""
	After    []int  // the tasks it comes after, by id
	// AfterX are the tasks in other projects it comes after, as
	// "<slug>/T<n>" or, on a paired server, "<alias>/<slug>/T<n>"
	// (programs, §6.1). tm reads and keeps them; only a program's director
	// writes them (ProgramAdd, ProgramEdit), the program layer checking
	// they are its members'.
	AfterX []string
	// Parts are the member tasks an umbrella task of a program's board
	// is made of (the programs design §5.3), as CrossRef refs
	// ("acme-api/T12", "mini/acme-ios/T7"), in the order they were
	// added. Its status is derived from theirs (program.Derive). Only
	// a program's board holds them (SetParts).
	Parts   []string
	Created string
	Updated string
	// DoneAt is when a done task finished: RFC 3339 in UTC, as tm's own
	// status change writes it, or YYYY-MM-DD when only the day is known
	// (FillDoneAt). "" on a task that isn't done, and on a done task
	// nothing has dated yet, which sorts by Updated (DoneKey).
	DoneAt   string
	Archived bool
}

// AfterRefs is every task it comes after as a ref: "T10", then the
// other projects' ("acme-api/T12"), for display.
func (t *Task) AfterRefs() []string {
	out := make([]string, 0, len(t.After)+len(t.AfterX))
	for _, n := range t.After {
		out = append(out, "T"+strconv.Itoa(n))
	}
	return append(out, t.AfterX...)
}

// Ref is the task's display id, "T12".
func (t *Task) Ref() string { return "T" + strconv.Itoa(t.ID) }

// StepsDone counts checked steps.
func (t *Task) StepsDone() int {
	n := 0
	for _, s := range t.Steps {
		if s.Done {
			n++
		}
	}
	return n
}

func (t *Task) renumber() {
	for i := range t.Steps {
		t.Steps[i].N = i + 1
	}
}

// ParseRef accepts "T12", "t12" or "12".
func ParseRef(s string) (int, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "T"), "t")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > maxID {
		return 0, false
	}
	return n, true
}

// Board is the parsed contents of TASKS.md (or tasks/ARCHIVE.md, whose
// NextID is unused).
type Board struct {
	NextID int
	Tasks  []*Task
	// Slug is the project's, for the links Render writes; "" writes
	// none. Parse leaves it empty.
	Slug string
}

// Find returns the task with the given id, or nil.
func (b *Board) Find(id int) *Task {
	for _, t := range b.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Remove takes the task out of the board and returns it, or nil.
func (b *Board) Remove(id int) *Task {
	for i, t := range b.Tasks {
		if t.ID == id {
			b.Tasks = append(b.Tasks[:i], b.Tasks[i+1:]...)
			return t
		}
	}
	return nil
}

// Sort puts tasks in canonical order: board groups, then priority
// (must first, none last), then id.
func (b *Board) Sort() { SortTasks(b.Tasks) }

// SortTasks orders tasks as the board does: by group, then priority,
// then id.
func SortTasks(ts []*Task) {
	sort.SliceStable(ts, func(i, j int) bool { return Less(ts[i], ts[j]) })
}

// Less is the board's order: group, then priority, then id.
func Less(a, b *Task) bool {
	if ra, rb := rank(a.Status), rank(b.Status); ra != rb {
		return ra < rb
	}
	if pa, pb := a.Priority.Rank(), b.Priority.Rank(); pa != pb {
		return pa < pb
	}
	return a.ID < b.ID
}

// DoneKey is what orders a done task among the others: when it finished,
// else the day it last changed. Dates sort as text, a day before the
// times of that day.
func (t *Task) DoneKey() string {
	if t.DoneAt != "" {
		return t.DoneAt
	}
	return t.Updated
}

// DoneLabel is what a done row says of DoneAt, in now's time zone: the
// time alone today, the day and time before that, the year too for
// another year's, and no time when only the day is known; "" when
// nothing dates the task, or it doesn't read.
func DoneLabel(at string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, at)
	timed := err == nil
	if timed {
		t = t.In(now.Location())
	} else if t, err = time.ParseInLocation("2006-01-02", at, now.Location()); err != nil {
		return ""
	}
	day := "2 Jan"
	if t.Year() != now.Year() {
		day = "2 Jan 2006"
	}
	switch {
	case !timed:
		return t.Format(day)
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	}
	return t.Format(day + " 15:04")
}

// NewestDoneFirst is ts in board order but for its done tasks, which
// list newest first (DoneKey), then by highest id: the order of every
// list of what finished. ts stays as it is.
func NewestDoneFirst(ts []*Task) []*Task {
	out := slices.Clone(ts)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Status != Done || b.Status != Done {
			return Less(a, b)
		}
		if ka, kb := a.DoneKey(), b.DoneKey(); ka != kb {
			return ka > kb
		}
		return a.ID > b.ID
	})
	return out
}

// SortByID orders tasks by id only, as the archive does.
func (b *Board) SortByID() {
	sort.SliceStable(b.Tasks, func(i, j int) bool { return b.Tasks[i].ID < b.Tasks[j].ID })
}

// checkTitle validates a one-line title.
func checkTitle(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", refuse("empty-title", "title is empty")
	}
	if strings.ContainsAny(s, "\r\n") {
		return "", refuse("invalid-title", "title must be one line")
	}
	return s, nil
}

// checkNotes rejects notes that would break the file's structure: lines
// that read as a board heading or a task heading.
func checkNotes(s string) (string, error) {
	s = trimBlankLines(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
	if s == "" {
		return "", nil
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if isStructural(line) {
			return "", refuse("invalid-notes", fmt.Sprintf("notes line %d looks like a heading of TASKS.md (%q); use #### or deeper", i+1, line))
		}
	}
	if priorityLine.MatchString(lines[0]) {
		return "", refuse("invalid-notes", fmt.Sprintf("notes can't start with %q, which reads as the task's priority; set it with --priority", lines[0]))
	}
	if _, ok := readPartsLine(lines[0]); ok {
		return "", refuse("invalid-notes", fmt.Sprintf("notes can't start with %q, which reads as an umbrella task's parts; set them with tm program task edit --parts", lines[0]))
	}
	if doneAtLine.MatchString(lines[0]) {
		return "", refuse("invalid-notes", fmt.Sprintf("notes can't start with %q, which reads as when the task finished; tm sets that when the task becomes done", lines[0]))
	}
	if stepLine.MatchString(lines[len(lines)-1]) {
		return "", refuse("invalid-notes", "notes end in a checklist, which would read as steps; add steps with `tm task steps add` or end the notes with text")
	}
	return s, nil
}

func checkStep(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", refuse("empty-step", "step text is empty")
	}
	if strings.ContainsAny(s, "\r\n") {
		return "", refuse("invalid-step", "step text must be one line")
	}
	return s, nil
}

// designRE is a design's name, a file name in designs/.
var designRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidDesign reports whether s can name a design.
func ValidDesign(s string) bool { return designRE.MatchString(s) }

func checkDesign(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s != "" && !designRE.MatchString(s) {
		return "", refuse("invalid-design", fmt.Sprintf("design %q must be a name of letters, digits, '.', '_' and '-' (as designs/<name>.md)", s))
	}
	return s, nil
}

// checkAfter reads the tasks a task comes after: refs as "T12", split
// by commas or spaces; an empty list clears them. They are sorted, once
// each, and never the task itself. It takes only this board's refs: it
// checks what a command writes.
func checkAfter(id int, refs []string) ([]int, error) {
	out, cross, err := readAfter(id, refs)
	if err == nil && len(cross) > 0 {
		err = refuse("invalid-after", fmt.Sprintf("%q is not a task id (T12)", cross[0]))
	}
	return out, err
}

// readAfter reads an "after" value as TASKS.md holds it: this board's
// refs ("T12") and, kept as text, refs to other projects' tasks
// ("acme-api/T12", "mini/acme-ios/T7"; CrossRef). Each list is sorted,
// once each.
func readAfter(id int, refs []string) ([]int, []string, error) {
	var out []int
	var cross []string
	for _, r := range refs {
		for _, f := range strings.FieldsFunc(r, func(c rune) bool { return c == ',' || c == ' ' }) {
			if strings.Contains(f, "/") {
				if _, _, _, ok := CrossRef(f); !ok {
					return nil, nil, refuse("invalid-after", fmt.Sprintf("%q is not a task ref (T12, <project>/T12 or <server>/<project>/T12)", f))
				}
				if !slices.Contains(cross, f) {
					cross = append(cross, f)
				}
				continue
			}
			n, ok := ParseRef(f)
			if !ok {
				return nil, nil, refuse("invalid-after", fmt.Sprintf("%q is not a task id (T12)", f))
			}
			if n == id {
				return nil, nil, refuse("invalid-after", fmt.Sprintf("T%d can't come after itself", id))
			}
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	slices.Sort(out)
	slices.Sort(cross)
	return out, cross, nil
}

// checkLabel validates owner, repo and thread values, which live in the
// one-line metadata, so they can't hold the separator.
func checkLabel(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n·") {
		return "", refuse("invalid-"+field, field+" must be one line without '·'")
	}
	return s, nil
}
