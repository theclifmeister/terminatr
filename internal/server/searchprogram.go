package server

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/search"
)

// program.search (the programs design §8): a program's index and its
// local members' indexes, as one. The program's ids are plain; a
// member's carry "<slug>:". A member is read with the thread's view
// (never its coordinator notes or inbox), and only when search_children
// is on for the program; paired members (<alias>/<slug>) aren't searched.

// searchMembers are program slug's local members the director may
// search, under cfg: programs on, search_children on, each a project on
// this server.
func searchMembers(cfg *config.Config, slug string) ([]string, *proto.Error) {
	if !features.Programs(cfg) {
		return nil, proto.Errorf(features.OffCode, "%s", features.OffMsg)
	}
	t, ok := cfg.Program(slug)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "no program %q", slug)
	}
	if !t.SearchChildren {
		return nil, nil
	}
	var out []string
	for _, m := range t.Projects {
		if !strings.Contains(m, "/") && project.ValidSlug(m) {
			out = append(out, m)
		}
	}
	return out, nil
}

// splitProgramID is a program search id's member and the member's own
// id: "acme-api:T4" is (acme-api, T4); an id whose prefix is no member
// (the program's own note:decisions#x) is ("", id).
func splitProgramID(id string, members []string) (member, rest string) {
	if i := strings.IndexByte(id, ':'); i > 0 && slices.Contains(members, id[:i]) {
		return id[:i], id[i+1:]
	}
	return "", id
}

// prefixed marks hit h as member's: its id with "<member>:", its path
// absolute (the program's answer holds many folders).
func prefixedHit(h proto.SearchHit, member, dir string) proto.SearchHit {
	if member != "" {
		h.ID = member + ":" + h.ID
	}
	if h.Path != "" && !filepath.IsAbs(h.Path) {
		h.Path = filepath.Join(dir, h.Path)
	}
	return h
}

// ProgramQuery is program.search's answer: the program's own chunks and
// its members' (unless p.Member names one), best first.
func (si *SearchService) ProgramQuery(cfg *config.Config, p proto.ProgramSearchParams, own bool) (proto.SearchQueryResult, *proto.Error) {
	var none proto.SearchQueryResult
	members, perr := searchMembers(cfg, p.Program)
	if perr != nil {
		return none, perr
	}
	if p.Member != "" && p.Member != p.Program && !slices.Contains(members, p.Member) {
		return none, proto.Errorf(proto.ErrBadParams, "search: %q isn't a member of program %s that can be searched", p.Member, p.Program)
	}
	limit := p.Limit
	if limit == 0 {
		limit = searchLimit
	}
	// The program's index first: it holds the inbox and the notes only
	// the director's side reads.
	memberIn := slices.DeleteFunc(slices.Clone(p.In), func(k string) bool { return k == search.KindInbox })
	out := proto.SearchQueryResult{Project: p.Program, Hits: []proto.SearchHit{}, Terms: []string{}}
	add := func(r proto.SearchQueryResult, member string) {
		for _, h := range r.Hits {
			out.Hits = append(out.Hits, prefixedHit(h, member, r.Dir))
		}
		out.Total += r.Total
		out.Read += r.Read
		for _, e := range r.Errors {
			if member != "" {
				e = member + ": " + e
			}
			out.Errors = append(out.Errors, e)
		}
		for _, t := range r.Terms {
			if !slices.Contains(out.Terms, t) {
				out.Terms = append(out.Terms, t)
			}
		}
	}
	if p.Member == "" || p.Member == p.Program {
		x, dir, perr := si.programIndex(cfg, p.Program)
		if perr != nil {
			return none, perr
		}
		sp := searchParamsOf(p, p.In)
		q, perr := prepQuery(sp, own)
		if perr != nil {
			return none, perr
		}
		r, perr := runQuery(x, dir, p.Program, sp, q)
		if perr != nil {
			return none, perr
		}
		add(r, "")
		out.Dir = dir
	}
	if p.Member != p.Program && !(len(memberIn) == 0 && len(p.In) > 0) {
		for _, m := range members {
			if p.Member != "" && p.Member != m {
				continue
			}
			r, perr := si.Query(searchParamsOf(p, memberIn, m), false)
			if perr != nil {
				if p.Member != "" {
					return none, perr
				}
				out.Errors = append(out.Errors, m+": "+perr.Message)
				continue
			}
			add(r, m)
		}
	}
	sort.SliceStable(out.Hits, func(i, j int) bool { return out.Hits[i].Score > out.Hits[j].Score })
	if len(out.Hits) > limit {
		out.Hits = out.Hits[:limit]
	}
	return out, nil
}

