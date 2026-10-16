package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client/clienttest"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// hookEnv runs `tm hook` in-process against a socket in a short temp dir.
func hookEnv(t *testing.T, payload string) (*Env, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	dir := ipctest.ShortTempDir(t, "tmhook")
	sock := filepath.Join(dir, "tm.sock")
	t.Setenv("TERMINATR_HOME", filepath.Join(dir, "home"))
	t.Setenv("TERMINATR_SOCKET", sock)
	var out, errb bytes.Buffer
	vars := map[string]string{"TERMINATR_SESSION": "s-1", "CLAUDE_CODE_MESSAGING_TOKEN": "child-token"}
	e := &Env{Stdin: strings.NewReader(payload), Stdout: &out, Stderr: &errb, Getenv: func(k string) string { return vars[k] }}
	return e, &out, &errb, sock
}

// fakeServer answers hook connections with stdout, after delay; got
// receives each request's params.
func fakeServer(t *testing.T, sock string, delay time.Duration, stdout string, got chan<- proto.HookEventParams) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				var hello proto.Hello
				line, _ := br.ReadBytes('\n')
				json.Unmarshal(line, &hello)
				b, _ := json.Marshal(proto.Hello{Protocol: proto.Protocol})
				c.Write(append(b, '\n'))
				var req proto.Request
				line, _ = br.ReadBytes('\n')
				json.Unmarshal(line, &req)
				var p proto.HookEventParams
				json.Unmarshal(req.Params, &p)
				if got != nil {
					got <- p
				}
				time.Sleep(delay)
				res, _ := json.Marshal(proto.HookEventResult{Stdout: stdout})
				b, _ = json.Marshal(proto.Response{ID: req.ID, Result: res})
				c.Write(append(b, '\n'))
			}()
		}
	}()
}

func runHookTimed(t *testing.T, e *Env, args ...string) time.Duration {
	t.Helper()
	start := time.Now()
	if code, ok := e.Run(append([]string{"hook"}, args...)); !ok || code != 0 {
		t.Fatalf("tm hook exit %d (handled %v)", code, ok)
	}
	return time.Since(start)
}

func TestHookDelivers(t *testing.T) {
	big := strings.Repeat("x", 200<<10) // far past a macOS datagram
	payload := `{"hook_event_name":"SessionStart","session_id":"a","source":"clear","prompt":"` + big + `","secret_field":"y"}`
	e, out, errb, sock := hookEnv(t, payload)
	got := make(chan proto.HookEventParams, 1)
	fakeServer(t, sock, 0, `{"ctx":1}`, got)
	runHookTimed(t, e, "--agent", "claude")
	p := <-got
	if p.Session != "s-1" || p.Agent != "claude" || p.Event != "SessionStart" {
		t.Fatalf("envelope %+v", p)
	}
	// The manifest's inject.token_env goes along, outside the payload.
	if p.Token != "child-token" {
		t.Errorf("token %q, want the hook's CLAUDE_CODE_MESSAGING_TOKEN", p.Token)
	}
	// Trimmed with the manifest's [hook] rules before it left.
	if _, ok := p.Payload["secret_field"]; ok {
		t.Error("untrimmed field sent")
	}
	if s, _ := p.Payload["prompt"].(string); len(s) != 500 {
		t.Errorf("prompt truncated to %d bytes, want 500", len(s))
	}
	if out.String() != `{"ctx":1}` || errb.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out, errb)
	}
}

