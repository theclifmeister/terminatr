package server

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// humanOnlyMethods are the methods proto.HumanOnly names.
var humanOnlyMethods = []string{
	proto.MethodProjectRename, proto.MethodProjectActive,
	proto.MethodModViews, proto.MethodModViewInput, proto.MethodModCommand, proto.MethodModRestart,
	proto.MethodNotifyClaim, proto.MethodNotifyDismiss,
	proto.MethodModActionCheck, proto.MethodModActionDone,
}

// onHumanSocketOnly reports whether err is the server socket's refusal
// of a human-only method.
func onHumanSocketOnly(err error) bool {
	var perr *proto.Error
	return errors.As(err, &perr) && perr.Code == "human-only" && strings.Contains(perr.Message, ipc.HumanName)
}

// TestHumanSocket: the server listens on the human socket beside its
// own; the human-only methods, mod.action.check's press tokens among
// them, answer there alone, and every other method on both. A control
// Dial takes the human socket.
func TestHumanSocket(t *testing.T) {
	p := testPaths(t)
	t.Setenv(caller.EnvSession, "") // the human's, even under an agent's tm
	startServer(t, p)
	if fi, err := os.Stat(p.HumanSocket()); err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("human socket %s: %v %v", p.HumanSocket(), fi, err)
	}

	srv, err := dialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	human, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	if got := human.conn.RemoteAddr().String(); got != p.HumanSocket() {
		t.Fatalf("Dial connected to %q, want the human socket %q", got, p.HumanSocket())
	}

	for _, m := range humanOnlyMethods {
		if !proto.HumanOnly(m) {
			t.Errorf("proto.HumanOnly(%q) = false", m)
		}
		if err := srv.Call(m, struct{}{}, nil); !onHumanSocketOnly(err) {
			t.Errorf("%s on the server socket: %v, want the human-only refusal", m, err)
		}
		// The handler answers: what it says of empty params doesn't
		// matter here, only that it isn't the socket's refusal.
		if err := human.Call(m, struct{}{}, nil); onHumanSocketOnly(err) {
			t.Errorf("%s on the human socket: %v", m, err)
		}
	}
	// The connection outlives a refusal, and the rest answer on both.
	for _, c := range []*Client{srv, human} {
		if err := c.Call(proto.MethodPing, nil, nil); err != nil {
			t.Errorf("ping: %v", err)
		}
	}
	// An agent's session doesn't try the human socket.
	t.Setenv(caller.EnvSession, "s-1")
	t.Setenv(caller.EnvRole, string(caller.Thread))
	agent, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if got := agent.conn.RemoteAddr().String(); got != p.Socket {
		t.Errorf("an agent's Dial connected to %q, want %q", got, p.Socket)
	}
	if proto.HumanOnly(proto.MethodCLIRun) || proto.HumanOnly(proto.MethodCallerWho) {
		t.Error("an agent's own methods are human-only")
	}
}

// TestHumanSocketFallback: where the human socket can't be reached (an
// agent's sandbox refuses it, an older server has none) a control Dial
// uses the server socket, which still serves everything but the
// human-only methods.
func TestHumanSocketFallback(t *testing.T) {
	p := testPaths(t)
	t.Setenv(caller.EnvSession, "")
	startServer(t, p)
	if err := os.Remove(p.HumanSocket()); err != nil {
		t.Fatal(err)
	}
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatalf("Dial without the human socket: %v", err)
	}
	defer c.Close()
	if got := c.conn.RemoteAddr().String(); got != p.Socket {
		t.Fatalf("Dial connected to %q, want %q", got, p.Socket)
	}
	if err := c.Call(proto.MethodPing, nil, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := c.Call(proto.MethodModActionCheck, proto.ModActionParams{Mod: "m", Action: "a"}, nil); !onHumanSocketOnly(err) {
		t.Fatalf("mod.action.check on the server socket: %v", err)
	}
}
