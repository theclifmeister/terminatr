package server

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Scopes of the session-addressed methods (method.scope) (docs/SPEC.md §11.1): what a
// caller other than the human on the human socket may name.
const (
	scopeNone    = iota // anyone may call it
	scopeOwn            // the caller's own session only
	scopeSees           // its own session, or for a coordinator its project's
	scopeProject        // the caller's own project
)

// mayAddress is the gate of a method that names a session or a project,
// after mayCall. The human on the human socket names any. Everyone else
// names what the pid walk (callerOf) allows: the session it was found in,
// a coordinator also its project's sessions, an agent its own project.
// A caller the walk finds in no session, on the server socket, names
// nothing: the human's own tm uses the human socket, so that is a process
// that left its session's tree. Params are decoded as the method's
// handler decodes them, and refused where they don't decode; params are
// those decoded, for the handler (callCtx.params).
func (s *Server) mayAddress(req proto.Request, m method, human bool, who func() caller.Caller) (params any, perr *proto.Error) {
	scope := m.scope
	if scope == scopeNone {
		return nil, nil
	}
	name, params, perr := m.name(req.Params)
	if perr != nil {
		return nil, perr
	}
	c := who()
	if unscoped(c, human) {
		return params, nil
	}
	switch scope {
	case scopeProject:
		if c.IsAgent() && c.Project != "" && c.Project == name {
			return params, nil
		}
		return nil, proto.Errorf(proto.ErrRefused, "%s: project %s isn't the caller's", req.Method, name)
	case scopeSees:
		if s.sees(c, name) {
			return params, nil
		}
	case scopeOwn:
		if c.Session != "" && c.Session == name {
			return params, nil
		}
	}
	return nil, proto.Errorf(proto.ErrRefused, "%s: session %s isn't the caller's", req.Method, name)
}

// unscoped reports whether caller c, on the human socket when human,
// names any session: the human there, which the pid walk doesn't place in
// an agent's session.
func unscoped(c caller.Caller, human bool) bool { return human && !c.IsAgent() }

// sees reports whether a scoped caller c sees session id (scopeSees): its
// own session, or for a coordinator one of its project's.
func (s *Server) sees(c caller.Caller, id string) bool {
	if c.Session != "" && c.Session == id {
		return true
	}
	return c.Kind == caller.Coordinator && c.Project != "" && s.projectOf(id) == c.Project
}

// listFor is session.list's answer for caller c, on the human socket when
// human. A scoped caller gets the sessions it sees (sees), and none of
// what only a console shows: the alert count, the mods' notices, columns,
// cells of projects, tasks and threads, status segments and toasts; its
// answer says it is Scoped.
func (s *Server) listFor(c caller.Caller, human bool) proto.SessionListResult {
	res := s.sessionList()
	if unscoped(c, human) {
		return res
	}
	res.Mods, res.ModNotices, res.Alerts = nil, nil, 0
	res.Scoped = true
	seen := res.Sessions[:0]
	for _, in := range res.Sessions {
		if s.sees(c, in.ID) {
			seen = append(seen, in)
		}
	}
	res.Sessions = seen
	return res
}

// projectOf is the project of session id, "" for none or no session.
func (s *Server) projectOf(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return ""
	}
	return s.records[id].Project
}
