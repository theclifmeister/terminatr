package tasks

import (
	"fmt"
	"regexp"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// doneNoteDay finds a note the status change wrote, "done (2026-11-06): …".
var doneNoteDay = regexp.MustCompile(`(?m)^done \(([0-9]{4}-[0-9]{2}-[0-9]{2})\)`)

// fillValue is when a done task with no DoneAt finished, as well as the
// records can say: the journal's time (journal holds them by task id),
// else the day of its latest done note, else the day it last changed.
// "" when none says.
func fillValue(t *Task, journal map[int]string) string {
	if at := journal[t.ID]; at != "" {
		return at
	}
	if m := doneNoteDay.FindAllStringSubmatch(t.Notes, -1); m != nil {
		return m[len(m)-1][1]
	}
	if len(t.Updated) >= len("2006-01-02") {
		return t.Updated[:len("2006-01-02")]
	}
	return ""
}

// DoneValue is when done task t finished: its DoneAt, or what the upkeep
// would fill in for it (journal holds the journal's times by task id).
func DoneValue(t *Task, journal map[int]string) string {
	if t.DoneAt != "" {
		return t.DoneAt
	}
	return fillValue(t, journal)
}

// NeedsFill reports whether a task on board could get a DoneAt it
// lacks, so that a caller reads the journal only then.
func NeedsFill(board *Board) bool {
	for _, t := range board.Tasks {
		if t.Status == Done && t.DoneAt == "" {
			return true
		}
	}
	return false
}

// fill sets DoneAt on the done tasks of b that have none, and says how
// many it set.
func fill(b *Board, journal map[int]string) int {
	n := 0
	for _, t := range b.Tasks {
		if t.Status != Done || t.DoneAt != "" {
			continue
		}
		if at := fillValue(t, journal); at != "" {
			t.DoneAt = at
			n++
		}
	}
	return n
}

// FillDoneAt dates the done tasks that have no DoneAt, on the board and
// in the archive: tasks that finished before tm recorded it, or that an
// older tm or a hand edit moved. journal holds the journal's finish
// times by task id. It changes nothing else, not Updated, and writes
// only a file with something to fill. It returns how many it dated.
func (s *Store) FillDoneAt(journal map[int]string) (int, error) {
	n := 0
	board, err := s.Load()
	if err != nil {
		return 0, err
	}
	// Peek first: a filled board stays unwritten.
	if peek := fill(board, journal); peek > 0 {
		err := s.mutate(func(b *Board) error {
			n += fill(b, journal)
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	archive, err := s.LoadArchive()
	if err != nil {
		return n, err
	}
	if fill(archive, journal) == 0 {
		return n, nil
	}
	err = mdfile.Update(s.ArchivePath(), func(old []byte) ([]byte, error) {
		a, err := Parse(old)
		if err != nil {
			return nil, fmt.Errorf("%s: %w; fix the file by hand, tm won't rewrite it", s.ArchivePath(), err)
		}
		n += fill(a, journal)
		a.Slug = s.linkSlug()
		return a.RenderArchive(), nil
	})
	return n, err
}
