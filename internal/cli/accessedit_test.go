package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestAccessWriters: tm access add, rm, drop, undrop and set are the
// human's, told by the server (an agent that unset its session's
// variables is still refused, and nothing is written with no server);
// the human's change config.toml, each journaled, a project's lists
// adding to all projects'; yolo on asks first.
func TestAccessWriters(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	cfg := filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml")
	run := func(who caller.Caller, ok bool, args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		e := &Env{
			Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }, Cwd: h.cwd, Caller: human,
			Identify: func() (caller.Caller, bool) { return who, ok },
		}
		code, _ := e.Run(append([]string{"access"}, args...))
		return code, out.String(), errb.String()
	}
	thr := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	writes := [][]string{
		{"add", "host", "registry.npmjs.org", "--all"},
		{"rm", "host", "registry.npmjs.org", "demo"},
		{"drop", "registry.npmjs.org", "demo"},
		{"undrop", "registry.npmjs.org", "demo"},
		{"set", "local-ports", "on", "demo"},
		{"set", "yolo", "on", "demo", "--yes"},
	}
	for _, args := range writes {
		for _, c := range []struct {
			who  caller.Caller
			ok   bool
			want string
		}{{thr, true, "human-only"}, {coord, true, "human-only"}, {human, false, "server isn't running"}} {
			if code, _, stderr := run(c.who, c.ok, args...); code == ExitOK || !strings.Contains(stderr, c.want) {
				t.Errorf("%v %v (server answers %v): exit %d, %q", args, c.who.Kind, c.ok, code, stderr)
			}
		}
	}
	if _, err := os.Stat(cfg); err == nil {
		b, _ := os.ReadFile(cfg)
		t.Fatalf("written:\n%s", b)
	}
	ok := func(args ...string) string {
		t.Helper()
		code, out, stderr := run(human, true, args...)
		if code != ExitOK {
			t.Fatalf("%v: exit %d, %q", args, code, stderr)
		}
		return out
	}
	fail := func(want string, args ...string) {
		t.Helper()
		if code, _, stderr := run(human, true, args...); code == ExitOK || !strings.Contains(stderr, want) {
			t.Errorf("%v: exit %d, %q", args, code, stderr)
		}
	}
	ok("add", "host", "registry.npmjs.org", "--all")
	ok("add", "excluded", "npx playwright *", "demo", "--without-asking")
	fail("all projects give registry.npmjs.org already", "add", "host", "registry.npmjs.org", "demo")
	fail("runs commands", "add", "allow", "Bash(make *)", "demo")
	fail("matches every command", "add", "excluded", "*", "demo")
	fail("kinds", "add", "nonsense", "x", "demo")
	ok("add", "approved", "make e2e", "demo")
	fail("every argument", "add", "approved", "make *", "demo")
	ok("add", "socket", "~/pg.sock", "demo")
	fail("pattern", "add", "socket", "~/run/*.sock", "demo")
	// A guide: its title, its text from --text or --file; --replace
	// changes it; drop and rm name it by its title.
	ok("add", "guide", "Test data", "--text", "Run make fixtures.\n", "--all")
	fail("listed already", "add", "guide", "Test data", "--text", "x", "--all")
	fail("--text <text> | --file", "add", "guide", "Style", "demo")
	fail("for tm access add guide", "add", "host", "x.dev", "--text", "x", "demo")
	fail("needs a text", "add", "guide", "Style", "--text", " ", "demo")
	fail("control or invisible", "add", "guide", "Style", "--text", "a\x1bb", "demo")
	body := filepath.Join(t.TempDir(), "style.md")
	os.WriteFile(body, []byte("Short lines.\n\tTabs kept.\n"), 0o600)
	ok("add", "guide", "Style", "--file", body, "demo")
	ok("add", "guide", "Test data", "--text", "Run make fixtures first.", "--all", "--replace")
	ok("drop", "Test data", "demo")
	if c, _ := config.Load(); !slices.Equal(c.ThreadSandbox("demo").Guides(), []config.Guide{{Title: "Style", Body: "Short lines.\n\tTabs kept."}}) ||
		!slices.Equal(c.ThreadSandbox("other").Guides(), []config.Guide{{Title: "Test data", Body: "Run make fixtures first."}}) {
		t.Errorf("guides: %q, %q", c.ThreadSandbox("demo").Guides(), c.ThreadSandbox("other").Guides())
	}
	ok("undrop", "Test data", "demo")
	ok("rm", "guide", "Style", "demo")
	ok("rm", "guide", "Test data", "--all")
	ok("drop", "registry.npmjs.org", "demo")
	fail("no rule", "drop", "nothing.dev", "demo")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ts := c.ThreadSandbox("demo")
	if len(ts.Given(config.SandboxHosts)) != 0 || strings.Join(ts.WithoutAsking(), ",") != "npx playwright *" {
		t.Errorf("demo gets %+v", ts)
	}
	ok("undrop", "registry.npmjs.org", "demo")
	if got := ts.Given(config.SandboxApproved); strings.Join(got, ",") != "make e2e" || strings.Join(ts.Given(config.SandboxSockets), ",") != "~/pg.sock" {
		t.Errorf("approved %q, sockets %q", got, ts.Given(config.SandboxSockets))
	}
	ok("rm", "approved", "make e2e", "demo")
	ok("rm", "excluded", "npx playwright *", "demo")
	fail("isn't in", "rm", "excluded", "npx playwright *", "demo")
	ok("set", "local-ports", "on", "demo")
	ok("set", "escapes", "off", "--all")
	fail("confirm with --yes", "set", "yolo", "on", "demo")
	if out := ok("set", "yolo", "on", "demo", "--yes"); !strings.Contains(out, "sandbox is off") {
		t.Errorf("yolo on: %q", out)
	}
	c, _ = config.Load()
	s, _ := c.Safety("demo")
	if ts := c.ThreadSandbox("demo"); !ts.LocalPorts || len(ts.Given(config.SandboxHosts)) != 1 || len(ts.Given(config.SandboxExcluded)) != 0 || !s.Yolo || s.SandboxEscapes {
		t.Errorf("demo: %+v %+v", ts, s)
	}
	p, _ := project.Open("demo")
	lines, _, _ := p.JournalTail(20)
	j := strings.Join(lines, "\n")
	for _, want := range []string{"settings.sandbox demo hosts + registry.npmjs.org (all projects)", "excluded_commands + npx playwright * (ask = false)", "drop + registry.npmjs.org", "drop - registry.npmjs.org", "settings.yolo demo yolo = true", "settings.sandbox_escapes demo sandbox_escapes = false"} {
		if !strings.Contains(j, want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}
