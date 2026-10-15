package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const sessionUsage = `usage: tm session list [--json]
       tm session start [--cwd DIR] [--cols N --rows N] [-- COMMAND ARGS…]
       tm session start --agent NAME [--cwd DIR] [--brief FILE] [--kickoff TEXT] [--model M] [--yolo]
       tm session stop ID
       tm session read ID [--scrollback] [--json]   (the screen as plain text)
       tm session keys ID [--enter] [--] [TEXT…]   (types TEXT literally; --enter presses Enter)
       tm session prompt ID TEXT   (pasted once the agent is idle)
       tm session wait ID [--state S[,S…]] [--timeout 30s]
       tm session ask ID < QUESTION.json   (for mods: waits for the answers)`

// sessionCmd implements `tm session …`: plain sessions outside projects.
func sessionCmd(e *Env, args []string) error {
	if len(args) == 0 {
		return srvUsage("session", sessionUsage)
	}
	switch args[0] {
	case "list", "ls":
		return sessionList(e, args[1:])
	case "start":
		return sessionStart(e, args[1:])
	case "stop":
		return sessionStop(e, args[1:])
	case "read":
		return sessionRead(e, args[1:])
	case "keys":
		return sessionKeys(e, args[1:])
	case "prompt":
		return sessionPrompt(e, args[1:])
	case "wait":
		return sessionWait(e, args[1:])
	case "ask":
		return sessionAsk(e, args[1:])
	}
	return srvUsage("session", sessionUsage)
}

func sessionList(e *Env, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	if _, err := f.Parse(args); err != nil {
		return srvFail("session list", err)
	}
	c, _, err := connect(true)
	if err != nil {
		return srvFail("session list", err)
	}
	defer c.Close()
	var res proto.SessionListResult
	if err := c.Call(proto.MethodSessionList, nil, &res); err != nil {
		return srvFail("session list", err)
	}
	if *asJSON {
		return e.printJSON(res.Sessions)
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range res.Sessions {
		what := shown(strings.Join(s.Argv, " "))
		if s.Agent != "" {
			what = s.Agent + " " + stateLine(s)
		}
		// A thread's session names its task, the thread id in brackets.
		role := s.Role
		switch {
		case s.Task != "":
			role += " " + s.Task + " (" + s.Thread + ")"
		case s.Thread != "":
			role += " " + s.Thread
		}
		fmt.Fprintf(tw, "%s\t%s\tpid %d\t%d×%d\t%s\t%s\t%s\n", s.ID, shown(role), s.PID, s.Cols, s.Rows,
			time.Since(s.Created).Round(time.Second), what, shown(s.Cwd))
	}
	tw.Flush()
	return nil
}

func sessionStart(e *Env, args []string) error {
	f := newFlags()
	// The command to run is the first positional argument: its own
	// flags are its own (tm session start bash -l).
	f.stopAtArg = true
	cwd := f.String("cwd")         // working directory (default: the current one)
	colsArg := f.String("cols")    // columns (default: this terminal's, or 80)
	rowsArg := f.String("rows")    // rows (default: this terminal's, or 24)
	agentName := f.String("agent") // agent to run, from tm agent list (instead of a command)
	brief := f.String("brief")     // file given to the agent as its brief (system prompt)
	kickoff := f.String("kickoff") // the agent's first prompt
	model := f.String("model")     // the agent's model
	yolo := f.Bool("yolo")         // skip the agent's own permission prompts
	role := f.String("role")       // coordinator, thread or shell (default)
	project := f.String("project") // project slug, for coordinator and thread roles
	thread := f.String("thread")   // thread id, for the thread role
	argv, err := f.Parse(args)
	if err != nil {
		return srvFail("session start", err)
	}
	if *agentName != "" && len(argv) > 0 {
		return srvUsage("session start", "pass --agent or a command, not both")
	}
	if *agentName == "" && (*brief != "" || *kickoff != "" || *model != "" || *yolo || *role != "") {
		return srvUsage("session start", "--brief, --kickoff, --model, --yolo and --role need --agent")
	}
	if *yolo && e.Caller.IsAgent() {
		// Turning permission prompts off is the human's call (§11.2).
		fmt.Fprintln(e.Stderr, "tm session start: coordinator-only: --yolo is a human call")
		return &exitError{ExitRefused}
	}
	cols, err := sizeArg("cols", *colsArg)
	if err != nil {
		return srvFail("session start", err)
	}
	rows, err := sizeArg("rows", *rowsArg)
	if err != nil {
		return srvFail("session start", err)
	}
	p := proto.SessionStartParams{Argv: argv, Cols: cols, Rows: rows,
		Agent: *agentName, Kickoff: *kickoff, Model: *model, Yolo: *yolo,
		Role: *role, Project: *project, Thread: *thread}
	if *brief != "" {
		abs, err := filepath.Abs(*brief)
		if err != nil {
			return srvUsage("session start", err.Error())
		}
		p.Brief = abs
	}
	if p.Cols == 0 || p.Rows == 0 {
		if c, r, ok := termSize(); ok {
			p.Cols, p.Rows = c, r
		}
	}
	dir := *cwd
	if dir == "" {
		dir = e.Cwd
	}
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return srvUsage("session start", err.Error())
		}
		p.Cwd = abs
	}
	c, _, err := connect(true)
	if err != nil {
		return srvFail("session start", err)
	}
	defer c.Close()
	var res proto.SessionStartResult
	if err := c.Call(proto.MethodSessionStart, p, &res); err != nil {
		return srvFail("session start", err)
	}
	fmt.Fprintln(e.Stdout, res.Session.ID)
	return nil
}

