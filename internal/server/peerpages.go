package server

// A shared project's pages, for a paired server whose project pages
// switch here is on for it (docs/SPEC.md §16.17): its board, overview
// and library, read-only. Each record is copied field by field from this
// server's own, so a field added to a task, the project or a library
// file stays home until it is added here (peerpages_test.go fails until
// it is decided). The memory has no method: nothing of it crosses.

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// sharedProject opens slug for a paired server's page: not-shared for a
// slug no shared project has.
func sharedProject(c caller.Caller, slug, method string) (*project.Project, error) {
	if c.Kind != caller.Peer || c.Alias == "" {
		return nil, proto.Errorf(proto.ErrRefused, "%s is a paired server's", method)
	}
	if !slices.Contains(sharedProjects(), slug) {
		return nil, errNotShared(slug)
	}
	p, err := project.Open(slug)
	if err != nil {
		return nil, errNotShared(slug)
	}
	return p, nil
}

func (e peerExport) Pages(c caller.Caller, slug string) (proto.PeerPages, error) {
	p, err := sharedProject(c, slug, proto.MethodPeerPages)
	if err != nil {
		return proto.PeerPages{}, err
	}
	out := proto.PeerPages{Project: slug, Tasks: []proto.PeerPageTask{},
		Overview: proto.PeerOverview{Goal: p.Meta.Goal, Body: cutBytes(p.Instructions, proto.PeerPagesMaxBody), Repos: len(p.Repos)}}
	b, err := p.Tasks().Load()
	if err != nil {
		return out, nil
	}
	out.Tasks, out.Truncated = peerPageTasks(b.Tasks, proto.PeerPagesBudget-len(out.Overview.Body)-len(out.Overview.Goal))
	return out, nil
}

// peerPageTasks are the board's tasks not archived, in board order, as
// many as fit budget bytes of text: done and dropped ones go first when
// they don't all fit. truncated says some were left out.
func peerPageTasks(ts []*tasks.Task, budget int) (out []proto.PeerPageTask, truncated bool) {
	var live, closed []proto.PeerPageTask
	for _, t := range ts {
		if t.Archived {
			continue
		}
		pt := peerPageTaskOf(t)
		if t.Status == tasks.Done || t.Status == tasks.Dropped {
			closed = append(closed, pt)
		} else {
			live = append(live, pt)
		}
	}
	keep := map[int]bool{}
	for _, pt := range slices.Concat(live, closed) {
		n := pageTaskBytes(pt)
		if len(keep) == proto.PeerPagesMaxTasks || n > budget {
			truncated = true
			continue
		}
		budget -= n
		keep[pt.ID] = true
	}
	out = []proto.PeerPageTask{}
	for _, pt := range slices.Concat(live, closed) {
		if keep[pt.ID] {
			out = append(out, pt)
		}
	}
	// Board order again: the closed ones were only put last to be cut
	// first.
	order := map[int]int{}
	for i, t := range ts {
		order[t.ID] = i
	}
	slices.SortStableFunc(out, func(a, b proto.PeerPageTask) int { return order[a.ID] - order[b.ID] })
	return out, truncated
}

// pageTaskBytes is about what pt's JSON takes.
func pageTaskBytes(pt proto.PeerPageTask) int {
	n := 96 + len(pt.Title) + len(pt.Status) + len(pt.Thread) + len(pt.Created) + len(pt.Updated)
	for _, s := range pt.Steps {
		n += 24 + len(s.Text)
	}
	return n
}

