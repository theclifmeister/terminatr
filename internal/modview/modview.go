// Package modview keeps the server's side of the views user mods draw
// (mods design §6, Views): the widget tree each mod last published for
// each of its views ($.ui.view.set), which consoles show which view
// (for view.opened and view.closed), and the views a mod asked to open
// ($.ui.view.open), which only a human input lets it do.
//
// A Store is safe for use from many goroutines. It holds data only: the
// server checks grants and publishes the events it returns.
package modview

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Limits on what one mod keeps here.
const (
	// maxTrees is how many trees one view keeps: one for every project
	// it runs for, and one for all.
	maxTrees = 64
	// MaxBytes is the JSON of all of a mod's trees together.
	MaxBytes = 4 << 20
	// MaxHeight is the most lines a dashboard card asks for.
	MaxHeight = 20
	// DefaultHeight is a card's height when the mod asks for none.
	DefaultHeight = 8
	// maxCount bounds a tab's count.
	maxCount = 99999
	// MaxSidebarHeight is the most body lines a sidebar.section, a
	// coordinator.section or a details.section asks for,
	// defaultSidebarHeight its lines when it asks for none (a right-pane
	// section of a mod older than API 1.7 gets the most: OldHeight).
	MaxSidebarHeight     = 12
	defaultSidebarHeight = 4
	// MaxProjectSectionHeight is the most body lines a
	// sidebar.project.section asks for, defaultProjectSectionHeight its
	// lines when it asks for none.
	MaxProjectSectionHeight     = 4
	defaultProjectSectionHeight = 2
	// maxBadgeSpans and maxBadgeCells bound a sidebar section's badge,
	// drawn on its header.
	maxBadgeSpans = 4
	maxBadgeCells = 12
	// iconCells is what an icon counts in a badge's cells: the widest an
	// icon set draws one (a private-use glyph and a blank).
	iconCells = 2
)

// Heights are the lines a view in slot gets when it asks for none, and
// the most it may ask for: a dashboard card's, or a section's. Other
// slots take the room they are given.
func Heights(slot string) (def, most int) {
	switch slot {
	case mod.SlotSidebarSection, mod.SlotCoordinatorSection, mod.SlotDetailsSection:
		return defaultSidebarHeight, MaxSidebarHeight
	case mod.SlotProjectSection:
		return defaultProjectSectionHeight, MaxProjectSectionHeight
	}
	return DefaultHeight, MaxHeight
}

// isSection says slot is a section's, whose null tree shows nothing
// (Store.Set).
func isSection(slot string) bool { return mod.IsSection(slot) }

// OldHeight is the height a tree of slot asks for, from mod m: a right-pane section of a mod older than 1.7 that asks
// none never knew of a default, so it gets the most (MaxSidebarHeight).
func OldHeight(slot string, m *mod.Manifest, height int) int {
	if height == 0 && mod.IsPanel(slot) && m != nil && m.Before(mod.SidebarAPIMinor) {
		return MaxSidebarHeight
	}
	return height
}

// CheckBadge checks a section's badge in place, as
// widget.CheckSpans does: at most maxBadgeSpans spans in
// maxBadgeCells cells, an icon counting iconCells.
func CheckBadge(ss []widget.Span) error {
	if len(ss) > maxBadgeSpans {
		return fmt.Errorf("a badge has at most %d spans, not %d", maxBadgeSpans, len(ss))
	}
	if err := widget.CheckSpans(ss); err != nil {
		return err
	}
	n := 0
	for _, s := range ss {
		n += ansi.StringWidth(s.Text)
		if s.Icon != "" {
			n += iconCells
		}
	}
	if n > maxBadgeCells {
		return fmt.Errorf("a badge is at most %d cells (an icon counts %d), not %d", maxBadgeCells, iconCells, n)
	}
	return nil
}

// InputWindow is how long after a human input to a mod its
// $.ui.view.open may open a view: the event's handler time.
const InputWindow = 5 * time.Second

// OpenLife is how long an open request waits for its console to take it.
const OpenLife = 10 * time.Second

// ShownLife is how long a console's shown views count without the
// console polling again: then they are closed for it.
const ShownLife = 15 * time.Second

