package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Hook deadlines (docs/SPEC.md §8.2). The hook runs synchronously inside
// the agent, so it must never hold the agent up, even with a wedged
// server.
var (
	hookDial     = 50 * time.Millisecond
	hookAck      = 250 * time.Millisecond // total, for events nobody answers
	hookResponse = 500 * time.Millisecond // total, when a response is expected
	// hookContext is the total for a [[hooks]] entry with timeout =
	// "context" (SessionStart): its response is the session's brief and
	// context, and a loaded machine after /clear can miss 500 ms, which
	// would leave the session without them.
	hookContext = 3 * time.Second
)

// maxHookPayload bounds what tm hook reads from the agent.
const maxHookPayload = 4 << 20

// hookLogEnv names a file tm hook appends a line to for each event it
// failed to deliver, and why (tests: the agent never shows it, and a
// lost SessionStart costs the session its context and prompt token).
const hookLogEnv = "TERMINATR_HOOK_LOG"

func init() { commands["hook"] = runHook }

// runHook is `tm hook --agent NAME`: the endpoint every generated hook
// file calls. It reads the payload on stdin, trims it with the agent's
// [hook] rules, sends it to the server over a stream connection and
// prints the server's response, if any. It always exits 0 and never
// writes to stderr: a failing hook shows up as an error in the agent.
func runHook(e *Env, args []string) error {
	name := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--agent" && i+1 < len(args):
			name = args[i+1]
			i++
		case len(a) > 8 && a[:8] == "--agent=":
			name = a[8:]
		}
	}
	session := e.Getenv("TERMINATR_SESSION")
	data, _ := io.ReadAll(io.LimitReader(e.Stdin, maxHookPayload))
	if name == "" || session == "" {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return nil
	}
	paths, err := client.ResolvePaths()
	if err != nil {
		hookLog(e, session, "", err)
		return nil
	}
	// The event's name is in [hook] event_field (default hook_event_name),
	// and its [[hooks]] entries say whether a response is expected and how
	// long to wait for it (timeout).
	var hook agent.HookTrim
	var hooks []agent.HookMap
	token := ""
	if reg, _ := agent.Load(paths.AgentsDir()); reg != nil {
		if a, ok := reg.Get(name); ok {
			hook = a.Sources().Hook
			if m := agent.ManifestOf(a); m != nil {
				if m.Inject.TokenEnv != "" {
					token = e.Getenv(m.Inject.TokenEnv)
				}
				hooks = m.Hooks
			}
		}
	}
	event, _ := payload[hook.EventField()].(string)
	payload = hook.Trim(payload)
	total := hookAck
	for _, h := range hooks {
		if h.Event != event || h.Respond == "" {
			continue
		}
		total = max(total, hookResponse)
		if h.Timeout == agent.HookTimeoutContext {
			total = max(total, hookContext)
		}
	}
	params := proto.HookEventParams{Session: session, Agent: name, Event: event,
		Payload: payload, Token: token}
	start := time.Now()
	out, err := sendHook(paths.Socket, params, total)
	if err != nil {
		hookLog(e, session, event, fmt.Errorf("%w (after %v of %v)", err, time.Since(start).Round(time.Millisecond), total))
	}
	if out != "" {
		io.WriteString(e.Stdout, out)
	}
	return nil
}

// hookLog appends why event wasn't delivered to $TERMINATR_HOOK_LOG, if
// set; never to stderr.
func hookLog(e *Env, session, event string, err error) {
	path := e.Getenv(hookLogEnv)
	if path == "" {
		return
	}
	f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if ferr != nil {
		return
	}
	fmt.Fprintf(f, "%s pid %d %s %s: %v\n", time.Now().Format("15:04:05.000000"), os.Getpid(), session, event, err)
	f.Close()
}

// sendHook delivers one event and returns the server's stdout for the
// agent, or why it couldn't: no server, a stale socket, a wedged server,
// a protocol mismatch, a refusal. The agent never sees the error.
func sendHook(socket string, p proto.HookEventParams, total time.Duration) (string, error) {
	deadline := time.Now().Add(total)
	ctx, cancel := context.WithTimeout(context.Background(), hookDial)
	c, err := ipc.Dial(ctx, ipc.Addr(socket))
	cancel()
	if err != nil {
		return "", fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	// The write shares the total: a write of its own deadline (100 ms)
	// lost events on a loaded Windows machine, where a socket write
	// completes through the runtime's poller (T217).
	c.SetDeadline(deadline)
	hello, _ := json.Marshal(proto.Hello{Protocol: proto.Protocol, Version: version.Version, Build: version.BuildID(), Kind: proto.KindHook})
	params, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	req, _ := json.Marshal(proto.Request{ID: 1, Method: proto.MethodHookEvent, Params: params})
	// Hello and request in one write: one round trip less.
	if _, err := c.Write(append(append(append(hello, '\n'), req...), '\n')); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	br := bufio.NewReader(c)
	var srv proto.Hello
	line, err := br.ReadBytes('\n')
	if err == nil {
		err = json.Unmarshal(line, &srv)
	}
	if err != nil {
		return "", fmt.Errorf("server hello: %w", err)
	}
	var resp proto.Response
	if line, err = br.ReadBytes('\n'); err == nil {
		err = json.Unmarshal(line, &resp)
	}
	if err == nil && resp.Error != nil {
		err = resp.Error
	}
	if err != nil {
		return "", fmt.Errorf("response: %w", err)
	}
	var res proto.HookEventResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return "", fmt.Errorf("result: %w", err)
	}
	return res.Stdout, nil
}
