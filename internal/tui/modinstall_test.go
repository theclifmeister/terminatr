package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// infraGuardMod asks for permissions, adds a rule for threads, offers
// an agent tool and a guard rule: everything the consent shows in full.
const infraGuardMod = `
id          = "infra-guard"
name        = "Infra guard"
version     = "1.2.0"
api         = "1.0"
description = "Keeps agents off production."
authors     = ["Ada"]

[provides]
main = "main.ts"

[provides.rules]
thread = "rules.md"

[permissions]
project_read = true
tools        = true

[[provides.tools]]
id = "prod_status"
description = "Says whether production is frozen."

[[provides.guard]]
id = "no-destroy"
commands = ["terraform destroy"]
message = "Infrastructure is the human's to change."
`

// modFolder writes a mod folder outside ~/.terminatr and returns it.
func modFolder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src")
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// infraFolder is infraGuardMod's folder.
func infraFolder(t *testing.T) string {
	return modFolder(t, map[string]string{"mod.toml": infraGuardMod, "main.ts": "", "rules.md": "Never touch prod.\n"})
}

// installFrom presses i in the Mods tab, pastes arg into the prompt and
// presses enter, then runs the fetch.
func installFrom(t testing.TB, m *dash, arg string) {
	press(m, "i")
	m.Update(tea.PasteMsg{Content: arg + "\n"})
	drain(t, m, keyPress(m, "enter"))
}

// staging lists the staging folders left in ~/.terminatr/mods.
func staging(t *testing.T) []string {
	t.Helper()
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	all, _ := filepath.Glob(filepath.Join(dir, ".staging-*"))
	return all
}

// TestModsInstall: i asks for a folder or a git URL; what it names is
// fetched and checked, then its consent is drawn in full (permissions,
// the rules addendum, the tool and the guard rule) with "Install
// <name>?". y installs it off and asks whether to turn it on; y turns
// it on with every permission it asks for, and the tab selects it.
func TestModsInstall(t *testing.T) {
	src, m := modsDash(t)
	dir := infraFolder(t)
	installFrom(t, m, dir)
	if got := src.fetchedMods; !slices.Equal(got, []string{dir}) {
		t.Fatalf("fetched %q, want the pasted folder", got)
	}
	if _, ok := m.top().(*installView); !ok {
		t.Fatalf("after the fetch: %T, want the consent (%q)", m.top(), m.msg)
	}
	out := screen(m)
	for _, want := range []string{"Install Infra guard", "Infra guard 1.2.0 (infra-guard) by Ada", "reads your projects", "Never touch prod.", "Rules it adds for every thread",
		"Tool it offers your threads", "Says whether production is frozen.", "Guard rule it adds", "Refuses the commands: terraform destroy",
		"Infrastructure is the human's to change.", "Install Infra guard?", "It installs off", "y install"} {
		if !strings.Contains(out, want) {
			t.Errorf("the install consent lacks %q:\n%s", want, out)
		}
	}
	modAct(t, m, src, "y")
	md := modState(t, src, "infra-guard")
	if md.State != mod.Disabled || len(md.Grants) != 0 {
		t.Fatalf("y installed it %s with %v, want off with nothing granted", md.State, md.Grants)
	}
	if s, ok := mod.ReadSource(md.Dir); !ok || s.Path != dir {
		t.Errorf("its source: %+v, want %s", s, dir)
	}
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Turn it on now?") {
		t.Fatalf("after the install: %T, want the question to turn it on", m.top())
	}
	modAct(t, m, src, "y")
	md = modState(t, src, "infra-guard")
	if md.State != mod.Enabled || !slices.Equal(md.Grants, []string{mod.PermProjectRead, mod.PermTools}) {
		t.Fatalf("y turned it %s with %v", md.State, md.Grants)
	}
	sv := m.top().(*settingsView)
	if sel, _ := sv.mods.selMod(m); sel.ID != "infra-guard" {
		t.Errorf("the tab selects %s, want the new mod", sel.ID)
	}
	if left := staging(t); len(left) > 0 {
		t.Errorf("staging left behind: %v", left)
	}
}

