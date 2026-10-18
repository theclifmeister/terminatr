package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sandboxData is popupData with all projects' sandbox rules, as Load
// reads them; alpha sets none of its own.
func sandboxData(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src, m := popupData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[defaults.sandbox]
excluded_commands = ["docker compose *"]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.data.Sandbox = map[string]config.SandboxTable{"": c.SandboxOf(""), "alpha": c.SandboxOf("alpha")}
	m.data.Server.OS = config.OSMac
	return src, m
}

// sandboxRow is the Sandbox tab's row label of slug's (all projects'
// for "") on macOS.
func sandboxRow(t *testing.T, slug, label string) setting {
	t.Helper()
	for _, r := range sandboxSettings(slug, config.OSMac) {
		if r.label == label {
			return r
		}
	}
	t.Fatalf("no row %q", label)
	return setting{}
}

// TestSandboxView: a project's excluded commands add to all projects';
// adding one asks first, saying it runs outside the sandbox; a
// match-all entry is refused, and so is one all projects give already;
// x removes the project's own, d drops one of all projects' and u takes
// it back.
func TestSandboxView(t *testing.T) {
	src, m := sandboxData(t)
	row := sandboxRow(t, "alpha", "Excluded commands")
	if v := row.value(m); v != "1" || row.unset != nil {
		t.Errorf("value %q, unset %v", v, row.unset != nil)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"outside the sandbox", "full file and network access", "docker compose *", "all projects", "add up", "d drop for alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	enter := func(text string) {
		t.Helper()
		in, ok := m.top().(*inputView)
		if !ok {
			t.Fatalf("no input: %T", m.top())
		}
		in.text = text
		drain(t, m, keyPress(m, "enter"))
	}
	given := func() []string {
		t.Helper()
		c, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		return c.ThreadSandbox("alpha").Given(config.SandboxExcluded)
	}
	keyPress(m, "a")
	enter("*")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "matches every command") {
		t.Errorf("* taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "a")
	enter("docker compose *")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "all projects give docker compose * already") {
		t.Errorf("all projects' entry taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "a")
	enter("npx playwright *")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "outside the sandbox") {
		t.Fatalf("no warning before adding: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if got := given(); !slices.Equal(got, []string{"docker compose *", "npx playwright *"}) {
		t.Errorf("given %q", got)
	}
	if v := row.value(m); v != "2 (1 alpha)" {
		t.Errorf("value %q", v)
	}
	v := m.top().(*sandboxView)
	v.sel = 1
	drain(t, m, keyPress(m, "x"))
	if got := given(); !slices.Equal(got, []string{"docker compose *"}) {
		t.Errorf("after x: %q", got)
	}
	v.sel = 0
	drain(t, m, keyPress(m, "d"))
	if got := given(); len(got) != 0 {
		t.Errorf("after the drop: %q", got)
	}
	if out := screen(m); !strings.Contains(out, "─ docker compose *") || !strings.Contains(out, "all projects · dropped") {
		t.Errorf("dropped entry not shown:\n%s", out)
	}
	drain(t, m, keyPress(m, "u"))
	want := []string{"projects.alpha excluded_commands + npx playwright *", "projects.alpha excluded_commands - npx playwright *",
		"projects.alpha drop + docker compose *", "projects.alpha drop - docker compose *"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	c, _ := config.Load()
	if got := c.SandboxOf("alpha"); len(got.Lists) != 0 {
		t.Errorf("left set: %+v", got)
	}
}

// TestHostsAndGuidance: a host asks first, saying every command may
// reach it, and warns of a shared host; a guidance text names those tm
// has, and its view says what each tells threads.
func TestHostsAndGuidance(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Network hosts").change(m))
	keyPress(m, "a")
	m.top().(*inputView).text = "https://x.dev"
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "isn't a host name") {
		t.Errorf("msg %q", m.msg)
	}
	keyPress(m, "a")
	m.top().(*inputView).text = "storage.googleapis.com"
	drain(t, m, keyPress(m, "enter"))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Every command a thread runs") || !strings.Contains(cv.question, "Google Cloud Storage") {
		t.Fatalf("no question before adding a host: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Guidance for threads").change(m))
	keyPress(m, "a")
	if out := screen(m); !strings.Contains(out, "browser-testing, browser-downloads") {
		t.Errorf("guidance prompt:\n%s", out)
	}
	m.top().(*inputView).text = "browser-testing"
	drain(t, m, keyPress(m, "enter"))
	if out := screen(m); !strings.Contains(out, "headless shell") {
		t.Errorf("guidance view:\n%s", out)
	}
	want := []string{"defaults hosts + storage.googleapis.com", "projects.alpha guides + browser-testing"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	c, _ := config.Load()
	if got := c.ThreadSandbox("alpha"); !slices.Equal(got.Given(config.SandboxHosts), []string{"storage.googleapis.com"}) || !slices.Equal(got.Given(config.SandboxGuides), []string{"browser-testing"}) {
		t.Errorf("alpha: %+v", got)
	}
}

// TestSandboxAllowAll: an allow rule without a warning is added at
// once, to all projects'; removing the last one leaves none set.
func TestSandboxAllowAll(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Allow rules").change(m))
	keyPress(m, "a")
	m.top().(*inputView).text = "WebFetch(domain:playwright.dev)"
	drain(t, m, keyPress(m, "enter"))
	keyPress(m, "a")
	m.top().(*inputView).text = "WebFetch"
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "allows every WebFetch call") {
		t.Errorf("msg %q", m.msg)
	}
	drain(t, m, keyPress(m, "x"))
	if want := []string{"defaults allow + WebFetch(domain:playwright.dev)", "defaults allow - WebFetch(domain:playwright.dev)"}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	c, _ := config.Load()
	if _, ok := c.SandboxOf("").Lists[config.SandboxAllow]; ok {
		t.Error("an empty all-projects list is still set")
	}
	data, _ := os.ReadFile(c.Path)
	if strings.Contains(string(data), "allow") {
		t.Errorf("file:\n%s", data)
	}
}

// TestAccessTab: the project popup's Access tab, after Settings: the
// guidance, prompts and escapes, the presets, the lists, the guard, the
// secret environment and tm's own rules, each under its section, and
// none of them under Settings any more; the footer names x on a row the
// project sets and e for the effective access.
func TestAccessTab(t *testing.T) {
	_, m := sandboxData(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 120})
	feed(m, keyPress(m, "a"))
	pv := m.top().(*projectView)
	keyPress(m, "5")
	if pv.tab != tabAccess {
		t.Fatalf("5: tab %d", pv.tab)
	}
	// The whole tab, row by row as it scrolls.
	out := screen(m)
	for range pv.access.rows {
		keyPress(m, "down")
		out += screen(m)
	}
	for _, want := range []string{"5 access", "Prefer rules inside the sandbox", "a last resort", "what applies on macOS",
		"Prompts and escapes", "Escapes", "ask you", "Yolo mode", "Coordinator approves",
		"Inside the sandbox", "Network hosts", "Local ports", "Guidance for threads",
		"Outside the sandbox", "Excluded commands", "Permission rules", "Allow rules",
		"Guard", "Guard mode", "Guard rules", "Guard allowances", "Guard refusals", "Secrets", "Secret environment",
		"Built in", "tm's rules", "p add a preset", "e effective access", "← → tabs"} {
		if !strings.Contains(out, want) {
			t.Errorf("Access tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "x follow all projects") || strings.Contains(out, "+ - number") {
		t.Errorf("footer names x or + - on a following row:\n%s", out)
	}
	for range pv.access.rows {
		keyPress(m, "up")
	}
	for _, r := range projectSettings("alpha") {
		for _, moved := range []string{"Escapes", "Yolo mode", "Coordinator approves", "Guard", "Guard mode", "Secret environment", "Excluded commands", "Allow rules"} {
			if r.label == moved {
				t.Errorf("Settings still has %q", r.label)
			}
		}
	}
	// Escapes: enter turns them off for the project, which then sets its
	// own (x follows all projects again).
	drain(t, m, keyPress(m, "enter"))
	cfg, _ := config.Load()
	if s, _ := cfg.Safety("alpha"); s.SandboxEscapes {
		t.Error("escapes still on for alpha")
	}
	// The row's label may scroll above the popup's top (its help is long):
	// its value, and the footer offering x.
	if out := screen(m); pv.access.rows[0].label != "Escapes" || pv.access.rows[0].value(m) != "never" || !strings.Contains(out, "x follow all projects") {
		t.Errorf("after enter on Escapes:\n%s", out)
	}
	// The , popup's third tab is all projects', its workflow rows left
	// on All projects.
	keyPress(m, "esc")
	press(m, ",", "3")
	out = screen(m)
	for range m.top().(*settingsView).tabs[tabAllAccess].rows {
		keyPress(m, "down")
		out += screen(m)
	}
	for _, want := range []string{"3 Access", "Escapes", "Yolo mode", "Network hosts", "Guard mode", "Secret environment", "A project's lists add to these", "alpha sets its own", "p add a preset"} {
		if !strings.Contains(out, want) {
			t.Errorf(", Access tab lacks %q:\n%s", want, out)
		}
	}
	for _, r := range allProjectsSettings() {
		if slices.Contains([]string{"Yolo mode", "Coordinator approves", "Guard", "Guard rules", "Secret environment"}, r.label) {
			t.Errorf("All projects still has %q", r.label)
		}
	}
}

// TestAccessBuiltIn: the Built in row lists tm's own rules for a new
// thread of the project, and nothing else; all projects' row only says
// where they are.
func TestAccessBuiltIn(t *testing.T) {
	src, m := sandboxData(t)
	src.access = proto.AccessShowResult{AccessSet: proto.AccessSet{OS: "darwin", Rules: []proto.AccessRule{
		{Where: proto.AccessInside, Kind: proto.AccessWorktree, Value: "<worktree>", Source: proto.AccessFromTM},
		{Where: proto.AccessOutside, Kind: proto.AccessExcluded, Value: "docker compose *", Source: proto.AccessFromAll}}}}
	row := guardRow(t, "alpha", "tm's rules")
	drain(t, m, row.change(m))
	v, ok := m.top().(*accessView)
	if !ok || len(v.res.Rules) != 1 || v.res.Rules[0].Source != proto.AccessFromTM {
		t.Fatalf("built in: %T %+v", m.top(), v)
	}
	if out := screen(m); !strings.Contains(out, "Built in (tm) · alpha") || strings.Contains(out, "docker compose") {
		t.Errorf("built in view:\n%s", out)
	}
	if all := guardRow(t, "", "tm's rules"); all.change != nil {
		t.Error("all projects' Built in row opens something")
	}
}

// TestSandboxTabOS: the tab shows what applies on the server's platform:
// no hosts, local ports or guidance on Linux or Windows, the lists on
// all three.
func TestSandboxTabOS(t *testing.T) {
	for _, goos := range []string{config.OSLinux, config.OSWindows} {
		var labels []string
		for _, r := range sandboxSettings("alpha", goos) {
			labels = append(labels, r.label)
		}
		if !slices.Equal(labels, []string{"Write folders", "Excluded commands", "Allow rules"}) {
			t.Errorf("%s: %q", goos, labels)
		}
		_, m := sandboxData(t)
		m.data.Server.OS = goos
		if out := strings.Join(accessHead(m, 200), " "); !strings.Contains(out, "don't apply there") || !strings.Contains(out, config.OSName(goos)) {
			t.Errorf("%s head: %s", goos, out)
		}
	}
	rows := sandboxSettings("", config.OSMac)
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.label)
	}
	if !slices.Equal(labels, []string{"Network hosts", "Local ports", "Guidance for threads", "Write folders", "Excluded commands", "Allow rules"}) ||
		rows[0].section != "Inside the sandbox" || rows[4].section != "Outside the sandbox" || rows[5].section != "Permission rules" {
		t.Errorf("macOS rows: %q", labels)
	}
}

