package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// TestAccessPresetAddChecked: tm access preset add writes config.toml
// from the caller's process, so who calls is the server's word: an
// agent that unset its session's variables is still refused, and with
// no server to ask the write is refused too; the human's goes through.
func TestAccessPresetAddChecked(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	thr := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	run := func(who caller.Caller, ok bool) (int, string) {
		var out, errb bytes.Buffer
		e := &Env{
			Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }, Cwd: h.cwd, Caller: human,
			Identify: func() (caller.Caller, bool) { return who, ok },
		}
		code, _ := e.Run([]string{"access", "preset", "add", "browser-downloads", "--all"})
		return code, errb.String()
	}
	for _, c := range []struct {
		who  caller.Caller
		ok   bool
		want string
	}{{thr, true, "human-only"}, {coord, true, "human-only"}, {human, false, "server isn't running"}} {
		code, stderr := run(c.who, c.ok)
		if code == ExitOK || !strings.Contains(stderr, c.want) {
			t.Errorf("%v (server answers %v): exit %d, %q", c.who.Kind, c.ok, code, stderr)
		}
		if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "playwright") {
			t.Fatalf("written:\n%s", b)
		}
	}
	if code, stderr := run(human, true); code != ExitOK {
		t.Fatalf("human: exit %d, %q", code, stderr)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), "cdn.playwright.dev") {
		t.Errorf("not written:\n%s", b)
	}
}
