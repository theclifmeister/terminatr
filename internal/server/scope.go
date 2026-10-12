package server

import (
	"encoding/json"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Scopes of the session-addressed methods (docs/SPEC.md §11.1): what a
// caller other than the human on the human socket may name.
const (
	scopeNone    = iota // anyone may call it
	scopeOwn            // the caller's own session only
	scopeSees           // its own session, or for a coordinator its project's
	scopeProject        // the caller's own project
)

// scopeOf is method m's scope.
func scopeOf(m string) int {
	switch m {
	case proto.MethodHookEvent, proto.MethodSessionAsk:
		// Events and questions come from inside the session they name:
		// tm hook and the agent's mod are the agent's children.
		return scopeOwn
	case proto.MethodSessionRead, proto.MethodSessionWatch, proto.MethodAgentExplain:
		return scopeSees
	case proto.MethodProjectWatch:
		return scopeProject
	}
	return scopeNone
}

// mayAddress is the gate of a method that names a session or a project,
// after mayCall. The human on the human socket names any. Everyone else
// names what the pid walk (callerOf) allows: the session it was found in,
// a coordinator also its project's sessions, an agent its own project.
// A caller the walk finds in no session, on the server socket, names
// nothing: the human's own tm uses the human socket, so that is a process
// that left its session's tree. Params that don't decode pass, for the
// method to refuse.
func (s *Server) mayAddress(req proto.Request, human bool, who func() caller.Caller) *proto.Error {
	scope := scopeOf(req.Method)
	if scope == scopeNone {
		return nil
	}
	var p struct {
		ID      string `json:"id"`
		Session string `json:"session"`
		Project string `json:"project"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
		return nil
	}
	if req.Method == proto.MethodHookEvent {
		p.ID = p.Session
	}
	c := who()
	if human && !c.IsAgent() {
		return nil
	}
	switch scope {
	case scopeProject:
		if c.IsAgent() && c.Project != "" && c.Project == p.Project {
			return nil
		}
		return proto.Errorf(proto.ErrRefused, "%s: project %s isn't the caller's", req.Method, p.Project)
	case scopeSees:
		if c.Kind == caller.Coordinator && c.Project != "" && s.projectOf(p.ID) == c.Project {
			return nil
		}
	}
	if c.Session != "" && c.Session == p.ID {
		return nil
	}
	return proto.Errorf(proto.ErrRefused, "%s: session %s isn't the caller's", req.Method, p.ID)
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
