package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// convManifest is an agent whose transcript tm reads: user lines are
// prompts, assistant lines text.
const convManifest = `manifest_version = 1
name = "conv"
[launch]
command = %q
args = ["-c", "sleep 60"]
[jsonl_tail]
path_field = "transcript_path"
[[jsonl_tail.rules]]
match = { type = "done" }
state = "idle"
[transcript]
tested = ["1."]
version_field = "version"
time_field = "at"
content = "content"
block_kind = "type"
string_block = "text"
[[transcript.items]]
kind = "prompt"
match = { type = "user" }
block = "text"
text = "text"
[[transcript.items]]
kind = "text"
match = { type = "assistant" }
block = "text"
text = "text"
`

// startConv starts a conv session and names its transcript, which holds
// lines already; it returns the session's id and the file.
func startConv(t *testing.T, p client.Paths, c *client.Client, lines string) (string, string) {
	t.Helper()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "conv", Cwd: t.TempDir()}, &started)
	f := filepath.Join(t.TempDir(), "conv.jsonl")
	if err := os.WriteFile(f, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	call(t, c, proto.MethodHookEvent, proto.HookEventParams{Session: started.Session.ID, Agent: "conv", Event: "SessionStart",
		Payload: map[string]any{"hook_event_name": "SessionStart", "transcript_path": f}}, nil)
	return started.Session.ID, f
}

func appendTo(t *testing.T, f, s string) {
	t.Helper()
	w, err := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(s)
	w.Close()
}

func setupConv(t *testing.T) (client.Paths, *client.Client) {
	t.Helper()
	p := testPaths(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	m := []byte(fmt.Sprintf(convManifest, testSh(t)))
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "conv.toml"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return p, c
}

// watchTranscript opens a transcript.watch stream: the answer and its
// events.
func watchTranscript(t *testing.T, p client.Paths, id string) (*client.Client, proto.TranscriptWatchResult, <-chan proto.TranscriptEvent) {
	t.Helper()
	w, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	var res proto.TranscriptWatchResult
	if err := w.Call(proto.MethodTranscriptWatch, proto.TranscriptWatchParams{Session: id}, &res); err != nil {
		t.Fatal(err)
	}
	ch := make(chan proto.TranscriptEvent, 64)
	go func() {
		defer close(ch)
		for {
			var ev proto.TranscriptEvent
			if w.ReadLine(&ev) != nil {
				return
			}
			ch <- ev
		}
	}()
	return w, res, ch
}

func nextEvent(t *testing.T, ch <-chan proto.TranscriptEvent) proto.TranscriptEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("stream ended")
		}
		return ev
	case <-time.After(10 * time.Second):
		t.Fatal("no event")
	}
	return proto.TranscriptEvent{}
}

// TestTranscriptWatch: a reset with the backlog, then the items appended,
// clean; a line the table doesn't know makes the stream unmapped, and
// the session's end closes it.
func TestTranscriptWatch(t *testing.T) {
	defer func(d time.Duration) { transcriptPoll = d }(transcriptPoll)
	transcriptPoll = 20 * time.Millisecond
	p, c := setupConv(t)
	id, f := startConv(t, p, c, `{"type":"user","version":"1.2","at":"2026-10-26T20:43:05Z","content":"hello"}`+"\n"+
		`{"type":"other"}`+"\n"+
		`{"type":"assistant","version":"1.2","content":[{"type":"text","text":"hi \u001b[2Jthere"}]}`+"\n"+
		`{"type":"assistant","content":[{"type":"text","text":"cut`)

	_, res, events := watchTranscript(t, p, id)
	if res.Session.ID != id || res.Status.State != proto.TranscriptMapped || res.Window != proto.TranscriptWindow {
		t.Fatalf("answer %+v", res)
	}
	ev := nextEvent(t, events)
	if ev.Event != proto.EventTranscriptReset || len(ev.Items) != 2 || ev.Items[0].Text != "hello" || ev.Items[1].Text != "hi [2Jthere" ||
		!ev.Items[0].At.Equal(time.Date(2026, 10, 26, 20, 43, 5, 0, time.UTC)) || ev.Items[0].From != "" ||
		ev.Status == nil || ev.Status.Version != "1.2" {
		t.Fatalf("reset %+v", ev)
	}

	appendTo(t, f, ` short"}]}`+"\n"+`{"type":"user","content":"next"}`+"\n"+`{"type":"user","content":"[tm] 1 new inbox item"}`+"\n")
	ev = nextEvent(t, events)
	if ev.Event != proto.EventTranscriptItems || len(ev.Items) != 3 || ev.Items[0].Text != "cut short" || ev.Items[1].Text != "next" ||
		ev.Items[1].From != "" || ev.Items[2].From != proto.FromTM {
		t.Fatalf("items %+v", ev)
	}

	appendTo(t, f, `{"type":"assistant","content":[{"type":"widget"},{"type":"widget"},{"type":"widget"}]}`+"\n")
	ev = nextEvent(t, events)
	if ev.Event != proto.EventTranscriptStatus || ev.Status.State != proto.TranscriptUnmapped {
		t.Fatalf("status %+v", ev)
	}

	call(t, c, proto.MethodSessionStop, proto.SessionIDParams{ID: id}, nil)
	for {
		ev = nextEvent(t, events)
		if ev.Event == proto.EventTranscriptClosed {
			break
		}
	}
	if ev.Reason != proto.PaneExited {
		t.Fatalf("closed %+v", ev)
	}
}

