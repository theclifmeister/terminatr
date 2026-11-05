package server

import (
	"context"
	"errors"
	"slices"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/filetree"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// files.list, files.read and files.stamp (proto.CapWorktreeFiles): a
// session's folder, read only. A session names its folder here, the
// caller never does: a thread's worktree, a coordinator's project's first
// repo at its main checkout. Package filetree holds the rules on paths.

// filesScope is session id's folder.
func (s *Server) filesScope(ctx context.Context, id string) (*filetree.Scope, *proto.Error) {
	if alias, _, ok := remoteID(id); ok {
		// No method of the peer link reads a worktree: a paired server's
		// files stay on it.
		return nil, proto.Errorf(proto.ErrNoFiles, "%s's files stay on %s", id, alias)
	}
	sess, perr := s.session(id)
	if perr != nil {
		return nil, perr
	}
	info := sess.Info()
	root, repo, base := "", "", ""
	switch info.Role {
	case proto.RoleThread:
		p, err := project.Open(info.Project)
		if err != nil {
			return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
		}
		r, err := thread.Load(p, info.Thread)
		if err != nil {
			return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
		}
		if r.Repo == "" {
			// A plain folder has no repo to vouch for it: it must be
			// where tm makes one.
			if err := thread.OwnFolder(p.Slug, r, nil); err != nil {
				return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
			}
		}
		root, repo, base = r.Worktree, r.Repo, r.Base
	case proto.RoleCoordinator:
		p, err := project.Open(info.Project)
		if err != nil {
			return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
		}
		if len(p.Repos) == 0 {
			return nil, proto.Errorf(proto.ErrNoFiles, "%s has no repo", p.Slug)
		}
		root, repo = p.Repos[0], p.Repos[0]
	case proto.RoleDirector:
		return nil, proto.Errorf(proto.ErrNoFiles, "a director has no repo")
	default:
		return nil, proto.Errorf(proto.ErrNoFiles, "a %s session has no folder tm knows", info.Role)
	}
	sc, err := filetree.Open(ctx, root, repo, base)
	if err != nil {
		return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
	}
	return sc, nil
}

// filesError is a read's refusal, by its code.
func filesError(err error) *proto.Error {
	code := proto.ErrRefused
	for _, c := range []struct {
		err  error
		code string
	}{{filetree.ErrPath, proto.ErrInvalidPath}, {filetree.ErrLinkOut, proto.ErrLinkOut}, {filetree.ErrIgnored, proto.ErrIgnored},
		{filetree.ErrGone, proto.ErrFileGone}, {filetree.ErrNotFile, proto.ErrNotFile}, {filetree.ErrNoRoot, proto.ErrNoFiles}} {
		if errors.Is(err, c.err) {
			code = c.code
			break
		}
	}
	return proto.Errorf(code, "%v", err)
}

func (s *Server) filesList(p proto.FilesListParams) (any, *proto.Error) {
	ctx, cancel := context.WithTimeout(context.Background(), changes.Timeout)
	defer cancel()
	sc, perr := s.filesScope(ctx, p.Session)
	if perr != nil {
		return nil, perr
	}
	cs, err := s.filesSeen.Changes(ctx, sc)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	res, err := sc.List(ctx, p.Path, filetree.Options{Ignored: p.Ignored, Query: p.Query, Changed: p.Changed}, cs)
	if err != nil {
		return nil, filesError(err)
	}
	return res, nil
}

func (s *Server) filesRead(p proto.FilesReadParams) (any, *proto.Error) {
	ctx, cancel := context.WithTimeout(context.Background(), changes.Timeout)
	defer cancel()
	sc, perr := s.filesScope(ctx, p.Session)
	if perr != nil {
		return nil, perr
	}
	res, err := sc.Read(ctx, p.Path, p.Offset, p.Ignored)
	if err != nil {
		return nil, filesError(err)
	}
	return res, nil
}

func (s *Server) filesStamp(p proto.FilesStampParams) (any, *proto.Error) {
	if len(p.Folders) > proto.MaxFilesStamped {
		return nil, proto.Errorf(proto.ErrBadParams, "files.stamp: over %d folders", proto.MaxFilesStamped)
	}
	ctx, cancel := context.WithTimeout(context.Background(), changes.Timeout)
	defer cancel()
	sc, perr := s.filesScope(ctx, p.Session)
	if perr != nil {
		return nil, perr
	}
	return proto.FilesStampResult{Stamp: sc.Stamp(ctx, slices.Clone(p.Folders))}, nil
}
