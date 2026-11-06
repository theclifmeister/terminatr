package server

// A project's daily numbers (docs/SPEC.md §5.1, History): this server's
// own (project.history), a shared project's to a paired server under the
// pages switch, and a paired server's (peer.history). Numbers only: no
// title, thread or path crosses.

import (
	"context"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/history"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (s *Server) projectHistory(p proto.HistoryParams) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	h, err := history.Read(pr)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	return h, nil
}

// History answers a paired server: the newest PeerHistoryDays days.
func (e peerExport) History(c caller.Caller, slug string) (proto.History, error) {
	p, err := sharedProject(c, slug, proto.MethodPeerPagesHistory)
	if err != nil {
		return proto.History{}, err
	}
	h, err := history.Read(p)
	if err != nil {
		return proto.History{}, err
	}
	if n := len(h.Days) - proto.PeerHistoryDays; n > 0 {
		h.Days, h.Truncated = h.Days[n:], true
	}
	return h, nil
}

func (s *Server) readPeerHistory(p proto.PeerPagesReadParams, _ callCtx) (any, *proto.Error) {
	if perr := checkPeerRead(proto.MethodPeerHistory, p.Alias, p.Project); perr != nil {
		return nil, perr
	}
	h, err := s.peers.History(context.Background(), p.Alias, p.Project)
	if err != nil {
		return nil, pagesError(p.Alias, err)
	}
	return h, nil
}
