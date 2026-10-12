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

// scopeOf is method m's scope and the session or project its params
// name, decoded as its handler decodes them.
func scopeOf(m string, raw json.RawMessage) (int, string, *proto.Error) {
	switch m {
	case proto.MethodHookEvent:
		// Events and questions come from inside the session they name:
		// tm hook and the agent's mod are the agent's children.
		var p proto.HookEventParams
		perr := decodeParams(raw, &p)
		return scopeOwn, p.Session, perr
	case proto.MethodSessionAsk:
		var p proto.SessionAskParams
		perr := decodeParams(raw, &p)
		return scopeOwn, p.ID, perr
	case proto.MethodSessionRead:
		var p proto.SessionReadParams
		perr := decodeParams(raw, &p)
		return scopeSees, p.ID, perr
	case proto.MethodSessionWatch, proto.MethodAgentExplain:
		var p proto.SessionIDParams
		perr := decodeParams(raw, &p)
		return scopeSees, p.ID, perr
	case proto.MethodProjectWatch:
		var p proto.ProjectWatchParams
		perr := decodeParams(raw, &p)
		return scopeProject, p.Project, perr
	}
	return scopeNone, "", nil
}

// mayAddress is the gate of a method that names a session or a project,
// after mayCall. The human on the human socket names any. Everyone else
// names what the pid walk (callerOf) allows: the session it was found in,
// a coordinator also its project's sessions, an agent its own project.
// A caller the walk finds in no session, on the server socket, names
// nothing: the human's own tm uses the human socket, so that is a process
// that left its session's tree. Params are decoded as the method's
// handler decodes them, and refused where they don't decode.
func (s *Server) mayAddress(req proto.Request, human bool, who func() caller.Caller) *proto.Error {
	scope, name, perr := scopeOf(req.Method, req.Params)
	if scope == scopeNone || perr != nil {
		return perr
	}
	c := who()
	if human && !c.IsAgent() {
		return nil
	}
	switch scope {
	case scopeProject:
		if c.IsAgent() && c.Project != "" && c.Project == name {
			return nil
		}
		return proto.Errorf(proto.ErrRefused, "%s: project %s isn't the caller's", req.Method, name)
	case scopeSees:
		if c.Kind == caller.Coordinator && c.Project != "" && s.projectOf(name) == c.Project {
			return nil
		}
	}
	if c.Session != "" && c.Session == name {
		return nil
	}
	return proto.Errorf(proto.ErrRefused, "%s: session %s isn't the caller's", req.Method, name)
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