// sizeArg is a --cols or --rows value; "" is 0, the default.
func sizeArg(name, v string) (uint16, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, usagef("--%s takes a number", name)
	}
	if n > 0xffff {
		return 0, usagef("size out of range")
	}
	return uint16(n), nil
}

func sessionStop(e *Env, args []string) error {
	if len(args) != 1 {
		return srvUsage("session stop", "usage: tm session stop ID")
	}
	c, _, err := connect(true)
	if err != nil {
		return srvFail("session stop", err)
	}
	defer c.Close()
	err = c.Call(proto.MethodSessionStop, proto.SessionIDParams{ID: args[0]}, nil)
	var perr *proto.Error
	if errors.As(err, &perr) && perr.Code == proto.ErrUnknownSession {
		// Idempotent: a session that is gone is stopped.
		fmt.Fprintf(e.Stdout, "%s not running\n", args[0])
		return nil
	}
	if err != nil {
		return srvFail("session stop", err)
	}
	fmt.Fprintf(e.Stdout, "%s stopped\n", args[0])
	return nil
}

func sessionRead(e *Env, args []string) error {
	f := newFlags()
	scrollback := f.Bool("scrollback") // include the scrollback
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail("session read", err)
	}
	if len(pos) != 1 {
		return srvUsage("session read", "usage: tm session read ID [--scrollback] [--json]")
	}
	id := pos[0]
	c, _, err := connect(true)
	if err != nil {
		return srvFail("session read", err)
	}
	defer c.Close()
	var res proto.SessionReadResult
	if err := c.Call(proto.MethodSessionRead, proto.SessionReadParams{ID: id, Scrollback: *scrollback}, &res); err != nil {
		return srvFail("session read", err)
	}
	if *asJSON {
		return e.printJSON(res)
	}
	fmt.Fprintln(e.Stdout, strings.TrimRight(res.Text, "\n"))
	return nil
}

func sessionKeys(e *Env, args []string) error {
	p, err := keysParams(args)
	if err != nil {
		return srvFail("session keys", err)
	}
	c, _, err := connect(true)
	if err != nil {
		return srvFail("session keys", err)
	}
	defer c.Close()
	if err := c.Call(proto.MethodSessionKeys, p, nil); err != nil {
		return srvFail("session keys", err)
	}
	return nil
}

// keysParams parses tm session keys' arguments: the session, then the
// words to type, joined by spaces, and --enter anywhere. Text that
// starts with - follows a --.
func keysParams(args []string) (proto.SessionKeysParams, error) {
	f := newFlags()
	// Key names are not parsed: 'enter' types e-n-t-e-r.
	enter := f.Bool("enter")
	words, err := f.Parse(args)
	if err != nil {
		return proto.SessionKeysParams{}, err
	}
	if len(words) == 0 || (len(words) == 1 && !*enter) {
		return proto.SessionKeysParams{}, usagef("usage: tm session keys ID [--enter] [--] [TEXT…]   (TEXT is typed literally; --enter presses Enter)")
	}
	p := proto.SessionKeysParams{ID: words[0], Data: strings.Join(words[1:], " ")}
	if *enter {
		p.Data += "\r"
	}
	return p, nil
}