// TestModsInstallNo: n on the consent installs nothing and removes the
// staged copy; n on the question after the install leaves it off.
func TestModsInstallNo(t *testing.T) {
	src, m := modsDash(t)
	installFrom(t, m, infraFolder(t))
	modAct(t, m, src, "n")
	if _, ok := m.top().(*settingsView); !ok || m.msg != "nothing installed" {
		t.Errorf("n on the consent: %T, %q", m.top(), m.msg)
	}
	if slices.ContainsFunc(src.data.Mods, func(md mod.Mod) bool { return md.ID == "infra-guard" }) {
		t.Fatal("n installed it")
	}
	if left := staging(t); len(left) > 0 {
		t.Errorf("staging left behind: %v", left)
	}
	installFrom(t, m, infraFolder(t))
	modAct(t, m, src, "y")
	modAct(t, m, src, "n")
	if md := modState(t, src, "infra-guard"); md.State != mod.Disabled {
		t.Errorf("n after the install left it %s", md.State)
	}
	if !strings.Contains(m.msg, "installed Infra guard, off") {
		t.Errorf("footer: %q", m.msg)
	}
}

// TestModsInstallRefused: what tm mod install refuses, the Mods tab
// refuses with the same reason, in the fetch's dialog, before any
// consent: a plain http:// URL, a subfolder leading out of the
// repository, a mod too big, one that needs a newer mod API, an id
// already installed, and a built-in mod's id. enter there takes the
// line back to change it.
func TestModsInstallRefused(t *testing.T) {
	big := map[string]string{"mod.toml": "id = \"big\"\nname = \"Big\"\nversion = \"1.0.0\"\napi = \"1.0\"\n"}
	for i := range 2001 {
		big[fmt.Sprintf("f/%04d", i)] = "x"
	}
	data := func(id, api string) map[string]string {
		return map[string]string{"mod.toml": "id = \"" + id + "\"\nname = \"X\"\nversion = \"1.0.0\"\napi = \"" + api + "\"\n"}
	}
	for _, tc := range []struct {
		name  string
		arg   func(t *testing.T) string
		wants string
	}{
		{"bad url", func(*testing.T) string { return "http://example.com/mod.git" }, "use https:// or ssh://"},
		{"no folder", func(*testing.T) string { return "/no/such/folder" }, "no such folder, and not a git URL"},
		{"refused path", func(*testing.T) string { return "https://example.com/r.git//a/../../etc" }, ".. isn't allowed"},
		{"too big", func(t *testing.T) string { return modFolder(t, big) }, "more than 2000 files"},
		{"incompatible", func(t *testing.T) string { return modFolder(t, data("later", "1.99")) }, "mod API 1.99"},
		{"installed", func(t *testing.T) string { return modFolder(t, data("nord", "1.0")) }, "nord is already installed"},
		{"built in", func(t *testing.T) string { return modFolder(t, data("memory-tab", "1.0")) }, "it is a built-in mod's id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, m := modsDash(t)
			arg := tc.arg(t)
			installFrom(t, m, arg)
			fv, ok := m.top().(*fetchView)
			if !ok || fv.err == nil {
				t.Fatalf("%s: %T, want the fetch's dialog with its error", arg, m.top())
			}
			if out := screen(m); !strings.Contains(ansi.Strip(out), "Can't install it") || !strings.Contains(fv.err.Error(), tc.wants) {
				t.Errorf("error %q, want %q:\n%s", fv.err, tc.wants, out)
			}
			if left := staging(t); len(left) > 0 {
				t.Errorf("staging left behind: %v", left)
			}
			press(m, "enter")
			if in, ok := m.top().(*inputView); !ok || in.text != arg {
				t.Errorf("enter on the error: %T, want the line back", m.top())
			}
			src.loadMods()
			if len(src.data.Mods) != 3+len(mod.BundledIDs()) {
				t.Errorf("a refused install changed the mods: %d", len(src.data.Mods))
			}
		})
	}
}