// searchParamsOf is p as search.query's params for project (the
// program's own index when none), In kinds in.
func searchParamsOf(p proto.ProgramSearchParams, in []string, project ...string) proto.SearchQueryParams {
	q := proto.SearchQueryParams{Words: p.Words, In: in, Since: p.Since, Until: p.Until, Limit: p.Limit}
	if len(project) > 0 {
		q.Project = project[0]
	}
	return q
}

// target is the index an id of program slug belongs to: a member's
// (thread view) or the program's own.
func (si *SearchService) target(cfg *config.Config, slug, id string, own bool) (x *search.Index, dir, member, rest string, all bool, perr *proto.Error) {
	members, perr := searchMembers(cfg, slug)
	if perr != nil {
		return nil, "", "", "", false, perr
	}
	member, rest = splitProgramID(id, members)
	if member != "" {
		x, dir, perr = si.index(member)
		return x, dir, member, rest, false, perr
	}
	x, dir, perr = si.programIndex(cfg, slug)
	return x, dir, "", rest, own, perr
}

// ProgramTimeline is program.search.timeline's answer: search.timeline's
// around a prefixed id, its ids prefixed.
func (si *SearchService) ProgramTimeline(cfg *config.Config, p proto.ProgramTimelineParams, own bool) (proto.SearchTimelineResult, *proto.Error) {
	var none proto.SearchTimelineResult
	if p.ID == "" {
		return none, proto.Errorf(proto.ErrBadParams, "search.timeline: no id")
	}
	x, dir, member, rest, all, perr := si.target(cfg, p.Program, p.ID, own)
	if perr != nil {
		return none, perr
	}
	before, after := orDefault(p.Before, 8), orDefault(p.After, 8)
	if before < 0 || after < 0 || before > searchMax || after > searchMax {
		return none, proto.Errorf(proto.ErrBadParams, "search.timeline: before and after take 1 to %d", searchMax)
	}
	t, err := x.Timeline(rest, before, after, p.All, all)
	if err != nil {
		return none, searchErr(err)
	}
	mark := func(c search.Chunk) proto.SearchHit { return prefixedHit(search.HitOf(c), member, dir) }
	out := proto.SearchTimelineResult{Project: p.Program, At: mark(t.At), Before: []proto.SearchHit{}, After: []proto.SearchHit{}}
	for _, c := range t.Before {
		out.Before = append(out.Before, mark(c))
	}
	for _, c := range t.After {
		out.After = append(out.After, mark(c))
	}
	return out, nil
}

// ProgramShow is program.search.show's answer: search.show's for each
// prefixed id, in the order asked, with search.show's bounds in all.
func (si *SearchService) ProgramShow(cfg *config.Config, p proto.ProgramShowParams, own bool) (proto.SearchShowResult, *proto.Error) {
	var none proto.SearchShowResult
	if len(p.IDs) == 0 {
		return none, proto.Errorf(proto.ErrBadParams, "search.show: no ids")
	}
	if len(p.IDs) > searchShowIDs {
		return none, proto.Errorf(proto.ErrBadParams, "search.show: at most %d ids", searchShowIDs)
	}
	out := proto.SearchShowResult{Project: p.Program, Items: []proto.SearchText{}}
	left := proto.SearchShowAll
	for _, id := range p.IDs {
		x, dir, member, rest, all, perr := si.target(cfg, p.Program, id, own)
		if perr != nil {
			return none, perr
		}
		if out.Dir == "" {
			out.Dir = dir
		}
		// One id at a time keeps the asked order across folders; the
		// byte budget is this call's.
		r, perr := showFrom(x, dir, p.Program, []string{rest}, all)
		if perr != nil {
			return none, perr
		}
		for _, it := range r.Items {
			it.SearchHit = prefixedHit(it.SearchHit, member, dir)
			for i := range it.Chunks {
				it.Chunks[i] = prefixedHit(it.Chunks[i], member, dir)
			}
			if len(it.Text) > left {
				it.Text, it.Cut = search.CutText(it.Text, max(left, 0))
			}
			left -= len(it.Text)
			out.Items = append(out.Items, it)
		}
	}
	return out, nil
}

// cfgNow is config.toml now; on a safety error the tables it gave.
func (s *Server) cfgNow() *config.Config {
	cfg, _ := config.Load()
	return cfg
}