// Tree is a published tree: for one project, or for all (""). A
// details.section's tree may be for one task or thread of its project
// instead, the row the details panel shows.
type Tree struct {
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	// Count is the number a project tab shows beside its title; nil for
	// none.
	Count *int `json:"count,omitempty"`
	// Height is the lines a dashboard card or a sidebar section asks
	// for, within its slot's most (Heights); 0 for its default.
	Height int `json:"height,omitempty"`
	// Badge is what a sidebar section's header shows beside its title,
	// folded or not (CheckBadge); nil for none.
	Badge []widget.Span  `json:"badge,omitempty"`
	Tree  *widget.Widget `json:"tree"`
	size  int
}

// Shown is a view a console shows: the mod's view, in a slot, for a
// project (and a details.section for the task or thread the details
// panel shows), at a size in cells.
type Shown struct {
	Mod     string `json:"mod"`
	View    string `json:"view"`
	Slot    string `json:"slot,omitempty"`
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// key is a shown view's identity: its size may change while it shows.
func (s Shown) key() Shown {
	return Shown{Mod: s.Mod, View: s.View, Slot: s.Slot, Project: s.Project, Task: s.Task, Thread: s.Thread}
}

// For says whether t is the tree for project, task and thread, exactly.
func (t Tree) For(project, task, thread string) bool {
	return t.Project == project && t.Task == task && t.Thread == thread
}

// Open is a mod's request that a console show one of its views.
type Open struct {
	Seq     uint64 `json:"seq"`
	Mod     string `json:"mod"`
	View    string `json:"view"`
	Project string `json:"project,omitempty"`
	pid     int
	at      time.Time
}

// input is the last human input to a mod: from which console, about
// which project, and when; used once it opened a view.
type input struct {
	pid     int
	project string
	at      time.Time
	used    bool
}

// console is what one console (by its process id) shows.
type console struct {
	shown []Shown
	at    time.Time
}

// Store is the server's mod views.
type Store struct {
	mu       sync.Mutex
	trees    map[string]map[string][]Tree // mod, view → its trees
	inputs   map[string]input             // by mod
	consoles map[int]*console
	opens    []Open
	seq      uint64
	now      func() time.Time
}

// New is an empty Store.
func New() *Store {
	return &Store{trees: map[string]map[string][]Tree{}, inputs: map[string]input{}, consoles: map[int]*console{}, now: time.Now}
}

// SetNow replaces the clock (tests).
func (s *Store) SetNow(now func() time.Time) { s.now = now }

// ErrFull is Set's error when the mod keeps too much.
type ErrFull string

func (e ErrFull) Error() string { return string(e) }

// Set publishes mod's tree t of view, in slot, for project ("" for every
// project the view shows); a nil tree clears it. A section's nil tree is
// kept, as a tree that says nothing: the console then draws no section
// there, not even its header, where one never published waits for the
// mod (sidebar sections design §4). Such a tree counts toward maxTrees,
// and makes room for another by going first. size is the tree's JSON
// size.
func (s *Store) Set(mod, view, slot string, t Tree, size int) error {
	t.size = size
	_, most := Heights(slot)
	t.Height = min(max(t.Height, 0), most)
	if t.Count != nil {
		c := min(max(*t.Count, 0), maxCount)
		t.Count = &c
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	views := s.trees[mod]
	if views == nil {
		views = map[string][]Tree{}
		s.trees[mod] = views
	}
	list := slices.DeleteFunc(views[view], func(o Tree) bool { return o.For(t.Project, t.Task, t.Thread) })
	if len(list) >= maxTrees {
		// A tree that says nothing makes room first, the oldest of them.
		if i := slices.IndexFunc(list, func(o Tree) bool { return o.Tree == nil }); i >= 0 {
			list = slices.Delete(list, i, i+1)
		}
	}
	if t.Tree == nil && (!isSection(slot) || len(list) >= maxTrees) {
		views[view] = list
		return nil
	}
	if len(list) >= maxTrees {
		return ErrFull("a view keeps at most 64 trees: one per project (or task or thread a section shows), and one for all; clear one on view.closed")
	}
	total := size
	for v, l := range views {
		for _, o := range l {
			if v != view || !o.For(t.Project, t.Task, t.Thread) {
				total += o.size
			}
		}
	}
	if total > MaxBytes {
		return ErrFull("a mod's view trees are at most 4 MB together")
	}
	views[view] = append(list, t)
	return nil
}

// Trees are mod's trees of view.
func (s *Store) Trees(mod, view string) []Tree {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.trees[mod][view])
}

// Clear forgets everything mod published: its host restarted or
// stopped, so its trees would be stale.
func (s *Store) Clear(mod string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.trees, mod)
	delete(s.inputs, mod)
	s.opens = slices.DeleteFunc(s.opens, func(o Open) bool { return o.Mod == mod })
}

