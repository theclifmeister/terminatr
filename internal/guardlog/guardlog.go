// Package guardlog is the guard's refusal log (docs/SPEC.md §8.6,
// Guard): the calls the guard refused, with the whole command line or
// file each was refused for, for the human to allow or keep refusing
// from the dashboard. It is the human's: it lives in tm's state folder,
// which no mod reaches and the guard keeps every agent from reading (its
// floor), and nothing of it goes to the journal, which keeps only the
// rule and a short summary.
//
// One entry stands for one call refused under one rule in one project,
// however often: a refusal of the same again counts it and moves it up.
// The human's decision stays with it: an entry kept refusing is no longer
// listed; one allowed but refused again (its allowance removed) is
// listed anew.
package guardlog

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// Decisions on an entry: none yet (listed), kept refusing, allowed.
const (
	Open    = ""
	Kept    = "kept"
	Allowed = "allowed"
)

// Entry is one refused call.
type Entry struct {
	// ID names the entry: its project, rule and subject, hashed.
	ID      string `json:"id"`
	Project string `json:"project"`
	// Thread and Session are the last ones refused it; Role is the
	// session's.
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
	Role    string `json:"role,omitempty"`
	Rule    string `json:"rule"`
	Tool    string `json:"tool"`
	// Summary is the journal's short account; Subject the whole command
	// line or file, which an allowance names.
	Summary string `json:"summary,omitempty"`
	Subject string `json:"subject"`
	// Path: Subject is a file, not a command line.
	Path bool `json:"path,omitempty"`
	// Floor: no allowance lifts the refusal; it can only be kept.
	Floor    bool      `json:"floor,omitempty"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Count    int       `json:"count"`
	Decision string    `json:"decision,omitempty"`
}

// MaxSubject bounds what an entry keeps of a subject, and maxEntries how
// many entries the log keeps (the oldest decided ones go first).
const (
	MaxSubject = 2000
	maxEntries = 500
)

// Path is the log's file under tm's state folder home.
func Path(home string) string { return filepath.Join(Dir(home), "refusals.json") }

// Dir is the log's folder: what the guard keeps every agent from reading.
func Dir(home string) string { return filepath.Join(home, "state", "guard") }

// id names an entry by its project, rule and subject.
func id(project, rule, subject string) string {
	h := sha256.Sum256([]byte(project + "\x00" + rule + "\x00" + subject))
	return hex.EncodeToString(h[:6])
}

// Record adds refusal e (Project, Rule, Tool, Subject and the rest) at
// now: a new entry, or one more of the same call. It reports whether the
// entry is newly listed: new, or refused again after it was allowed.
func Record(home string, e Entry, now time.Time) (Entry, bool, error) {
	if len(e.Subject) > MaxSubject {
		e.Subject = e.Subject[:MaxSubject]
	}
	e.ID = id(e.Project, e.Rule, e.Subject)
	listed := false
	err := update(home, func(list []Entry) []Entry {
		i := slices.IndexFunc(list, func(x Entry) bool { return x.ID == e.ID })
		if i < 0 {
			e.First, e.Last, e.Count, e.Decision = now, now, 1, Open
			listed = true
			return prune(append(list, e))
		}
		x := list[i]
		x.Thread, x.Session, x.Role, x.Tool, x.Summary, x.Floor = e.Thread, e.Session, e.Role, e.Tool, e.Summary, e.Floor
		x.Last, x.Count = now, x.Count+1
		if x.Decision == Allowed {
			x.Decision, listed = Open, true
		}
		list[i] = x
		e = x
		return list
	})
	return e, listed, err
}

// prune keeps the newest maxEntries, the decided ones going first.
func prune(list []Entry) []Entry {
	oldest := func(ok func(Entry) bool) int {
		i := -1
		for j, x := range list {
			if ok(x) && (i < 0 || x.Last.Before(list[i].Last)) {
				i = j
			}
		}
		return i
	}
	for len(list) > maxEntries {
		i := oldest(func(x Entry) bool { return x.Decision != Open })
		if i < 0 {
			i = oldest(func(Entry) bool { return true })
		}
		list = slices.Delete(list, i, i+1)
	}
	return list
}

// List is project's open entries (all projects' for ""), the latest
// refused first.
func List(home, project string) ([]Entry, error) {
	all, err := read(Path(home))
	var out []Entry
	for _, e := range all {
		if e.Decision == Open && (project == "" || e.Project == project) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b Entry) int { return b.Last.Compare(a.Last) })
	return out, err
}

// ErrGone is Decide's error for an entry the log no longer has.
var ErrGone = errors.New("that refusal is no longer in the guard's log")

// Decide records the human's decision on entry id (Kept or Allowed) and
// returns the entry.
func Decide(home, entry, decision string) (Entry, error) {
	if decision != Kept && decision != Allowed {
		return Entry{}, fmt.Errorf("unknown decision %q", decision)
	}
	var out Entry
	found := false
	err := update(home, func(list []Entry) []Entry {
		for i := range list {
			if list[i].ID == entry {
				list[i].Decision = decision
				out, found = list[i], true
			}
		}
		return list
	})
	if err == nil && !found {
		err = ErrGone
	}
	return out, err
}

func read(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Entry
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}

// update rewrites the log with fn, under its lock; a log that doesn't
// read starts again empty rather than refusing every write.
func update(home string, fn func([]Entry) []Entry) error {
	if home == "" {
		return errors.New("no state folder")
	}
	dir := Dir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := Path(home)
	unlock, err := mdfile.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	list, _ := read(path)
	list = fn(list)
	b, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(path, b, 0o600)
}

// Label is how the dashboard names an entry's who: its thread, else its
// role.
func (e Entry) Label() string { return cmp.Or(e.Thread, e.Role, "a session") }
