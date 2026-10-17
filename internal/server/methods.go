package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// method is one control method, declared in one place: who may call it
// (mayCall), what it may name (mayAddress) and what runs it (dispatch,
// or a stream that takes the connection over).
type method struct {
	gate gate
	// scope is what a caller other than the human on the human socket
	// may name, and name the session or project the params name, decoded
	// as the handler decodes them (scoped); name also returns the params
	// decoded, which the handler then takes as they are (callCtx.params).
	scope int
	name  func(raw json.RawMessage) (string, any, *proto.Error)
	// call answers the request; stream, when set instead, serves the
	// connection from then on.
	call   handler
	stream func(s *Server, c net.Conn, br *bufio.Reader, req proto.Request, peerPID int)
}

// gate is who may call a method, before it runs (mayCall).
type gate int

const (
	// The zero value, an entry without a gate: anyone.
	_ gate = iota
	// gateHuman: the human only, on the human socket (ipc.HumanName,
	// docs/SPEC.md §11.1), and still not a caller the pid walk places in
	// an agent's session. An agent's own uses of these (tm thread start,
	// prompt, …) run inside the server through cli.run, whose calls come
	// in on the human socket after the command's own caller check.
	gateHuman
	// gateNotAgent: either socket, but never a caller in an agent's
	// session (view.digest, a test's poke at the console of a pid: an e2e
	// run from inside a thread has no human socket).
	gateNotAgent
	// gateAgentOrHuman: an agent the pid walk finds, or the human on the
	// human socket (cli.run: one the walk calls the human, a process that
	// left its session's tree, would run project commands as the human,
	// whose own CLI uses the human socket).
	gateAgentOrHuman
)

// callCtx is what a handler knows of its call besides the params.
type callCtx struct {
	method string
	// params are the request's params as mayAddress decoded them, nil
	// for a method without a scope.
	params  any
	peerPID int
	human   bool // on the human socket
	who     func() caller.Caller
}

// handler answers one request.
type handler func(s *Server, req proto.Request, c callCtx) (any, *proto.Error)

// typed is the handler of f, which takes the request's params decoded
// into a P: those mayAddress decoded when they are a P, so a scoped
// method's params are decoded once.
func typed[P any](f func(s *Server, p P, c callCtx) (any, *proto.Error)) handler {
	return func(s *Server, req proto.Request, c callCtx) (any, *proto.Error) {
		if p, ok := c.params.(P); ok {
			return f(s, p, c)
		}
		var p P
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return f(s, p, c)
	}
}

// plain is the handler of f, which takes no params.
func plain(f func(s *Server, c callCtx) any) handler {
	return func(s *Server, _ proto.Request, c callCtx) (any, *proto.Error) { return f(s, c), nil }
}

// scoped is the name of a method of scope scope: what of its params,
// decoded into a P, names a session or a project.
func scoped[P any](of func(P) string) func(json.RawMessage) (string, any, *proto.Error) {
	return func(raw json.RawMessage) (string, any, *proto.Error) {
		var p P
		if perr := decodeParams(raw, &p); perr != nil {
			return of(p), nil, perr
		}
		return of(p), p, nil
	}
}

// stream is a method's stream of a server function that takes no pid.
func stream(f func(s *Server, c net.Conn, br *bufio.Reader, req proto.Request)) func(*Server, net.Conn, *bufio.Reader, proto.Request, int) {
	return func(s *Server, c net.Conn, br *bufio.Reader, req proto.Request, _ int) { f(s, c, br, req) }
}

// sessionID names the session of SessionIDParams.
func sessionID(p proto.SessionIDParams) string { return p.ID }

// viewMethod is every view.* method methods doesn't name: the human's
// console steering (views.do).
var viewMethod = method{gate: gateHuman, call: typed(func(s *Server, p proto.ViewParams, c callCtx) (any, *proto.Error) {
	return s.views.do(c.method, p)
})}

// methodOf is method m's entry; ok false for a method the server doesn't
// know, which any caller may call and learn so.
func methodOf(m string) (method, bool) {
	if e, ok := methods[m]; ok {
		return e, true
	}
	if strings.HasPrefix(m, "view.") {
		return viewMethod, true
	}
	return method{}, false
}

// methods are the control methods by name (docs/SPEC.md §11.1).
var methods map[string]method

