package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/search"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// tm search's server side (design agent-recall, T921): one index per
// project (search.Open over <home>/state/search/<slug>), loaded on
// first use and let go after SearchIdle without one, and search.* on
// both sockets (proto.CapSearch).

// SearchIdle is how long a project's index stays in memory unused.
const SearchIdle = 10 * time.Minute

// SearchFresh is how long after one walk of the project a query takes
// the index as it is: search, timeline and show in a row walk once.
const SearchFresh = time.Second

// searchLimit is how many hits a query answers when it names none,
// searchMax the most it may ask for, and searchShowIDs the most ids
// one show takes.
const (
	searchLimit   = 20
	searchMax     = 200
	searchShowIDs = 50
)

// SearchService answers tm search's three layers over indexes it opens
// by project slug and holds: the server's, or a tm command's own when
// no server answers. The zero value is ready.
type SearchService struct {
	mu sync.Mutex
	m  map[string]*searchEnt
}

type searchEnt struct {
	x    *search.Index
	dir  string // the project's folder
	used time.Time
}

// index is project slug's index and folder, opened on first use.
func (si *SearchService) index(slug string) (*search.Index, string, *proto.Error) {
	if slug == "" {
		return nil, "", proto.Errorf(proto.ErrBadParams, "search: no project")
	}
	dir, err := project.Dir(slug)
	if err != nil {
		return nil, "", proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, "", proto.Errorf(proto.ErrBadParams, "no project %q", slug)
	}
	root, err := home.SearchDir()
	if err != nil {
		return nil, "", proto.Errorf(proto.ErrInternal, "%v", err)
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	if si.m == nil {
		si.m = map[string]*searchEnt{}
	}
	e := si.m[slug]
	if e == nil || e.dir != dir {
		x := search.Open(dir, filepath.Join(root, slug))
		x.SetFresh(SearchFresh)
		e = &searchEnt{x: x, dir: dir}
		si.m[slug] = e
	}
	e.used = time.Now()
	return e.x, dir, nil
}

// Open reports how many indexes are in memory.
func (si *SearchService) Open() int {
	si.mu.Lock()
	defer si.mu.Unlock()
	return len(si.m)
}

// CloseIdle lets go of the indexes unused for idle (all, for 0),
// writing what changed; it answers the errors of those writes.
func (si *SearchService) CloseIdle(idle time.Duration) error {
	si.mu.Lock()
	var gone []*searchEnt
	for slug, e := range si.m {
		if idle == 0 || time.Since(e.used) >= idle {
			gone = append(gone, e)
			delete(si.m, slug)
		}
	}
	si.mu.Unlock()
	var errs []error
	for _, e := range gone {
		if err := e.x.Close(); err != nil {
			errs = append(errs, fmt.Errorf("search index %s: %w", e.x.Dir(), err))
		}
	}
	return errors.Join(errs...)
}

// runSearchIdle closes idle indexes each minute, and all when ctx ends.
func (s *Server) runSearchIdle(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := s.searchIdx.CloseIdle(0); err != nil {
				s.log.Print(err)
			}
			return
		case <-t.C:
			if err := s.searchIdx.CloseIdle(SearchIdle); err != nil {
				s.log.Print(err)
			}
		}
	}
}

// SeesAll reports whether caller c sees the coordinator's chunks too
// (notes for the coordinator, the inbox, where link messages arrive):
// the human and the coordinator, not a thread, a mod or a peer.
func SeesAll(c caller.Caller) bool { return !c.IsAgent() || c.Kind == caller.Coordinator }

func (s *Server) searchQuery(p proto.SearchQueryParams, c callCtx) (any, *proto.Error) {
	return s.searchIdx.Query(p, SeesAll(c.who()))
}

func (s *Server) searchTimeline(p proto.SearchTimelineParams, c callCtx) (any, *proto.Error) {
	return s.searchIdx.Timeline(p, SeesAll(c.who()))
}

func (s *Server) searchShow(p proto.SearchShowParams, c callCtx) (any, *proto.Error) {
	return s.searchIdx.Show(p, SeesAll(c.who()))
}

