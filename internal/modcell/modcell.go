// Package modcell is the server's cell store (mods design §4: events are
// pushed, cells are published): the latest cell each code mod published
// per column and target ($.ui.cell.set) and its status-bar segment
// ($.ui.status.set). Consoles get them with the rows they already ask
// for (proto.ModCells), so a mod is never called while a frame is drawn
// and a slow mod shows a stale cell, never a slow sidebar.
//
// The store is written to a file, so the last cells show again after the
// server restarts, before the mods' hosts have published anew. What it
// reads back is checked as a mod's call is.
package modcell

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Limits, so one mod can't grow the store, or the rows sent with every
// session.list, without bound.
const (
	// maxCells is how many cells a mod keeps; past it a new target is
	// refused (a cell set again, or cleared, still is).
	maxCells = 2000
	// MaxText is the bytes of all the spans' text of one cell or status
	// segment: a column is at most view.MaxModColumnWidth cells wide.
	MaxText = 256
)

// saveDelay gathers a burst of sets into one write of the file.
const saveDelay = time.Second

// fileVersion is the cells file's format.
const fileVersion = 1

type key struct {
	mod, column string
	target      proto.CellTarget
}

// Store is the cells and status segments, by mod.
type Store struct {
	path string

	mu     sync.Mutex
	cells  map[key][]widget.Span
	count  map[string]int // cells per mod
	status map[string][]widget.Span
	timer  *time.Timer
	// gen counts changes; saved is the gen last written.
	gen, saved uint64
}

// file is the cells file's JSON.
type file struct {
	Version int               `json:"version"`
	Cells   []proto.ModCell   `json:"cells"`
	Status  []proto.ModStatus `json:"status"`
}

// Open is the store kept in path ("" for none: tests), with the cells the
// file holds. A file that doesn't read is a store with none.
func Open(path string) *Store {
	s := &Store{path: path, cells: map[key][]widget.Span{}, count: map[string]int{}, status: map[string][]widget.Span{}}
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var f file
	if json.Unmarshal(b, &f) != nil || f.Version != fileVersion {
		return s
	}
	for _, c := range f.Cells {
		if CheckTarget(c.Target) == nil && Check(c.Spans) == nil && c.Mod != "" && c.Column != "" && len(c.Spans) > 0 {
			k := key{c.Mod, c.Column, c.Target}
			if _, ok := s.cells[k]; !ok && s.count[c.Mod] < maxCells {
				s.cells[k] = c.Spans
				s.count[c.Mod]++
			}
		}
	}
	for _, st := range f.Status {
		if st.Mod != "" && len(st.Spans) > 0 && Check(st.Spans) == nil {
			s.status[st.Mod] = st.Spans
		}
	}
	return s
}

// CheckTarget checks a cell's target is one of its four shapes.
func CheckTarget(t proto.CellTarget) error {
	switch {
	case t.Session != "":
		if t.Project != "" || t.Task != "" || t.Thread != "" {
			return errors.New("a session target names the session alone")
		}
	case t.Project == "":
		return errors.New("a target names a project, or a session")
	case t.Task != "" && t.Thread != "":
		return errors.New("a target names a task or a thread, not both")
	}
	for _, f := range []string{t.Project, t.Task, t.Thread, t.Session} {
		if len(f) > 128 || strings.ContainsFunc(f, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
			return errors.New("a target's field is too long or has spaces or control characters")
		}
	}
	return nil
}

// Check checks a cell's or status segment's spans in place, as
// widget.CheckSpans does, and bounds their text (MaxText).
func Check(ss []widget.Span) error {
	if err := widget.CheckSpans(ss); err != nil {
		return err
	}
	n := 0
	for _, s := range ss {
		n += len(s.Text)
	}
	if n > MaxText {
		return fmt.Errorf("%d bytes of text, over %d", n, MaxText)
	}
	return nil
}

