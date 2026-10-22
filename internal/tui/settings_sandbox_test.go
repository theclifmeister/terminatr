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
	// At protection sandboxed, where the Access tab shows the sandbox's
	// rows.
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[defaults]
protection = "sandboxed"

[defaults.sandbox]
excluded_commands = ["docker compose *"]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.data.Sandbox = map[string]config.SandboxTable{"": c.SandboxOf(""), "alpha": c.SandboxOf("alpha")}
	all, _ := c.AllProjects()
	m.data.Defaults = &all
	for i := range m.data.Projects {
		s, _ := c.Safety(m.data.Projects[i].Slug)
		m.data.Projects[i].Safety = &s
	}
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
// X removes the project's own, drops one of all projects' and takes the
// drop back.
func TestSandboxView(t *testing.T) {
	src, m := sandboxData(t)
	row := sandboxRow(t, "alpha", "Excluded commands")
	if v := row.value(m); v != "1" || row.unset != nil {
		t.Errorf("value %q, unset %v", v, row.unset != nil)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"outside the sandbox", "full file and network access", "docker compose *", "all projects", "add up", "X drop for alpha"} {
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
		in.lineEdit = newLine(text)
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
	keyPress(m, "A")
	enter("*")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "matches every command") {
		t.Errorf("* taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "A")
	enter("docker compose *")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "all projects give docker compose * already") {
		t.Errorf("all projects' entry taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "A")
	enter("npx playwright *")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "outside the sandbox") {
		t.Fatalf("no warning before adding: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	if got := given(); !slices.Equal(got, []string{"docker compose *", "npx playwright *"}) {
		t.Errorf("given %q", got)
	}
	if v := row.value(m); v != "2 (1 alpha)" {
		t.Errorf("value %q", v)
	}
	v := m.top().(*sandboxView)
	v.sel = 1
	drain(t, m, keyPress(m, "X"))
	if got := given(); !slices.Equal(got, []string{"docker compose *"}) {
		t.Errorf("after X: %q", got)
	}
	v.sel = 0
	drain(t, m, keyPress(m, "X"))
	if got := given(); len(got) != 0 {
		t.Errorf("after the drop: %q", got)
	}
	if out := screen(m); !strings.Contains(out, "─ docker compose *") || !strings.Contains(out, "all projects · dropped") {
		t.Errorf("dropped entry not shown:\n%s", out)
	}
	drain(t, m, keyPress(m, "X"))
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
// reach it, and warns of a shared host; a guide is a title, then its
// text in the editor, listed by its title alone (no line under it),
// enter editing it; a title the list has is refused, and leaving a changed
// text asks first.
func TestHostsAndGuidance(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Network hosts").change(m))
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("https://x.dev")
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "isn't a host name") {
		t.Errorf("msg %q", m.msg)
	}
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("storage.googleapis.com")
	drain(t, m, keyPress(m, "enter"))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Every command a thread runs") || !strings.Contains(cv.question, "Google Cloud Storage") {
		t.Fatalf("no question before adding a host: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Guidance for threads").change(m))
	ctrlS := tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("Test data")
	drain(t, m, keyPress(m, "enter"))
	if _, ok := m.top().(*textEditor); !ok {
		t.Fatalf("no editor after the title: %T", m.top())
	}
	m.Update(tea.PasteMsg{Content: "Run make fixtures.\r\n\x1b[2JNever edit testdata/.\n\n"})
	drain(t, m, m.key(ctrlS))
	if out := screen(m); !strings.Contains(out, "Test data") || strings.Contains(out, "Run make fixtures.") || !strings.Contains(out, "enter show, edit") {
		t.Errorf("guidance view:\n%s", out)
	}
	c, _ := config.Load()
	if got := c.ThreadSandbox("alpha").Guides(); !slices.Equal(got, []config.Guide{{Title: "Test data", Body: "Run make fixtures.\n[2JNever edit testdata/."}}) {
		t.Errorf("alpha: %q", got)
	}
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("Test data")
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "listed already") {
		t.Errorf("same title: %q", m.msg)
	}
	// enter edits it; esc after a change asks first.
	keyPress(m, "enter")
	ed, ok := m.top().(*textEditor)
	if !ok || string(ed.text) != "Run make fixtures.\n[2JNever edit testdata/." {
		t.Fatalf("enter: %T", m.top())
	}
	keyPress(m, "up")
	keyPress(m, "ctrl+a")
	keyPress(m, "#")
	keyPress(m, "esc")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("esc after a change: %T", m.top())
	}
	keyPress(m, "N")
	drain(t, m, m.key(ctrlS))
	want := []string{"defaults hosts + storage.googleapis.com", "projects.alpha guides + Test data", "projects.alpha guides ~ Test data"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	c, _ = config.Load()
	if got := c.ThreadSandbox("alpha"); !slices.Equal(got.Given(config.SandboxHosts), []string{"storage.googleapis.com"}) || got.Guides()[0].Body != "#Run make fixtures.\n[2JNever edit testdata/." {
		t.Errorf("alpha: %+v", got)
	}
}

// TestSandboxAllowAll: an allow rule without a warning is added at
// once, to all projects'; removing the last one leaves none set.
func TestSandboxAllowAll(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Allow rules").change(m))
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("WebFetch(domain:playwright.dev)")
	drain(t, m, keyPress(m, "enter"))
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("WebFetch")
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "allows every WebFetch call") {
		t.Errorf("msg %q", m.msg)
	}
	drain(t, m, keyPress(m, "X"))
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
// guidance, prompts and escapes, the lists, the guard, the
// secret environment and tm's own rules, each under its section, and
// none of them under Settings any more; the footer names X on a row the
// project sets and E for the effective access.
func TestAccessTab(t *testing.T) {
	_, m := sandboxData(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 120})
	feed(m, keyPress(m, "A"))
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
		"Protection and prompts", "Protection", "sandboxed: + the thread sandbox", "Yolo mode", "Coordinator approves",
		"Inside the sandbox", "Network hosts", "Local ports", "Guidance for threads",
		"Outside the sandbox", "Excluded commands", "Permission rules", "Allow rules",
		"Guard", "Guard mode", "Guard rules", "Guard allowances", "Guard refusals", "Secrets", "Secret environment",
		"Built in", "tm's rules", "E effective access", "← → tabs"} {
		if !strings.Contains(out, want) {
			t.Errorf("Access tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "X follow all projects") || strings.Contains(out, "+ - number") {
		t.Errorf("footer names x or + - on a following row:\n%s", out)
	}
	for range pv.access.rows {
		keyPress(m, "up")
	}
	for _, r := range projectSettings("alpha") {
		for _, moved := range []string{"Protection", "Yolo mode", "Coordinator approves", "Guard", "Guard mode", "Secret environment", "Excluded commands", "Allow rules"} {
			if r.label == moved {
				t.Errorf("Settings still has %q", r.label)
			}
		}
	}
	// Protection: enter raises it a level for the project, which then
	// sets its own (X follows all projects again).
	drain(t, m, keyPress(m, "enter"))
	cfg, _ := config.Load()
	if s, _ := cfg.Safety("alpha"); s.Protection != config.ProtectStrict {
		t.Errorf("alpha's protection %q, want strict", s.Protection)
	}
	// The row's label may scroll above the popup's top (its help is long):
	// its value, and the footer offering X.
	if out := screen(m); pv.access.rows[0].label != "Protection" || pv.access.rows[0].value(m) != "strict: sandbox, no escapes" || !strings.Contains(out, "X follow all projects") {
		t.Errorf("after enter on Protection:\n%s", out)
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
	for _, want := range []string{"3 access", "Protection", "Yolo mode", "Network hosts", "Guard mode", "Secret environment", "A project's lists add to these", "alpha sets its own", "R reset"} {
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
// no hosts or local ports on Linux or Windows, guidance and the lists
// on all three.
func TestSandboxTabOS(t *testing.T) {
	for _, goos := range []string{config.OSLinux, config.OSWindows} {
		var labels []string
		for _, r := range sandboxSettings("alpha", goos) {
			labels = append(labels, r.label)
		}
		if !slices.Equal(labels, []string{"Guidance for threads", "Write folders", "Excluded commands", "Approved commands", "Allow rules"}) {
			t.Errorf("%s: %q", goos, labels)
		}
		_, m := sandboxData(t)
		m.data.Server.OS = goos
		if out := strings.Join(accessHead(m, "alpha", 200), " "); !strings.Contains(out, "don't apply there") || !strings.Contains(out, config.OSName(goos)) {
			t.Errorf("%s head: %s", goos, out)
		}
	}
	rows := sandboxSettings("", config.OSMac)
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.label)
	}
	if !slices.Equal(labels, []string{"Network hosts", "Local ports", "Guidance for threads", "Write folders", "Sockets", "Excluded commands", "Approved commands", "Allow rules"}) ||
		rows[0].section != "Inside the sandbox" || rows[5].section != "Outside the sandbox" || rows[6].section != "" || rows[7].section != "Permission rules" {
		t.Errorf("macOS rows: %q", labels)
	}
}

// TestLocalPortsRow: a project follows all projects' local ports until
// it is changed there (its off is its own: the drop of all projects'
// on); X follows them again, asking first when that opens them; all
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
	drain(t, m, keyPress(m, "Y"))
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
	// X follows all projects' on: it asks first; N keeps alpha's off.
	drain(t, m, row.unset(m))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Follow all projects' local ports") {
		t.Fatalf("X didn't ask before following all projects' on: %T", m.top())
	}
	drain(t, m, keyPress(m, "N"))
	if row.from(m) != "" {
		t.Error("N followed all projects")
	}
	drain(t, m, row.unset(m))
	drain(t, m, keyPress(m, "Y"))
	drain(t, m, all.change(m))
	want := []string{"defaults local_ports = true", "projects.alpha local_ports = false", "projects.alpha local_ports follows all projects", "defaults local_ports = false"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	if c, _ = config.Load(); c.SandboxOf("").LocalPorts != nil || c.SandboxOf("alpha").LocalPorts != nil {
		t.Errorf("left set: %+v %+v", c.SandboxOf(""), c.SandboxOf("alpha"))
	}
}

// TestNoPresets: tm has no presets (T642): p on a project's Access tab
// opens nothing and the tab names none.
func TestNoPresets(t *testing.T) {
	src, m := sandboxData(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 80})
	feed(m, keyPress(m, "A"))
	keyPress(m, "5")
	keyPress(m, "p")
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("p: %T", m.top())
	}
	if out := screen(m); strings.Contains(strings.ToLower(out), "preset") {
		t.Errorf("names a preset:\n%s", out)
	}
	if len(src.sandbox) != 0 {
		t.Errorf("saved %q", src.sandbox)
	}
}

