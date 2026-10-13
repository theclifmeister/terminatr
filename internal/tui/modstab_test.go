package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The fake's mod calls are the real ones, as the human, on the test's
// TERMINATR_HOME; its Load lists the mods again when modsLive is set.
func (f *fakeSource) human() *ServerSource {
	return &ServerSource{Caller: caller.Caller{Kind: caller.Human}}
}

func (f *fakeSource) ModAct(id, verb string) error { return f.human().ModAct(id, verb) }
func (f *fakeSource) ApplyModDefaults(id string) ([]mod.Suggestion, error) {
	return f.human().ApplyModDefaults(id)
}
func (f *fakeSource) SetModGrants(id string, grants []string) error {
	return f.human().SetModGrants(id, grants)
}
func (f *fakeSource) SetModSetting(id, key string, value any) error {
	return f.human().SetModSetting(id, key, value)
}
func (f *fakeSource) FetchModUpdate(id string) (*mod.Staged, error) {
	return f.human().FetchModUpdate(id)
}
func (f *fakeSource) ReplaceMod(id string, st *mod.Staged, grant []string) error {
	return f.human().ReplaceMod(id, st, grant)
}
func (f *fakeSource) FetchMod(arg string) (*mod.Staged, error) {
	f.fetchedMods = append(f.fetchedMods, arg)
	return f.human().FetchMod(arg)
}
func (f *fakeSource) InstallMod(st *mod.Staged) error { return f.human().InstallMod(st) }
func (f *fakeSource) ModLog(id string, max int64) (string, bool, error) {
	return f.human().ModLog(id, max)
}

func (f *fakeSource) RestartMod(id string) error {
	f.restarted = append(f.restarted, id)
	return nil
}

// loadMods lists the installed mods into f's data, as ServerSource.Load.
func (f *fakeSource) loadMods() {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	if reg, err := mod.Load(cfg); err == nil {
		f.data.Mods, f.data.ModData = reg.Mods, modData(reg.Mods)
	}
}

const reviewAgeMod = `
id          = "review-age"
name        = "Review age"
version     = "0.3.1"
api         = "1.0"
description = "How long each task has waited in review."
authors     = ["Ada"]

[provides]
main    = "main.ts"
columns = [{ id = "age", title = "age", width = 4, where = ["sidebar.thread"] }]

[permissions]
project_read = true
net          = ["api.example.com"]

[provides.rules]
coordinator = "rules.md"

[settings]
warn_after_hours = { type = "int", default = 24, min = 1, max = 720, label = "Warn after (hours)" }
wording          = { type = "string", default = "waiting", label = "Word in the column" }
`