// Input notes a human input to mod from console pid, about project: for
// a while (InputWindow), the mod may open one view. The open it allows
// has a Seq greater than the one returned.
func (s *Store) Input(mod string, pid int, project string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs[mod] = input{pid: pid, project: project, at: s.now()}
	return s.seq
}

// Open asks for mod's view to be opened on the console of the human
// input it follows, about that input's project. It reports false when no
// human input to the mod is recent enough or it already opened a view:
// a timer or an event can't pop a view in front of the user.
func (s *Store) Open(mod, view string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.inputs[mod]
	now := s.now()
	if !ok || in.used || now.Sub(in.at) > InputWindow {
		return false
	}
	in.used = true
	s.inputs[mod] = in
	s.seq++
	s.opens = append(s.opens, Open{Seq: s.seq, Mod: mod, View: view, Project: in.project, pid: in.pid, at: now})
	return true
}

// Opens are the open requests for console pid, oldest first. A console
// acts on each once, by its Seq.
func (s *Store) Opens(pid int) []Open {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.opens = slices.DeleteFunc(s.opens, func(o Open) bool { return now.Sub(o.at) > OpenLife })
	var out []Open
	for _, o := range s.opens {
		if o.pid == pid {
			out = append(out, o)
		}
	}
	return out
}

// Show sets what console pid shows now and returns the views that
// opened and closed: newly shown by any console, or shown by none any
// more. A view several consoles show opens with the first and closes
// with the last. It also closes the views of consoles silent for
// ShownLife.
func (s *Store) Show(pid int, shown []Shown) (opened, closed []Shown) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.allLocked()
	now := s.now()
	if len(shown) == 0 {
		delete(s.consoles, pid)
	} else {
		s.consoles[pid] = &console{shown: slices.Clone(shown), at: now}
	}
	s.expireLocked(now)
	return diff(before, s.allLocked())
}

// Expire closes the views of consoles silent for ShownLife.
func (s *Store) Expire() (closed []Shown) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.allLocked()
	s.expireLocked(s.now())
	_, closed = diff(before, s.allLocked())
	return closed
}

func (s *Store) expireLocked(now time.Time) {
	for pid, c := range s.consoles {
		if now.Sub(c.at) > ShownLife {
			delete(s.consoles, pid)
		}
	}
}

// ShownBy are mod's views that some console shows, one each.
func (s *Store) ShownBy(mod string) []Shown {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Shown
	for _, v := range s.allLocked() {
		if v.Mod == mod {
			out = append(out, v)
		}
	}
	return out
}

// allLocked is every shown view once, by key, with the size of the
// lowest console's; sorted.
func (s *Store) allLocked() []Shown {
	pids := make([]int, 0, len(s.consoles))
	for pid := range s.consoles {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	seen := map[Shown]bool{}
	var out []Shown
	for _, pid := range pids {
		for _, v := range s.consoles[pid].shown {
			if !seen[v.key()] {
				seen[v.key()] = true
				out = append(out, v)
			}
		}
	}
	slices.SortFunc(out, compare)
	return out
}

func compare(a, b Shown) int {
	return cmp.Or(cmp.Compare(a.Mod, b.Mod), cmp.Compare(a.View, b.View), cmp.Compare(a.Slot, b.Slot), cmp.Compare(a.Project, b.Project),
		cmp.Compare(a.Task, b.Task), cmp.Compare(a.Thread, b.Thread))
}

// diff is what is in after and not before (opened), and the other way
// (closed), by key.
func diff(before, after []Shown) (opened, closed []Shown) {
	has := func(list []Shown, v Shown) bool {
		return slices.ContainsFunc(list, func(o Shown) bool { return o.key() == v.key() })
	}
	for _, v := range after {
		if !has(before, v) {
			opened = append(opened, v)
		}
	}
	for _, v := range before {
		if !has(after, v) {
			closed = append(closed, v)
		}
	}
	return opened, closed
}
