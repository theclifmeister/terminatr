package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// sandboxData is popupData with all projects' sandbox lists, as Load
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
	m.data.Sandbox = map[string]map[string][]string{"": c.SandboxOf(""), "alpha": c.SandboxOf("alpha")}
	m.data.SandboxPresets = map[string]map[string]bool{"": c.SandboxPresetsOf(""), "alpha": c.SandboxPresetsOf("alpha")}
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

// TestSandboxView: a project follows all projects' list until it is
// changed there; adding an excluded command asks first, saying it runs
// outside the sandbox; a match-all entry is refused; f follows all
// projects again.
func TestSandboxView(t *testing.T) {
	src, m := sandboxData(t)
	row := sandboxRow(t, "alpha", "Excluded commands")
	if v, from := row.value(m), row.from(m); v != "1" || from != config.AllProjectsName {
		t.Errorf("value %q from %q", v, from)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"outside the sandbox", "full file and network access", "docker compose *", "All projects' list, which alpha follows", "f follow all projects"} {
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
	keyPress(m, "a")
	enter("*")
	if len(src.sandbox) != 0 || !strings.Contains(m.msg, "matches every command") {
		t.Errorf("* taken: %v %q", src.sandbox, m.msg)
	}
	keyPress(m, "a")
	enter("npx playwright *")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "outside the sandbox") {
		t.Fatalf("no warning before adding: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	// The project's own list starts from all projects'.
	if want := []string{`projects.alpha excluded_commands ["docker compose *" "npx playwright *"]`}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	if row.from(m) != "" {
		t.Error("still follows all projects")
	}
	// A broad entry's warning is put too, and listed under it.
	keyPress(m, "a")
	enter("bash *")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "runs other programs") {
		t.Fatalf("no broad warning: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	if !strings.Contains(screen(m), "runs other programs") {
		t.Errorf("warning not listed:\n%s", screen(m))
	}
	// x removes the selected (the last added), f follows all projects.
	drain(t, m, keyPress(m, "x"))
	c, _ := config.Load()
	if got, _ := c.ThreadSandbox("alpha"); !reflect.DeepEqual(got.ExcludedCommands, []string{"docker compose *", "npx playwright *"}) {
		t.Errorf("written %+v", got)
	}
	drain(t, m, keyPress(m, "f"))
	c, _ = config.Load()
	if got := c.SandboxOf("alpha"); len(got) != 0 {
		t.Errorf("still its own: %v", got)
	}
	if row.from(m) != config.AllProjectsName {
		t.Error("doesn't follow all projects")
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
	if want := []string{`defaults allow ["WebFetch(domain:playwright.dev)"]`, `defaults allow []`}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	c, _ := config.Load()
	if _, ok := c.SandboxOf("")[config.SandboxAllow]; ok {
		t.Error("an empty all-projects list is still set")
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
	for _, want := range []string{"5 access", "Prefer a preset", "a last resort", "what applies on macOS", "Inside the sandbox: write folders", "Write folders",
		"Prompts and escapes", "Escapes", "ask you or auto mode", "Yolo mode", "Coordinator approves",
		"Inside the sandbox: presets", "Browser testing (Playwright, Chromium)", "Browser downloads (npm, Playwright)", "Local dev servers",
		"Outside the sandbox", "Excluded commands", "1 · all projects", "Permission rules", "Allow rules",
		"Guard", "Guard mode", "Guard rules", "Guard allowances", "Guard refusals", "Secrets", "Secret environment",
		"Built in", "tm's rules", "e effective access", "← → tabs"} {
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
	for _, want := range []string{"3 Access", "Escapes", "Yolo mode", "Browser testing", "Guard mode", "Secret environment", "unless the project sets its own", "alpha sets its own"} {
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
// no preset on Linux or Windows, the lists on all three.
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
		if out := strings.Join(accessHead(m, 200), " "); !strings.Contains(out, "No preset applies there") || !strings.Contains(out, config.OSName(goos)) {
			t.Errorf("%s head: %s", goos, out)
		}
	}
	rows := sandboxSettings("", config.OSMac)
	if len(rows) != 6 || rows[0].section != "Inside the sandbox: presets" || rows[3].section != "Inside the sandbox: write folders" || rows[4].section != "Outside the sandbox" || rows[5].section != "Permission rules" {
		t.Errorf("macOS rows: %+v", rows)
	}
}

// TestSandboxPresetRows: a project follows all projects' switch until
// it is changed there; x follows them again; all projects' off is no
// switch at all.
func TestSandboxPresetRows(t *testing.T) {
	src, m := sandboxData(t)
	all := sandboxRow(t, "", "Local dev servers")
	row := sandboxRow(t, "alpha", "Local dev servers")
	if v, from := row.value(m), row.from(m); v != "off" || from != config.AllProjectsName {
		t.Errorf("value %q from %q", v, from)
	}
	drain(t, m, all.change(m))
	// Turning one on asks first, saying what local binding opens.
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "lets the thread out of the sandbox") {
		t.Fatalf("no question before turning it on: %T", m.top())
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
	if got, _ := c.ThreadSandbox("alpha"); len(got.Presets) != 0 {
		t.Errorf("alpha: %+v", got)
	}
	if got, _ := c.ThreadSandbox("beta"); !slices.Equal(got.Presets, []string{config.SandboxDevServers}) {
		t.Errorf("beta: %+v", got)
	}
	// x follows all projects' on: it asks first.
	drain(t, m, row.unset(m))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "lets the thread out of the sandbox") {
		t.Fatalf("x didn't ask before following all projects' on: %T", m.top())
	}
	drain(t, m, keyPress(m, "y"))
	drain(t, m, all.change(m))
	// Browser downloads asks too, naming the hosts it opens; browser
	// testing, which opens none, doesn't.
	drain(t, m, sandboxRow(t, "", "Browser downloads (npm, Playwright)").change(m))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "registry.npmjs.org") || !strings.Contains(cv.question, "storage.googleapis.com is every Google Cloud Storage bucket's host") {
		t.Fatalf("no question before turning browser downloads on: %T", m.top())
	}
	drain(t, m, keyPress(m, "n"))
	if _, ok := m.top().(*confirmView); ok {
		t.Fatal("confirm still open")
	}
	want := []string{"defaults local_dev_servers on", "projects.alpha local_dev_servers off", "projects.alpha local_dev_servers follow", "defaults local_dev_servers follow"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	if c, _ = config.Load(); len(c.SandboxPresetsOf("")) != 0 || len(c.SandboxPresetsOf("alpha")) != 0 {
		t.Errorf("left set: %v %v", c.SandboxPresetsOf(""), c.SandboxPresetsOf("alpha"))
	}
}

// TestSandboxFollowAsks: x on a project whose own switch is off, where
// all projects' is on, turns the preset on for it, so it asks first as
// turning it on does; n keeps the project's own off.
func TestSandboxFollowAsks(t *testing.T) {
	src, m := sandboxData(t)
	m.data.SandboxPresets = map[string]map[string]bool{"": {config.SandboxDownloads: true, config.SandboxBrowser: true}, "alpha": {config.SandboxDownloads: false, config.SandboxBrowser: false}}
	row := sandboxRow(t, "alpha", "Browser downloads (npm, Playwright)")
	drain(t, m, row.unset(m))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "Follow all projects'") || !strings.Contains(cv.question, "storage.googleapis.com") {
		t.Fatalf("x didn't ask: %T", m.top())
	}
	drain(t, m, keyPress(m, "n"))
	if len(src.sandbox) != 0 || row.from(m) != "" {
		t.Fatalf("n saved %q", src.sandbox)
	}
	drain(t, m, row.unset(m))
	drain(t, m, keyPress(m, "y"))
	if want := []string{"projects.alpha browser_downloads follow"}; !slices.Equal(src.sandbox, want) || row.from(m) != config.AllProjectsName {
		t.Errorf("y saved %q", src.sandbox)
	}
	// Browser testing opens nothing: x follows without asking.
	drain(t, m, sandboxRow(t, "alpha", "Browser testing (Playwright, Chromium)").unset(m))
	if _, ok := m.top().(*confirmView); ok || len(src.sandbox) != 2 {
		t.Errorf("browser testing x: %T %q", m.top(), src.sandbox)
	}
}

// TestSandboxCoveredNote: an excluded command an allow rule of a file
// written before covers says it runs outside the sandbox without asking,
// in both lists' views; a new Bash rule isn't added (T574 case 1).
func TestSandboxCoveredNote(t *testing.T) {
	_, m := sandboxData(t)
	m.data.Sandbox[""][config.SandboxAllow] = []string{"Bash(docker compose:*)"}
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
	if l := m.data.Sandbox[""][config.SandboxAllow]; !slices.Equal(l, []string{"Bash(docker compose:*)"}) {
		t.Errorf("allow rules %q", l)
	}
}

// TestSandboxExcludedAsk: an excluded command asks first until w lets it
// run without asking, which says what that opens first; w again asks
// first. All projects' list isn't changed from a project's view.
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
	if got, _ := c.ThreadSandbox("alpha"); !slices.Equal(got.WithoutAsking, []string{"docker compose *"}) {
		t.Errorf("config: %+v", got)
	}
	drain(t, m, keyPress(m, "w"))
	if _, ok := m.top().(*confirmView); ok {
		t.Error("asking first asked")
	}
	if want := "defaults docker compose * ask=true"; src.sandbox[len(src.sandbox)-1] != want {
		t.Errorf("writes %q", src.sandbox)
	}
	keyPress(m, "esc")
	drain(t, m, sandboxRow(t, "alpha", "Excluded commands").change(m))
	keyPress(m, "w")
	if !strings.Contains(m.msg, "all projects' list") {
		t.Errorf("a project changed all projects' list: %q", m.msg)
	}
}

// TestSandboxPresetHumanOnly: inside an agent the preset write is
// refused, and nothing is written.
func TestSandboxPresetHumanOnly(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	on := true
	s := &ServerSource{Caller: caller.Caller{Kind: caller.Thread, Project: "alpha", Thread: "t-0001"}}
	if err := s.SetSandboxPreset(config.DefaultsTable, config.SandboxBrowser, &on); err != errHumanOnly {
		t.Errorf("agent: %v", err)
	}
	if c, _ := config.Load(); len(c.SandboxPresetsOf("")) != 0 {
		t.Error("written")
	}
}

// TestSandboxBadEntryFlagged: an entry that fails the checks (written by
// hand in config.toml) is marked with its reason, and the list says the
// project's sandbox settings are dropped until it is fixed or removed.
func TestSandboxBadEntryFlagged(t *testing.T) {
	_, m := sandboxData(t)
	m.data.Sandbox[""][config.SandboxExcluded] = []string{"docker compose *", "make a$X"}
	row := sandboxRow(t, "", "Excluded commands")
	if v := row.value(m); v != "2, 1 invalid" {
		t.Errorf("value %q", v)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"✗ make a$X", "has a variable", "dropped at launch"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "✗ docker compose") {
		t.Errorf("good entry flagged:\n%s", out)
	}
}