// installMod writes a mod folder under ~/.terminatr/mods.
func installMod(t *testing.T, id string, files map[string]string) {
	t.Helper()
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, id, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// modsDash is a dashboard over three installed mods (Review age, which
// adds a rule for the coordinator; Nord, a theme; and one that needs a
// newer mod API), with the settings popup open on its Mods tab.
func modsDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	installMod(t, "review-age", map[string]string{"mod.toml": reviewAgeMod, "main.ts": "", "rules.md": "Label every review task with its age.\n"})
	installMod(t, "nord", map[string]string{"mod.toml": "id = \"nord\"\nname = \"Nord\"\nversion = \"0.4.0\"\napi = \"1.0\"\ndescription = \"The Nord palette.\"\n[provides]\ntheme = \"t.toml\"\n", "t.toml": ""})
	installMod(t, "future", map[string]string{"mod.toml": "id = \"future\"\nname = \"Future\"\nversion = \"1.0.0\"\napi = \"1.9\"\n"})
	src := &fakeSource{data: testData()}
	src.loadMods()
	m := newDash(DashOptions{Source: src, Width: 160, Height: 50, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",", "3")
	return src, m
}

// modAct presses key and runs what it starts, then polls the source
// again, mods included.
func modAct(m *dash, src *fakeSource, key string) {
	run(m, keyPress(m, key))
	src.loadMods()
	m.setData(src.data)
}

// selectMod selects the Mods tab's mod id.
func selectMod(t *testing.T, m *dash, id string) {
	t.Helper()
	for i, md := range m.data.Mods {
		if md.ID == id {
			m.top().(*settingsView).mods.sel = i
			return
		}
	}
	t.Fatalf("no mod %q", id)
}

func modState(t *testing.T, src *fakeSource, id string) mod.Mod {
	t.Helper()
	for _, md := range src.data.Mods {
		if md.ID == id {
			return md
		}
	}
	t.Fatalf("no mod %q", id)
	return mod.Mod{}
}

// TestModsTabList: the settings popup's third tab lists every installed
// mod with its mark, name, what it provides, version and status (off,
// or why it can't run), and the selected one's description under the
// list; its keys are the Mods tab's. l is its log there, not the next
// tab; → still moves on.
func TestModsTabList(t *testing.T) {
	_, m := modsDash(t)
	out := screen(m)
	for _, want := range []string{"3 Mods", "○ Nord", "theme", "0.4.0", "off", "Review age", "code · column · rules", "0.3.1", "▲ Future", "incompatible: future needs mod API 1.9", "e examples", "p permissions"} {
		if !strings.Contains(out, want) {
			t.Errorf("Mods tab lacks %q:\n%s", want, out)
		}
	}
	selectMod(t, m, "review-age")
	if out := screen(m); !strings.Contains(out, "Review age: How long each task has waited in review. By Ada.") {
		t.Errorf("the selected mod's description is missing:\n%s", out)
	}
	press(m, "l")
	if _, ok := m.top().(*textView); !ok {
		t.Fatalf("l opened %T, not the log", m.top())
	}
	if out := screen(m); !strings.Contains(out, "No log yet") {
		t.Errorf("an empty log says so:\n%s", out)
	}
	press(m, "q")
	run(m, keyPress(m, "right"))
	if sv := m.top().(*settingsView); sv.tab != 0 {
		t.Errorf("→ from Mods goes to tab %d, want General", sv.tab)
	}
}

// TestModsConsent: enter on an off mod opens its consent, each
// permission in plain words, its rules addendum in full and "Enable
// <name>?"; n leaves it off, y turns it on with every permission it
// asks for, saved to the settings file; enter on an on mod turns it
// off with no question. A mod that can't run says why.
func TestModsConsent(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "enter")
	out := screen(m)
	for _, want := range []string{"Enable Review age", "reads your projects", "talks to api.example.com", "Label every review task with its age.", "Rules it adds for every coordinator"} {
		if !strings.Contains(out, want) {
			t.Errorf("consent lacks %q:\n%s", want, out)
		}
	}
	modAct(m, src, "n")
	if md := modState(t, src, "review-age"); md.State != mod.Disabled {
		t.Fatalf("n turned it %s", md.State)
	}
	press(m, "enter")
	modAct(m, src, "y")
	md := modState(t, src, "review-age")
	if md.State != mod.Enabled || !slices.Equal(md.Grants, []string{mod.PermProjectRead, mod.PermNet}) {
		t.Fatalf("after y: %s with %v", md.State, md.Grants)
	}
	if cfg, _ := config.Load(); !cfg.Mods["review-age"].Enabled {
		t.Errorf("the settings file doesn't have it on: %+v", cfg.Mods)
	}
	if out := screen(m); !strings.Contains(out, "● Review age") || !strings.Contains(out, "reads your projects · talks to api.example.com") {
		t.Errorf("an on mod shows ● and its permissions:\n%s", out)
	}
	modAct(m, src, "enter")
	if md := modState(t, src, "review-age"); md.State != mod.Disabled {
		t.Errorf("enter on an on mod left it %s", md.State)
	}

	selectMod(t, m, "future")
	press(m, "enter")
	if _, ok := m.top().(*settingsView); !ok || !strings.Contains(m.msg, "Future can't be turned on") {
		t.Errorf("enter on a mod that can't run: %T, %q", m.top(), m.msg)
	}
}

