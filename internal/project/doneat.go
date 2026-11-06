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

// doneTimes reads the journal, the month archives first, and answers
// when each task last became done: the first done line after any other
// status, so a later note on a done task doesn't move it. A task that
// has since left done has none.
func (p *Folder) doneTimes() (map[int]string, error) {
	out := map[int]string{}
	line := func(l string) {
		m := journalStatus.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
		if m == nil {
			return
		}
		at, err := time.Parse(time.RFC3339, m[1])
		if err != nil {
			return
		}
		id, err := strconv.Atoi(m[3])
		if err != nil {
			return
		}
		if m[2] == "task.status" && !detailSaysDone(m[4]) {
			delete(out, id)
			return
		}
		if _, ok := out[id]; !ok {
			out[id] = at.UTC().Format(time.RFC3339)
		}
	}
	names, err := filepath.Glob(p.Path("journal", "*.md.gz"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	for _, name := range names {
		if err := readGzipLines(name, line); err != nil {
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

// detailSaysDone reads a task.status line's detail, "done", "done: why"
// or "done (approved by the user)".
func detailSaysDone(detail string) bool {
	word, _, _ := strings.Cut(detail, " ")
	return strings.TrimSuffix(word, ":") == string(tasks.Done)
}

func readGzipLines(name string, line func(string)) error {
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
