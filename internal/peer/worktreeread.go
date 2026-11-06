package peer

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/filetree"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// A paired server's folders, read from this side. The owning server holds
// every rule (internal/server/peerworktree.go there), so nothing here keeps a
// secret: what arrives is only that server's word, and an agent wrote it. So
// each answer is cleaned before a console sees it, and one that doesn't add up
// is refused. Nothing from here goes to disk.

var (
	// worktreeWait bounds a read and a stamp: a screen waits for them.
	worktreeWait = 15 * time.Second
	// worktreeGitWait bounds a listing and a diff: the far side's git may
	// take its changes.Timeout, 30 s, on a first look.
	worktreeGitWait = 35 * time.Second
	// worktreeStampFresh is how old a stamp this server's consoles share
	// gets: however many look, the owning server is asked once in it.
	worktreeStampFresh = 5 * time.Second
)

const (
	// maxStampsSeen bounds the stamps kept, one per session and folders.
	maxStampsSeen = 256
	// maxPeerRepos bounds the repos a session may choose from.
	maxPeerRepos = 256
	// maxPeerStamp bounds a stamp: an opaque word, compared, never shown.
	maxPeerStamp = 256
)

// ErrNoWorktree means the peer's tm serves no folders (its hello doesn't
// list CapWorktree): it is never asked.
var ErrNoWorktree = proto.Errorf(proto.ErrNoPeerFiles, "the paired server's tm shares no files")

// errWorktreeAddUp refuses an answer that can't be what was asked for.
func errWorktreeAddUp() error {
	return proto.Errorf(proto.ErrRefused, "the paired server's answer doesn't add up")
}

// worktreeCall asks alias's link, for at most wait, and only a link whose
// hello lists CapWorktree.
func worktreeCall[R any](ctx context.Context, m *Manager, alias, method string, params any, wait time.Duration) (R, error) {
	var r R
	if m == nil {
		return r, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return r, errNotUp
	}
	if st := l.status(); st.State == proto.LinkUp && !slices.Contains(st.Caps, CapWorktree) {
		return r, ErrNoWorktree
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	err := l.call(ctx, method, params, &r)
	return r, err
}

// WorktreeRepos is what a session of the paired server alias's may choose:
// each repo by the name that server gives it, which is what Path says too,
// so a client echoes it as any repo's. A thread has none.
func (m *Manager) WorktreeRepos(ctx context.Context, alias string, p proto.PeerWorktreeParams) (proto.FilesReposResult, error) {
	r, err := worktreeCall[proto.PeerWorktreeRepos](ctx, m, alias, proto.MethodPeerWorktreeRepos, p, worktreeWait)
	if err != nil {
		return proto.FilesReposResult{}, err
	}
	return cleanWorktreeRepos(r), nil
}

// WorktreeList lists a folder of a session of the paired server alias's, or
// finds files under it, cleaned (cleanWorktreeList). Root stays "": the
// folder's path is that server's.
func (m *Manager) WorktreeList(ctx context.Context, alias string, p proto.PeerWorktreeListParams) (proto.FilesListResult, error) {
	rel, err := filetree.CleanPath(p.Path)
	if err != nil || len(p.Query) > proto.MaxFilesQuery {
		return proto.FilesListResult{}, proto.PeerWorktreeRefusal(proto.ErrInvalidPath)
	}
	p.Path = rel
	r, err := worktreeCall[proto.PeerWorktreeList](ctx, m, alias, proto.MethodPeerWorktreeList, p, worktreeGitWait)
	if err != nil {
		return proto.FilesListResult{}, err
	}
	return cleanWorktreeList(p, r)
}

// WorktreeRead reads a chunk of a file of a session of the paired server
// alias's, checked (cleanWorktreeRead).
func (m *Manager) WorktreeRead(ctx context.Context, alias string, p proto.PeerWorktreeReadParams) (proto.FilesReadResult, error) {
	rel, err := filetree.CleanPath(p.Path)
	if err != nil {
		return proto.FilesReadResult{}, proto.PeerWorktreeRefusal(proto.ErrInvalidPath)
	}
	if p.Offset < 0 {
		return proto.FilesReadResult{}, proto.PeerWorktreeRefusal(proto.ErrBadParams)
	}
	p.Path = rel
	r, err := worktreeCall[proto.PeerWorktreeRead](ctx, m, alias, proto.MethodPeerWorktreeRead, p, worktreeWait)
	if err != nil {
		return proto.FilesReadResult{}, err
	}
	return cleanWorktreeRead(p, r)
}

// WorktreeDiff says what changed in a file of a session of the paired
// server alias's, cleaned (cleanWorktreeDiff).
func (m *Manager) WorktreeDiff(ctx context.Context, alias string, p proto.PeerWorktreeDiffParams) (proto.FilesDiffResult, error) {
	rel, err := filetree.CleanPath(p.Path)
	if err != nil {
		return proto.FilesDiffResult{}, proto.PeerWorktreeRefusal(proto.ErrInvalidPath)
	}
	p.Path = rel
	r, err := worktreeCall[proto.PeerWorktreeDiff](ctx, m, alias, proto.MethodPeerWorktreeDiff, p, worktreeGitWait)
	if err != nil {
		return proto.FilesDiffResult{}, err
	}
	return cleanWorktreeDiff(p, r)
}

// stampShare is the stamps this server's consoles share: each asked once in
// worktreeStampFresh, and whoever looks meanwhile waits for that answer or
// has the last. Its zero value is ready.
type stampShare struct {
	mu   sync.Mutex
	seen map[string]*stampSeen
}

// stampSeen is one stamp's last answer; mu is held while asking.
type stampSeen struct {
	mu    sync.Mutex
	at    time.Time
	stamp string
	err   error
}

// entry is the place of key, making room for a new one when it must.
func (s *stampShare) entry(key string) *stampSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.seen[key]
	if e != nil {
		return e
	}
	if s.seen == nil || len(s.seen) >= maxStampsSeen {
		// One a request is asking for just goes on, unkept.
		s.seen = map[string]*stampSeen{}
	}
	e = &stampSeen{}
	s.seen[key] = e
	return e
}