// TestLocalPortsRow: a project follows all projects' local ports until
// it is changed there (its off is its own: the drop of all projects'
// on); x follows them again, asking first when that opens them; all
// projects' off is no setting at all.
func TestLocalPortsRow(t *testing.T) {
	src, m := sandboxData(t)
	all := sandboxRow(t, "", "Local ports")
	row := sandboxRow(t, "alpha", "Local ports")
	if v, from := row.value(m), row.from(m); v != "off" || from != config.AllProjectsName {
		t.Errorf("value %q from %q", v, from)
	}
	drain(t, m, all.change(m))
	// Opening them asks first, saying what local ports open.
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "lets the thread out of the sandbox") {
		t.Fatalf("no question before opening them: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if v, from := row.value(m), row.from(m); v != "on" || from != config.AllProjectsName {
		t.Errorf("after all projects on: value %q from %q", v, from)
	}
	drain(t, m, row.change(m))
	if v, from := row.value(m), row.from(m); v != "off" || from != "" {
		t.Errorf("after alpha off: value %q from %q", v, from)
	}
	c, _ := config.Load()
	if c.ThreadSandbox("alpha").LocalPorts || !c.ThreadSandbox("beta").LocalPorts {
		t.Errorf("alpha %+v, beta %+v", c.ThreadSandbox("alpha"), c.ThreadSandbox("beta"))
	}
	// x follows all projects' on: it asks first; n keeps alpha's off.
	drain(t, m, row.unset(m))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Follow all projects' local ports") {
		t.Fatalf("x didn't ask before following all projects' on: %T", m.top())
	}
	drain(t, m, keyPress(m, "n"))
	if row.from(m) != "" {
		t.Error("n followed all projects")
	}
	drain(t, m, row.unset(m))
	drain(t, m, keyPress(m, "y"))
	drain(t, m, all.change(m))
	want := []string{"defaults local_ports = true", "projects.alpha local_ports = false", "projects.alpha local_ports follows all projects", "defaults local_ports = false"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	if c, _ = config.Load(); c.SandboxOf("").LocalPorts != nil || c.SandboxOf("alpha").LocalPorts != nil {
		t.Errorf("left set: %+v %+v", c.SandboxOf(""), c.SandboxOf("alpha"))
	}
}