// TestModsWantsMore: a mod on with less than it asks for shows ◆ and
// what it wants more; p takes a permission back at once and grants one
// after y; enter on it asks for what it wants more, o turns it off.
func TestModsWantsMore(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "enter")
	modAct(m, src, "y")
	press(m, "p")
	if out := screen(m); !strings.Contains(out, "Review age · permissions") || !strings.Contains(out, "granted") {
		t.Fatalf("p doesn't list its permissions:\n%s", out)
	}
	press(m, "down") // talks to api.example.com
	modAct(m, src, "enter")
	if md := modState(t, src, "review-age"); !slices.Equal(md.WantsMore, []string{mod.PermNet}) {
		t.Fatalf("taking net back: wants more %v", md.WantsMore)
	}
	press(m, "esc")
	out := screen(m)
	if !strings.Contains(out, "◆ Review age") || !strings.Contains(out, "wants more: talks to api.example.com") {
		t.Errorf("a mod that wants more shows ◆:\n%s", out)
	}
	press(m, "enter")
	if out := screen(m); !strings.Contains(out, "Grant Review age what it wants more?") || !strings.Contains(out, "· new") {
		t.Errorf("its consent asks for what it wants more:\n%s", out)
	}
	modAct(m, src, "o")
	if md := modState(t, src, "review-age"); md.State != mod.Disabled {
		t.Errorf("o left it %s", md.State)
	}
	press(m, "p", "down", "enter")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "talks to api.example.com") {
		t.Fatalf("granting asks first: %T", m.top())
	}
	modAct(m, src, "y")
	if md := modState(t, src, "review-age"); !md.Granted(mod.PermNet) {
		t.Errorf("y didn't grant net: %v", md.Grants)
	}
}

// TestModsSettingsPage: s lists the mod's own settings from its
// manifest; + changes a number within its range, x puts it back.
func TestModsSettingsPage(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "s")
	if out := screen(m); !strings.Contains(out, "warn_after_hours") || !strings.Contains(out, "Warn after (hours)") || !strings.Contains(out, "24") {
		t.Fatalf("s doesn't show its settings:\n%s", out)
	}
	modAct(m, src, "+")
	if md := modState(t, src, "review-age"); md.Settings["warn_after_hours"] != int64(25) {
		t.Errorf("+ set %v (%s)", md.Settings["warn_after_hours"], m.msg)
	}
	modAct(m, src, "x")
	if md := modState(t, src, "review-age"); md.Settings["warn_after_hours"] != int64(24) {
		t.Errorf("x left %v", md.Settings["warn_after_hours"])
	}
	press(m, "esc")
	selectMod(t, m, "nord")
	press(m, "s")
	if out := screen(m); !strings.Contains(out, "It has no settings.") {
		t.Errorf("a mod without settings says so:\n%s", out)
	}
}

// TestModsTextSetting: enter on a text setting opens a line with its
// value; enter saves what was typed or pasted, esc leaves it, x puts
// the default back, and a value with a control character is refused.
func TestModsTextSetting(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "s", "down")
	if out := screen(m); !strings.Contains(out, "wording") || !strings.Contains(out, "waiting") {
		t.Fatalf("s doesn't show the text setting:\n%s", out)
	}
	clear := tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	edit := func(text string) {
		t.Helper()
		press(m, "enter")
		in, ok := m.top().(*inputView)
		if !ok || in.text != modState(t, src, "review-age").Settings["wording"] {
			t.Fatalf("enter doesn't open its value: %T", m.top())
		}
		m.Update(clear)
		m.Update(tea.PasteMsg{Content: text})
	}

	edit("in review for")
	press(m, "esc")
	if md := modState(t, src, "review-age"); md.Settings["wording"] != "waiting" || m.msg != "wording unchanged" {
		t.Errorf("esc saved %q (%q)", md.Settings["wording"], m.msg)
	}
	if _, ok := m.top().(*modSettingsView); !ok {
		t.Fatalf("esc doesn't go back to the settings: %T", m.top())
	}

	edit("in review for")
	modAct(m, src, "enter")
	if md := modState(t, src, "review-age"); md.Settings["wording"] != "in review for" {
		t.Errorf("enter saved %q (%s)", md.Settings["wording"], m.msg)
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"))
	if !strings.Contains(string(data), "[mods.review-age.settings]\nwording = \"in review for\"\n") {
		t.Errorf("config.toml:\n%s", data)
	}

	modAct(m, src, "x")
	if md := modState(t, src, "review-age"); md.Settings["wording"] != "waiting" {
		t.Errorf("x left %q", md.Settings["wording"])
	}

	// Typed text stays on one line; the write refuses a control
	// character or a value too long all the same.
	if err := src.SetModSetting("review-age", "wording", "two\nlines"); err == nil || !strings.Contains(err.Error(), "one line") {
		t.Errorf("a newline: %v", err)
	}
	if err := src.SetModSetting("review-age", "wording", strings.Repeat("a", mod.MaxTextSetting+1)); err == nil {
		t.Error("a value over the cap was saved")
	}
	edit("a\tb")
	if in := m.top().(*inputView); in.text != "a b" {
		t.Errorf("a pasted tab: %q", in.text)
	}
	press(m, "esc")
}