// The hook never holds the agent up and never says anything when the
// server is missing, stale or wedged (docs/SPEC.md §8.2). Each limit
// tells the deadlines apart rather than timing them: a missing server
// is given up on before hookAck, an event nobody answers well before
// hookContext, and the wedged server's late answer (after 5 s) is never
// read.
func TestHookDeadlines(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, sock string)
		event string
		max   time.Duration
	}{
		{"server down", func(*testing.T, string) {}, "Stop", hookAck},
		{"stale socket", func(t *testing.T, sock string) {
			clienttest.StaleSocket(t, sock)
		}, "Stop", hookAck},
		{"wedged, no response expected", func(t *testing.T, sock string) {
			fakeServer(t, sock, 5*time.Second, "late", nil)
		}, "Stop", hookContext / 2},
		{"wedged, response expected", func(t *testing.T, sock string) {
			fakeServer(t, sock, 5*time.Second, "late", nil)
		}, "SessionStart", hookContext + time.Second},
		{"never says hello", func(t *testing.T, sock string) {
			clienttest.Silent(t, sock)
		}, "SessionStart", hookContext + time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, out, errb, sock := hookEnv(t, `{"hook_event_name":"`+c.event+`"}`)
			c.setup(t, sock)
			if d := runHookTimed(t, e, "--agent", "claude"); d > c.max {
				t.Errorf("took %v, limit %v", d, c.max)
			}
			if out.Len() != 0 || errb.Len() != 0 {
				t.Errorf("stdout %q stderr %q", out, errb)
			}
		})
	}
}

func TestHookIgnoresJunk(t *testing.T) {
	for _, in := range []string{"", "not json", "[1,2]", `{"hook_event_name":1}`} {
		e, out, errb, sock := hookEnv(t, in)
		fakeServer(t, sock, 0, "x", nil)
		runHookTimed(t, e, "--agent", "claude")
		if errb.Len() != 0 || (in != `{"hook_event_name":1}` && out.Len() != 0) {
			t.Errorf("%q: stdout %q stderr %q", in, out, errb)
		}
	}
	// No --agent, or not inside a session: nothing is sent.
	e, out, _, sock := hookEnv(t, `{"hook_event_name":"Stop"}`)
	got := make(chan proto.HookEventParams, 1)
	fakeServer(t, sock, 0, "x", got)
	runHookTimed(t, e)
	if out.Len() != 0 || len(got) != 0 {
		t.Fatal("sent without --agent")
	}
}

// FuzzHookInput: whatever the agent writes on stdin, tm hook exits 0 and
// stays silent on stderr.
func FuzzHookInput(f *testing.F) {
	f.Add([]byte(`{"hook_event_name":"PostToolUse","tool_name":"TaskCreate","tool_input":{"subject":"a"}}`))
	f.Add([]byte(`{"hook_event_name":"SessionStart","source":"clear"}`))
	f.Add([]byte("\xff\xfe{"))
	f.Fuzz(func(t *testing.T, in []byte) {
		var out, errb bytes.Buffer
		e := &Env{Stdin: bytes.NewReader(in), Stdout: &out, Stderr: &errb,
			Getenv: func(k string) string { return map[string]string{"TERMINATR_SESSION": "s-1"}[k] }}
		t.Setenv("TERMINATR_SOCKET", "/nonexistent/tm.sock")
		if code, _ := e.Run([]string{"hook", "--agent", "claude"}); code != 0 || errb.Len() != 0 {
			t.Fatalf("exit %d stderr %q", code, errb.String())
		}
	})
}

// SessionStart's context outlasts the 500 ms of other responses: a slow
// server inside hookContext still delivers it, and one past it does not.
func TestHookSessionStartLimit(t *testing.T) {
	if hookContext < 2*time.Second || hookContext <= hookResponse {
		t.Fatalf("hookContext %v, hookResponse %v", hookContext, hookResponse)
	}
	e, out, _, sock := hookEnv(t, `{"hook_event_name":"SessionStart","source":"clear"}`)
	fakeServer(t, sock, hookResponse+500*time.Millisecond, `{"ctx":1}`, nil)
	runHookTimed(t, e, "--agent", "claude")
	if out.String() != `{"ctx":1}` {
		t.Errorf("slow SessionStart context lost: %q", out)
	}
}

