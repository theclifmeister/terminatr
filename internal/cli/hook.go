package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	// hookTries and hookRetry: a "context" event (SessionStart) the server
	// didn't take (no connection, closed, refused) is sent again, within
	// hookContext: lost, it would leave the session without its context,
	// and the server without the agent's messaging token (T493).
	hookTries = 3
	hookRetry = 100 * time.Millisecond
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
	total, tries := hookAck, 1
	for _, h := range hooks {
		if h.Event != event || h.Respond == "" {
			continue
		}
		total = max(total, hookResponse)
		if h.Timeout == agent.HookTimeoutContext {
			total, tries = max(total, hookContext), hookTries
		}
	}
	params := proto.HookEventParams{Session: session, Agent: name, Event: event,
		Payload: payload, Token: token}
	start := time.Now()
	out := sendHook(paths.Socket, params, total, tries, func(try int, err error) {
		hookLog(e, session, event, fmt.Errorf("%w (try %d of %d, after %v of %v)", err, try, tries, time.Since(start).Round(time.Millisecond), total))
	})
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
// agent. Every failure (no server, a stale socket, a wedged server, a
// protocol mismatch, a refusal) returns "" and goes to failed, never to
// the agent. One the server didn't take is tried again, up to tries times
// in all, while total lasts.
func sendHook(socket string, p proto.HookEventParams, total time.Duration, tries int, failed func(try int, err error)) string {
	deadline := time.Now().Add(total)
	for i := 1; ; i++ {
		out, again, err := sendHookOnce(socket, p, deadline)
		if err == nil {
			return out
		}
		failed(i, err)
		if !again || i >= tries || time.Until(deadline) < hookRetry+hookDial {
			return ""
		}
		time.Sleep(hookRetry)
	}
}

// sendHookOnce is one try of sendHook. again means the server didn't
// take the event: no connection, a connection closed before its answer,
// or a refusal. A timeout is not: the deadline is spent.
func sendHookOnce(socket string, p proto.HookEventParams, deadline time.Time) (out string, again bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), hookDial)
	c, err := ipc.Dial(ctx, ipc.Addr(socket))
	cancel()
	if err != nil {
		return "", true, fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	// The write shares the total: a write of its own deadline (100 ms)
	// lost events on a loaded Windows machine, where a socket write
	// completes through the runtime's poller (T217).
	c.SetDeadline(deadline)
	params, err := json.Marshal(p)
	if err != nil {
		return "", false, err
	}
	// Hello and request in one write: one round trip less.
	var msg bytes.Buffer
	proto.WriteLine(&msg, proto.Hello{Protocol: proto.Protocol, Version: version.Version, Build: version.BuildID(), Kind: proto.KindHook})
	proto.WriteLine(&msg, proto.Request{ID: 1, Method: proto.MethodHookEvent, Params: params})
	if _, err := c.Write(msg.Bytes()); err != nil {
		return "", !isTimeout(err), fmt.Errorf("write: %w", err)
	}
	br := bufio.NewReader(c)
	var srv proto.Hello
	if err := proto.ReadLine(br, &srv); err != nil {
		return "", readAgain(err), fmt.Errorf("server hello: %w", err)
	}
	var resp proto.Response
	if err := proto.ReadLine(br, &resp); err != nil {
		return "", readAgain(err), fmt.Errorf("response: %w", err)
	}
	if resp.Error != nil {
		return "", true, fmt.Errorf("response: %w", resp.Error)
	}
	var res proto.HookEventResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return "", false, fmt.Errorf("result: %w", err)
	}
	return res.Stdout, false, nil
}

// readAgain reports whether a failed proto.ReadLine is worth another
// try: a connection that failed, not a deadline or a line that isn't
// the answer.
func readAgain(err error) bool {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	if errors.As(err, &se) || errors.As(err, &te) || errors.Is(err, proto.ErrLineTooLong) {
		return false
	}
	return !isTimeout(err)
}

// isTimeout reports whether err is a deadline's.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
