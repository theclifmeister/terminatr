package modsup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

const toolMod = `$.tool.handle("echo", (input, call) => input.text + " for " + call.project);`

func (f *fixture) callTool(id, tool, input string) (string, error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := f.sup.CallTool(ctx, id, modproto.ToolCall{Tool: tool, Input: json.RawMessage(input), Project: "tm", Role: "thread"})
	return res.Text, err
}

// TestToolCallStartsALazyHost: a mod that only answers tools idles until
// an agent calls one, which starts it and gets its answer.
func TestToolCallStartsALazyHost(t *testing.T) {
	f := newFixture(t, nil)
	spec := f.mod("m", "main.js", toolMod)
	f.sup.Set([]Spec{spec})
	f.waitState("m", Running) // first run: starts at once
	if got, err := f.callTool("m", "echo", `{"text":"hi"}`); err != nil || got != "hi for tm" {
		t.Fatalf("running: %q %v", got, err)
	}
	f.sup.Set(nil)
	f.waitFor("m gone", func() bool { return len(f.sup.Status()) == 0 })

	f.sup.Set([]Spec{spec})
	f.waitState("m", Idle)
	time.Sleep(50 * time.Millisecond)
	if st := f.status("m"); st.State != Idle || st.PID != 0 {
		t.Fatalf("a tool-only mod started before a call: %+v", st)
	}
	if got, err := f.callTool("m", "echo", `{"text":"woke"}`); err != nil || got != "woke for tm" {
		t.Fatalf("idle: %q %v", got, err)
	}
	if st := f.status("m"); st.State != Running {
		t.Fatalf("after the call %+v", st)
	}
	var perr *modproto.Error
	if _, err := f.callTool("m", "nope", `{}`); !errors.As(err, &perr) || perr.Code != modproto.CodeNoTool {
		t.Fatalf("unknown tool: %v", err)
	}
}

func TestToolCallRefusals(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.callTool("none", "echo", `{}`); err == nil || !strings.Contains(err.Error(), "doesn't run") {
		t.Fatalf("unsupervised: %v", err)
	}
	f.sup.Set([]Spec{f.mod("m", "main.js", `throw new Error("boom")`)})
	f.waitState("m", Failed)
	if _, err := f.callTool("m", "echo", `{}`); err == nil || !strings.Contains(err.Error(), "mod m failed") {
		t.Fatalf("failed mod: %v", err)
	}
}

// TestToolCallWaitsForAStartingHost: a call while the host loads is
// answered once it runs; one whose context ends first says so.
func TestToolCallWaitsForAStartingHost(t *testing.T) {
	f := newFixture(t, nil)
	f.sup.Set([]Spec{f.mod("m", "main.js", `const end = Date.now() + 300; while (Date.now() < end) {}`+"\n"+toolMod)})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.sup.CallTool(ctx, "m", modproto.ToolCall{Tool: "echo"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("short context: %v", err)
	}
	if got, err := f.callTool("m", "echo", `{"text":"late"}`); err != nil || got != "late for tm" {
		t.Fatalf("starting: %q %v", got, err)
	}
}
