package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// TestMayAddress: a session-addressed call names the caller's own session
// (hook.event, session.ask), or one it may see (session.read, .wait and
// .watch, agent.explain: a coordinator also its project's), or its own project
// (project.watch). The human on the human socket names any; a caller the
// pid walk finds in no session names none on the server socket.
func TestMayAddress(t *testing.T) {
	s := &Server{
		sessions: map[string]*session.Session{"s-1": nil, "s-2": nil, "s-3": nil, "s-4": nil, "s-5": nil},
		records: map[string]SessionRecord{
			"s-1": {ID: "s-1", Role: proto.RoleCoordinator, Project: "p"},
			"s-2": {ID: "s-2", Role: proto.RoleThread, Project: "p", Thread: "t-0001"},
			"s-3": {ID: "s-3", Role: proto.RoleThread, Project: "p", Thread: "t-0002"},
			"s-4": {ID: "s-4", Role: proto.RoleShell},
			"s-5": {ID: "s-5", Role: proto.RoleThread, Project: "q", Thread: "t-0001"},
		},
	}
	human := caller.Caller{Kind: caller.Human}
	shell := caller.Caller{Kind: caller.Human, Session: "s-4"}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "p", Session: "s-1"}
	thread := caller.Caller{Kind: caller.Thread, Project: "p", Thread: "t-0001", Session: "s-2"}
	hook := func(id string) any { return proto.HookEventParams{Session: id, Agent: "claude"} }
	id := func(id string) any { return proto.SessionIDParams{ID: id} }
	project := func(slug string) any { return proto.ProjectWatchParams{Project: slug} }
	for _, c := range []struct {
		m      string
		params any
		humanS bool
		c      caller.Caller
		ok     bool
	}{
		{proto.MethodHookEvent, hook("s-2"), false, thread, true},
		{proto.MethodHookEvent, hook("s-3"), false, thread, false},
		{proto.MethodHookEvent, hook("s-2"), false, coord, false},
		{proto.MethodHookEvent, hook("s-4"), false, shell, true}, // an agent run by hand in a shell
		{proto.MethodHookEvent, hook("s-2"), false, human, false},
		{proto.MethodHookEvent, hook("s-2"), true, human, true},
		{proto.MethodHookEvent, hook("s-1"), true, thread, false}, // an agent on the human socket
		{proto.MethodSessionAsk, proto.SessionAskParams{ID: "s-2"}, false, thread, true},
		{proto.MethodSessionAsk, proto.SessionAskParams{ID: "s-1"}, false, thread, false},
		{proto.MethodSessionAsk, proto.SessionAskParams{ID: "s-2"}, false, coord, false},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-2"}, false, thread, true},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-4"}, false, thread, false},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-3"}, false, coord, true},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-5"}, false, coord, false},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-4"}, false, coord, false},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-4"}, false, human, false},
		{proto.MethodSessionRead, proto.SessionReadParams{ID: "s-4"}, true, human, true},
		{proto.MethodSessionWatch, id("s-1"), false, thread, false},
		{proto.MethodSessionWatch, id("s-1"), false, coord, true},
		{proto.MethodAgentExplain, id("s-3"), false, thread, false},
		{proto.MethodAgentExplain, id("s-3"), false, coord, true},
		{proto.MethodProjectWatch, project("p"), false, coord, true},
		{proto.MethodProjectWatch, project("p"), false, thread, true},
		{proto.MethodProjectWatch, project("q"), false, coord, false},
		{proto.MethodProjectWatch, project("p"), false, human, false},
		{proto.MethodProjectWatch, project("q"), true, human, true},
		{proto.MethodSessionList, nil, false, human, true},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-2"}, false, thread, true},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-3"}, false, thread, false},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-3"}, false, coord, true},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-5"}, false, coord, false},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-4"}, false, human, false},
		{proto.MethodSessionWait, proto.SessionWaitParams{ID: "s-4"}, true, human, true},
	} {
		raw, _ := json.Marshal(c.params)
		who := func() caller.Caller { return c.c }
		perr := s.mayAddress(proto.Request{Method: c.m, Params: raw}, c.humanS, who)
		if (perr == nil) != c.ok {
			t.Errorf("%s %s (human socket %v) from %v: %v, want ok %v", c.m, raw, c.humanS, c.c, perr, c.ok)
		}
	}
}

