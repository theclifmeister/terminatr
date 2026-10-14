package peer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// What a paired server may read of this one (docs/SPEC.md §16.9). A
// link's requests reach a dispatcher with its own short list of peer.*
// methods, never the control API; each is read-only and goes through an
// Exporter, the server's narrow view of the projects the user shares.

// CapWatch is the link capability of peer.projects and peer.watch.
const CapWatch = "watch"

// Exporter is what the server lets a paired server read. Every method
// answers only for projects the user shares (an active project with
// share on): another slug is proto.ErrNotShared, whether it exists or
// not. c is the paired server, caller kind peer.
type Exporter interface {
	// Projects are the shared projects.
	Projects(c caller.Caller) ([]proto.PeerProject, error)
	// Watch is a shared project's state now.
	Watch(c caller.Caller, project string) (proto.PeerWatch, error)
	// Changes signals when the server's state may have changed; stop
	// ends that. Watch is read again on each signal and every poll.
	Changes() (ch <-chan struct{}, stop func())
}

// handler answers one request of a paired server: the result, or an
// error the peer reads.
type handler func(ctx context.Context, params json.RawMessage) (any, error)

// watchPoll is how often peer.watch reads the state again without a
// signal: the board, inbox and threads change on disk.
var watchPoll = 2 * time.Second

// dispatcher answers a paired server's requests from the Exporter.
type dispatcher struct {
	ex     Exporter
	caller caller.Caller
}

// methods are the peer.* methods a paired server may call: nothing
// else is reachable from a link. All of them read.
func (d *dispatcher) methods() map[string]handler {
	if d == nil || d.ex == nil {
		return nil
	}
	return map[string]handler{
		proto.MethodPeerProjects: d.projects,
		proto.MethodPeerWatch:    d.watch,
	}
}

func (d *dispatcher) projects(ctx context.Context, _ json.RawMessage) (any, error) {
	ps, err := d.ex.Projects(d.caller)
	if err != nil {
		return nil, err
	}
	if ps == nil {
		ps = []proto.PeerProject{}
	}
	return proto.PeerProjects{Projects: ps}, nil
}

func (d *dispatcher) watch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerWatchParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "peer.watch: %v", err)
		}
	}
	if p.Project == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.watch needs a project")
	}
	wait := time.Duration(p.Wait) * time.Second
	if p.Wait <= 0 || p.Wait > proto.PeerWatchMaxWait {
		wait = proto.PeerWatchMaxWait * time.Second
	}
	ch, stop := d.ex.Changes()
	defer stop()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	t := time.NewTicker(watchPoll)
	defer t.Stop()
	for {
		w, err := d.ex.Watch(d.caller, p.Project)
		if err != nil {
			return nil, err
		}
		w.Rev = rev(w)
		if w.Rev != p.Rev {
			return w, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return w, nil
		case <-ch:
		case <-t.C:
		}
	}
}

// rev names a state: the hash of its JSON without the rev.
func rev(w proto.PeerWatch) string {
	w.Rev = ""
	b, _ := json.Marshal(w)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12])
}

// asProtoError is err as the peer reads it: a *proto.Error as it is,
// anything else internal, without its words (they may name a path).
func asProtoError(err error) *proto.Error {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe
	}
	return proto.Errorf(proto.ErrInternal, "the peer couldn't answer")
}
