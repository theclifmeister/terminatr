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
	m.top().(*inputView).text = "Bash(npx playwright *)"
	drain(t, m, keyPress(m, "enter"))
	keyPress(m, "a")
	m.top().(*inputView).text = "Bash"
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "allows every Bash call") {
		t.Errorf("msg %q", m.msg)
	}
	drain(t, m, keyPress(m, "x"))
	if want := []string{`defaults allow ["Bash(npx playwright *)"]`, `defaults allow []`}; !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %v", src.sandbox)
	}
	c, _ := config.Load()
	if _, ok := c.SandboxOf("")[config.SandboxAllow]; ok {
		t.Error("an empty all-projects list is still set")
	}
}

// TestSandboxTab: the project popup's Sandbox tab, after Settings: the
// guidance, the presets under Presets, the lists under Advanced, and
// none of them under Settings any more.
func TestSandboxTab(t *testing.T) {
	_, m := sandboxData(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	feed(m, keyPress(m, "a"))
	pv := m.top().(*projectView)
	keyPress(m, "5")
	if pv.tab != tabSandbox {
		t.Fatalf("5: tab %d", pv.tab)
	}
	out := screen(m)
	for _, want := range []string{"5 sandbox", "Prefer a preset", "a last resort", "may run any code as you without asking", "what applies on macOS",
		"Presets", "Browser testing (Playwright, Chromium)", "Local dev servers", "Claude and Codex threads", "Advanced", "Excluded commands", "1 · all projects", "Allow rules"} {
		if !strings.Contains(out, want) {
			t.Errorf("Sandbox tab lacks %q:\n%s", want, out)
		}
	}
	for _, r := range projectSettings("alpha") {
		if strings.Contains(r.label, "xcluded") || strings.Contains(r.label, "llow rules") {
			t.Errorf("Settings still has %q", r.label)
		}
	}
	// The , popup's third tab is all projects'.
	keyPress(m, "esc")
	press(m, ",", "3")
	if out := screen(m); !strings.Contains(out, "3 Sandbox") || !strings.Contains(out, "Browser testing") || !strings.Contains(out, "unless the project sets its own") {
		t.Errorf(", Sandbox tab:\n%s", out)
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
		if !slices.Equal(labels, []string{"Excluded commands", "Allow rules"}) {
			t.Errorf("%s: %q", goos, labels)
		}
		_, m := sandboxData(t)
		m.data.Server.OS = goos
		if out := strings.Join(sandboxHead(m, 200), " "); !strings.Contains(out, "No preset applies there") || !strings.Contains(out, config.OSName(goos)) {
			t.Errorf("%s head: %s", goos, out)
		}
	}
	rows := sandboxSettings("", config.OSMac)
	if len(rows) != 4 || rows[0].section != "Presets" || rows[2].section != "Advanced" {
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
	drain(t, m, row.unset(m))
	drain(t, m, all.change(m))
	want := []string{"defaults local_dev_servers on", "projects.alpha local_dev_servers off", "projects.alpha local_dev_servers follow", "defaults local_dev_servers follow"}
	if !slices.Equal(src.sandbox, want) {
		t.Errorf("saved %q", src.sandbox)
	}
	if c, _ = config.Load(); len(c.SandboxPresetsOf("")) != 0 || len(c.SandboxPresetsOf("alpha")) != 0 {
		t.Errorf("left set: %v %v", c.SandboxPresetsOf(""), c.SandboxPresetsOf("alpha"))
	}
}

// TestSandboxCoveredNote: an excluded command an allow rule covers says
// it runs outside the sandbox without asking, in both lists' views and
// when added.
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
	keyPress(m, "a")
	m.top().(*inputView).text = "Bash(docker compose up *)"
	drain(t, m, keyPress(m, "enter"))
	if cv, ok := m.top().(*confirmView); ok {
		t.Errorf("asked for a rule that covers nothing excluded: %q", cv.question)
	}
	keyPress(m, "a")
	m.top().(*inputView).text = "Bash(docker *)"
	drain(t, m, keyPress(m, "enter"))
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "covers the excluded docker compose *") {
		t.Errorf("no covered note before adding: %T", m.top())
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
