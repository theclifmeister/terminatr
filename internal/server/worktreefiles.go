package server

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/filetree"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// files.list, files.read and files.stamp (proto.CapWorktreeFiles): a
// session's folder, read only. A session names its folder here, the
// caller never does: a thread's worktree, a coordinator's project's repo
// at its main checkout. The caller's one say is which repo of that
// project's list, so never a folder off it. Package filetree holds the
// rules on paths.

// filesScope is session id's folder; choice picks a coordinator's repo,
// "" the first.
func (s *Server) filesScope(ctx context.Context, id, choice string) (*filetree.Scope, *proto.Error) {
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
		if choice != "" {
			return nil, proto.Errorf(proto.ErrBadParams, "a thread has its one worktree: no repo to choose")
		}
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
		if !s.repoVouched(p, r) {
			return nil, proto.Errorf(proto.ErrNoFiles, "%s's repo isn't on %s's list", r.ID, p.Slug)
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
		root = p.Repos[0]
		if choice != "" {
			listed, ok := p.ListedRepo(choice)
			if !ok {
				return nil, proto.Errorf(proto.ErrNoFiles, "%s isn't on %s's list", choice, p.Slug)
			}
			root = listed
		}
		repo = root
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

// filesRepos lists what session p.Session may choose: a coordinator's
// project's repos. It answers even when a repo's folder is gone, so the
// choice outlives a repo that can't open.
func (s *Server) filesRepos(p proto.FilesReposParams) (any, *proto.Error) {
	res := proto.FilesReposResult{Repos: []proto.FileRepo{}}
	if _, _, ok := remoteID(p.Session); ok {
		return res, nil
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		return nil, perr
	}
	info := sess.Info()
	if info.Role != proto.RoleCoordinator {
		return res, nil
	}
	pr, err := project.Open(info.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrNoFiles, "%v", err)
	}
	res.Repos = fileRepos(pr.Repos)
	return res, nil
}

// fileRepos names the repos of a list, each by its folder's name, with
// its parent folder too when two share that.
func fileRepos(list []string) []proto.FileRepo {
	named := map[string]int{}
	for _, r := range list {
		named[tasks.RepoName(r)]++
	}
	out := make([]proto.FileRepo, 0, len(list))
	for _, r := range list {
		name := tasks.RepoName(r)
		if named[name] > 1 {
			name = filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(r)), name))
		}
		out = append(out, proto.FileRepo{Name: name, Path: r})
	}
	return out
}

// repoVouched says something other than thread record r vouches for the
// repo it names: the project's list, or, since a session adopted from any
// repo on disk keeps that repo off the list, the adopted session itself,
// live on this server with its folder in the recorded worktree. A record
// with no repo has none to vouch for.
func (s *Server) repoVouched(p *project.Project, r *thread.Record) bool {
	if r.Repo == "" || p.ListsRepo(r.Repo) {
		return true
	}
	if !r.Adopted || r.Session == "" {
		return false
	}
	sess, perr := s.session(r.Session)
	if perr != nil {
		return false
	}
	info := sess.Info()
	if info.Project != p.Slug || info.Thread != r.ID {
		return false
	}
	_, ok := fsx.Rel(realPath(r.Worktree), realPath(info.Cwd))
	return ok
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
	sc, perr := s.filesScope(ctx, p.Session, p.Repo)
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
	sc, perr := s.filesScope(ctx, p.Session, p.Repo)
	if perr != nil {
		return nil, perr
	}
	read := sc.Read
	if p.Binary {
		read = sc.ReadBytes
	}
	res, err := read(ctx, p.Path, p.Offset, p.Ignored)
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
	sc, perr := s.filesScope(ctx, p.Session, p.Repo)
	if perr != nil {
		return nil, perr
	}
	return proto.FilesStampResult{Stamp: s.filesSeen.Stamp(ctx, sc, slices.Clone(p.Folders))}, nil
}
