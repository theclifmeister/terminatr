package server

import (
	"context"
	"errors"
	"slices"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The files methods on a paired server's session ("<alias>/<session>"): the
// link asks that server (peer.Manager.Worktree*), which holds every rule and
// reads its switch at each request. Here, only which project the session is
// in, as the mirror last read it, and the refusals in this server's words.
// There is nothing to ask for ignored files or a binary's bytes with: those
// params don't cross. Nothing read here goes to disk, to the search index or
// to an agent: these methods are the human's.

// remoteFolder is what names a paired server's session over its link: the
// alias, and the session's project and id there.
func (s *Server) remoteFolder(id string) (string, proto.PeerWorktreeParams, *proto.Error) {
	var none proto.PeerWorktreeParams
	alias, sid, _ := remoteID(s.remoteCanon(id))
	list := s.mirror.List()
	i := slices.IndexFunc(list.Peers, func(p proto.PeerMirror) bool { return p.Alias == alias })
	if i < 0 {
		return "", none, proto.Errorf(proto.ErrUnknownSession, "no paired server %s", alias)
	}
	for _, pr := range list.Peers[i].Projects {
		if slices.ContainsFunc(pr.Sessions, func(ps proto.PeerSession) bool { return ps.ID == sid }) {
			return alias, proto.PeerWorktreeParams{Project: pr.Project, Session: sid}, nil
		}
	}
	if list.Peers[i].State != proto.LinkUp {
		return "", none, remoteFilesOffline(alias)
	}
	return "", none, proto.Errorf(proto.ErrUnknownSession, "%s isn't running a session %s", alias, sid)
}

func remoteFilesOffline(alias string) *proto.Error {
	return proto.Errorf(proto.ErrPeerOffline, "%s is offline", alias)
}

// remoteFilesError is a failed read of a paired server's folder in this
// server's words, by its code: those of the other side, which could say
// anything, never reach a console, and a code that isn't one of these methods'
// is ErrInternal.
func remoteFilesError(alias, slug string, err error) *proto.Error {
	var pe *proto.Error
	switch {
	case peer.WorktreeBusy(err):
		return proto.Errorf(proto.ErrRateLimited, "%s is slow to answer", alias)
	case !errors.As(err, &pe):
		return remoteFilesOffline(alias)
	}
	switch pe.Code {
	case proto.ErrNoPeerFiles, proto.ErrUnknownMethod:
		return proto.Errorf(proto.ErrNoPeerFiles, "%s runs an older tm, which can't share files", alias)
	case proto.ErrFilesOff:
		return proto.Errorf(proto.ErrFilesOff, "%s doesn't share %s's files", alias, slug)
	case proto.ErrNotShared:
		return proto.Errorf(proto.ErrNotShared, "%s doesn't share %s any more", alias, slug)
	case proto.ErrWithheld:
		return proto.Errorf(proto.ErrWithheld, "its name says it may hold secrets, so it stays on %s", alias)
	case proto.ErrIgnored:
		return proto.Errorf(proto.ErrIgnored, "git-ignored files stay on %s", alias)
	case proto.ErrRefused:
		return proto.Errorf(proto.ErrRefused, "%s's answer doesn't add up", alias)
	}
	return proto.PeerWorktreeRefusal(pe.Code)
}

func (s *Server) remoteFilesRepos(p proto.FilesReposParams) (any, *proto.Error) {
	alias, at, perr := s.remoteFolder(p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := s.peers.WorktreeRepos(context.Background(), alias, at)
	if err != nil {
		return nil, remoteFilesError(alias, at.Project, err)
	}
	return res, nil
}

func (s *Server) remoteFilesList(p proto.FilesListParams) (any, *proto.Error) {
	alias, at, perr := s.remoteFolder(p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := s.peers.WorktreeList(context.Background(), alias, proto.PeerWorktreeListParams{Project: at.Project, Session: at.Session,
		Repo: p.Repo, Path: p.Path, Query: p.Query, Changed: p.Changed})
	if err != nil {
		return nil, remoteFilesError(alias, at.Project, err)
	}
	return res, nil
}

func (s *Server) remoteFilesRead(p proto.FilesReadParams) (any, *proto.Error) {
	alias, at, perr := s.remoteFolder(p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := s.peers.WorktreeRead(context.Background(), alias, proto.PeerWorktreeReadParams{Project: at.Project, Session: at.Session,
		Repo: p.Repo, Path: p.Path, Offset: p.Offset})
	if err != nil {
		return nil, remoteFilesError(alias, at.Project, err)
	}
	return res, nil
}

func (s *Server) remoteFilesDiff(p proto.FilesDiffParams) (any, *proto.Error) {
	alias, at, perr := s.remoteFolder(p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := s.peers.WorktreeDiff(context.Background(), alias, proto.PeerWorktreeDiffParams{Project: at.Project, Session: at.Session,
		Repo: p.Repo, Path: p.Path})
	if err != nil {
		return nil, remoteFilesError(alias, at.Project, err)
	}
	return res, nil
}

func (s *Server) remoteFilesStamp(p proto.FilesStampParams) (any, *proto.Error) {
	alias, at, perr := s.remoteFolder(p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := s.peers.WorktreeStamp(context.Background(), alias, proto.PeerWorktreeStampParams{Project: at.Project, Session: at.Session,
		Repo: p.Repo, Folders: p.Folders})
	if err != nil {
		return nil, remoteFilesError(alias, at.Project, err)
	}
	return res, nil
}
