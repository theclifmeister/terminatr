package server

// A shared project's folders, for a paired server whose files switch here
// is on for it. The reading server is only a caller, so every rule is here:
// the link has checked the rate and the switch (internal/peer); then, on
// every request, the caller, that the project is shared, the session, the
// folder it decides, and the paths, by a far filetree.Scope. Each record is
// copied field by field from this server's own, so a field added to
// proto.FileEntry or a result stays home until it is added here
// (peerworktree_test.go fails until it is decided). Nothing here writes.

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/filetree"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// peerWorktrees is what those reads keep: git's answers, and whether the
// log has said this peer reads this session.
type peerWorktrees struct {
	seen filetree.Cache
	logf func(format string, a ...any)
	said sync.Map // alias and session
}

// reads logs a paired server's first read of a session: no paths, and no
// more lines as it goes on.
func (w *peerWorktrees) reads(alias, id string) {
	if w == nil || w.logf == nil {
		return
	}
	if _, had := w.said.LoadOrStore(alias+"\x00"+id, true); !had {
		w.logf("session %s: paired server %s reads its folder", id, alias)
	}
}

// cache is where git's answers are kept.
func (e peerExport) cache() *filetree.Cache {
	if e.worktrees == nil {
		return &filetree.Cache{}
	}
	return &e.worktrees.seen
}

// worktreeSession holds a request to the rules up to the folder: the
// caller, a shared project, and a session of it, its coordinator or a
// thread. Any other session answers as one that isn't there.
func (e peerExport) worktreeSession(c caller.Caller, slug, id string) (*project.Project, proto.SessionInfo, error) {
	var none proto.SessionInfo
	if c.Kind != caller.Peer || c.Alias == "" {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrFilesOff)
	}
	if !slices.Contains(sharedProjects(), slug) {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrNotShared)
	}
	p, err := project.Open(slug)
	if err != nil {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrNotShared)
	}
	// Nothing chains through to a third server.
	if _, _, remote := remoteID(id); remote {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrUnknownSession)
	}
	sessions := e.sessions()
	i := slices.IndexFunc(sessions, func(info proto.SessionInfo) bool { return info.ID == id })
	if id == "" || i < 0 {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrUnknownSession)
	}
	info := sessions[i]
	if info.Project != slug || (info.Role != proto.RoleCoordinator && info.Role != proto.RoleThread) {
		return nil, none, proto.PeerWorktreeRefusal(proto.ErrUnknownSession)
	}
	return p, info, nil
}

// worktreeScope is the folder a request reads: the session decides it, and
// only a checkout of a repo on the project's list crosses. A thread has its
// worktree, so no plain folder and no adopted repo off the list; a
// coordinator a listed repo at its main checkout, by name, the first when
// repo is "".
func (e peerExport) worktreeScope(ctx context.Context, c caller.Caller, slug, id, repo string) (*filetree.Scope, error) {
	p, info, err := e.worktreeSession(c, slug, id)
	if err != nil {
		return nil, err
	}
	root, checkout, base := "", "", ""
	switch info.Role {
	case proto.RoleThread:
		if repo != "" {
			return nil, proto.PeerWorktreeRefusal(proto.ErrBadParams)
		}
		r, err := thread.Load(p, info.Thread)
		if err != nil || r.Repo == "" || !p.ListsRepo(r.Repo) {
			return nil, proto.PeerWorktreeRefusal(proto.ErrNoFiles)
		}
		root, checkout, base = r.Worktree, r.Repo, r.Base
	default:
		repos := fileRepos(p.Repos)
		i := 0
		if repo != "" {
			i = slices.IndexFunc(repos, func(r proto.FileRepo) bool { return r.Name == repo })
		}
		if i < 0 || i >= len(repos) {
			return nil, proto.PeerWorktreeRefusal(proto.ErrNoFiles)
		}
		root, checkout = repos[i].Path, repos[i].Path
	}
	sc, err := filetree.OpenFar(ctx, root, checkout, base)
	if err != nil {
		return nil, proto.PeerWorktreeRefusal(proto.ErrNoFiles)
	}
	e.worktrees.reads(c.Alias, id)
	return sc, nil
}

func (e peerExport) WorktreeRepos(_ context.Context, c caller.Caller, pp proto.PeerWorktreeParams) (proto.PeerWorktreeRepos, error) {
	out := proto.PeerWorktreeRepos{Repos: []string{}}
	p, info, err := e.worktreeSession(c, pp.Project, pp.Session)
	if err != nil {
		return out, err
	}
	if info.Role == proto.RoleCoordinator {
		for _, r := range fileRepos(p.Repos) {
			out.Repos = append(out.Repos, r.Name)
		}
	}
	return out, nil
}

func (e peerExport) WorktreeList(ctx context.Context, c caller.Caller, pp proto.PeerWorktreeListParams) (proto.PeerWorktreeList, error) {
	ctx, cancel := context.WithTimeout(ctx, changes.Timeout)
	defer cancel()
	sc, err := e.worktreeScope(ctx, c, pp.Project, pp.Session, pp.Repo)
	if err != nil {
		return proto.PeerWorktreeList{}, err
	}
	cs, err := e.cache().Changes(ctx, sc)
	if err != nil {
		return proto.PeerWorktreeList{}, proto.PeerWorktreeRefusal(proto.ErrInternal)
	}
	res, err := sc.List(ctx, pp.Path, filetree.Options{Query: pp.Query, Changed: pp.Changed}, cs)
	if err != nil {
		return proto.PeerWorktreeList{}, proto.PeerWorktreeRefusal(filesCode(err))
	}
	return peerWorktreeListOf(res), nil
}

