package cli

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/service"
)

// TestSrvCode: a server command's error is printed with its subcommand
// and mapped to an exit code in one place, Env.report.
func TestSrvCode(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{&usageError{"bad"}, ExitUsage},
		{&proto.Error{Code: proto.ErrBadParams, Message: "bad"}, ExitUsage},
		{&proto.Error{Code: proto.ErrRefused, Message: "no"}, ExitRefused},
		{&proto.Error{Code: proto.ErrUnknownSession, Message: "gone"}, ExitRefused},
		{&proto.Error{Code: "internal", Message: "x"}, ExitIO},
		{fmt.Errorf("wrapped: %w", service.ErrNoConsole), ExitRefused},
		{&proto.MismatchError{}, ExitIO},
		{errors.New("dial: refused"), ExitIO},
	} {
		var errb bytes.Buffer
		e := &Env{Stderr: &errb}
		if got := e.report("tm session", srvFail("session start", c.err)); got != c.code {
			t.Errorf("%v: exit %d, want %d", c.err, got, c.code)
		}
		if want := "tm session start: " + c.err.Error() + "\n"; errb.String() != want {
			t.Errorf("printed %q, want %q", errb.String(), want)
		}
	}
	var errb bytes.Buffer
	if got := (&Env{Stderr: &errb}).report("tm", srvUsage("watch", "no session")); got != ExitUsage || errb.String() != "tm watch: no session\n" {
		t.Errorf("srvUsage: exit %d, printed %q", got, errb.String())
	}
}

// TestFlagsStopAtArg: with stopAtArg the first positional argument ends
// the flags, so a command's own flags reach it.
func TestFlagsStopAtArg(t *testing.T) {
	f := newFlags()
	f.stopAtArg = true
	cwd := f.String("cwd")
	pos, err := f.Parse([]string{"--cwd", "/x", "bash", "-l", "--cwd", "y"})
	if err != nil || *cwd != "/x" || !reflect.DeepEqual(pos, []string{"bash", "-l", "--cwd", "y"}) {
		t.Errorf("pos %q, cwd %q, err %v", pos, *cwd, err)
	}
}