// TestTranscriptWindow: the stream reads on only while the client's
// acks leave room, and the backlog is capped.
func TestTranscriptWindow(t *testing.T) {
	defer func(d time.Duration, w int) { transcriptPoll, transcriptWindow = d, w }(transcriptPoll, transcriptWindow)
	transcriptPoll, transcriptWindow = 20*time.Millisecond, proto.TranscriptBacklog+5
	p, c := setupConv(t)
	var lines []byte
	for i := range proto.TranscriptBacklog + 50 {
		lines = append(lines, `{"type":"user","content":"p`+strconv.Itoa(i)+`"}`+"\n"...)
	}
	id, f := startConv(t, p, c, string(lines))
	w, _, events := watchTranscript(t, p, id)
	ev := nextEvent(t, events)
	if len(ev.Items) != proto.TranscriptBacklog || ev.Skipped != 50 || ev.Items[0].Text != "p50" {
		t.Fatalf("reset: %d items, %d skipped, first %+v", len(ev.Items), ev.Skipped, ev.Items[0])
	}
	for i := range 20 {
		appendTo(t, f, `{"type":"user","content":"more`+strconv.Itoa(i)+`"}`+"\n")
	}
	got := 0
	quiet := time.After(500 * time.Millisecond)
wait:
	for {
		select {
		case ev := <-events:
			got += len(ev.Items)
		case <-quiet:
			break wait
		}
	}
	if got != 5 {
		t.Fatalf("took %d items past the window's 5", got)
	}
	w.WriteLine(proto.TranscriptInput{Ack: proto.TranscriptBacklog + got})
	for got < 20 {
		got += len(nextEvent(t, events).Items)
	}
	if got != 20 {
		t.Fatalf("got %d items, want 20", got)
	}
}

// TestTranscriptUnmappedAgent: an agent without a [transcript] table, or
// a session without an agent, answers unmapped, so the client shows the
// terminal.
func TestTranscriptUnmappedAgent(t *testing.T) {
	p, c := setupConv(t)
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	_, res, events := watchTranscript(t, p, started.Session.ID)
	if res.Status.State != proto.TranscriptUnmapped || res.Status.Reason == "" {
		t.Fatalf("answer %+v", res)
	}
	if ev := nextEvent(t, events); ev.Event != proto.EventTranscriptReset || len(ev.Items) != 0 || ev.Status.State != proto.TranscriptUnmapped {
		t.Fatalf("reset %+v", ev)
	}
	// A conv session before its agent names a transcript waits.
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "conv", Cwd: t.TempDir()}, &started)
	_, res, _ = watchTranscript(t, p, started.Session.ID)
	if res.Status.State != proto.TranscriptWaiting {
		t.Fatalf("answer %+v", res)
	}
}