// TestModsInstallCancel: esc while the fetch runs drops what comes; the
// staged copy is removed and no consent opens.
func TestModsInstallCancel(t *testing.T) {
	_, m := modsDash(t)
	press(m, "i")
	m.Update(tea.PasteMsg{Content: infraFolder(t)})
	cmd := keyPress(m, "enter")
	if out := screen(m); !strings.Contains(out, "Fetching and checking it") {
		t.Errorf("while it fetches:\n%s", out)
	}
	press(m, "esc")
	drain(t, m, cmd)
	if _, ok := m.top().(*settingsView); !ok {
		t.Errorf("after esc: %T, want the Mods tab", m.top())
	}
	if left := staging(t); len(left) > 0 {
		t.Errorf("staging left behind: %v", left)
	}
}

// TestModsInstallAgent: tm refuses to fetch or install a mod inside an
// agent, as for every mod change.
func TestModsInstallAgent(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	s := &ServerSource{Caller: caller.Caller{Kind: caller.Thread, Thread: "t-0001"}}
	if _, err := s.FetchMod(infraFolder(t)); !errors.Is(err, mod.ErrHumanOnly) {
		t.Errorf("an agent's fetch: %v", err)
	}
	st, err := (&ServerSource{Caller: caller.Caller{Kind: caller.Human}}).FetchMod(infraFolder(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := s.InstallMod(st); !errors.Is(err, mod.ErrHumanOnly) {
		t.Errorf("an agent's install: %v", err)
	}
}

// TestModsExamplesInstall: in the examples list, i installs the
// selected example from its tree URL at this tm's tag; a built-in one
// says it is built in already and fetches nothing.
func TestModsExamplesInstall(t *testing.T) {
	src, m := modsDash(t)
	press(m, "e")
	ev := m.top().(*examplesView)
	ev.sel = slices.IndexFunc(mod.Examples, func(e mod.Example) bool { return e.ID == "memory-tab" })
	press(m, "i")
	if len(src.fetchedMods) != 0 || !strings.Contains(m.msg, "memory-tab is built in already") {
		t.Errorf("i on a built-in example: fetched %q, %q", src.fetchedMods, m.msg)
	}
	ev.sel = slices.IndexFunc(mod.Examples, func(e mod.Example) bool { return e.ID == "standup" })
	if out := screen(m); !strings.Contains(out, "i install") {
		t.Errorf("the examples' keys:\n%s", out)
	}
	keyPress(m, "i") // its fetch isn't run: it would clone from GitHub
	fv, ok := m.top().(*fetchView)
	if !ok || fv.arg != mod.ExampleSource("standup") {
		t.Fatalf("i on standup: %T, want its fetch of %s", m.top(), mod.ExampleSource("standup"))
	}
	if out := screen(m); !strings.Contains(out, "github.com/theclifmeister/terminatr/tree/") {
		t.Errorf("the fetch names the example's URL:\n%s", out)
	}
}

// TestModInstallConsentGolden: the install consent, as tm draws it, is
// testdata/mod-install-consent.golden (go test -run
// TestModInstallConsentGolden -update rewrites it).
func TestModInstallConsentGolden(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	st, err := (&ServerSource{Caller: caller.Caller{Kind: caller.Human}}).FetchMod(infraFolder(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Source.Path = "/home/ada/mods/infra-guard"
	var b strings.Builder
	for _, l := range (&installView{st: st}).lines(72) {
		b.WriteString(strings.TrimRight(ansi.Strip(l), " ") + "\n")
	}
	path := filepath.Join("testdata", "mod-install-consent.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestModInstallConsentGolden -update writes it)", err)
	}
	if got := b.String(); got != string(want) {
		t.Errorf("the install consent differs from %s:\n%s", path, got)
	}
}
