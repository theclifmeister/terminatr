package peer

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Paired needs (docs/SPEC.md §16.16): a shared project's needs in full,
// on peer.watch, for a paired server whose needs switch here is view or
// answer. Off, the default, it reads only the masked list every paired
// server reads.

// CapNeeds is the link capability of the needs switch: the server shows
// its needs in full on peer.watch to a paired server whose switch lets
// it, and says that switch on peer.projects and peer.panes.
const CapNeeds = "needs"

// NeedsExporter is an Exporter that shows a shared project's needs in
// full (CapNeeds).
type NeedsExporter interface {
	// Needs are the shared project's needs in full: never an escape's
	// menu, dialog or options (PeerNeed.Escape).
	Needs(c caller.Caller, project string) ([]proto.PeerNeed, error)
}

// needsLevel is this server's needs switch for the peer now.
func (l *link) needsLevel() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peer.NeedsLevel()
}

// setNeeds changes the needs switch for the peer: its next peer.watch
// answer follows (the watches read again within watchPoll), and a peer
// that takes peer.panes hears of it now.
func (l *link) setNeeds(level string) {
	l.mu.Lock()
	l.peer.Needs = level
	s := l.sess
	l.mu.Unlock()
	if s != nil {
		l.tellSwitches(s)
	}
}

// needsOf are project's needs for the peer at its switch now: none
// below view.
func (d *dispatcher) needsOf(project string) ([]proto.PeerNeed, error) {
	nx, ok := d.ex.(NeedsExporter)
	if !ok || d.needs == nil || !proto.NeedsShown(d.needs()) {
		return nil, nil
	}
	ns, err := nx.Needs(d.caller, project)
	if err != nil {
		return nil, err
	}
	for i := range ns {
		if ns[i].Escape {
			// Never what an escape asks: only the user there answers it.
			ns[i].Question, ns[i].Approval = nil, nil
		}
	}
	return ns, nil
}