// TestModsFailure: a mod whose host failed shows ▲ failed with its last
// error and its cells' fate; the footer says so once, naming where the
// details are. A built-in mod says built in, and can't be removed or
// updated on its own; x removes another after y.
func TestModsFailure(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "enter")
	modAct(m, src, "y")
	m.msg = ""
	src.data.ModHosts = map[string]proto.ModHost{"review-age": {ID: "review-age", State: "failed", Err: "401 from api.example.com"}}
	m.setData(src.data)
	if m.msg != "mod Review age failed · , then 3 for details" {
		t.Errorf("footer: %q", m.msg)
	}
	m.msg = ""
	m.setData(src.data)
	if m.msg != "" {
		t.Errorf("the note shows again: %q", m.msg)
	}
	out := screen(m)
	for _, want := range []string{"▲ Review age", "failed: 401 from api.example.com", "l shows its log"} {
		if !strings.Contains(out, want) {
			t.Errorf("a failed mod lacks %q:\n%s", want, out)
		}
	}

	src.data.ModHosts = nil
	for i := range src.data.Mods {
		src.data.Mods[i].Builtin = src.data.Mods[i].ID == "nord"
	}
	m.setData(src.data)
	selectMod(t, m, "nord")
	if out := screen(m); !strings.Contains(out, "theme · built in") {
		t.Errorf("a built-in mod says so:\n%s", out)
	}
	for _, k := range []string{"x", "u"} {
		m.msg = ""
		press(m, k)
		if _, ok := m.top().(*settingsView); !ok || !strings.Contains(m.msg, "built in") {
			t.Errorf("%s on a built-in mod: %T, %q", k, m.top(), m.msg)
		}
	}

	selectMod(t, m, "future")
	press(m, "x")
	modAct(m, src, "y")
	if slices.ContainsFunc(src.data.Mods, func(md mod.Mod) bool { return md.ID == "future" }) {
		t.Errorf("x, y didn't remove it: %v", src.data.Mods)
	}
}

