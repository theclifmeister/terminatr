package server

// Share requests (docs/SPEC.md §16.9, Asking for a share): the console
// asks a paired server's user to share a project, and answers a request
// from one, through the human's methods; internal/link keeps them.

import (
	"slices"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// peerList is the mirror with each peer's share requests (link.ShareState);
// Gen adds their changes.
func (s *Server) peerList() proto.PeerList {
	l := s.mirror.List()
	shares, rev := link.ShareState()
	for i := range l.Peers {
		l.Peers[i].Shares = shares[l.Peers[i].Alias]
	}
	l.Gen += uint64(rev)
	return l
}

// askShare sends a share request, refused at once (old-tm) when the
// server's hello says its tm doesn't take them.
func (s *Server) askShare(p proto.PeerShareAskParams, c callCtx) (any, *proto.Error) {
	r, err := link.AskShare(c.who(), p.Alias, p.For, p.Note, s.peerOlder(peer.HomeNames().Alias(p.Alias)))
	if err != nil {
		return nil, codeErr(err)
	}
	return r, nil
}

// peerOlder reports that the paired server alias's last hello lists
// messages but not share requests: a tm before link.ShareSince.
func (s *Server) peerOlder(alias string) bool {
	if s.peers == nil {
		return false
	}
	st := s.peers.Status()
	if st == nil {
		return false
	}
	for _, l := range st.Peers {
		if l.Alias == alias {
			return slices.Contains(l.Caps, peer.CapMsg) && !slices.Contains(l.Caps, peer.CapShare)
		}
	}
	return false
}

// answerShare accepts a share request with a project, which shares it,
// or declines it.
func (s *Server) answerShare(p proto.PeerShareAnswerParams, c callCtx) (any, *proto.Error) {
	r, err := link.AnswerShare(c.who(), p.ID, p.Accept, p.Project)
	if err != nil {
		return nil, codeErr(err)
	}
	s.kick()
	return r, nil
}
