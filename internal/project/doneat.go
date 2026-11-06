package project

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// journalStatus reads a journal line that moves a task: its time, the
// action, the task's number and the detail. The actor is one word or,
// as "human (app)", a word and its bracket.
var journalStatus = regexp.MustCompile(`^(\S+) (?:\S+ \([^)]*\)|\S+) (task\.status|task\.done) T([0-9]+)(?: (.*))?$`)

// StatusLine is a journal line that moves a task.
type StatusLine struct {
	At   time.Time
	Task int
	// Status is the status it moved to: "done" for a ticker's task.done.
	Status string
}

// ParseStatusLine reads a journal line that moves a task.
func ParseStatusLine(l string) (StatusLine, bool) {
	if !strings.Contains(l, " task.") {
		return StatusLine{}, false // most lines; the expression is slower
	}
	m := journalStatus.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
	if m == nil {
		return StatusLine{}, false
	}
	at, err := time.Parse(time.RFC3339, m[1])
	if err != nil {
		return StatusLine{}, false
	}
	id, err := strconv.Atoi(m[3])
	if err != nil {
		return StatusLine{}, false
	}
	status := string(tasks.Done)
	if m[2] == "task.status" {
		word, _, _ := strings.Cut(m[4], " ")
		status = strings.TrimSuffix(word, ":")
	}
	return StatusLine{At: at, Task: id, Status: status}, true
}

// doneTimes reads the journal, the month archives first, and answers
// when each task last became done: the first done line after any other
// status, so a later note on a done task doesn't move it. A task that
// has since left done has none.
func (p *Folder) doneTimes() (map[int]string, error) {
	out := map[int]string{}
	line := func(l string) {
		s, ok := ParseStatusLine(l)
		if !ok {
			return
		}
		if s.Status != string(tasks.Done) {
			delete(out, s.Task)
			return
		}
		if _, ok := out[s.Task]; !ok {
			out[s.Task] = s.At.UTC().Format(time.RFC3339)
		}
	}
	names, err := filepath.Glob(p.Path("journal", "*.md.gz"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	for _, name := range names {
		if err := ReadJournalArchive(name, line); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(p.Path("JOURNAL.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return out, readLines(f, line)
}

// ReadJournalArchive calls line for each line of a month archive
// (journal/<yyyy-mm>.md.gz). What it read before an error stays read.
func ReadJournalArchive(name string, line func(string)) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	return readLines(zr, line)
}

func readLines(r io.Reader, line func(string)) error {
	br := bufio.NewReader(r)
	for {
		l, err := br.ReadString('\n')
		if l != "" {
			line(l)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// FillDoneAt dates the done tasks that have no finish time yet
// (tasks.Store.FillDoneAt) from tm's own records. It reads the journal
// only when one needs it, and never git, so a project with no repo
// fills the same. It returns how many it dated.
func (p *Project) FillDoneAt() (int, error) {
	s := p.Tasks()
	b, err := s.Load()
	if err != nil {
		return 0, err
	}
	a, err := s.LoadArchive()
	if err != nil {
		return 0, err
	}
	if !tasks.NeedsFill(b) && !tasks.NeedsFill(a) {
		return 0, nil
	}
	times, err := p.doneTimes()
	if err != nil {
		return 0, err
	}
	return s.FillDoneAt(times)
}
