package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// humanLine reads a journal line by the human: its time and where the
// human acted, tasks.MarkDone's words for it. Project.new is left out: tm
// writes it for whoever made the project, and nobody has acted on it yet.
func humanLine(l string) (at time.Time, where string, ok bool) {
	stamp, rest, ok := strings.Cut(strings.TrimRight(l, "\r\n"), " ")
	if !ok {
		return at, "", false
	}
	rest, ok = strings.CutPrefix(rest, "human ")
	if !ok {
		return at, "", false
	}
	if via, ok := strings.CutPrefix(rest, "("); ok {
		via, rest, ok = strings.Cut(via, ") ")
		if !ok {
			return at, "", false
		}
		where = actedWhere(via)
	}
	if strings.HasPrefix(rest, "project.new ") {
		return at, "", false
	}
	at, err := time.Parse(time.RFC3339, stamp)
	return at, where, err == nil
}

// actedWhere says a journal's Via (caller.Caller) in words.
func actedWhere(via string) string {
	switch {
	case via == "app":
		return "in the app"
	case strings.HasPrefix(via, "iphone: "):
		return "on the iPhone"
	case strings.HasPrefix(via, "peer: "):
		alias, _, _ := strings.Cut(strings.TrimPrefix(via, "peer: "), " › ")
		return "through " + alias
	}
	return ""
}

// LastHumanAct is the time and place of the last line by the human in the
// journal, its month archives read, newest first, only when JOURNAL.md has
// none. Ok is false when there is none, or none can be read: nothing here
// is worth failing a read of the board for.
func (p *Folder) LastHumanAct() (at time.Time, where string, ok bool) {
	line := func(l string) {
		if a, w, is := humanLine(l); is {
			at, where, ok = a, w, true
		}
	}
	f, err := os.Open(p.Path("JOURNAL.md"))
	if err == nil {
		defer f.Close()
		_ = readLines(f, line)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return at, "", false
	}
	if ok {
		return at, where, true
	}
	names, _ := filepath.Glob(p.Path("journal", "*.md.gz"))
	slices.Sort(names)
	for _, name := range slices.Backward(names) {
		_ = ReadJournalArchive(name, line)
		if ok {
			return at, where, true
		}
	}
	return at, "", false
}