// peerWorktreeListOf is res as a paired server may see it: no Root, and no
// entry git ignores. Its entries stop at what a line carries, however JSON
// writes their names.
func peerWorktreeListOf(res proto.FilesListResult) proto.PeerWorktreeList {
	out := proto.PeerWorktreeList{Path: res.Path, Git: res.Git, Files: res.Files, Added: res.Added, Deleted: res.Deleted,
		Entries: make([]proto.PeerFileEntry, 0, len(res.Entries)), Truncated: res.Truncated}
	wire := 0
	for _, e := range res.Entries {
		if e.Ignored {
			continue
		}
		pe := peerFileEntryOf(e)
		b, _ := json.Marshal(pe)
		if wire += len(b) + 1; wire > proto.PeerWorktreeMaxHunks {
			out.Truncated = true
			break
		}
		out.Entries = append(out.Entries, pe)
	}
	return out
}

// peerFileEntryOf is e as a paired server may see it.
func peerFileEntryOf(e proto.FileEntry) proto.PeerFileEntry {
	return proto.PeerFileEntry{Name: e.Name, Path: e.Path, Kind: e.Kind, Size: e.Size, MTime: e.MTime, Status: e.Status, Changed: e.Changed,
		Link: e.Link, LinkOut: e.LinkOut, Withheld: e.Withheld}
}

func (e peerExport) WorktreeRead(ctx context.Context, c caller.Caller, pp proto.PeerWorktreeReadParams) (proto.PeerWorktreeRead, error) {
	ctx, cancel := context.WithTimeout(ctx, changes.Timeout)
	defer cancel()
	sc, err := e.worktreeScope(ctx, c, pp.Project, pp.Session, pp.Repo)
	if err != nil {
		return proto.PeerWorktreeRead{}, err
	}
	res, err := sc.Read(ctx, pp.Path, pp.Offset, false)
	if err != nil {
		return proto.PeerWorktreeRead{}, proto.PeerWorktreeRefusal(filesCode(err))
	}
	return peerWorktreeReadOf(res), nil
}

// peerWorktreeReadOf is res as a paired server may see it: every field, for
// now, so a conversion, which a new field on either side stops compiling.
func peerWorktreeReadOf(res proto.FilesReadResult) proto.PeerWorktreeRead {
	return proto.PeerWorktreeRead(res)
}

func (e peerExport) WorktreeStamp(ctx context.Context, c caller.Caller, pp proto.PeerWorktreeStampParams) (proto.PeerWorktreeStamp, error) {
	if len(pp.Folders) > proto.PeerWorktreeStamped {
		return proto.PeerWorktreeStamp{}, proto.PeerWorktreeRefusal(proto.ErrBadParams)
	}
	ctx, cancel := context.WithTimeout(ctx, changes.Timeout)
	defer cancel()
	sc, err := e.worktreeScope(ctx, c, pp.Project, pp.Session, pp.Repo)
	if err != nil {
		return proto.PeerWorktreeStamp{}, err
	}
	return proto.PeerWorktreeStamp{Stamp: e.cache().Stamp(ctx, sc, slices.Clone(pp.Folders))}, nil
}

func (e peerExport) WorktreeDiff(ctx context.Context, c caller.Caller, pp proto.PeerWorktreeDiffParams) (proto.PeerWorktreeDiff, error) {
	ctx, cancel := context.WithTimeout(ctx, changes.Timeout)
	defer cancel()
	sc, err := e.worktreeScope(ctx, c, pp.Project, pp.Session, pp.Repo)
	if err != nil {
		return proto.PeerWorktreeDiff{}, err
	}
	cs, err := e.cache().Changes(ctx, sc)
	if err != nil {
		return proto.PeerWorktreeDiff{}, proto.PeerWorktreeRefusal(proto.ErrInternal)
	}
	res, err := sc.Diff(ctx, pp.Path, false, cs)
	if err != nil {
		return proto.PeerWorktreeDiff{}, proto.PeerWorktreeRefusal(filesCode(err))
	}
	return peerWorktreeDiffOf(res), nil
}

// peerWorktreeDiffOf is res as a paired server may see it.
func peerWorktreeDiffOf(res proto.FilesDiffResult) proto.PeerWorktreeDiff {
	out := proto.PeerWorktreeDiff{Name: res.Name, Path: res.Path, Status: res.Status, From: res.From, Base: res.Base, Size: res.Size,
		MTime: res.MTime, Added: res.Added, Deleted: res.Deleted, Binary: res.Binary, TooBig: res.TooBig, Truncated: res.Truncated}
	for _, h := range res.Hunks {
		out.Hunks = append(out.Hunks, peerFileHunkOf(h))
	}
	return out
}

// peerFileHunkOf is h as a paired server may see it: every field, as
// peerWorktreeReadOf.
func peerFileHunkOf(h proto.FilesHunk) proto.PeerFileHunk {
	return proto.PeerFileHunk(h)
}
