package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

const lintToolManifest = `id = "lint"
name = "Lint"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.ts"

[[provides.tools]]
id = "lint_report"
description = "Lints a report draft."
roles = ["thread"]
[provides.tools.input]
required = ["text"]
[provides.tools.input.properties.text]
type = "string"
[provides.tools.input.properties.max]
type = "integer"

[[provides.tools]]
id = "ping"
description = "Never answers pong."

[permissions]
tools = true
`

const lintToolMain = `
type Lint = { text: string; max?: number };
$.tool.handle("lint_report", (input: any, call: any) => (input as Lint).text.length + " chars from " + call.role + " " + call.thread + " of " + call.project);
$.tool.handle("ping", async () => { throw new Error("no pong today"); });
`

// TestModAgentTools: an enabled mod's tools are listed for the sessions
// of the projects it runs for, by role, beside a thread's own; a call
// goes to the mod's host as the session, with its input checked against
// the tool's schema first.
func TestModAgentTools(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	write := func(path, text string) {
		t.Helper()
		path = filepath.Join(tmHome, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("mods/lint/mod.toml", lintToolManifest)
	write("mods/lint/main.ts", lintToolMain)
	write("config.toml", "[mods.lint]\nenabled = true\ngrants = [\"tools\"]\nprojects = [\"proj\"]\n[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n")

	rt, err := os.MkdirTemp(ipc.ShortDir(), "tmmod")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(rt) })
	s := &Server{opts: Options{Bin: os.Args[0]}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}

	// No mod hosts run: no mod tools.
	thr := SessionRecord{ID: "s-1", Role: proto.RoleThread, Project: "proj", Thread: "t-0001", Cwd: rt}
	if got := toolNames(s.sessionTools(thr)); got != "report,status,steps,done" {
		t.Fatalf("without hosts: %s", got)
	}
	sup := s.newModHosts()
	t.Cleanup(sup.Close)
	s.modSup.Store(sup)
	specs, ok := s.modSpecs()
	if !ok || len(specs) != 1 {
		t.Fatalf("specs %+v", specs)
	}
	sup.Set(specs)

	coord := SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Project: "proj", Cwd: rt}
	other := SessionRecord{ID: "s-3", Role: proto.RoleThread, Project: "other", Thread: "t-0002", Cwd: rt}
	for _, tc := range []struct {
		rec  SessionRecord
		want string
	}{{thr, "report,status,steps,done,lint_lint_report,lint_ping"}, {coord, "lint_ping"}, {other, "report,status,steps,done"}} {
		if got := toolNames(s.sessionTools(tc.rec)); got != tc.want {
			t.Errorf("%s of %s: %s, want %s", tc.rec.Role, tc.rec.Project, got, tc.want)
		}
	}
	tools := s.sessionTools(thr)
	if d := tools[4]; d.Description != "[mod: lint] Lints a report draft." || d.InputSchema["additionalProperties"] != false {
		t.Fatalf("def %+v", d)
	}

	// Over the mod socket of a live thread session.
	b, _ := agent.Builtin("claude")
	m, err := agent.ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.inSession = testInSession
	sock, err := s.listenMod("s-1", rt)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.Start(session.Config{
		ID: "s-1", Role: proto.RoleThread, Argv: []string{testSh(t), "-c", "exec cat"}, Cwd: rt,
		Env: []string{"PATH=/usr/bin:/bin"}, Cols: 80, Rows: 24, Logf: t.Logf,
		Agent: &session.AgentConfig{Agent: agent.FromManifest(m), Home: rt, ModSocket: sock},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Stop(time.Second) })
	s.mu.Lock()
	s.sessions["s-1"], s.records["s-1"] = sess, thr
	s.mu.Unlock()
	c := modClient(sock)
	c.Timeout = 30 * time.Second

	resp, err := c.Get("http://terminatr/v1/tools")
	if err != nil {
		t.Fatal(err)
	}
	var list proto.ToolListResult
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if got := toolNames(list.Tools); got != "report,status,steps,done,lint_lint_report,lint_ping" {
		t.Fatalf("GET /v1/tools: %s", got)
	}

	call := func(name, body string) (int, ModToolResult) {
		t.Helper()
		resp, err := c.Post("http://terminatr/v1/tools/"+name, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", name, err)
		}
		defer resp.Body.Close()
		var r ModToolResult
		json.NewDecoder(resp.Body).Decode(&r)
		return resp.StatusCode, r
	}
	if code, r := call("lint_lint_report", `{"text":"hello","max":3}`); code != http.StatusOK || r.Text != "5 chars from thread t-0001 of proj" {
		t.Fatalf("lint_report: %d %+v", code, r)
	}
	if code, r := call("lint_ping", ``); code != http.StatusUnprocessableEntity || r.Error != "no pong today" {
		t.Fatalf("ping: %d %+v", code, r)
	}
	for _, bad := range []struct{ body, want string }{
		{`{}`, "text is required"},
		{`{"text":5}`, "text: want string, not integer"},
		{`{"text":"x","max":1.5}`, "max: want integer, not number"},
		{`{"text":"x","mood":"good"}`, "mood is not one of the tool's inputs"},
		{`[1]`, "not a JSON object"},
	} {
		if code, r := call("lint_lint_report", bad.body); code != http.StatusBadRequest || !strings.Contains(r.Error, bad.want) {
			t.Errorf("%s: %d %+v, want %q", bad.body, code, r, bad.want)
		}
	}
	if code, _ := call("lint_nope", `{}`); code != http.StatusNotFound {
		t.Fatalf("unknown mod tool: %d", code)
	}
	logged, _ := os.ReadFile(filepath.Join(tmHome, "logs", "mods", "lint.log"))
	if !strings.Contains(string(logged), "tool lint_report for thread t-0001 of proj (s-1)") || !strings.Contains(string(logged), "error: no pong today") {
		t.Errorf("mod log:\n%s", logged)
	}

	// tool.list and tool.run (tm mcp), the session found from the pid.
	res, perr := s.toolListRPC(sess.PID())
	if perr != nil || toolNames(res.(proto.ToolListResult).Tools) != "report,status,steps,done,lint_lint_report,lint_ping" {
		t.Fatalf("tool.list: %+v %v", res, perr)
	}
	res, perr = s.toolRunRPC(proto.ToolRunParams{Name: "lint_lint_report", Input: json.RawMessage(`{"text":"abc"}`)}, sess.PID())
	if rr, _ := res.(proto.ToolRunResult); perr != nil || rr.IsError || !strings.HasPrefix(rr.Text, "3 chars") {
		t.Fatalf("tool.run: %+v %v", res, perr)
	}

	// The human revokes the tools grant: the tools go from the list, and
	// a call is refused, over the socket and over tool.run alike.
	write("config.toml", "[mods.lint]\nenabled = true\ngrants = []\nprojects = [\"proj\"]\n[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n")
	s.modBr.mu.Lock()
	s.modBr.reg = nil // what modRegistryAge does within a second
	s.modBr.mu.Unlock()
	if got := toolNames(s.sessionTools(thr)); got != "report,status,steps,done" {
		t.Fatalf("after the revoke: %s", got)
	}
	if code, _ := call("lint_lint_report", `{"text":"x"}`); code != http.StatusNotFound {
		t.Fatalf("a call after the revoke: %d", code)
	}
	res, perr = s.toolListRPC(sess.PID())
	if perr != nil || toolNames(res.(proto.ToolListResult).Tools) != "report,status,steps,done" {
		t.Fatalf("tool.list after the revoke: %+v %v", res, perr)
	}
	res, _ = s.toolRunRPC(proto.ToolRunParams{Name: "lint_lint_report", Input: json.RawMessage(`{"text":"abc"}`)}, sess.PID())
	if rr := res.(proto.ToolRunResult); !rr.IsError || !strings.Contains(rr.Text, "tools grant") {
		t.Fatalf("tool.run after the revoke: %+v", rr)
	}
	write("config.toml", "[mods.lint]\nenabled = true\ngrants = [\"tools\"]\nprojects = [\"proj\"]\n[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n")
	s.modBr.mu.Lock()
	s.modBr.reg = nil
	s.modBr.mu.Unlock()

	// A coordinator gets the mod tools for its role, and not a thread's.
	s.mu.Lock()
	s.records["s-1"] = coord
	s.mu.Unlock()
	if code, _ := call("lint_lint_report", `{"text":"x"}`); code != http.StatusNotFound {
		t.Fatalf("a thread-only tool for a coordinator: %d", code)
	}
	if code, _ := call("status", `{"activity":"x"}`); code != http.StatusForbidden {
		t.Fatalf("a thread tool for a coordinator: %d", code)
	}
	if _, perr := s.toolListRPC(sess.PID()); perr != nil {
		t.Fatalf("tool.list for a coordinator: %v", perr)
	}
	if _, perr := s.toolListRPC(1); perr == nil {
		t.Fatal("tool.list from outside a session")
	}
}

func toolNames(ts []proto.ToolDef) string {
	var names []string
	for _, t := range ts {
		names = append(names, t.Name)
	}
	return strings.Join(names, ",")
}
