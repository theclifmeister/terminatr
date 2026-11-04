package server

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// parityServer has project p's coordinator (s-1) and program acme's
// director (s-2), each with the caller its session gets.
func parityServer() (s *Server, coord, director caller.Caller) {
	s = &Server{
		sessions: map[string]*session.Session{"s-1": nil, "s-2": nil, "s-3": nil},
		records: map[string]SessionRecord{
			"s-1": {ID: "s-1", Role: proto.RoleCoordinator, Project: "p"},
			"s-2": {ID: "s-2", Role: proto.RoleDirector, Project: "acme"},
			"s-3": {ID: "s-3", Role: proto.RoleThread, Project: "p", Thread: "t-0001"},
		},
	}
	coord = caller.Caller{Kind: caller.Coordinator, Project: "p", Session: "s-1"}
	director = caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-2"}
	return s, coord, director
}

// directorMethodReasons are the methods a coordinator calls on its own
// seat and a director doesn't call on its own, each with why.
var directorMethodReasons = map[string]string{
	proto.MethodProjectWatch:   "names a project; a director's own slug is a program's, and it names members (scopeProgram)",
	proto.MethodSearchQuery:    "a project's; a director has program.search over its members",
	proto.MethodSearchTimeline: "same",
	proto.MethodSearchShow:     "same",
}

// TestSeatParityMethods: every method in the table a coordinator may call
// on its own seat, a director may call on its own, or it says why not. A
// new method needs no entry unless it tells them apart.
func TestSeatParityMethods(t *testing.T) {
	s, coord, director := parityServer()
	mayOwn := func(name string, m method, c caller.Caller) bool {
		who := func() caller.Caller { return c }
		if mayCall(name, m.gate, false, who) != nil {
			return false
		}
		if m.scope == scopeNone {
			return true
		}
		m.name = func(json.RawMessage) (string, any, *proto.Error) {
			if m.scope == scopeOwn || m.scope == scopeSees {
				return c.Session, nil, nil
			}
			return c.Project, nil, nil
		}
		_, perr := s.mayAddress(proto.Request{Method: name}, m, false, who)
		return perr == nil
	}
	differ := map[string]bool{}
	for name, m := range methods {
		if mayOwn(name, m, coord) && !mayOwn(name, m, director) {
			differ[name] = true
			if directorMethodReasons[name] == "" {
				t.Errorf("%s: a coordinator may call it on its own seat and a director not: make it work, or say why in directorMethodReasons", name)
			}
		}
	}
	for name := range directorMethodReasons {
		if !differ[name] {
			t.Errorf("%s now works for a director: drop it from directorMethodReasons", name)
		}
	}
}

// TestSeatParityGates: what handlers check themselves, past the table.
// gap: entries pin behaviour the T990 report lists as a bug; fix the bug
// and change the entry.
func TestSeatParityGates(t *testing.T) {
	s, coord, director := parityServer()
	cc := func(c caller.Caller) callCtx { return callCtx{who: func() caller.Caller { return c }} }
	thread := proto.SessionInfo{ID: "s-3", Role: proto.RoleThread, Project: "p"}
	for _, c := range []struct {
		action string
		gate   func(c caller.Caller, slug string) error
		dir    bool
		reason string
	}{
		{"session.answer a thread's menu", func(c caller.Caller, _ string) error {
			in := thread
			in.Project = c.Project
			if perr := s.mayAnswer(in, c, false); perr != nil {
				return perr
			}
			return nil
		}, false, "user decision 2026-11-03: the director never answers; it recommends"},
		{"access.show", func(c caller.Caller, slug string) error {
			if _, perr := mayShowAccess(cc(c), slug, ""); perr != nil {
				return perr
			}
			return nil
		}, false, "safety: threads' access is the human's and the project's coordinator's"},
		{"access.explain", func(c caller.Caller, slug string) error {
			if _, perr := mayExplainAccess(cc(c), slug); perr != nil {
				return perr
			}
			return nil
		}, false, "gap (minor): a director can't ask what its own commands would do"},
		{"tool.list and tool.run", func(c caller.Caller, _ string) error {
			if _, perr := s.callerSession(c); perr != nil {
				return perr
			}
			return nil
		}, false, "gap: sessionTools offers a director the search tools, but no MCP call reaches them"},
	} {
		if err := c.gate(coord, coord.Project); err != nil {
			t.Errorf("%s: a coordinator: %v", c.action, err)
		}
		if err := c.gate(director, director.Project); (err == nil) != c.dir {
			t.Errorf("%s: a director: %v, want allowed %v (%s)", c.action, err, c.dir, c.reason)
		}
	}
}

// TestSeatParityTools: every tool a coordinator is offered, a director is
// offered too, or it says why not.
func TestSeatParityTools(t *testing.T) {
	s, _, _ := parityServer()
	reasons := map[string]string{"link_files": "a project's link files; a program has none"}
	names := func(rec SessionRecord) []string {
		var out []string
		for _, tool := range s.sessionTools(rec) {
			out = append(out, tool.Name)
		}
		return out
	}
	director := names(s.records["s-2"])
	for _, name := range names(s.records["s-1"]) {
		if !slices.Contains(director, name) && reasons[name] == "" {
			t.Errorf("tool %s: a coordinator's and not a director's: offer it, or say why here", name)
		}
	}
	for name := range reasons {
		if slices.Contains(director, name) {
			t.Errorf("tool %s is a director's now: drop its reason", name)
		}
	}
}

// TestDirectorHookSettingsFloor: a director can't rewrite the hook
// settings in its cwd, as a coordinator and a thread can't: hooks run
// outside what the guard judges.
func TestDirectorHookSettingsFloor(t *testing.T) {
	s, root := programFixture(t)
	writeConfig(t, programsOn)
	cwd := filepath.Join(root, "_hook-test")
	out := s.guardNoWrite(SessionRecord{Role: proto.RoleDirector, Project: "acme", Cwd: cwd}, loadLaunchConfig())
	joined := strings.Join(out, "\n")
	for _, want := range hookSettings(cwd) {
		if !strings.Contains(joined, want) {
			t.Errorf("a director's floor lacks %s:\n%s", want, joined)
		}
	}
}