func init() {
	methods = map[string]method{
		// Anyone's.
		proto.MethodPing:           {call: plain(func(*Server, callCtx) any { return map[string]bool{"pong": true} })},
		proto.MethodCallerWho:      {call: plain(func(s *Server, c callCtx) any { return s.whoIs(c.who()) })},
		proto.MethodServerStatus:   {call: plain(func(s *Server, _ callCtx) any { return s.status() })},
		proto.MethodServerKeychain: {call: plain(func(*Server, callCtx) any { return keychainStatus() })},
		proto.MethodServerCodeHost: {call: plain(func(*Server, callCtx) any { return codeHostStatus() })},
		proto.MethodSessionList:    {call: plain(func(s *Server, c callCtx) any { return s.listFor(c.who(), c.human) })},
		proto.MethodAgentList:      {call: plain(func(s *Server, _ callCtx) any { return s.agentList() })},
		proto.MethodModHosts:       {call: plain(func(s *Server, _ callCtx) any { return s.modHosts() })},
		proto.MethodCLIRun: {gate: gateAgentOrHuman, call: typed(func(s *Server, p proto.CLIRunParams, c callCtx) (any, *proto.Error) {
			res, perr := s.cliRun(p, c.who())
			s.kick()
			return res, perr
		})},
		proto.MethodToolRun: {call: typed(func(s *Server, p proto.ToolRunParams, c callCtx) (any, *proto.Error) {
			return s.toolRunRPC(p, c.who())
		})},
		proto.MethodToolList: {call: func(s *Server, _ proto.Request, c callCtx) (any, *proto.Error) { return s.toolListRPC(c.who()) }},
		proto.MethodViewDigest: {gate: gateNotAgent, call: typed(func(s *Server, p proto.ViewDigestParams, _ callCtx) (any, *proto.Error) {
			n := s.views.askDigest(p.PID)
			if n == 0 {
				return nil, proto.Errorf(proto.ErrBadParams, "no console of pid %d", p.PID)
			}
			return proto.ViewDigestResult{Consoles: n}, nil
		})},
		// session.answer has its own gate (mayAnswer): the human, or the
		// coordinator of the session's project.
		proto.MethodSessionAnswer: {call: typed(func(s *Server, p proto.SessionAnswerParams, c callCtx) (any, *proto.Error) {
			return s.answer(p, c)
		})},
		proto.MethodModActionCheck: {gate: gateHuman, call: typed(func(s *Server, p proto.ModActionParams, c callCtx) (any, *proto.Error) {
			return s.modActionCheck(p, c.peerPID, c.who())
		})},
		proto.MethodModActionDone: {gate: gateHuman, call: typed(func(s *Server, p proto.ModActionDoneParams, c callCtx) (any, *proto.Error) {
			return s.modActionDone(p, c.peerPID, c.who())
		})},

		// Named by a session or a project: the caller's own (mayAddress).
		// Events and questions come from inside the session they name:
		// tm hook and the agent's mod are the agent's children.
		proto.MethodHookEvent: {scope: scopeOwn, name: scoped(func(p proto.HookEventParams) string { return p.Session }),
			call: typed(func(s *Server, p proto.HookEventParams, _ callCtx) (any, *proto.Error) { return s.hookEvent(p), nil })},
		proto.MethodSessionAsk: {scope: scopeOwn, name: scoped(func(p proto.SessionAskParams) string { return p.ID }),
			stream: stream((*Server).serveAsk)},
		proto.MethodSessionRead: {scope: scopeSees, name: scoped(func(p proto.SessionReadParams) string { return p.ID }),
			call: typed(func(s *Server, p proto.SessionReadParams, _ callCtx) (any, *proto.Error) { return s.read(p) })},
		proto.MethodSessionWait: {scope: scopeSees, name: scoped(func(p proto.SessionWaitParams) string { return p.ID }),
			call: typed(func(s *Server, p proto.SessionWaitParams, _ callCtx) (any, *proto.Error) { return s.wait(p) })},
		proto.MethodSessionWatch: {scope: scopeSees, name: scoped(sessionID), stream: stream((*Server).serveWatch)},
		proto.MethodAgentExplain: {scope: scopeSees, name: scoped(sessionID),
			call: typed(func(s *Server, p proto.SessionIDParams, _ callCtx) (any, *proto.Error) { return s.explain(p.ID) })},
		proto.MethodProjectWatch: {scope: scopeProject, name: scoped(func(p proto.ProjectWatchParams) string { return p.Project }),
			stream: stream((*Server).serveProjectWatch)},

		// The human's.
		proto.MethodServerStop: {gate: gateHuman, call: typed(func(s *Server, p proto.ServerStopParams, _ callCtx) (any, *proto.Error) {
			return s.stop(p)
		})},
		proto.MethodSessionStart: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionStartParams, _ callCtx) (any, *proto.Error) {
			return s.startSession(p)
		})},
		proto.MethodSessionStop: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionIDParams, _ callCtx) (any, *proto.Error) {
			return s.stopSession(p.ID)
		})},
		proto.MethodSessionKeys: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionKeysParams, _ callCtx) (any, *proto.Error) {
			return s.keys(p)
		})},
		proto.MethodSessionPrompt: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionPromptParams, _ callCtx) (any, *proto.Error) {
			return s.prompt(p)
		})},
		proto.MethodSecretTest: {gate: gateHuman, call: typed(func(s *Server, p proto.SecretTestParams, _ callCtx) (any, *proto.Error) {
			return s.secretTest(p)
		})},
		proto.MethodSessionClear: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionIDParams, c callCtx) (any, *proto.Error) {
			return s.clearSession(p.ID, c.who())
		})},
		proto.MethodSessionRemote: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionRemoteParams, _ callCtx) (any, *proto.Error) {
			return s.remote(p)
		})},
		proto.MethodSessionModel: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionModelParams, _ callCtx) (any, *proto.Error) {
			return s.syncModel(p.ID)
		})},
		proto.MethodSessionAdopt: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionAdoptParams, _ callCtx) (any, *proto.Error) {
			return s.adopt(p)
		})},
		proto.MethodSessionAdoptAsk: {gate: gateHuman, call: typed(func(s *Server, p proto.SessionAdoptAskParams, _ callCtx) (any, *proto.Error) {
			return s.adoptAsk(p)
		})},
		proto.MethodQuestionsOpen: {gate: gateHuman, call: typed(func(s *Server, p proto.QuestionsOpenParams, _ callCtx) (any, *proto.Error) {
			return s.openQuestions(p)
		})},
		proto.MethodCoordinatorClear: {gate: gateHuman, call: typed(func(s *Server, p proto.CoordinatorClearParams, _ callCtx) (any, *proto.Error) {
			return s.saveClear(p)
		})},
		proto.MethodProjectRename: {gate: gateHuman, call: typed(func(s *Server, p proto.ProjectRenameParams, c callCtx) (any, *proto.Error) {
			return s.renameProject(p, c.who())
		})},
		proto.MethodProjectActive: {gate: gateHuman, call: typed(func(s *Server, p proto.ProjectActiveParams, c callCtx) (any, *proto.Error) {
			res, perr := s.setActive(p, c.who())
			s.kick()
			return res, perr
		})},
		proto.MethodProjectsWatch: {gate: gateHuman, stream: stream((*Server).serveProjectsWatch)},
		proto.MethodAgentReload: {gate: gateHuman, call: plain(func(s *Server, _ callCtx) any {
			s.loadAgents()
			return s.agentList()
		})},
		proto.MethodPeerReload: {gate: gateHuman, call: plain(func(s *Server, _ callCtx) any {
			s.peers.Reload()
			return s.peers.Status()
		})},
		proto.MethodPeerList: {gate: gateHuman, call: plain(func(s *Server, _ callCtx) any { return s.mirror.List() })},
		proto.MethodModViews: {gate: gateHuman, call: typed(func(s *Server, p proto.ModViewsParams, c callCtx) (any, *proto.Error) {
			return s.modViews(p, c.peerPID, c.who())
		})},
		proto.MethodModViewInput: {gate: gateHuman, call: typed(func(s *Server, p proto.ModViewInputParams, c callCtx) (any, *proto.Error) {
			return s.modViewInput(p, c.peerPID, c.who())
		})},
		proto.MethodModCommand: {gate: gateHuman, call: typed(func(s *Server, p proto.ModCommandParams, c callCtx) (any, *proto.Error) {
			return s.modCommand(p, c.peerPID, c.who())
		})},
		proto.MethodModRestart: {gate: gateHuman, call: typed(func(s *Server, p proto.ModRestartParams, c callCtx) (any, *proto.Error) {
			return s.restartMod(p.ID, c.who())
		})},
		proto.MethodNotifyClaim:   {gate: gateHuman, call: notice(proto.MethodNotifyClaim)},
		proto.MethodNotifyDismiss: {gate: gateHuman, call: notice(proto.MethodNotifyDismiss)},
		proto.MethodViewSubscribe: {gate: gateHuman, stream: (*Server).serveViewStream},
	}
}

// notice is the handler of notify method m.
func notice(m string) handler {
	return typed(func(s *Server, p proto.NoticeParams, c callCtx) (any, *proto.Error) {
		return s.noticeCall(m, p, c.who())
	})
}
