package server

// What paired servers may read of this one (docs/SPEC.md §16.9): the
// projects the user shares, each as its ProjectWatch and its sessions,
// trimmed. The link's dispatcher (internal/peer) reaches only these
// methods, never the control API, and each reads.

import (
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// peerExport is the server as peer.Exporter: its sessions, a
// project's state with them, and its watchers' wakes.
type peerExport struct {
	sessions func() []proto.SessionInfo
	state    func(slug string, sessions []proto.SessionInfo) (proto.ProjectWatch, error)
	changes  func() (<-chan struct{}, func())
}

// peerExport is the server's peer.Exporter.
func (s *Server) peerExport() peerExport {
	return peerExport{
		sessions: func() []proto.SessionInfo { return s.list().Sessions },
		state:    s.projectWatchWith,
		changes: func() (<-chan struct{}, func()) {
			ch := s.watch.add()
			return ch, func() { s.watch.remove(ch) }
		},
	}
}

// errNotShared answers a slug no shared project has, the same whether
// the project exists or not.
func errNotShared(slug string) error {
	return proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
}

// sharedProjects are the slugs of the projects offered to paired
// servers, sorted: share on, active, not archived (config.Safety).
func sharedProjects() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for _, slug := range cfg.ProjectSlugs() {
		if !project.ValidSlug(slug) {
			continue
		}
		if s, err := cfg.Safety(slug); err == nil && s.Share && s.Active && !s.Archived {
			out = append(out, slug)
		}
	}
	return out
}

func (e peerExport) Projects(c caller.Caller) ([]proto.PeerProject, error) {
	if c.Kind != caller.Peer {
		return nil, proto.Errorf(proto.ErrRefused, "peer.projects is a paired server's")
	}
	cfg, _ := config.Load()
	sessions := e.sessions()
	var out []proto.PeerProject
	for _, slug := range sharedProjects() {
		p, err := project.Open(slug)
		if err != nil {
			continue
		}
		w, err := e.state(slug, sessions)
		if err != nil {
			continue
		}
		pp := proto.PeerProject{Project: slug, Goal: p.Meta.Goal, NeedsYou: len(w.NeedsYou), Threads: len(w.Threads), Ready: len(w.Ready)}
		if s, err := cfg.Safety(slug); err == nil {
			pp.Paused = s.Paused
		}
		out = append(out, pp)
	}
	return out, nil
}

func (e peerExport) Watch(c caller.Caller, slug string) (proto.PeerWatch, error) {
	if c.Kind != caller.Peer {
		return proto.PeerWatch{}, proto.Errorf(proto.ErrRefused, "peer.watch is a paired server's")
	}
	shared := sharedProjects()
	if !slices.Contains(shared, slug) {
		return proto.PeerWatch{}, errNotShared(slug)
	}
	sessions := e.sessions()
	w, err := e.state(slug, sessions)
	if err != nil {
		return proto.PeerWatch{}, errNotShared(slug)
	}
	// A link names another project: only a shared one may show.
	w.Links = slices.DeleteFunc(w.Links, func(l proto.WatchLink) bool { return !slices.Contains(shared, l.Peer) })
	if len(w.Links) == 0 {
		w.Links = nil
	}
	pw := proto.PeerWatch{Watch: w, Sessions: []proto.PeerSession{}}
	for _, info := range sessions {
		if info.Project == slug {
			pw.Sessions = append(pw.Sessions, proto.PeerSessionOf(info))
		}
	}
	return pw, nil
}

func (e peerExport) Changes() (<-chan struct{}, func()) { return e.changes() }