// Set publishes mod's cell in its column for target t; empty spans clear
// it. The caller has checked t (CheckTarget), the spans (Check) and that
// the column is the mod's. It reports whether anything changed.
func (s *Store) Set(mod, column string, t proto.CellTarget, spans []widget.Span) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{mod, column, t}
	old, had := s.cells[k]
	switch {
	case len(spans) == 0 && !had:
		return false, nil
	case len(spans) == 0:
		delete(s.cells, k)
		if s.count[mod]--; s.count[mod] <= 0 {
			delete(s.count, mod)
		}
	case had && slices.Equal(old, spans):
		return false, nil
	case !had && s.count[mod] >= maxCells:
		return false, fmt.Errorf("%s has %d cells, the most a mod keeps: clear some first", mod, maxCells)
	default:
		if !had {
			s.count[mod]++
		}
		s.cells[k] = slices.Clone(spans)
	}
	s.changed()
	return true, nil
}

// SetStatus publishes mod's status-bar segment; empty spans clear it.
func (s *Store) SetStatus(mod string, spans []widget.Span) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.status[mod]
	switch {
	case len(spans) == 0 && !had, had && slices.Equal(old, spans):
		return false
	case len(spans) == 0:
		delete(s.status, mod)
	default:
		s.status[mod] = slices.Clone(spans)
	}
	s.changed()
	return true
}

// Keep drops the cells and segments of every mod keep refuses (removed or
// turned off) and, for the rest, the cells of columns keep no longer
// lists (columns(mod) false; an updated manifest dropped them).
func (s *Store) Keep(keep func(mod string) bool, column func(mod, column string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := false
	for k := range s.cells {
		if !keep(k.mod) || !column(k.mod, k.column) {
			delete(s.cells, k)
			if s.count[k.mod]--; s.count[k.mod] <= 0 {
				delete(s.count, k.mod)
			}
			dropped = true
		}
	}
	for m := range s.status {
		if !keep(m) {
			delete(s.status, m)
			dropped = true
		}
	}
	if dropped {
		s.changed()
	}
}

// changed counts a change and has the file written soon. s.mu is held.
func (s *Store) changed() {
	s.gen++
	if s.path != "" && s.timer == nil {
		s.timer = time.AfterFunc(saveDelay, func() { _ = s.Flush() })
	}
}

// Flush writes the store's file now if it changed since it was last
// written (the server stopping).
func (s *Store) Flush() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.path == "" || s.gen == s.saved {
		s.mu.Unlock()
		return nil
	}
	f := file{Version: fileVersion, Cells: s.list(nil), Status: s.statusList(nil)}
	gen := s.gen
	s.mu.Unlock()
	b, err := json.Marshal(f)
	if err == nil {
		err = writeAtomic(s.path, b)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.saved = max(s.saved, gen)
	s.mu.Unlock()
	return nil
}

// Cells are the cells of the mods show allows (nil: every mod) whose
// target where accepts, sorted by mod, column and target.
func (s *Store) Cells(show func(mod string) bool, where func(proto.CellTarget) bool) []proto.ModCell {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.list(show)
	if where != nil {
		out = slices.DeleteFunc(out, func(c proto.ModCell) bool { return !where(c.Target) })
	}
	return out
}

// Status is the status segments of the mods show allows, by mod.
func (s *Store) Status(show func(mod string) bool) []proto.ModStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusList(show)
}

func (s *Store) list(show func(string) bool) []proto.ModCell {
	var out []proto.ModCell
	for k, ss := range s.cells {
		if show == nil || show(k.mod) {
			out = append(out, proto.ModCell{Mod: k.mod, Column: k.column, Target: k.target, Spans: ss})
		}
	}
	slices.SortFunc(out, func(a, b proto.ModCell) int {
		return cmp.Or(cmp.Compare(a.Mod, b.Mod), cmp.Compare(a.Column, b.Column),
			cmp.Compare(a.Target.Project, b.Target.Project), cmp.Compare(a.Target.Task, b.Target.Task),
			cmp.Compare(a.Target.Thread, b.Target.Thread), cmp.Compare(a.Target.Session, b.Target.Session))
	})
	return out
}

func (s *Store) statusList(show func(string) bool) []proto.ModStatus {
	var out []proto.ModStatus
	for m, ss := range s.status {
		if show == nil || show(m) {
			out = append(out, proto.ModStatus{Mod: m, Spans: ss})
		}
	}
	slices.SortFunc(out, func(a, b proto.ModStatus) int { return cmp.Compare(a.Mod, b.Mod) })
	return out
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cells-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