// stateLine is an agent session's state for listings: state/reason,
// todo progress and the current item.
func stateLine(s proto.SessionInfo) string {
	st := s.State
	if st == "" {
		st = "unknown"
	}
	if s.Reason != "" {
		st += "/" + s.Reason
	}
	if s.StateSources != "" {
		st += " (" + s.StateSources + ")"
	}
	if s.TodosTotal > 0 {
		st += fmt.Sprintf(" %d/%d todos", s.TodosDone, s.TodosTotal)
	}
	if s.Current != "" {
		st += " ▸ " + s.Current
	}
	if s.Queued > 0 {
		st += fmt.Sprintf(" · %d queued", s.Queued)
		if n := s.QueueNote(time.Now()); n != "" {
			st += " (" + n + ")"
		}
	}
	return shown(st)
}

func sessionPrompt(e *Env, args []string) error {
	if len(args) < 2 {
		return srvUsage("session prompt", "usage: tm session prompt ID TEXT")
	}
	id, text := args[0], strings.Join(args[1:], " ")
	if text == "-" {
		b, err := e.readArg("-")
		if err != nil {
			return srvUsage("session prompt", err.Error())
		}
		text = strings.TrimRight(b, "\n")
	}
	c, _, err := connect(false)
	if err != nil {
		return srvFail("session prompt", err)
	}
	defer c.Close()
	var res proto.SessionPromptResult
	if err := c.Call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: id, Text: text}, &res); err != nil {
		return srvFail("session prompt", err)
	}
	switch res.Via {
	case "channel":
		fmt.Fprintf(e.Stdout, "%s: sent\n", id)
	default:
		fmt.Fprintf(e.Stdout, "%s: queued (pasted once the agent is idle)\n", id)
	}
	return nil
}

func sessionWait(e *Env, args []string) error {
	f := newFlags()
	states := f.String("state")       // comma-separated states to wait for (default: any change)
	timeoutArg := f.String("timeout") // give up after this long (exit 1)
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail("session wait", err)
	}
	if len(pos) != 1 {
		return srvUsage("session wait", "usage: tm session wait ID [--state S[,S…]] [--timeout 30s]")
	}
	id, timeout := pos[0], 30*time.Second
	if f.IsSet("timeout") {
		if timeout, err = time.ParseDuration(*timeoutArg); err != nil {
			return srvUsage("session wait", "--timeout takes a duration, such as 30s")
		}
	}
	p := proto.SessionWaitParams{ID: id, TimeoutMS: int(timeout.Milliseconds())}
	if *states != "" {
		p.States = strings.Split(*states, ",")
	}
	c, _, err := connect(false)
	if err != nil {
		return srvFail("session wait", err)
	}
	defer c.Close()
	var res proto.SessionWaitResult
	if err := c.Call(proto.MethodSessionWait, p, &res); err != nil {
		return srvFail("session wait", err)
	}
	st := res.State
	if res.Reason != "" {
		st += "/" + res.Reason
	}
	fmt.Fprintln(e.Stdout, st)
	if res.TimedOut {
		fmt.Fprintf(e.Stderr, "tm session wait: timed out after %v\n", timeout)
		return &exitError{ExitRefused}
	}
	return nil
}

// sessionAsk implements `tm session ask ID` (docs/SPEC.md §3.3, Ask), the
// mod's side of an open question: it reads the question menu as JSON
// (proto.Question) on stdin, keeps it open on the session until every
// question has an answer through `tm thread answer`, and prints the
// answers as one JSON object by question text. It exits 1 with nothing
// printed when the question closed without answers; killing it takes the
// question down.
func sessionAsk(e *Env, args []string) error {
	const usage = "usage: tm session ask ID < QUESTION.json"
	if len(args) != 1 {
		return srvUsage("session ask", usage)
	}
	var q proto.Question
	if err := json.NewDecoder(e.Stdin).Decode(&q); err != nil {
		return srvUsage("session ask", "the question on stdin: "+err.Error()+"\n"+usage)
	}
	p, err := client.ResolvePaths()
	if err != nil {
		return srvFail("session ask", err)
	}
	ans, err := client.AskSession(p, args[0], q)
	if err != nil {
		return srvFail("session ask", err)
	}
	if ans == nil {
		fmt.Fprintln(e.Stderr, "tm session ask: the question closed unanswered")
		return &exitError{ExitRefused}
	}
	b, _ := json.Marshal(ans)
	if _, err := e.Stdout.Write(append(b, '\n')); err != nil {
		return &exitError{ExitIO}
	}
	return nil
}