// peerPageTaskOf is t as a paired server may see it on its board: its
// notes, owner, repo, PR and reports stay home; its title and steps are
// cut to proto.PeerPagesTitle and proto.PeerPagesStep runes.
func peerPageTaskOf(t *tasks.Task) proto.PeerPageTask {
	pt := proto.PeerPageTask{ID: t.ID, Title: cutRunes(t.Title, proto.PeerPagesTitle), Status: string(t.Status), Thread: t.Thread,
		Created: t.Created, Updated: t.Updated}
	for i, s := range t.Steps {
		if i == proto.PeerPagesMaxSteps {
			break
		}
		pt.Steps = append(pt.Steps, proto.PeerStep{Text: cutRunes(s.Text, proto.PeerPagesStep), Done: s.Done})
	}
	return pt
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// cutBytes is s cut to at most n bytes, at a rune's start.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

func (e peerExport) Library(c caller.Caller, slug string) (proto.PeerLibrary, error) {
	p, err := sharedProject(c, slug, proto.MethodPeerPagesLibrary)
	if err != nil {
		return proto.PeerLibrary{}, err
	}
	out := proto.PeerLibrary{Files: []proto.PeerLibraryFile{}}
	// The threads' files only: a linked project's (link.LibraryFiles)
	// came from another server, not this one's to pass on.
	files, err := thread.Library(p)
	if err != nil {
		return out, nil
	}
	for _, f := range files {
		if len(out.Files) == proto.PeerPagesMaxFiles {
			break
		}
		if f.Peer != "" || f.Message != "" {
			continue
		}
		out.Files = append(out.Files, peerLibraryFileOf(f))
	}
	return out, nil
}

// peerLibraryFileOf is f as a paired server may see it.
func peerLibraryFileOf(f thread.LibFile) proto.PeerLibraryFile {
	return proto.PeerLibraryFile{Thread: f.Thread, Task: f.Task, Title: f.Title, State: f.State, Name: f.Name, Size: f.Size, Time: f.Time}
}

func (e peerExport) LibraryRead(c caller.Caller, pp proto.PeerLibraryReadParams) (proto.LibraryReadResult, error) {
	p, err := sharedProject(c, pp.Project, proto.MethodPeerPagesLibraryRead)
	if err != nil {
		return proto.LibraryReadResult{}, err
	}
	// A thread's library file only, never one a link brought (those
	// are named by a message id, which ValidID refuses here).
	if !thread.ValidID(pp.Thread) {
		return proto.LibraryReadResult{}, proto.Errorf("unknown-thread", "%q is not a thread id like t-0003", pp.Thread)
	}
	data, size, mod, err := thread.ReadLibraryChunk(p, pp.Thread, pp.Name, pp.Offset, proto.PeerLibraryChunk)
	if err != nil {
		var terr *tasks.Error
		if errors.As(err, &terr) {
			return proto.LibraryReadResult{}, proto.Errorf(terr.Code, "%s", terr.Msg)
		}
		return proto.LibraryReadResult{}, proto.Errorf(proto.ErrRefused, "%s can't be read now", pp.Name)
	}
	if size > proto.PeerLibraryMaxRead {
		return proto.LibraryReadResult{}, proto.Errorf("too-large", "%s is %d bytes, over the %d a paired server reads", pp.Name, size, proto.PeerLibraryMaxRead)
	}
	return proto.LibraryReadResult{Name: pp.Name, Size: size, MTime: mod, Offset: pp.Offset, Data: data}, nil
}

// checkPeerRead checks a console's alias and project.
func checkPeerRead(method, alias, slug string) *proto.Error {
	if alias == "" || strings.ContainsAny(alias, "/@") || !project.ValidSlug(slug) {
		return proto.Errorf(proto.ErrBadParams, "%s needs a paired server and its project", method)
	}
	return nil
}

// readPeerPages is peer.pages-read: the paired server alias's board and
// overview of its project slug, for a console.
func (s *Server) readPeerPages(p proto.PeerPagesReadParams, _ callCtx) (any, *proto.Error) {
	if perr := checkPeerRead(proto.MethodPeerPagesRead, p.Alias, p.Project); perr != nil {
		return nil, perr
	}
	pp, err := s.peers.Pages(context.Background(), p.Alias, p.Project)
	if err != nil {
		return nil, pagesError(p.Alias, err)
	}
	return pp, nil
}

// listPeerLibrary is peer.library-list.
func (s *Server) listPeerLibrary(p proto.PeerPagesReadParams, _ callCtx) (any, *proto.Error) {
	if perr := checkPeerRead(proto.MethodPeerLibraryList, p.Alias, p.Project); perr != nil {
		return nil, perr
	}
	lib, err := s.peers.Library(context.Background(), p.Alias, p.Project)
	if err != nil {
		return nil, pagesError(p.Alias, err)
	}
	return lib, nil
}

// readPeerLibrary is peer.library-read.
func (s *Server) readPeerLibrary(p proto.PeerLibraryReadReadParams, _ callCtx) (any, *proto.Error) {
	if perr := checkPeerRead(proto.MethodPeerLibraryRead, p.Alias, p.Project); perr != nil {
		return nil, perr
	}
	if !thread.ValidID(p.Thread) || p.Name == "" || p.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.library-read needs a thread, a file name and an offset")
	}
	r, err := s.peers.LibraryRead(context.Background(), p.Alias,
		proto.PeerLibraryReadParams{Project: p.Project, Thread: p.Thread, Name: p.Name, Offset: p.Offset})
	if err != nil {
		return nil, pagesError(p.Alias, err)
	}
	return r, nil
}

// pagesError is a failed page read in this server's words, its code
// kept: no-pages (a tm without them) and pages-off both read as "not
// shared" in the apps.
func pagesError(alias string, err error) *proto.Error {
	var pe *proto.Error
	switch {
	case errors.As(err, &pe) && (pe.Code == proto.ErrUnknownMethod || pe.Code == proto.ErrNoPages):
		return proto.Errorf(proto.ErrNoPages, "%s's tm shares no project pages; update it to share them", alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrPagesOff:
		return proto.Errorf(proto.ErrPagesOff, "%s doesn't share this project's pages with this server: on %s, tm peer pages <this server> <project> on", alias, alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrNotShared:
		return proto.Errorf(proto.ErrNotShared, "%s doesn't share that project any more", alias)
	case errors.As(err, &pe):
		return pe
	}
	return proto.Errorf(proto.ErrRefused, "%s can't be reached now (%v)", alias, err)
}
