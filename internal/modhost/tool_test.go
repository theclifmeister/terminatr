package modhost

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

func (s *server) toolCall(tool, input string) *modproto.Message {
	s.t.Helper()
	id := s.request(modproto.MethodToolCall, modproto.ToolCall{Tool: tool, Input: json.RawMessage(input),
		Project: "tm", Role: "thread", Thread: "t-0001", Session: "s-1"})
	return s.reply(id)
}

func toolText(t *testing.T, m *modproto.Message) string {
	t.Helper()
	if m.Error != nil {
		t.Fatalf("tool.call: %v", m.Error)
	}
	var r modproto.ToolResult
	_ = json.Unmarshal(m.Result, &r)
	return r.Text
}

func TestToolCalls(t *testing.T) {
	s := startHost(t, `
		$.tool.handle("echo", (input, call) => input.text + " from " + [call.tool, call.project, call.role, call.thread, call.session].join(","));
		$.tool.handle("json", () => ({ok: true, n: 2}));
		$.tool.handle("none", () => {});
		$.tool.handle("later", async (input) => {
			const p = await $.call("project.get", {slug: "tm"});
			return "tasks " + p.tasks.length;
		});
		$.tool.handle("throws", () => { throw new Error("no draft"); });
		$.tool.handle("rejects", async () => { throw new Error("later no"); });
		$.tool.handle("empty", (input) => Object.keys(input).length);
	`, time.Second)
	if r := s.hello(); !slices.Equal(r.Tools, []string{"echo", "empty", "json", "later", "none", "rejects", "throws"}) {
		t.Fatalf("hello tools %v", r.Tools)
	}
	if got := toolText(t, s.toolCall("echo", `{"text":"hi"}`)); got != "hi from echo,tm,thread,t-0001,s-1" {
		t.Fatalf("echo %q", got)
	}
	if got := toolText(t, s.toolCall("json", `{}`)); got != `{"ok":true,"n":2}` {
		t.Fatalf("json %q", got)
	}
	if got := toolText(t, s.toolCall("none", `{}`)); got != "" {
		t.Fatalf("none %q", got)
	}
	if got := toolText(t, s.toolCall("empty", ``)); got != "0" {
		t.Fatalf("empty input %q", got)
	}

	// An async handler is answered once its promise settles.
	id := s.request(modproto.MethodToolCall, modproto.ToolCall{Tool: "later", Input: json.RawMessage(`{}`)})
	call := s.method("project.get")
	_ = s.conn.Reply(*call.ID, map[string]any{"tasks": []int{1, 2}})
	if got := toolText(t, s.reply(id)); got != "tasks 2" {
		t.Fatalf("later %q", got)
	}

	for tool, want := range map[string]string{"throws": "no draft", "rejects": "later no"} {
		m := s.toolCall(tool, `{}`)
		if m.Error == nil || m.Error.Code != modproto.CodeToolFailed || !strings.Contains(m.Error.Message, want) {
			t.Fatalf("%s: %+v", tool, m.Error)
		}
		if f := s.failed(); f.Where != "tool:"+tool || !strings.Contains(f.Error, want) {
			t.Fatalf("%s failed %+v", tool, f)
		}
	}
	if m := s.toolCall("nope", `{}`); m.Error == nil || m.Error.Code != modproto.CodeNoTool {
		t.Fatalf("unknown tool %+v", m.Error)
	}
	// The host lives on.
	if got := toolText(t, s.toolCall("echo", `{"text":"again"}`)); !strings.HasPrefix(got, "again") {
		t.Fatalf("after errors %q", got)
	}
}

func TestToolCallBeforeHelloAndBadHandle(t *testing.T) {
	s := startHost(t, `$.tool.handle("x");`, time.Second)
	if m := s.toolCall("x", `{}`); m.Error == nil || m.Error.Code != modproto.CodeNotReady {
		t.Fatalf("before hello %+v", m.Error)
	}
	id := s.request(modproto.MethodHello, modproto.Hello{Protocol: modproto.Protocol, API: "1.0", Mod: "test"})
	if m := s.reply(id); m.Error == nil || !strings.Contains(m.Error.Message, "needs a tool id and a function") {
		t.Fatalf("hello %+v", m.Error)
	}
}

func TestSpinInAToolExits3(t *testing.T) {
	s := startHost(t, `$.tool.handle("spin", () => { for (;;) {} });`, 50*time.Millisecond)
	s.hello()
	s.request(modproto.MethodToolCall, modproto.ToolCall{Tool: "spin"})
	if f := s.failed(); f.Kind != modproto.KindInterrupted || f.Where != "tool:spin" {
		t.Fatalf("failed %+v", f)
	}
	if c := s.exit(); c != modproto.ExitInterrupted {
		t.Fatalf("exit %d", c)
	}
}
