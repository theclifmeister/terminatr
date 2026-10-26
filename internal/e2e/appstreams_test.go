package e2e

// The macOS app's streams (docs/SPEC.md §3.3, Pane VT and Transcript),
// against the fake Claude: transcript.watch follows a thread's
// conversation as items, and pane.vt shows its pane as plain VT and
// takes its keys, typed text raising the takeover.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const chatScript = `
[[step]]
do = "todo_create"
subject = "Read the code"

[[step]]
do = "transcript"
payload = { type = "assistant", message = { role = "assistant", content = [{ type = "tool_use", id = "toolu_e2e", name = "Bash", input = { command = "go test ./..." } }] } }

[[step]]
do = "transcript"
payload = { type = "user", message = { role = "user", content = [{ type = "tool_result", tool_use_id = "toolu_e2e", content = "ok  pkg\nok  other", is_error = false }] } }

[[step]]
do = "stream"
ms = 50
text = "CHAT-DONE"
`

// humanControl dials the human socket, as tm app bridge does.
func humanControl(t *testing.T, env *Env) *client.Client {
	t.Helper()
	c, err := client.DialAt(client.Paths{Socket: env.Socket}.HumanSocket(), proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestSmokeAppStreams(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	for _, kv := range env.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			os.WriteFile(filepath.Join(v, "thread-chat.toml"), []byte(chatScript), 0o644)
		}
	}
	th := startThread(t, env, projDir)

	w := humanControl(t, env)
	if !slices.Contains(w.Server.Caps, proto.CapTranscript) || !slices.Contains(w.Server.Caps, proto.CapPaneVT) {
		t.Fatalf("hello caps %v", w.Server.Caps)
	}
	var res proto.TranscriptWatchResult
	if err := w.Call(proto.MethodTranscriptWatch, proto.TranscriptWatchParams{Session: th.ID}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Status.State != proto.TranscriptMapped || res.Status.Agent != "claude" {
		t.Fatalf("answer %+v", res)
	}
	var reset proto.TranscriptEvent
	if err := w.ReadLine(&reset); err != nil || reset.Event != proto.EventTranscriptReset {
		t.Fatalf("first event %+v, %v", reset, err)
	}

	env.MustCLI("thread", "prompt", "t-0001", "run thread-chat", "--project", "demo")
	var items []proto.TranscriptItem
	has := func(want proto.TranscriptItem) bool {
		return slices.ContainsFunc(items, func(it proto.TranscriptItem) bool {
			return it.Kind == want.Kind && it.Text == want.Text && it.Tool == want.Tool && it.Lines == want.Lines
		})
	}
	events := make(chan proto.TranscriptEvent, 64)
	go func() {
		defer close(events)
		for {
			var ev proto.TranscriptEvent
			if w.ReadLine(&ev) != nil {
				return
			}
			events <- ev
		}
	}()
	todos := false
	timeout := time.After(agentWait)
	for !(has(proto.TranscriptItem{Kind: proto.ItemPrompt, Text: "run thread-chat"}) &&
		has(proto.TranscriptItem{Kind: proto.ItemTool, Tool: "Bash", Text: "go test ./..."}) &&
		has(proto.TranscriptItem{Kind: proto.ItemResult, Text: "ok pkg", Lines: 2}) &&
		has(proto.TranscriptItem{Kind: proto.ItemText, Text: "CHAT-DONE"}) && todos) {
		var ev proto.TranscriptEvent
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("stream ended; items so far %+v (todos %v)", items, todos)
			}
			ev = e
		case <-timeout:
			t.Fatalf("items so far %+v (todos %v)", items, todos)
		}
		items = append(items, ev.Items...)
		for _, it := range ev.Items {
			todos = todos || it.Kind == proto.ItemTodos && len(it.Todos) == 1 && it.Todos[0].Text == "Read the code"
		}
		w.WriteLine(proto.TranscriptInput{Ack: len(ev.Items)})
	}

	// The pane, in plain VT: the conversation on screen.
	v := humanControl(t, env)
	var pres proto.PaneVTResult
	if err := v.Call(proto.MethodPaneVT, proto.PaneVTParams{Session: th.ID, Console: "app"}, &pres); err != nil {
		t.Fatal(err)
	}
	var ev proto.PaneVTEvent
	if err := v.ReadLine(&ev); err != nil || ev.Event != proto.EventPaneRepaint || !strings.Contains(string(ev.Data), "CHAT-DONE") {
		t.Fatalf("repaint %+v, %v", ev.Event, err)
	}
	// Typed text is the human's: the thread is taken over.
	v.WriteLine(proto.PaneVTInput{Input: []byte("hello from the app")})
	waitInbox(t, env, "takeover")
}
