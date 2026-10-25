package tasks

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Status is one of the six task statuses (§6.1).
type Status string

const (
	Open    Status = "open"    // captured
	Ready   Status = "ready"   // on deck
	Started Status = "started" // in motion
	Blocked Status = "blocked" // waiting on something
	Review  Status = "review"  // finished, waiting for the human
	Done    Status = "done"
)

// Statuses lists every status in board order.
var Statuses = []Status{Blocked, Review, Started, Ready, Open, Done}

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
)

// Groups lists the board sections in order.
var Groups = []Group{NeedsYou, InMotion, OnDeck, Backlog, DoneG}

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
	}
	return 4
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
	Notes    string
	Steps    []Step
	Thread   string
	Owner    string
	Repo     string // the repo its thread works in, by name or path, or NoRepo
	Design   string // the design it builds, by name (designs/<name>.md), or ""
	After    []int  // the tasks it comes after, by id
	Created  string
	Updated  string
	Archived bool
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

// Sort puts tasks in canonical order: board groups, then id.
func (b *Board) Sort() {
	sort.SliceStable(b.Tasks, func(i, j int) bool {
		ri, rj := rank(b.Tasks[i].Status), rank(b.Tasks[j].Status)
		if ri != rj {
			return ri < rj
		}
		return b.Tasks[i].ID < b.Tasks[j].ID
	})
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
// each, and never the task itself.
func checkAfter(id int, refs []string) ([]int, error) {
	var out []int
	for _, r := range refs {
		for _, f := range strings.FieldsFunc(r, func(c rune) bool { return c == ',' || c == ' ' }) {
			n, ok := ParseRef(f)
			if !ok {
				return nil, refuse("invalid-after", fmt.Sprintf("%q is not a task id (T12)", f))
			}
			if n == id {
				return nil, refuse("invalid-after", fmt.Sprintf("T%d can't come after itself", id))
			}
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	slices.Sort(out)
	return out, nil
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