// TestSandboxCoveredNote: an excluded command an allow rule of a file
// written before covers has no warning line in either list's view; a new
// Bash or Edit rule isn't added (T574 case 1).
func TestSandboxCoveredNote(t *testing.T) {
	_, m := sandboxData(t)
	m.data.Sandbox[""].Lists[config.SandboxAllow] = []string{"Bash(docker compose:*)"}
	drain(t, m, sandboxRow(t, "", "Excluded commands").change(m))
	if out := screen(m); strings.Contains(out, "covers it") {
		t.Errorf("excluded view:\n%s", out)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "", "Allow rules").change(m))
	if out := screen(m); strings.Contains(out, "covers the excluded") {
		t.Errorf("allow view:\n%s", out)
	}
	for _, rule := range []string{"Bash(docker *)", "Edit(/tmp/pw/**)"} {
		keyPress(m, "A")
		m.top().(*inputView).lineEdit = newLine(rule)
		drain(t, m, keyPress(m, "enter"))
		if _, ok := m.top().(*confirmView); ok || !strings.Contains(m.msg, "not added") {
			t.Errorf("%s: %T, %q", rule, m.top(), m.msg)
		}
	}
}

// TestSandboxExcludedAsk: an excluded command asks first until W lets it
// run without asking, which says what that opens first; W again asks
// first. All projects' entries aren't changed from a project's view.
func TestSandboxExcludedAsk(t *testing.T) {
	src, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "", "Excluded commands").change(m))
	if out := screen(m); !strings.Contains(out, "asks first") || !strings.Contains(out, "W ask first or not") {
		t.Errorf("excluded view:\n%s", out)
	}
	keyPress(m, "W")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "run without asking") {
		t.Fatalf("no question before running without asking: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	if out := screen(m); !strings.Contains(out, "runs without asking") {
		t.Errorf("after W:\n%s", out)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("alpha").WithoutAsking(); !slices.Equal(got, []string{"docker compose *"}) {
		t.Errorf("config: %q", got)
	}
	drain(t, m, keyPress(m, "W"))
	if _, ok := m.top().(*confirmView); ok {
		t.Error("asking first asked")
	}
	if c, _ = config.Load(); len(c.ThreadSandbox("alpha").WithoutAsking()) != 0 {
		t.Errorf("after W again: %q (writes %q)", c.ThreadSandbox("alpha").WithoutAsking(), src.sandbox)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Excluded commands").change(m))
	keyPress(m, "W")
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

// TestSandboxEdit: enter on an entry of the view's own table opens it
// for editing, an excluded command's ask after its value; the change
// is saved as one, asking first as add does, and all projects' entry
// takes a project's drop of it along. The checks of add refuse a new
// value the same way; all projects' entries aren't changed from a
// project's view; e on a guide changes its title and text.
func TestSandboxEdit(t *testing.T) {
	src, m := sandboxData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`sandbox_lists = "add"

[defaults.sandbox]
excluded_commands = ["docker compose *"]
hosts = ["a.dev"]

[projects.alpha.sandbox]
drop = ["docker compose *"]
hosts = ["web.dev"]
guides = [{ title = "Old", body = "Old text." }]
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.data.Sandbox = map[string]config.SandboxTable{"": c.SandboxOf(""), "alpha": c.SandboxOf("alpha")}
	input := func(want, text string) {
		t.Helper()
		in, ok := m.top().(*inputView)
		if !ok || in.value() != want {
			t.Fatalf("no input of %q: %T", want, m.top())
		}
		in.lineEdit = newLine(text)
		drain(t, m, keyPress(m, "enter"))
	}
	drain(t, m, sandboxRow(t, "", "Excluded commands").change(m))
	if out := screen(m); !strings.Contains(out, "enter edit") {
		t.Errorf("keys:\n%s", out)
	}
	keyPress(m, "enter")
	input("docker compose *", "docker compose up *")
	if _, ok := m.top().(*menuView); !ok {
		t.Fatalf("no ask choice: %T", m.top())
	}
	keyPress(m, "down")
	drain(t, m, keyPress(m, "enter"))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "Change docker compose * to docker compose up * for all projects?") || !strings.Contains(cv.question, "outside the sandbox") || !strings.Contains(cv.question, "without asking") {
		t.Fatalf("no question before the change: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	if !strings.Contains(m.msg, "the projects that dropped docker compose * drop docker compose up *") {
		t.Errorf("msg %q", m.msg)
	}
	c, _ = config.Load()
	if got := c.ThreadSandbox("").WithoutAsking(); !slices.Equal(got, []string{"docker compose up *"}) || !slices.Equal(c.SandboxOf("alpha").Lists[config.SandboxDrop], []string{"docker compose up *"}) {
		t.Errorf("all projects' %q, alpha drops %q", got, c.SandboxOf("alpha").Lists[config.SandboxDrop])
	}
	if !slices.Equal(m.data.Sandbox["alpha"].Lists[config.SandboxDrop], []string{"docker compose up *"}) {
		t.Errorf("shown: alpha drops %q", m.data.Sandbox["alpha"].Lists[config.SandboxDrop])
	}
	keyPress(m, "esc")
	// A project's view: all projects' entry, dropped, is not its to edit.
	drain(t, m, sandboxRow(t, "alpha", "Excluded commands").change(m))
	keyPress(m, "e")
	if _, ok := m.top().(*sandboxView); !ok || !strings.Contains(m.msg, "X takes it back") {
		t.Errorf("dropped entry: %T %q", m.top(), m.msg)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Network hosts").change(m))
	keyPress(m, "enter")
	if !strings.Contains(m.msg, "change it in the , popup") {
		t.Errorf("all projects' entry: %q", m.msg)
	}
	m.top().(*sandboxView).sel = 1
	keyPress(m, "enter")
	input("web.dev", "a.dev")
	if !strings.Contains(m.msg, "not changed: all projects give a.dev already") {
		t.Errorf("msg %q", m.msg)
	}
	keyPress(m, "e")
	input("web.dev", "https://x.dev")
	if !strings.Contains(m.msg, "not changed") || !strings.Contains(m.msg, "isn't a host name") {
		t.Errorf("msg %q", m.msg)
	}
	keyPress(m, "e")
	input("web.dev", "web2.dev")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Every command a thread runs") {
		t.Fatalf("no question before the change: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	keyPress(m, "esc")
	// e on a guide: its title, then its text.
	drain(t, m, sandboxRow(t, "alpha", "Guidance for threads").change(m))
	if out := screen(m); !strings.Contains(out, "e edit title") {
		t.Errorf("keys:\n%s", out)
	}
	keyPress(m, "e")
	input("Old", "New")
	ed, ok := m.top().(*textEditor)
	if !ok || string(ed.text) != "Old text." {
		t.Fatalf("no editor of the text: %T", m.top())
	}
	drain(t, m, m.key(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}))
	c, _ = config.Load()
	if got := c.ThreadSandbox("alpha"); !slices.Equal(got.Given(config.SandboxHosts), []string{"a.dev", "web2.dev"}) || !slices.Equal(got.Guides(), []config.Guide{{Title: "New", Body: "Old text."}}) {
		t.Errorf("alpha hosts %q, guides %q", got.Given(config.SandboxHosts), got.Guides())
	}
	want := []string{"defaults excluded_commands ~ docker compose * → docker compose up * (ask = false)", "projects.alpha hosts ~ web.dev → web2.dev", "projects.alpha guides ~ Old → New"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
}

// TestSandboxWideApproved: a wide approved command is the human's to
// set (T619): added after its warning, which says what it leaves open;
// editing one into a wide pattern says it too, and the list carries no
// warning line under it.
func TestSandboxWideApproved(t *testing.T) {
	_, m := sandboxData(t)
	drain(t, m, sandboxRow(t, "alpha", "Approved commands").change(m))
	keyPress(m, "A")
	m.top().(*inputView).lineEdit = newLine("prlctl *")
	drain(t, m, keyPress(m, "enter"))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "every argument of prlctl") {
		t.Fatalf("no warning before adding: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	keyPress(m, "enter")
	m.top().(*inputView).lineEdit = newLine("sh -c *")
	drain(t, m, keyPress(m, "enter"))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "runs the code that follows it") {
		t.Fatalf("no warning before the change: %T", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	// The warning is the question's only: no line under the entry.
	if out := screen(m); !strings.Contains(out, "sh -c *") || strings.Contains(out, "runs the code") || strings.Contains(out, "every argument") {
		t.Errorf("list:\n%s", out)
	}
	c, _ := config.Load()
	if got := c.ThreadSandbox("alpha").Given(config.SandboxApproved); !slices.Equal(got, []string{"sh -c *"}) {
		t.Errorf("approved %q", got)
	}
}
