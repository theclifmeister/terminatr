package peer

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A shared project's folders, for a paired server whose files switch here
// is on for the project: list, read, stamp, diff and repo names, nothing
// that writes. The server holds the rules on sessions, folders and paths
// (internal/server/peerworktree.go); here, the rate and the switch, and the
// words of every refusal.

// CapWorktree is the link capability of peer.worktree.*.
const CapWorktree = "worktree"

// WorktreeExporter is an Exporter that also serves a shared project's
// folders. The link checked the rate and the files switch already. Any
// error crosses as its code's fixed words (proto.PeerWorktreeRefusal).
type WorktreeExporter interface {
	WorktreeRepos(ctx context.Context, c caller.Caller, p proto.PeerWorktreeParams) (proto.PeerWorktreeRepos, error)
	WorktreeList(ctx context.Context, c caller.Caller, p proto.PeerWorktreeListParams) (proto.PeerWorktreeList, error)
	WorktreeRead(ctx context.Context, c caller.Caller, p proto.PeerWorktreeReadParams) (proto.PeerWorktreeRead, error)
	WorktreeStamp(ctx context.Context, c caller.Caller, p proto.PeerWorktreeStampParams) (proto.PeerWorktreeStamp, error)
	WorktreeDiff(ctx context.Context, c caller.Caller, p proto.PeerWorktreeDiffParams) (proto.PeerWorktreeDiff, error)
}

// worktreeRate bounds one peer's requests: git on this machine is what they
// cost, whatever they ask. Its zero value is ready.
type worktreeRate struct {
	mu      sync.Mutex
	tokens  float64
	last    time.Time
	running int
}

// take is one request's place, and how to give it back; false over the rate.
func (r *worktreeRate) take(now time.Time) (func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last.IsZero() {
		r.tokens = proto.PeerWorktreeRate
	} else {
		r.tokens = min(r.tokens+now.Sub(r.last).Seconds()*proto.PeerWorktreeRate, proto.PeerWorktreeRate)
	}
	r.last = now
	if r.tokens < 1 || r.running >= proto.PeerWorktreeRunning {
		return nil, false
	}
	r.tokens--
	r.running++
	return func() {
		r.mu.Lock()
		r.running--
		r.mu.Unlock()
	}, true
}

// worktreeMethods are the link's methods of a shared project's folders,
// when the Exporter serves them.
func (d *dispatcher) worktreeMethods(m map[string]handler) {
	ex, ok := d.ex.(WorktreeExporter)
	if !ok || d.worktrees == nil {
		return
	}
	m[proto.MethodPeerWorktreeRepos] = worktreeHandler(d, func(p proto.PeerWorktreeParams) string { return p.Project }, ex.WorktreeRepos)
	m[proto.MethodPeerWorktreeList] = worktreeHandler(d, func(p proto.PeerWorktreeListParams) string { return p.Project }, ex.WorktreeList)
	m[proto.MethodPeerWorktreeRead] = worktreeHandler(d, func(p proto.PeerWorktreeReadParams) string { return p.Project }, ex.WorktreeRead)
	m[proto.MethodPeerWorktreeStamp] = worktreeHandler(d, func(p proto.PeerWorktreeStampParams) string { return p.Project }, ex.WorktreeStamp)
	m[proto.MethodPeerWorktreeDiff] = worktreeHandler(d, func(p proto.PeerWorktreeDiffParams) string { return p.Project }, ex.WorktreeDiff)
}

// worktreeHandler takes a request through the rate and the switch to read,
// the switch read at each request, and puts any refusal in its fixed words.
func worktreeHandler[P, R any](d *dispatcher, project func(P) string, read func(context.Context, caller.Caller, P) (R, error)) handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		done, ok := d.worktreeLimit.take(time.Now())
		if !ok {
			return nil, proto.PeerWorktreeRefusal(proto.ErrRateLimited)
		}
		defer done()
		var p P
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, proto.PeerWorktreeRefusal(proto.ErrBadParams)
		}
		// Off, and no such project, answer alike.
		if slug := project(p); slug == "" || !d.worktrees(slug) {
			return nil, proto.PeerWorktreeRefusal(proto.ErrFilesOff)
		}
		res, err := read(ctx, d.caller, p)
		if err != nil {
			return nil, worktreeRefusal(err)
		}
		if d.worktreeRead != nil {
			d.worktreeRead()
		}
		return res, nil
	}
}

// worktreeRefusal is err in its code's fixed words.
func worktreeRefusal(err error) *proto.Error {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return proto.PeerWorktreeRefusal(pe.Code)
	}
	return proto.PeerWorktreeRefusal(proto.ErrInternal)
}

// filesOn is this server's files switch for peer id now, for project, read
// from the file each time: a link's copy can lag a change, and off must hold
// at the next request. A file that can't be read is off.
func (m *Manager) filesOn(id ID, project string) bool {
	peers, err := m.Allowed()
	if err != nil {
		return false
	}
	i := slices.IndexFunc(peers, func(q Peer) bool { return q.ID() == id })
	return i >= 0 && peers[i].FilesOn(project)
}

// noteFilesRead remembers when peer id last read a folder here, for the
// Network popup: the link's own record, not in a file, and no path.
func (m *Manager) noteFilesRead(id ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.filesRead == nil {
		m.filesRead = map[ID]time.Time{}
	}
	m.filesRead[id] = time.Now()
}