// TestModsHostStates: an enabled code mod is marked by its host's
// state (mods design §8): running ● with its pid and memory in the
// details, starting ◌, idle ● waiting for its first event, backoff ▲
// restarting in so many seconds, failed ▲; a data mod is unchanged. r
// restarts a code mod that is on, and refuses a data mod and an off one.
func TestModsHostStates(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	press(m, "enter")
	modAct(m, src, "y")
	selectMod(t, m, "nord")
	press(m, "enter")
	modAct(m, src, "y")
	host := func(h proto.ModHost) {
		h.ID = "review-age"
		src.data.ModHosts = map[string]proto.ModHost{"review-age": h, "nord": {ID: "nord", State: "failed", Err: "never"}}
		m.msg = ""
		m.setData(src.data)
		selectMod(t, m, "review-age")
	}
	for _, c := range []struct {
		name string
		h    proto.ModHost
		want []string
	}{
		{"running", proto.ModHost{State: "running", PID: 4242, RSS: 42_000_000, Restarts: 1}, []string{"● Review age", "Runs as pid 4242, using 42", "restarted 1×"}},
		{"starting", proto.ModHost{State: "starting"}, []string{"◌ Review age", "Starting: its host loads its script."}},
		{"idle", proto.ModHost{State: "idle"}, []string{"● Review age", "waits for its first", "Waits for its first event: its host starts then."}},
		{"backoff", proto.ModHost{State: "backoff", Next: time.Now().Add(4500 * time.Millisecond), Err: "exit status 1"}, []string{"▲ Review age", "restarting in 5s: exit status 1", "r restarts it now"}},
		{"failed", proto.ModHost{State: "failed", Err: "crashed 3 times in 5m"}, []string{"▲ Review age", "failed: crashed 3 times in 5m", "r restarts it; l shows its log"}},
	} {
		host(c.h)
		out := screen(m)
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s lacks %q:\n%s", c.name, want, out)
			}
		}
		if strings.Contains(out, "▲ Nord") || strings.Contains(out, "failed: never") {
			t.Errorf("%s: a data mod took a host's state:\n%s", c.name, out)
		}
		if note := strings.Contains(m.msg, "failed"); note != (c.name == "failed") {
			t.Errorf("%s: footer %q", c.name, m.msg)
		}
	}

	run(m, keyPress(m, "r"))
	if !slices.Equal(src.restarted, []string{"review-age"}) || m.msg != "restarted Review age" {
		t.Errorf("r: restarted %v, %q", src.restarted, m.msg)
	}
	selectMod(t, m, "nord")
	m.msg = ""
	run(m, keyPress(m, "r"))
	if len(src.restarted) != 1 || !strings.Contains(m.msg, "no code to restart") {
		t.Errorf("r on a data mod: restarted %v, %q", src.restarted, m.msg)
	}
	selectMod(t, m, "review-age")
	modAct(m, src, "enter")
	m.msg = ""
	run(m, keyPress(m, "r"))
	if len(src.restarted) != 1 || !strings.Contains(m.msg, "is off") {
		t.Errorf("r on an off mod: restarted %v, %q", src.restarted, m.msg)
	}
}

// TestModsExamples: e shows where the example mods are and each one's
// install line, and installs nothing; with no mod of the user's
// installed the tab lists the built-in ones, says how to install one
// and points at e.
func TestModsExamples(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	src := &fakeSource{data: testData()}
	src.loadMods()
	m := newDash(DashOptions{Source: src, Width: 160, Height: 60, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",", "3")
	if out := screen(m); !strings.Contains(out, "Default theme") || !strings.Contains(out, "No mods of your own yet") || !strings.Contains(out, "lists terminatr's example mods") {
		t.Errorf("the tab with only built-in mods:\n%s", out)
	}
	press(m, "e")
	out := screen(m)
	for _, want := range []string{"Example mods", mod.ExamplesURL, "git clone --depth 1 --branch " + mod.ExamplesRef + " " + mod.ExamplesRepo, "tm mod install " + mod.ExamplesURL + "/nord", "memory-tab", "tm mod install " + mod.ExamplesURL + "/standup", "copy it under another id", "Nothing here is installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("examples lack %q:\n%s", want, out)
		}
	}
	src.loadMods()
	for _, md := range src.data.Mods {
		if !md.Builtin {
			t.Errorf("e installed %s", md.ID)
		}
	}
}

