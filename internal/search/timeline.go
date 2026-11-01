package search

import (
	"cmp"
	"fmt"
	"slices"
)

// Timeline is what happened around a chunk, in time order: the chunks
// before it and after it (at most Before and After of each) of its task
// and its thread, and with All (or for a chunk of neither, such as a
// note) the project's journal lines too.
type Timeline struct {
	At     Chunk   `json:"at"`
	Before []Chunk `json:"before"`
	After  []Chunk `json:"after"`
	// Stats are the update's before it.
	Stats Stats `json:"stats"`
}

// Timeline updates the index and answers the timeline of chunk id:
// before and after chunks at most on each side of it, ordered by time
// (then id). With coordinator false the coordinator's chunks are left
// out, and a coordinator's chunk is ErrNoChunk.
func (x *Index) Timeline(id string, before, after int, all, coordinator bool) (Timeline, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	st, err := x.refresh()
	if err != nil {
		return Timeline{}, err
	}
	c := x.byID[id]
	if c == nil || c.dead || c.Coordinator && !coordinator {
		return Timeline{}, fmt.Errorf("%s: %w", id, ErrNoChunk)
	}
	all = all || c.Task == "" && c.Thread == ""
	related := func(o *chunk) bool {
		switch {
		case o.dead || o.Coordinator && !coordinator:
			return false
		case c.Task != "" && o.Task == c.Task, c.Thread != "" && o.Thread == c.Thread:
			return true
		}
		return all && o.Kind == KindJournal
	}
	var list []Chunk
	for _, o := range x.byID {
		if o != c && related(o) {
			list = append(list, o.Chunk)
		}
	}
	byTime := func(a, b Chunk) int { return cmp.Or(a.Time.Compare(b.Time), cmp.Compare(a.ID, b.ID)) }
	slices.SortFunc(list, byTime)
	at, _ := slices.BinarySearchFunc(list, c.Chunk, byTime)
	t := Timeline{At: c.Chunk, Before: list[max(at-max(before, 0), 0):at], After: list[at:min(at+max(after, 0), len(list))], Stats: st}
	return t, nil
}

// Children updates the index and answers the chunks of a thread (an id
// t-0833) or a task (T914), by time: its briefs, follow-ups, reports and
// library for a thread; the task itself, its design and its threads'
// for a task. Journal lines are left out (they are its timeline), and
// for coordinator false the coordinator's chunks.
func (x *Index) Children(ref string, coordinator bool) ([]Chunk, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if _, err := x.refresh(); err != nil {
		return nil, err
	}
	thread := threadIDRE.MatchString(ref)
	var out []Chunk
	for _, o := range x.byID {
		if o.dead || o.Coordinator && !coordinator || o.Kind == KindJournal {
			continue
		}
		if thread && o.Thread == ref || !thread && o.Task == ref {
			out = append(out, o.Chunk)
		}
	}
	slices.SortFunc(out, func(a, b Chunk) int { return cmp.Or(a.Time.Compare(b.Time), cmp.Compare(a.ID, b.ID)) })
	return out, nil
}