// TestPresetAdd: p on a project's Access tab lists the presets; enter
// shows the rules one adds, space leaves one out, enter adds the rest
// as plain rules; adding it again offers only what is missing, and A
// adds to all projects instead.
func TestPresetAdd(t *testing.T) {
	src, m := sandboxData(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 80})
	feed(m, keyPress(m, "a"))
	keyPress(m, "5")
	keyPress(m, "p")
	if _, ok := m.top().(*presetsView); !ok {
		t.Fatalf("p: %T", m.top())
	}
	out := screen(m)
	for _, p := range config.Presets {
		if !strings.Contains(out, p.Title) {
			t.Errorf("lacks %q:\n%s", p.Title, out)
		}
	}
	keyPress(m, "down")
	keyPress(m, "enter")
	out = screen(m)
	for _, want := range []string{"Add Browser downloads (npm, Playwright) for alpha", "[x] host", "registry.npmjs.org", "storage.googleapis.com", "[x] guidance", "browser-downloads",
		"Already there: nothing", "enter add 5 rules", "A add to all projects instead"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	for _, risk := range []string{"bucket", "every command"} {
		if strings.Contains(out, risk) {
			t.Errorf("a risk remark (%q):\n%s", risk, out)
		}
	}
	// Leave storage.googleapis.com out.
	for range 3 {
		keyPress(m, "down")
	}
	keyPress(m, "space")
	if out := screen(m); !strings.Contains(out, "[ ] host") || !strings.Contains(out, "enter add 4 rules") {
		t.Errorf("after space:\n%s", out)
	}
	drain(t, m, keyPress(m, "enter"))
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("after adding: %T", m.top())
	}
	c, _ := config.Load()
	got := c.SandboxOf("alpha")
	if !slices.Equal(got.Lists[config.SandboxHosts], config.BrowserDomains[:3]) || !slices.Equal(got.Lists[config.SandboxGuides], []string{"browser-downloads"}) {
		t.Errorf("alpha: %+v", got)
	}
	// Again: only the left-out host is offered; A adds it to all projects.
	keyPress(m, "p")
	keyPress(m, "down")
	keyPress(m, "enter")
	out = screen(m)
	if !strings.Contains(out, "enter add 1 rule") || !strings.Contains(out, "Already there: host registry.npmjs.org") {
		t.Errorf("again:\n%s", out)
	}
	keyPress(m, "A")
	if out := screen(m); !strings.Contains(out, "for all projects") || !strings.Contains(out, "enter add 5 rules") || !strings.Contains(out, "A add to alpha instead") {
		t.Errorf("after A:\n%s", out)
	}
	keyPress(m, "A")
	drain(t, m, keyPress(m, "enter"))
	if want := []string{
		"projects.alpha preset browser-downloads: host registry.npmjs.org, host cdn.playwright.dev, host playwright.download.prss.microsoft.com, guidance browser-downloads",
		"projects.alpha preset browser-downloads: host storage.googleapis.com"}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	// Local dev servers adds local ports, as a plain setting.
	keyPress(m, "p")
	keyPress(m, "down")
	keyPress(m, "down")
	keyPress(m, "enter")
	drain(t, m, keyPress(m, "enter"))
	if c, _ := config.Load(); !c.ThreadSandbox("alpha").LocalPortsOwn || !c.ThreadSandbox("alpha").LocalPorts {
		t.Errorf("local ports: %+v", c.ThreadSandbox("alpha"))
	}
}