// Query is search.query's answer, for a caller who sees all (SeesAll)
// or not.
func (si *SearchService) Query(p proto.SearchQueryParams, all bool) (proto.SearchQueryResult, *proto.Error) {
	var none proto.SearchQueryResult
	q := search.Query{Words: p.Words, Task: p.Task, Thread: p.Thread, Coordinator: all, Limit: p.Limit}
	if strings.TrimSpace(p.Words) == "" {
		return none, proto.Errorf(proto.ErrBadParams, "search: no words")
	}
	switch {
	case q.Limit == 0:
		q.Limit = searchLimit
	case q.Limit < 0 || q.Limit > searchMax:
		return none, proto.Errorf(proto.ErrBadParams, "search: limit takes 1 to %d", searchMax)
	}
	for _, k := range p.In {
		if !slices.Contains(search.Kinds, k) {
			return none, proto.Errorf(proto.ErrBadParams, "search: %q is not one of %s", k, strings.Join(search.Kinds, ", "))
		}
		if k == search.KindInbox && !all {
			return none, proto.Errorf(proto.ErrRefused, "search: the inbox is the coordinator's")
		}
	}
	q.Kinds = p.In
	var err error
	if q.Since, err = searchDay(p.Since, false); err != nil {
		return none, proto.Errorf(proto.ErrBadParams, "search: since: %v", err)
	}
	if q.Until, err = searchDay(p.Until, true); err != nil {
		return none, proto.Errorf(proto.ErrBadParams, "search: until: %v", err)
	}
	if p.Reindex && !all {
		return none, proto.Errorf(proto.ErrRefused, "search: a thread doesn't rebuild the index")
	}
	x, dir, perr := si.index(p.Project)
	if perr != nil {
		return none, perr
	}
	if p.Reindex {
		if _, err := x.Reindex(); err != nil {
			return none, proto.Errorf(proto.ErrInternal, "search: %v", err)
		}
	}
	r, err := x.Search(q)
	if err != nil {
		return none, proto.Errorf(proto.ErrInternal, "search: %v", err)
	}
	out := proto.SearchQueryResult{Project: p.Project, Dir: dir, Hits: make([]proto.SearchHit, 0, len(r.Hits)), Total: r.Total,
		Terms: r.Terms, Read: r.Stats.Read, Errors: r.Stats.Errors}
	if out.Terms == nil {
		out.Terms = []string{}
	}
	for _, h := range r.Hits {
		sh := search.HitOf(h.Chunk)
		sh.Score, sh.Terms, sh.More = h.Score, h.Terms, h.More
		out.Hits = append(out.Hits, sh)
	}
	return out, nil
}

// searchDay is a YYYY-MM-DD day's start, or for end its last moment;
// "" is none.
func searchDay(v string, end bool) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, errors.New("want YYYY-MM-DD")
	}
	if end {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}

// searchErr is an index error as the wire carries it: an id it doesn't
// hold is ErrNoChunk.
func searchErr(err error) *proto.Error {
	if errors.Is(err, search.ErrNoChunk) {
		return proto.Errorf(proto.ErrNoChunk, "%v", err)
	}
	return proto.Errorf(proto.ErrInternal, "search: %v", err)
}

// Timeline is search.timeline's answer: 8 chunks on each side when the
// params name no number.
func (si *SearchService) Timeline(p proto.SearchTimelineParams, all bool) (proto.SearchTimelineResult, *proto.Error) {
	var none proto.SearchTimelineResult
	if p.ID == "" {
		return none, proto.Errorf(proto.ErrBadParams, "search.timeline: no id")
	}
	before, after := orDefault(p.Before, 8), orDefault(p.After, 8)
	if before < 0 || after < 0 || before > searchMax || after > searchMax {
		return none, proto.Errorf(proto.ErrBadParams, "search.timeline: before and after take 1 to %d", searchMax)
	}
	x, _, perr := si.index(p.Project)
	if perr != nil {
		return none, perr
	}
	t, err := x.Timeline(p.ID, before, after, p.All, all)
	if err != nil {
		return none, searchErr(err)
	}
	out := proto.SearchTimelineResult{Project: p.Project, At: search.HitOf(t.At), Before: []proto.SearchHit{}, After: []proto.SearchHit{}}
	for _, ch := range t.Before {
		out.Before = append(out.Before, search.HitOf(ch))
	}
	for _, ch := range t.After {
		out.After = append(out.After, search.HitOf(ch))
	}
	return out, nil
}

func orDefault(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

// Show is search.show's answer: each id's text, at most
// proto.SearchShowEach bytes each and proto.SearchShowAll in all; a
// bare thread id lists its chunks, a task's chunk comes with them.
func (si *SearchService) Show(p proto.SearchShowParams, all bool) (proto.SearchShowResult, *proto.Error) {
	var none proto.SearchShowResult
	if len(p.IDs) == 0 {
		return none, proto.Errorf(proto.ErrBadParams, "search.show: no ids")
	}
	if len(p.IDs) > searchShowIDs {
		return none, proto.Errorf(proto.ErrBadParams, "search.show: at most %d ids", searchShowIDs)
	}
	x, dir, perr := si.index(p.Project)
	if perr != nil {
		return none, perr
	}
	out := proto.SearchShowResult{Project: p.Project, Dir: dir, Items: []proto.SearchText{}}
	left := proto.SearchShowAll
	for _, id := range p.IDs {
		it := proto.SearchText{SearchHit: proto.SearchHit{ID: id}}
		ch, text, err := x.Text(id, all)
		object := tasks.ValidThreadID(id) || tasks.ValidRef(id)
		switch {
		case err == nil:
			it.SearchHit = search.HitOf(ch)
			it.Bytes = len(text)
			it.Text, it.Cut = search.CutText(text, min(proto.SearchShowEach, left))
			left -= len(it.Text)
		case errors.Is(err, search.ErrNoChunk) && object:
		default:
			it.Error = err.Error()
		}
		if object && it.Error == "" {
			kids, err := x.Children(id, all)
			if err != nil {
				return none, searchErr(err)
			}
			for _, k := range kids {
				if k.ID != id {
					it.Chunks = append(it.Chunks, search.HitOf(k))
				}
			}
			if it.Path == "" && len(it.Chunks) == 0 {
				it.Error = id + ": " + search.ErrNoChunk.Error()
			}
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
