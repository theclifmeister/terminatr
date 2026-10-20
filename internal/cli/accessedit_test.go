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
		{"edit", "host", "registry.npmjs.org", "x.dev", "--all"},
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
	// A wide pattern is the human's to set (T619): added, with a note.
	// The note goes to stderr: the list itself carries no warning lines.
	if code, out, stderr := run(human, true, "add", "approved", "prlctl *", "demo"); code != ExitOK || strings.Contains(out, "note:") ||
		!strings.Contains(stderr, "note: prlctl * ") || !strings.Contains(stderr, "every argument of prlctl") {
		t.Errorf("add approved prlctl *: exit %d, out %q, stderr %q", code, out, stderr)
	}
	ok("rm", "approved", "prlctl *", "demo")
	fail("tm itself", "add", "approved", "env tm *", "demo")
	fail("without a shell", "add", "approved", "sh -c 'make test'", "demo")
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
	if ts := c.ThreadSandbox("demo"); !ts.LocalPorts || len(ts.Given(config.SandboxHosts)) != 1 || len(ts.Given(config.SandboxExcluded)) != 0 || !s.Yolo || s.Protection != config.ProtectStrict {
		t.Errorf("demo: %+v %+v", ts, s)
	}
	// Reset: demo's switches follow all projects again, its lists stay.
	fail("confirm with --yes", "reset", "demo")
	if out := ok("reset", "demo", "--yes"); !strings.Contains(out, "reset for demo: yolo") {
		t.Errorf("reset: %q", out)
	}
	c, _ = config.Load()
	if s, _ := c.Safety("demo"); s.Yolo || s.Protection != config.ProtectStrict || !c.ThreadSandbox("demo").LocalPorts {
		t.Errorf("demo after reset: %+v", s)
	}
	if out := ok("reset", "demo", "--yes"); !strings.Contains(out, "nothing to reset") {
		t.Errorf("a second reset: %q", out)
	}
	p, _ := project.Open("demo")
	lines, _, _ := p.JournalTail(40)
	j := strings.Join(lines, "\n")
	for _, want := range []string{"settings.sandbox demo hosts + registry.npmjs.org (all projects)", "excluded_commands + npx playwright * (ask = false)", "drop + registry.npmjs.org", "drop - registry.npmjs.org", "settings.yolo demo yolo = true", "settings.protection demo protection = strict (all projects)", "settings.reset demo yolo"} {
		if !strings.Contains(j, want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}

// TestAccessEdit: tm access edit changes an entry in place, its ask
// kept unless a flag says, checked as add checks a new one; all
// projects' entry takes a project's drop of it along; a project can't
// edit all projects' entry, nor make its own one all projects give.
func TestAccessEdit(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		e := &Env{
			Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }, Cwd: h.cwd, Caller: human,
			Identify: func() (caller.Caller, bool) { return human, true },
		}
		code, _ := e.Run(append([]string{"access"}, args...))
		return code, out.String(), errb.String()
	}
	ok := func(args ...string) string {
		t.Helper()
		code, out, stderr := run(args...)
		if code != ExitOK {
			t.Fatalf("%v: exit %d, %q", args, code, stderr)
		}
		return out
	}
	fail := func(want string, args ...string) {
		t.Helper()
		if code, _, stderr := run(args...); code == ExitOK || !strings.Contains(stderr, want) {
			t.Errorf("%v: exit %d, %q", args, code, stderr)
		}
	}
	ok("add", "excluded", "docker compose *", "--all", "--without-asking")
	ok("add", "host", "a.dev", "--all")
	// An entry without * takes the bare command only (T632): said.
	if code, _, stderr := run("add", "excluded", "make e2e", "demo"); code != ExitOK || !strings.Contains(stderr, "make e2e takes only make e2e itself, with no arguments: make e2e * takes it with them") {
		t.Errorf("bare excluded command: %q", stderr)
	}
	ok("drop", "docker compose *", "demo")
	out := ok("edit", "excluded", "docker compose *", "docker compose up *", "--all")
	for _, want := range []string{"changed for all projects: excluded command docker compose * → docker compose up *", "demo drops docker compose up * now"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	ok("edit", "excluded", "make e2e", "make e2e *", "demo", "--without-asking")
	fail("nothing to change", "edit", "excluded", "make e2e *", "make e2e *", "demo", "--without-asking")
	fail("every command", "edit", "excluded", "make e2e *", "*", "demo")
	fail("all projects'", "edit", "host", "a.dev", "b.dev", "demo")
	fail("isn't in", "edit", "host", "x.dev", "b.dev", "demo")
	ok("add", "host", "web.dev", "demo")
	fail("all projects give a.dev already", "edit", "host", "web.dev", "a.dev", "demo")
	fail("for tm access edit excluded", "edit", "host", "web.dev", "w.dev", "demo", "--asks-first")
	ok("add", "approved", "make test", "demo")
	if code, out, stderr := run("edit", "approved", "make test", "make *", "demo"); code != ExitOK || strings.Contains(out, "note:") || !strings.Contains(stderr, "make runs other programs") {
		t.Errorf("edit into make *: exit %d, out %q, stderr %q", code, out, stderr)
	}
	ok("add", "guide", "Old", "--text", "Text.", "demo")
	ok("edit", "guide", "Old", "New", "demo", "--text", "New text.")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ts := c.ThreadSandbox("demo")
	if got := ts.Given(config.SandboxExcluded); !slices.Equal(got, []string{"make e2e *"}) || !slices.Equal(ts.WithoutAsking(), []string{"make e2e *"}) {
		t.Errorf("demo excluded %q, without asking %q", got, ts.WithoutAsking())
	}
	if !slices.Equal(ts.Drops, []string{"docker compose up *"}) || !slices.Equal(ts.Guides(), []config.Guide{{Title: "New", Body: "New text."}}) {
		t.Errorf("demo drops %q, guides %q", ts.Drops, ts.Guides())
	}
	if got := c.ThreadSandbox("").WithoutAsking(); !slices.Equal(got, []string{"docker compose up *"}) {
		t.Errorf("all projects' without asking %q", got)
	}
	p, _ := project.Open("demo")
	lines, _, _ := p.JournalTail(20)
	j := strings.Join(lines, "\n")
	for _, want := range []string{"excluded_commands ~ docker compose * → docker compose up * (all projects)", "drop ~ docker compose * → docker compose up *", "excluded_commands ~ make e2e → make e2e * (ask = false)"} {
		if !strings.Contains(j, want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
}