// TestSandboxCoveredNote: an excluded command an allow rule of a file
// written before covers says it runs outside the sandbox without asking,
// in both lists' views; a new Bash or Edit rule isn't added (T574 case
// 1).
func TestSandboxCoveredNote(t *testing.T) {
	_, m := sandboxData(t)
	m.data.Sandbox[""].Lists[config.SandboxAllow] = []string{"Bash(docker compose:*)"}
	drain(t, m, sandboxRow(t, "", "Excluded commands").change(m))
	if out := screen(m); !strings.Contains(out, "Bash(docker compose:*) covers it") {
		t.Errorf("excluded view:\n%s", out)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "", "Allow rules").change(m))
	if out := screen(m); !strings.Contains(out, "covers the excluded docker compose *") {
		t.Errorf("allow view:\n%s", out)
	}
	for _, rule := range []string{"Bash(docker *)", "Edit(/tmp/pw/**)"} {
		keyPress(m, "a")
		m.top().(*inputView).text = rule
		drain(t, m, keyPress(m, "enter"))
		if _, ok := m.top().(*confirmView); ok || !strings.Contains(m.msg, "not added") {
			t.Errorf("%s: %T, %q", rule, m.top(), m.msg)
		}
	}
}

// TestSandboxExcludedAsk: an excluded command asks first until w lets it
// run without asking, which says what that opens first; w again asks
// first. All projects' entries aren't changed from a project's view.
func TestSandboxExcludedAsk(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Excluded commands").change(m))
	if out := screen(m); !strings.Contains(out, "asks first") || !strings.Contains(out, "w ask first or not") {
		t.Errorf("excluded view:\n%s", out)
	}
	keyPress(m, "w")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "run without asking") {
		t.Fatalf("no question before running without asking: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if out := screen(m); !strings.Contains(out, "runs without asking") {
		t.Errorf("after w:\n%s", out)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("alpha").WithoutAsking(); !slices.Equal(got, []string{"docker compose *"}) {
		t.Errorf("config: %q", got)
	}
	drain(t, m, keyPress(m, "w"))
	if _, ok := m.top().(*confirmView); ok {
		t.Error("asking first asked")
	}
	if c, _ = config.Load(); len(c.ThreadSandbox("alpha").WithoutAsking()) != 0 {
		t.Errorf("after w again: %q (writes %q)", c.ThreadSandbox("alpha").WithoutAsking(), src.sandbox)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Excluded commands").change(m))
	keyPress(m, "w")
	if !strings.Contains(m.msg, "all projects'") {
		t.Errorf("a project changed all projects' entry: %q", m.msg)
	}
}

// TestSandboxHumanOnly: inside an agent a sandbox write is refused,
// and nothing is written.
func TestSandboxHumanOnly(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	s := &ServerSource{Caller: caller.Caller{Kind: caller.Thread, Project: "alpha", Thread: "t-0001"}}
	err := s.EditSandbox(config.DefaultsTable, "hosts + x.dev", func(t *config.SandboxTable) error { t.Add(config.SandboxHosts, "x.dev"); return nil })
	if err != errHumanOnly {
		t.Errorf("agent: %v", err)
	}
	if c, _ := config.Load(); len(c.SandboxOf("").Lists) != 0 {
		t.Error("written")
	}
}

// TestSandboxBadEntryFlagged: an entry that fails the checks (written by
// hand in config.toml) is marked with its reason, and the list says
// threads start without it, and only it.
func TestSandboxBadEntryFlagged(t *testing.T) {
	_, m := sandboxData(t)
	m.data.Sandbox[""].Lists[config.SandboxExcluded] = []string{"docker compose *", "make a$X"}
	row := sandboxRow(t, "", "Excluded commands")
	if v := row.value(m); v != "1 (1 refused)" {
		t.Errorf("value %q", v)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"✗ make a$X", "has a variable", "and only it"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✗ docker compose") {
		t.Errorf("good entry flagged:\n%s", out)
	}
}