// scopeProbe runs inside session $TERMINATR_SESSION: it calls the server
// socket at sock, naming its own session and session other, and prints
// what was refused.
func scopeProbe(sock, other string) int {
	c, err := dialAt(sock, proto.KindControl, proto.Protocol)
	if err != nil {
		fmt.Println("probe:", err)
		return 1
	}
	defer c.Close()
	refused := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) && perr.Code == proto.ErrRefused {
			return "refused"
		}
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	own := os.Getenv(caller.EnvSession)
	var list proto.SessionListResult
	listed := refused(c.Call(proto.MethodSessionList, nil, &list))
	for _, in := range list.Sessions {
		listed += " " + in.ID
	}
	fmt.Printf("probe own=%s other=%s hook=%s wait=%s list=%s done\n",
		refused(c.Call(proto.MethodSessionRead, proto.SessionReadParams{ID: own}, nil)),
		refused(c.Call(proto.MethodSessionRead, proto.SessionReadParams{ID: other}, nil)),
		refused(c.Call(proto.MethodHookEvent, proto.HookEventParams{Session: other, Agent: "claude"}, nil)),
		refused(c.Call(proto.MethodSessionWait, proto.SessionWaitParams{ID: other, TimeoutMS: 1}, nil)),
		listed)
	return 0
}

// TestScopeInSession: a process in a session's tree, on the server
// socket, reads, waits on and lists its own session and nothing else; the test, which the pid
// walk finds in no session, reads both only on the human socket.
func TestScopeInSession(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var ids []string
	for range 2 {
		var started proto.SessionStartResult
		call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir(), Cols: 120, Rows: 20}, &started)
		ids = append(ids, started.Session.ID)
	}
	call(t, c, proto.MethodSessionKeys, proto.SessionKeysParams{ID: ids[0],
		Data: fmt.Sprintf("'%s' scope-probe '%s' %s\r", os.Args[0], p.Socket, ids[1])}, nil)
	want := "probe own=ok other=refused hook=refused wait=refused list=ok " + ids[0] + " done"
	eventually(t, "the probe", func() bool {
		var rr proto.SessionReadResult
		call(t, c, proto.MethodSessionRead, proto.SessionReadParams{ID: ids[0]}, &rr)
		if strings.Contains(rr.Text, "probe ") && strings.Contains(rr.Text, " done") && !strings.Contains(rr.Text, want) {
			t.Fatalf("probe: %s", rr.Text)
		}
		return strings.Contains(rr.Text, want)
	})
	srv, err := dialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	var perr *proto.Error
	if err := srv.Call(proto.MethodSessionRead, proto.SessionReadParams{ID: ids[1]}, nil); !errors.As(err, &perr) || perr.Code != proto.ErrRefused {
		t.Errorf("session.read on the server socket from no session: %v, want refused", err)
	}
}

// TestMayAddressParams: the gate reads the params as the method does; a
// field that doesn't decode is refused, not let through unchecked.
func TestMayAddressParams(t *testing.T) {
	s := &Server{sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}}
	thread := caller.Caller{Kind: caller.Thread, Project: "p", Thread: "t-0001", Session: "s-2"}
	who := func() caller.Caller { return thread }
	for _, raw := range []string{
		`{"id":"s-3","scrollback":"x"}`,
		`{"id":"s-2","ID":"s-3"}`,
		`{"id":"s-3","id":"s-2","id":"s-3"}`,
		`[]`,
	} {
		if perr := s.mayAddress(proto.Request{Method: proto.MethodSessionRead, Params: json.RawMessage(raw)}, false, who); perr == nil {
			t.Errorf("session.read %s from s-2: allowed", raw)
		}
	}
}
