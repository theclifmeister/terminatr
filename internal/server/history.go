package server

// A project's daily numbers (docs/SPEC.md §5.1, History): this server's
// own (project.history), with each repo's releases; a shared project's to
// a paired server under the pages switch, and a paired server's
// (peer.history), numbers only: no title, thread, repo, tag or path
// crosses. And which of its tags count (release.tags, release.tags.set).

import (
	"context"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
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
	h, err := history.Records(p)
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

func (s *Server) releaseTags(p proto.ReleaseTagsParams) (any, *proto.Error) {
	if _, err := project.Open(p.Project); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	return releaseTagsOf(p.Project)
}

// releaseTagsSet sets a project's release_tags: its own, or back to all
// projects', journaled as a change in the settings is.
func (s *Server) releaseTagsSet(p proto.ReleaseTagsSetParams, c callCtx) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	var value []string
	switch p.Mode {
	case proto.ReleaseTagsFollow:
		err = config.UnsetProject(p.Project, "release_tags")
	case proto.ReleaseTagsRule:
		value = []string{}
	case proto.ReleaseTagsNone:
		value = []string{config.ReleaseTagsOff}
	case proto.ReleaseTagsPatterns:
		value = p.Patterns
		if len(value) == 0 || config.ReleaseTagsAreOff(value) {
			return nil, proto.Errorf(proto.ErrBadParams, "patterns needs a pattern")
		}
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "no mode %q", p.Mode)
	}
	if value != nil {
		err = config.SetProject(p.Project, "release_tags", value)
	}
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	detail := "(follows all projects)"
	if value != nil {
		detail = strings.Join(value, " ")
		if len(value) == 0 {
			detail = "(the default rule)"
		}
	}
	pr.Journal(c.who(), "settings.release_tags", p.Project, detail)
	return releaseTagsOf(p.Project)
}

func releaseTagsOf(slug string) (any, *proto.Error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	safety, err := cfg.Safety(slug)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	all, err := cfg.AllProjects()
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	out := proto.ReleaseTags{Project: slug, Mode: proto.ReleaseTagsRule, Own: slices.Contains(cfg.Own(slug), "release_tags"),
		AllProjects: config.ReleaseTagsWords(all.ReleaseTags)}
	switch {
	case config.ReleaseTagsAreOff(safety.ReleaseTags):
		out.Mode = proto.ReleaseTagsNone
	case len(safety.ReleaseTags) > 0:
		out.Mode, out.Patterns = proto.ReleaseTagsPatterns, safety.ReleaseTags
	}
	return out, nil
}
