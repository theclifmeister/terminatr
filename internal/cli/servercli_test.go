package cli

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

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