// TestHookSessionStartRetried (T493): a SessionStart the server didn't
// take (closed before its hello, or refused) is sent again, so neither
// the context nor the messaging token is lost; an event that needs no
// context is not.
func TestHookSessionStartRetried(t *testing.T) {
	for _, event := range []string{"SessionStart", "Stop"} {
		t.Run(event, func(t *testing.T) {
			e, out, _, sock := hookEnv(t, `{"hook_event_name":"`+event+`","source":"startup"}`)
			log := filepath.Join(t.TempDir(), "hook.log")
			getenv := e.Getenv
			e.Getenv = func(k string) string {
				if k == "TERMINATR_HOOK_LOG" {
					return log
				}
				return getenv(k)
			}
			ln, err := net.Listen("unix", sock)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ln.Close() })
			conns := make(chan int, 8)
			go func() {
				for n := 1; ; n++ {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					conns <- n
					if n == 1 {
						c.Close() // gone before its hello
						continue
					}
					br := bufio.NewReader(c)
					br.ReadBytes('\n')
					b, _ := json.Marshal(proto.Hello{Protocol: proto.Protocol})
					c.Write(append(b, '\n'))
					var req proto.Request
					line, _ := br.ReadBytes('\n')
					json.Unmarshal(line, &req)
					resp := proto.Response{ID: req.ID}
					if n == 2 {
						resp.Error = &proto.Error{Code: proto.ErrRefused, Message: "refused"}
					} else {
						resp.Result, _ = json.Marshal(proto.HookEventResult{Stdout: `{"ctx":1}`})
					}
					b, _ = json.Marshal(resp)
					c.Write(append(b, '\n'))
					c.Close()
				}
			}()
			runHookTimed(t, e, "--agent", "claude")
			if event == "Stop" {
				if len(conns) != 1 || out.Len() != 0 {
					t.Fatalf("Stop: %d connections, stdout %q", len(conns), out)
				}
				return
			}
			if len(conns) != 3 || out.String() != `{"ctx":1}` {
				t.Fatalf("SessionStart: %d connections, stdout %q", len(conns), out)
			}
			// Each failed try is logged, though the event arrived.
			b, _ := os.ReadFile(log)
			if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 2 ||
				!strings.Contains(lines[0], "try 1 of 3") || !strings.Contains(lines[1], "refused") {
				t.Fatalf("hook log:\n%s", b)
			}
		})
	}
}

// The event's name comes from [hook] event_field, and a [[hooks]] entry's
// timeout = "context" gives its response the longer deadline, for any
// agent (docs/SPEC.md §8.2).
func TestHookManifestWireFormat(t *testing.T) {
	e, out, _, sock := hookEnv(t, `{"type":"agent_start","id":"x"}`)
	agents := filepath.Join(os.Getenv("TERMINATR_HOME"), "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `manifest_version = 1
name = "pi"
[launch]
command = "pi"
[hook]
event_field = "type"
keep = ["id"]
[[hooks]]
event = "agent_start"
timeout = "context"
respond = "{{json .Context}}"
`
	if err := os.WriteFile(filepath.Join(agents, "pi.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got := make(chan proto.HookEventParams, 1)
	fakeServer(t, sock, hookResponse+500*time.Millisecond, `"ctx"`, got)
	runHookTimed(t, e, "--agent", "pi")
	p := <-got
	if p.Event != "agent_start" || p.Payload["type"] != "agent_start" || p.Payload["id"] != "x" {
		t.Fatalf("event %q payload %v", p.Event, p.Payload)
	}
	if out.String() != `"ctx"` {
		t.Errorf("slow context lost: %q", out)
	}
}

// TestHookLog: an event tm hook couldn't deliver is logged to
// $TERMINATR_HOOK_LOG, still silently for the agent; a delivered one
// isn't (T491: a lost SessionStart left no trace).
func TestHookLog(t *testing.T) {
	e, out, errb, sock := hookEnv(t, `{"hook_event_name":"SessionStart","session_id":"a"}`)
	log := filepath.Join(t.TempDir(), "hook.log")
	vars := map[string]string{"TERMINATR_SESSION": "s-1", "TERMINATR_HOOK_LOG": log}
	e.Getenv = func(k string) string { return vars[k] }
	runHookTimed(t, e, "--agent", "claude")
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "s-1 SessionStart: dial: ") || out.Len() != 0 || errb.Len() != 0 {
		t.Fatalf("log %q stdout %q stderr %q", b, out, errb)
	}

	os.Remove(log)
	fakeServer(t, sock, 0, "", nil)
	e.Stdin = strings.NewReader(`{"hook_event_name":"Stop","session_id":"a"}`)
	runHookTimed(t, e, "--agent", "claude")
	if b, err := os.ReadFile(log); err == nil {
		t.Fatalf("a delivered event was logged: %q", b)
	}
}
