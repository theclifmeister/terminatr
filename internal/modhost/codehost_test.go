package modhost

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// A code host mod's methods: the handler's args in, its answer out as
// JSON (nothing as null), a throw or rejection as CodeHandlerFailed, a
// method it doesn't handle as CodeNoHandler, any other method unknown.
func TestCodeHostCalls(t *testing.T) {
	s := startHost(t, `
		$.codehost.handle("pr", async (a) => {
			if (a.branch === "none") return null;
			const p = await $.call("process.run", {argv: ["glab"]});
			return {number: a.number || 7, state: "OPEN", remote: a.remote.path, out: p.stdout};
		});
		$.codehost.handle("prState", (a) => ({state: a.branch === "m" ? "MERGED" : "OPEN"}));
		$.codehost.handle("merge", () => { throw new Error("409 Conflict"); });
		$.codehost.handle("doctor", () => {});
	`, DefaultBudget)
	if r := s.hello(); !slices.Equal(r.CodeHost, []string{"doctor", "merge", "pr", "prState"}) {
		t.Fatalf("hello codehost %v", r.CodeHost)
	}
	args := map[string]any{"remote": map[string]string{"kind": "gitlab", "host": "gitlab.com", "path": "acme/shop"}, "branch": "feat"}
	id := s.request("codehost.pr", args)
	call := s.method("process.run")
	_ = s.conn.Reply(*call.ID, map[string]any{"code": 0, "stdout": "x"})
	if m := s.reply(id); m.Error != nil || string(m.Result) != `{"number":7,"state":"OPEN","remote":"acme/shop","out":"x"}` {
		t.Fatalf("pr %s %+v", m.Result, m.Error)
	}
	args["branch"] = "none"
	if m := s.reply(s.request("codehost.pr", args)); m.Error != nil || string(m.Result) != "null" {
		t.Fatalf("no PR %s %+v", m.Result, m.Error)
	}
	args["branch"] = "m"
	if m := s.reply(s.request("codehost.prState", args)); string(m.Result) != `{"state":"MERGED"}` {
		t.Fatalf("prState %s", m.Result)
	}
	if m := s.reply(s.request("codehost.doctor", args)); m.Error != nil || string(m.Result) != "null" {
		t.Fatalf("doctor %s %+v", m.Result, m.Error)
	}
	if m := s.reply(s.request("codehost.merge", args)); m.Error == nil || m.Error.Code != modproto.CodeHandlerFailed || m.Error.Message != "409 Conflict" {
		t.Fatalf("merge %+v", m.Error)
	}
	if f := s.failed(); f.Where != "codehost.merge" || !strings.Contains(f.Error, "409") {
		t.Fatalf("failed %+v", f)
	}
	if m := s.reply(s.request("codehost.failedLog", args)); m.Error == nil || m.Error.Code != modproto.CodeNoHandler {
		t.Fatalf("not handled %+v", m.Error)
	}
	if m := s.reply(s.request("codehost.delete", args)); m.Error == nil || m.Error.Code != modproto.CodeMethodNotFound {
		t.Fatalf("not a code host method %+v", m.Error)
	}
}

func TestCodeHostBeforeHelloAndBadHandle(t *testing.T) {
	s := startHost(t, `$.codehost.handle("delete", () => 1);`, DefaultBudget)
	if m := s.reply(s.request("codehost.pr", map[string]any{})); m.Error == nil || m.Error.Code != modproto.CodeNotReady {
		t.Fatalf("before hello %+v", m.Error)
	}
	id := s.request(modproto.MethodHello, modproto.Hello{Protocol: modproto.Protocol, API: "1.1", Mod: "test"})
	if m := s.reply(id); m.Error == nil || !strings.Contains(m.Error.Message, "needs one of pr, prState") {
		t.Fatalf("hello %+v", m.Error)
	}
}
