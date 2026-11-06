package server

// peer.switch (docs/SPEC.md §16.8, Who raises a switch): the human's way to
// raise what a paired server may do. peers.json alone raises nothing.

import (
	"errors"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (s *Server) peerSwitch(p proto.PeerSwitchParams, c callCtx) (any, *proto.Error) {
	if s.peers == nil {
		return nil, proto.Errorf(proto.ErrRefused, "this server keeps no paired servers")
	}
	projectSwitch := p.Switch == proto.PeerSwitchPages || p.Switch == proto.PeerSwitchFiles
	if projectSwitch && !project.ValidSlug(p.Project) {
		return nil, proto.Errorf(proto.ErrBadParams, "%q isn't a project", p.Project)
	}
	if p.Switch == proto.PeerSwitchFiles && p.Value == "on" && !s.sharedNow(p.Project) {
		return nil, proto.Errorf("not-shared", "%s isn't shared: turn on Shared with paired servers in its Settings tab first", p.Project)
	}
	res, err := s.peers.SetSwitch(p)
	var unknown *peer.UnknownPeerError
	var notAllowed *peer.NotAllowedError
	switch {
	case errors.As(err, &unknown):
		return nil, proto.Errorf("unknown-peer", "no peer %q (tm peer list)", p.Alias)
	case errors.As(err, &notAllowed):
		return nil, proto.Errorf("not-allowed", "%s", notAllowed.Error())
	case err != nil:
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	who := c.who()
	s.log.Printf("peer %s: %s %s %s, by %s", res.Alias, p.Switch, p.Project, p.Value, who.Kind)
	s.journalPeerSwitch(who, p, res)
	return res, nil
}

// sharedNow says whether slug is shared with the paired servers now.
func (s *Server) sharedNow(slug string) bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	sf, err := cfg.Safety(slug)
	return err == nil && sf.Share && sf.Active && !sf.Archived
}

// journalPeerSwitch keeps the change in the journal of what it opens: the
// project it names, else every shared one, since the switch reaches those.
func (s *Server) journalPeerSwitch(who caller.Caller, p proto.PeerSwitchParams, res proto.PeerSwitchResult) {
	action := "peer." + p.Switch
	if p.Project != "" {
		if pr, err := project.Open(p.Project); err == nil {
			pr.Journal(who, action, res.Alias, p.Value)
		}
		return
	}
	list, err := project.List()
	if err != nil {
		return
	}
	for _, sum := range list {
		if !s.sharedNow(sum.Slug) {
			continue
		}
		if pr, err := project.Open(sum.Slug); err == nil {
			pr.Journal(who, action, res.Alias, p.Value)
		}
	}
}