// worktreeStampFolders are the folders a stamp asks about: at most what the
// link takes, in one order and each once, so consoles showing the same share
// it. What isn't a path of the folder has no say.
func worktreeStampFolders(folders []string) []string {
	out := make([]string, 0, min(len(folders), proto.PeerWorktreeStamped))
	for _, f := range folders {
		if rel, err := filetree.CleanPath(f); err == nil {
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out[:min(len(out), proto.PeerWorktreeStamped)]
}

// WorktreeStamp says whether what a list of a session of the paired server
// alias's answered may have changed, from an answer at most
// worktreeStampFresh old. A refusal is shared as a stamp is, so off is seen
// by then. A busy or slow server isn't a change, or an error, once there is
// a stamp: that stamp stands, and it is asked again when this one is old.
func (m *Manager) WorktreeStamp(ctx context.Context, alias string, p proto.PeerWorktreeStampParams) (proto.FilesStampResult, error) {
	if m == nil {
		return proto.FilesStampResult{}, errNotUp
	}
	p.Folders = worktreeStampFolders(p.Folders)
	key := strings.Join(append([]string{alias, p.Project, p.Session, p.Repo}, p.Folders...), "\x00")
	e := m.stamps.entry(key)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.at.IsZero() && time.Since(e.at) < worktreeStampFresh {
		return proto.FilesStampResult{Stamp: e.stamp}, e.err
	}
	r, err := worktreeCall[proto.PeerWorktreeStamp](ctx, m, alias, proto.MethodPeerWorktreeStamp, p, worktreeWait)
	e.at = time.Now()
	switch {
	case err == nil:
		e.stamp, e.err = textsafe.Line(r.Stamp, maxPeerStamp), nil
	case WorktreeBusy(err) && e.stamp != "":
		e.err = nil
	default:
		e.stamp, e.err = "", err
	}
	return proto.FilesStampResult{Stamp: e.stamp}, e.err
}

// WorktreeBusy says err means only that the owning server didn't answer in
// time or is over its rate: nothing is wrong, and a view keeps what it has.
func WorktreeBusy(err error) bool {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code == proto.ErrRateLimited
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// cleanWorktreeRepos is a peer.worktree.repos answer of real names, each
// once, in its order, cut to its most.
func cleanWorktreeRepos(r proto.PeerWorktreeRepos) proto.FilesReposResult {
	out := proto.FilesReposResult{Repos: []proto.FileRepo{}}
	for _, name := range r.Repos {
		if len(out.Repos) == maxPeerRepos {
			break
		}
		// A name is a folder's, its parent's too when two share one.
		rel, err := filetree.CleanPath(name)
		if err != nil || rel == "" || slices.ContainsFunc(out.Repos, func(f proto.FileRepo) bool { return f.Name == rel }) {
			continue
		}
		out.Repos = append(out.Repos, proto.FileRepo{Name: rel, Path: rel})
	}
	return out
}

// cleanWorktreeList is a peer.worktree.list answer as a console may see it,
// or a refusal when it answers another folder. An entry whose path isn't a
// path of what was asked for, in its clean form, is dropped, as is one seen
// before; a name is its path's; words are checked, numbers not negative, the
// list cut to its most, and a withheld entry is its name alone.
func cleanWorktreeList(p proto.PeerWorktreeListParams, l proto.PeerWorktreeList) (proto.FilesListResult, error) {
	if l.Path != p.Path {
		return proto.FilesListResult{}, errWorktreeAddUp()
	}
	flat := p.Query != "" || p.Changed
	most := proto.MaxFilesEntries
	if flat {
		most = proto.MaxFilesFound
	}
	out := proto.FilesListResult{Path: l.Path, Git: l.Git, Files: max(l.Files, 0), Added: max(l.Added, 0), Deleted: max(l.Deleted, 0),
		Entries: make([]proto.FileEntry, 0, min(len(l.Entries), most)), Truncated: l.Truncated}
	seen := make(map[string]bool, min(len(l.Entries), most))
	for _, e := range l.Entries {
		rel, err := filetree.CleanPath(e.Path)
		if err != nil || rel == "" || seen[rel] || !worktreeEntryUnder(p.Path, rel, flat) {
			continue
		}
		if len(out.Entries) == most {
			out.Truncated = true
			break
		}
		seen[rel] = true
		fe := proto.FileEntry{Name: path.Base(rel), Path: rel, Kind: e.Kind, Size: max(e.Size, 0), MTime: e.MTime, Status: fileMark(e.Status),
			Changed: e.Changed, Link: e.Link, LinkOut: e.LinkOut, Withheld: e.Withheld}
		if fe.Kind != proto.FileKindFile && fe.Kind != proto.FileKindDir {
			fe.Kind = proto.FileKindOther
		}
		if fe.Withheld {
			fe.Size, fe.MTime = 0, time.Time{}
		}
		out.Entries = append(out.Entries, fe)
	}
	return out, nil
}

// worktreeEntryUnder says rel may answer a listing of folder: right in it,
// or, flat, anywhere under it.
func worktreeEntryUnder(folder, rel string, flat bool) bool {
	if flat {
		return folder == "" || strings.HasPrefix(rel, folder+"/")
	}
	dir := path.Dir(rel)
	return dir == folder || (dir == "." && folder == "")
}

// fileMark is mark if it is one of a file's, else none.
func fileMark(mark string) string {
	switch mark {
	case proto.FileModified, proto.FileAdded, proto.FileDeleted:
		return mark
	}
	return ""
}

// cleanWorktreeRead is a peer.worktree.read answer as a console may see it,
// or a refusal when it isn't a chunk of what was asked for: its name is the
// path's, and the chunk must fit its bounds and the file's size. A chunk
// that isn't text is called binary here too, without its bytes, whatever the
// answer says. Text keeps its bytes as they came, so chunks still add up: a
// console cleans what it shows, as of a local file.
func cleanWorktreeRead(p proto.PeerWorktreeReadParams, r proto.PeerWorktreeRead) (proto.FilesReadResult, error) {
	if p.Path == "" || r.Path != p.Path || r.Offset != p.Offset || r.Size < 0 || len(r.Data) > proto.PeerLibraryChunk {
		return proto.FilesReadResult{}, errWorktreeAddUp()
	}
	if end := r.Offset + int64(len(r.Data)); len(r.Data) > 0 && (end > r.Size || end > proto.PeerWorktreeMaxRead) {
		return proto.FilesReadResult{}, errWorktreeAddUp()
	}
	out := proto.FilesReadResult{Name: path.Base(p.Path), Path: p.Path, Size: r.Size, MTime: r.MTime, Offset: r.Offset,
		Binary: r.Binary || filetree.ChunkBinary(r.Data), Truncated: r.Truncated || r.Size > proto.PeerWorktreeMaxRead, Data: r.Data}
	if out.Binary {
		out.Data = nil
	}
	return out, nil
}

// cleanWorktreeDiff is a peer.worktree.diff answer as a console may see it,
// or a refusal when it answers another file or its hunks can't be placed:
// they come in order, not overlapping, within the most lines a file looked at
// can have. A removed line stays one line, and removed text stops at the most
// that crosses; what isn't text or is too big has no hunks.
func cleanWorktreeDiff(p proto.PeerWorktreeDiffParams, d proto.PeerWorktreeDiff) (proto.FilesDiffResult, error) {
	if p.Path == "" || d.Path != p.Path {
		return proto.FilesDiffResult{}, errWorktreeAddUp()
	}
	out := proto.FilesDiffResult{Name: path.Base(p.Path), Path: p.Path, Status: fileMark(d.Status), Base: textsafe.Line(d.Base, 200),
		Size: max(d.Size, 0), MTime: d.MTime, Added: max(d.Added, 0), Deleted: max(d.Deleted, 0), Binary: d.Binary, TooBig: d.TooBig, Truncated: d.Truncated}
	if from, err := filetree.CleanPath(d.From); err == nil {
		out.From = from
	}
	if out.Binary || out.TooBig {
		return out, nil
	}
	nextOld, nextNew, text := 1, 1, 0
	for _, h := range d.Hunks {
		if h.Old < nextOld || h.New < nextNew || h.Added < 0 || h.Old > proto.MaxFilesDiff+1 || h.New > proto.MaxFilesDiff+1 ||
			h.Added > proto.MaxFilesDiff+1 || len(h.Removed) > proto.MaxFilesDiff+1 {
			return proto.FilesDiffResult{}, errWorktreeAddUp()
		}
		nextOld, nextNew = h.Old+len(h.Removed), h.New+h.Added
		fh := proto.FilesHunk{Old: h.Old, New: h.New, Added: h.Added}
		for _, l := range h.Removed {
			if text += len(l); text > proto.PeerWorktreeMaxHunks {
				out.Truncated = true
				break
			}
			fh.Removed = append(fh.Removed, strings.ReplaceAll(filetree.CleanRemoved(l), "\n", " "))
		}
		out.Hunks = append(out.Hunks, fh)
		if out.Truncated {
			break
		}
	}
	return out, nil
}