// TestModsUpdate: u on a mod installed from a folder says when it is up
// to date; when its source asks for more, it shows what it now asks
// for and updates on y, granting that to a mod that is on.
func TestModsUpdate(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	srcDir := filepath.Join(t.TempDir(), "review-age")
	write := func(manifest string) {
		for name, body := range map[string]string{"mod.toml": manifest, "main.ts": "", "rules.md": "Label every review task with its age.\n"} {
			os.MkdirAll(srcDir, 0o755)
			if err := os.WriteFile(filepath.Join(srcDir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(reviewAgeMod)
	st, err := mod.Fetch(mod.Source{Path: srcDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Install(caller.Caller{Kind: caller.Human}, st); err != nil {
		t.Fatal(err)
	}
	st.Close()
	src := &fakeSource{data: testData()}
	src.loadMods()
	m := newDash(DashOptions{Source: src, Width: 160, Height: 50, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",", "3", "enter")
	modAct(m, src, "y")

	modAct(m, src, "u")
	if m.msg != "Review age is up to date" {
		t.Errorf("u with nothing new: %q", m.msg)
	}
	write(strings.Replace(strings.Replace(reviewAgeMod, `"0.3.1"`, `"0.4.0"`, 1), `net          = ["api.example.com"]`, "net          = [\"api.example.com\"]\ncommands     = [\"glab\"]", 1))
	modAct(m, src, "u")
	out := screen(m)
	for _, want := range []string{"Update Review age", "0.3.1 → 0.4.0", "It now asks for", "runs glab"} {
		if !strings.Contains(out, want) {
			t.Errorf("the update's question lacks %q:\n%s", want, out)
		}
	}
	modAct(m, src, "y")
	md := modState(t, src, "review-age")
	if md.Manifest.Version != "0.4.0" || !md.Granted(mod.PermCommands) || md.State != mod.Enabled {
		t.Errorf("after y: %s %v %s (%s)", md.Manifest.Version, md.Grants, md.State, m.msg)
	}
}

// TestModsProjectLine: a project's Settings tab has a Mods line once a
// mod is limited to some projects, naming those that run for it.
func TestModsProjectLine(t *testing.T) {
	src, m := modsDash(t)
	press(m, "esc")
	if rows := m.projectRows("beta"); slices.ContainsFunc(rows, func(r setting) bool { return r.label == "Mods" }) {
		t.Error("a Mods line with no mod limited to some projects")
	}
	if err := mod.SetProjects(caller.Caller{Kind: caller.Human}, "nord", []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	if err := mod.SetEnabled(caller.Caller{Kind: caller.Human}, "nord", true); err != nil {
		t.Fatal(err)
	}
	src.loadMods()
	m.setData(src.data)
	for slug, want := range map[string]string{"beta": "Nord", "alpha": "none limited to it"} {
		rows := m.projectRows(slug)
		i := slices.IndexFunc(rows, func(r setting) bool { return r.label == "Mods" })
		if i < 0 {
			t.Fatalf("%s has no Mods line", slug)
		}
		if got := rows[i].value(m); got != want {
			t.Errorf("%s: Mods %q, want %q", slug, got, want)
		}
	}
}

// TestModsDataAndBuiltin: an enabled data mod whose file doesn't load
// is ▲ with what doesn't load; a built-in mod says it is built in, and
// one tm can't do without isn't turned off.
func TestModsDataAndBuiltin(t *testing.T) {
	src, m := modsDash(t)
	installMod(t, "askew", map[string]string{"mod.toml": "id = \"askew\"\nname = \"Askew\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nlayout = \"l.toml\"\n", "l.toml": "sidebar = [\"bogus\"]\n"})
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte("[mods.askew]\nenabled = true\n"), 0o600)
	src.loadMods()
	m.setData(src.data)
	selectMod(t, m, "askew")
	out := screen(m)
	for _, want := range []string{"▲ Askew", "doesn't load its layout", `Doesn't load its layout: sidebar: no column "bogus"`} {
		if !strings.Contains(out, want) {
			t.Errorf("askew lacks %q:\n%s", want, out)
		}
	}
	selectMod(t, m, mod.BundledTheme)
	out = screen(m)
	for _, want := range []string{"● Default theme", "theme · built in", "Built into tm", "It can't be turned off; pick another theme in General settings."} {
		if !strings.Contains(out, want) {
			t.Errorf("default-theme lacks %q:\n%s", want, out)
		}
	}
	modAct(m, src, "enter")
	if !strings.Contains(m.msg, "can't be turned off") || modState(t, src, mod.BundledTheme).State != mod.Enabled {
		t.Errorf("enter on default-theme: %q", m.msg)
	}
	press(m, "x")
	if !strings.Contains(m.msg, "Default theme is built in: tm can't do without it") {
		t.Errorf("x on default-theme: %q", m.msg)
	}
	selectMod(t, m, mod.BundledTemplates)
	modAct(m, src, "enter")
	if modState(t, src, mod.BundledTemplates).State != mod.Disabled {
		t.Errorf("enter on default-templates: %q", m.msg)
	}
}

// TestModsDefaults: d shows the defaults a mod suggests for all
// projects beside what they follow now; y applies them once it's on,
// to config.toml's [defaults], leaving the one the human set there and
// the one already at its value. Its consent lists them.
func TestModsDefaults(t *testing.T) {
	src, m := modsDash(t)
	installMod(t, "policy", map[string]string{"mod.toml": "id = \"policy\"\nname = \"Policy\"\nversion = \"1.0.0\"\napi = \"1.1\"\n" +
		"[defaults]\nauto_close = \"days\"\nauto_close_days = 3\npr_followup = true\n"})
	if err := config.SetDefaults("auto_close_days", 5); err != nil {
		t.Fatal(err)
	}
	src.loadMods()
	m.setData(src.data)
	selectMod(t, m, "review-age")
	press(m, "d")
	if _, ok := m.top().(*settingsView); !ok || !strings.Contains(m.msg, "Review age suggests no defaults") {
		t.Fatalf("d on a mod without defaults: %T %q", m.top(), m.msg)
	}
	selectMod(t, m, "policy")
	press(m, "enter")
	if out := screen(m); !strings.Contains(out, "It suggests defaults for all projects") || !strings.Contains(out, `auto_close = "days"`) {
		t.Errorf("consent lacks the defaults:\n%s", out)
	}
	press(m, "esc")
	press(m, "d")
	out := screen(m)
	for _, want := range []string{"Policy · defaults", `"merged" → "days"`, "5 · yours, stays (it suggests 3)", "true · already", "It is off"} {
		if !strings.Contains(out, want) {
			t.Errorf("defaults lacks %q:\n%s", want, out)
		}
	}
	press(m, "y")
	if cfg, _ := config.Load(); slices.Contains(cfg.OwnDefaults(), "auto_close") {
		t.Fatal("y applied an off mod's defaults")
	}
	press(m, "esc")
	modAct(m, src, "enter")
	modAct(m, src, "y")
	press(m, "d")
	if out := screen(m); !strings.Contains(out, "y apply") {
		t.Errorf("an on mod's defaults offer y:\n%s", out)
	}
	modAct(m, src, "y")
	if !strings.Contains(m.msg, "all projects take Policy's auto_close") {
		t.Errorf("msg %q", m.msg)
	}
	cfg, _ := config.Load()
	s, _ := cfg.AllProjects()
	if s.AutoClose != config.CloseDays || s.AutoCloseDays != 5 || !slices.Equal(cfg.OwnDefaults(), []string{"auto_close", "auto_close_days"}) {
		t.Errorf("after y: %s %d, [defaults] sets %v", s.AutoClose, s.AutoCloseDays, cfg.OwnDefaults())
	}
}

// TestModsSidebarHidden: a mod with a sidebar section says it is
// hidden while the sidebar is slim or hidden, and not while it's full.
func TestModsSidebarHidden(t *testing.T) {
	_, m := modsDash(t)
	md := mod.Mod{ID: "ticker", Manifest: &mod.Manifest{ID: "ticker", Name: "Ticker", Provides: mod.Provides{Main: "main.ts",
		Views: []mod.View{{ID: "ticker", Title: "ticker", Slot: mod.SlotSidebarSection}}}}}
	const want = "Its sidebar sections are hidden"
	if got := strings.Join(modAbout(m, md, 200), "\n"); strings.Contains(got, want) {
		t.Errorf("full sidebar:\n%s", got)
	}
	m.layout.Sidebar.Slim = true
	if got := strings.Join(modAbout(m, md, 200), "\n"); !strings.Contains(got, want) {
		t.Errorf("slim sidebar:\n%s", got)
	}
	m.layout.Sidebar.Slim, m.layout.Sidebar.Hidden = false, true
	if got := strings.Join(modAbout(m, md, 200), "\n"); !strings.Contains(got, want) {
		t.Errorf("hidden sidebar:\n%s", got)
	}
}