// TestSessionKeysParams: --enter counts wherever it is (REF-03: the
// stdlib parser stopped at the first word and typed "--enter"); a --
// types what follows literally.
func TestSessionKeysParams(t *testing.T) {
	for _, c := range []struct {
		args []string
		want proto.SessionKeysParams
	}{
		{[]string{"s-1", "hello", "--enter"}, proto.SessionKeysParams{ID: "s-1", Data: "hello\r"}},
		{[]string{"s-1", "--enter", "hello", "world"}, proto.SessionKeysParams{ID: "s-1", Data: "hello world\r"}},
		{[]string{"--enter", "s-1"}, proto.SessionKeysParams{ID: "s-1", Data: "\r"}},
		{[]string{"s-1", "--", "ls", "-la", "--enter"}, proto.SessionKeysParams{ID: "s-1", Data: "ls -la --enter"}},
	} {
		got, err := keysParams(c.args)
		if err != nil || got != c.want {
			t.Errorf("%q: %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	for _, args := range [][]string{nil, {"s-1"}, {"s-1", "-x"}} {
		if _, err := keysParams(args); err == nil {
			t.Errorf("%q: no error", args)
		}
	}
}

// serverHumanCommands are the server commands only the human runs. None
// of them reaches a server here: each test has a scratch home of its own,
// with no server in it.
var serverHumanCommands = [][]string{
	{"server", "stop"},
	{"server", "stop", "--yes"},
	{"server", "stop", "--force"},
	{"server", "restart"},
	{"server", "restart", "--yes"},
	{"server", "restart", "--no-launchd"},
	{"server", "service", "install"},
	{"server", "service", "uninstall"},
	{"server", "service", "install", "--print"},
}

// TestServerCommandsHumanOnly: every agent's stop, restart and service
// are refused as the human's, with where the user runs them for those in
// a pane, and nothing is run; the agent's other server commands stay.
func TestServerCommandsHumanOnly(t *testing.T) {
	h := newHarness(t)
	serviceRun = func(name string, args ...string) error {
		t.Errorf("ran %s %v", name, args)
		return nil
	}
	t.Cleanup(func() { serviceRun = nil })
	director := caller.Caller{Kind: caller.Director, Project: "prog"}
	for _, c := range []caller.Caller{coord, thr, director, caller.ForMod("m"), caller.ForPeer("mini")} {
		for _, args := range serverHumanCommands {
			code, out, errs := h.run(c, "", args...)
			if code != ExitRefused || out != "" || !strings.Contains(errs, "human-only") || !strings.Contains(errs, "the user's, in their own terminal") {
				t.Errorf("%s %v: exit %d, stdout %q, stderr %q", c.Kind, args, code, out, errs)
			}
			if c.Kind != caller.Mod && c.Kind != caller.Peer && !strings.Contains(errs, "in the "+string(c.Kind)+"'s session") {
				t.Errorf("%s %v: no pane hint: %q", c.Kind, args, errs)
			}
		}
		// Its status, help and usage errors are as before.
		if code, _, errs := h.run(c, "", "server", "status"); code != ExitRefused || strings.Contains(errs, "human-only") {
			t.Errorf("%s status: exit %d, stderr %q", c.Kind, code, errs)
		}
		h.expect(ExitUsage, "unknown flag", c, "server", "stop", "--nope")
		if code, out, _ := h.run(c, "", "server", "stop", "--help"); code != ExitOK || !strings.Contains(out, "usage: tm server") {
			t.Errorf("%s stop --help: exit %d, stdout %q", c.Kind, code, out)
		}
	}
}

// TestServerCommandsHuman: the human's stop and service run as before
// (restart, which goes on to start a server, is TestRestartServer's).
func TestServerCommandsHuman(t *testing.T) {
	h := newHarness(t)
	ran := 0
	serviceRun = func(string, ...string) error { ran++; return nil }
	t.Cleanup(func() { serviceRun = nil })
	for _, args := range [][]string{{"server", "stop"}, {"server", "stop", "--yes"}, {"server", "stop", "--force"}} {
		if out := h.ok(human, args...); out != "not running\n" {
			t.Errorf("%v: %q", args, out)
		}
	}
	if out := h.ok(human, "server", "service", "install", "--print"); !strings.Contains(out, "server") {
		t.Errorf("--print: %q", out)
	}
	if ran != 0 {
		t.Errorf("--print ran the service manager %d times", ran)
	}
	if out := h.ok(human, "server", "service", "uninstall"); !strings.Contains(out, "removed") {
		t.Errorf("uninstall: %q", out)
	}
}

// TestServerHumanAsksTheServer: an agent whose session's variables are
// gone is still refused when the server knows it; with no answer, in time
// or at all, the environment decides, so the user can stop a hung server.
func TestServerHumanAsksTheServer(t *testing.T) {
	newHarness(t)
	run := func(identify func() (caller.Caller, bool), args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		e := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getenv: func(string) string { return "" },
			Cwd: t.TempDir(), Caller: human, Identify: identify}
		code, ok := e.Run(args)
		if !ok {
			t.Fatalf("not handled: %v", args)
		}
		return code, out.String(), errb.String()
	}
	run(nil, "server", "stop") // makes the run folder --force looks in
	for _, c := range []caller.Caller{thr, coord} {
		code, out, errs := run(func() (caller.Caller, bool) { return c, true }, "server", "stop", "--force")
		if code != ExitRefused || out != "" || !strings.Contains(errs, "human-only") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", c.Kind, code, out, errs)
		}
	}
	old := identifyWait
	identifyWait = 10 * time.Millisecond
	t.Cleanup(func() { identifyWait = old })
	hung := make(chan struct{})
	t.Cleanup(func() { close(hung) })
	for name, identify := range map[string]func() (caller.Caller, bool){
		"human":   func() (caller.Caller, bool) { return human, true },
		"no one":  func() (caller.Caller, bool) { return caller.Caller{}, false },
		"silence": func() (caller.Caller, bool) { <-hung; return thr, true },
	} {
		if code, out, errs := run(identify, "server", "stop", "--force"); code != ExitOK || out != "not running\n" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, code, out, errs)
		}
	}
}

// TestServerCommandUsage: the server, session, agent and watch families
// refuse bad command lines with exit 2 and the subcommand's name, before
// any server is asked.
func TestServerCommandUsage(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		want string
		args []string
	}{
		{"usage: tm session list", []string{"session"}},
		{"tm session list: unknown flag --bogus", []string{"session", "list", "--bogus"}},
		{"tm session start: pass --agent or a command, not both", []string{"session", "start", "--agent", "claude", "bash"}},
		{"tm session start: --brief, --kickoff", []string{"session", "start", "--model", "m", "bash"}},
		{"tm session start: size out of range", []string{"session", "start", "--cols", "70000"}},
		{"tm session start: --rows takes a number", []string{"session", "start", "--rows", "x"}},
		{"tm session read: usage", []string{"session", "read", "s-1", "s-2"}},
		{"tm session keys: usage", []string{"session", "keys", "s-1"}},
		{"tm session wait: --timeout takes a duration", []string{"session", "wait", "s-1", "--timeout", "soon"}},
		{"tm session wait: usage", []string{"session", "wait"}},
		{"tm session stop: usage", []string{"session", "stop"}},
		{"tm server: usage", []string{"server", "bogus"}},
		{"tm server start: usage", []string{"server", "start", "x"}},
		{"tm server stop: unknown flag", []string{"server", "stop", "--nope"}},
		{"tm server service: usage", []string{"server", "service", "install", "x"}},
		{"tm agent explain: usage", []string{"agent", "explain"}},
		{"tm agent check: usage", []string{"agent", "check"}},
		{"tm agent: usage", []string{"agent"}},
		{"tm watch: usage", []string{"watch", "--session", "s-1", "--project", "demo"}},
		{"tm attach: unknown flag -x", []string{"attach", "-x"}},
		{"tm doctor: usage", []string{"doctor", "x"}},
		{"tm update: usage", []string{"update", "--json"}},
	} {
		h.expect(ExitUsage, c.want, human, c.args...)
	}
}
