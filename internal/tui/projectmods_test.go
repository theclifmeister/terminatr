package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// projectMods opens slug's project popup on its Mods tab, mod id
// selected.
func projectMods(t *testing.T, m *dash, slug, id string) *projectView {
	t.Helper()
	for m.top() != nil {
		m.pop()
	}
	m.openTab(slug, tabProjMods, 0)
	pv := m.projectPopupView()
	i := slices.IndexFunc(m.data.Mods, func(md mod.Mod) bool { return md.ID == id })
	if i < 0 {
		t.Fatalf("no mod %q", id)
	}
	pv.sel[tabProjMods] = i
	return pv
}

// TestProjectModsTab: the project popup's last tab lists every mod with
// where it runs in the project's words; space turns one on for the
// project after its consent, on for one more without, and off as tm mod
// disable --project does; off for a mod that runs for every project
// asks, then limits it to the others.
func TestProjectModsTab(t *testing.T) {
	src, m := modsDash(t)
	projectMods(t, m, "beta", "review-age")
	out := screen(m)
	for _, want := range []string{"9 messages", " mods ", "Review age", "follows all projects: off", "settings: all projects", "space on/off here"} {
		if !strings.Contains(out, want) {
			t.Errorf("the Mods tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0 mods") {
		t.Error("the Mods tab has a number key")
	}

	modAct(t, m, src, "space")
	cv, ok := m.top().(*consentView)
	if !ok || cv.slug != "beta" || !strings.Contains(screen(m), "Enable Review age for beta?") {
		t.Fatalf("space on an off mod: %T\n%s", m.top(), screen(m))
	}
	modAct(t, m, src, "Y")
	if md := modState(t, src, "review-age"); md.State != mod.Enabled || !slices.Equal(md.Projects, []string{"beta"}) || m.msg != "Review age on for beta" {
		t.Fatalf("after Y: %s %v (%s)", md.State, md.Projects, m.msg)
	}
	if out := screen(m); !strings.Contains(lineWith(out, "Review age"), "on for this project") {
		t.Errorf("after Y:\n%s", out)
	}

	// Another project: off there, on with no consent (it grants nothing new).
	projectMods(t, m, "alpha", "review-age")
	if out := screen(m); !strings.Contains(lineWith(out, "Review age"), "off for this project · runs for beta") {
		t.Errorf("alpha's row:\n%s", out)
	}
	modAct(t, m, src, "space")
	if md := modState(t, src, "review-age"); !slices.Equal(md.Projects, []string{"beta", "alpha"}) {
		t.Fatalf("on for alpha: %v (%s, %T)", md.Projects, m.msg, m.top())
	}

	// Off for one, then the last: the mod is off and follows all projects.
	modAct(t, m, src, "space")
	projectMods(t, m, "beta", "review-age")
	modAct(t, m, src, "space")
	if md := modState(t, src, "review-age"); md.State == mod.Enabled || len(md.Projects) != 0 {
		t.Fatalf("off for both: %s %v (%s)", md.State, md.Projects, m.msg)
	}

	// A mod on for every project: off for this one asks, then runs only
	// for the others.
	if err := mod.Enable(caller.Caller{Kind: caller.Human}, "review-age", ""); err != nil {
		t.Fatal(err)
	}
	src.loadMods()
	m.setData(src.data)
	projectMods(t, m, "beta", "review-age")
	if out := screen(m); !strings.Contains(lineWith(out, "Review age"), "follows all projects: on") {
		t.Errorf("on for all:\n%s", out)
	}
	modAct(t, m, src, "space")
	if c, ok := m.top().(*confirmView); !ok || !strings.Contains(c.question, "Run it only for alpha instead?") {
		t.Fatalf("off for one of all: %T %+v", m.top(), m.top())
	}
	modAct(t, m, src, "Y")
	if md := modState(t, src, "review-age"); md.State != mod.Enabled || !slices.Equal(md.Projects, []string{"alpha"}) || m.msg != "Review age off for beta" {
		t.Fatalf("after Y: %s %v (%s)", md.State, md.Projects, m.msg)
	}
}

// TestProjectModsSettings: enter opens the mod's settings for the
// project, where a key it doesn't set follows all projects, + writes the
// project's own value and X follows all projects again; the row and the
// Settings tab's Mods line count it (mod API 1.11). A built-in mod's
// settings stay all projects'.
func TestProjectModsSettings(t *testing.T) {
	src, m := modsDash(t)
	if err := mod.Enable(caller.Caller{Kind: caller.Human}, "review-age", ""); err != nil {
		t.Fatal(err)
	}
	src.loadMods()
	m.setData(src.data)
	projectMods(t, m, "beta", "review-age")
	modAct(t, m, src, "enter")
	if out := screen(m); !strings.Contains(out, "Review age · settings · beta") || !strings.Contains(out, "all projects") {
		t.Fatalf("the project's page:\n%s", out)
	}
	modAct(t, m, src, "+")
	md := modState(t, src, "review-age")
	if md.SettingsFor("beta")["warn_after_hours"] != int64(25) || md.Settings["warn_after_hours"] != int64(24) {
		t.Fatalf("+ set %v over %v (%s)", md.ProjectSettings, md.Settings, m.msg)
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"))
	if !strings.Contains(string(data), "[projects.beta.mods.review-age.settings]\nwarn_after_hours = 25\n") {
		t.Errorf("config.toml:\n%s", data)
	}
	press(m, "esc")
	if out := screen(m); !strings.Contains(lineWith(out, "Review age"), "settings: 1 of its own") {
		t.Errorf("the row after +:\n%s", out)
	}
	rows := m.projectRows("beta")
	if r := rows[len(rows)-1]; r.label != "Mods" || r.value(m) != "Review age · settings of its own for 1" {
		t.Errorf("the Settings tab's last row: %s %q", r.label, r.value(m))
	}
	if slices.ContainsFunc(rows, func(r setting) bool { return strings.HasSuffix(r.label, " settings") }) {
		t.Error("a mod's settings row is still on the Settings tab")
	}
	modAct(t, m, src, "enter")
	modAct(t, m, src, "X")
	if md := modState(t, src, "review-age"); len(md.ProjectSettings["beta"]) != 0 || m.msg != "warn_after_hours: follows all projects" {
		t.Errorf("X left %v (%s)", md.ProjectSettings, m.msg)
	}

	// The Settings tab's Mods line goes to this tab.
	projectMods(t, m, "beta", "review-age")
	pv := m.projectPopupView()
	pv.tab = tabSettings
	pv.settings.sel = len(pv.settings.rows) - 1
	press(m, "enter")
	if pv.tab != tabProjMods {
		t.Errorf("enter on the Mods line: tab %d", pv.tab)
	}

	// A built-in mod with settings: enter says where they are.
	for _, md := range m.data.Mods {
		if md.Builtin && md.Manifest != nil && len(md.Manifest.Settings) > 0 {
			projectMods(t, m, "beta", md.ID)
			press(m, "enter")
			if _, ok := m.top().(*projectView); !ok || !strings.Contains(m.msg, "same for every project") {
				t.Errorf("enter on built-in %s: %T %q", md.ID, m.top(), m.msg)
			}
			break
		}
	}
}

// TestProjectModsAgent: inside an agent the tab only shows: turning a mod
// on or off for a project, or limiting it, is refused.
func TestProjectModsAgent(t *testing.T) {
	modsDash(t)
	agent := &ServerSource{Caller: caller.Caller{Kind: caller.Thread, Project: "beta"}}
	if err := agent.ModAct("review-age", "enable", "beta"); err == nil {
		t.Error("an agent enabled a mod for a project")
	}
	if err := agent.SetModProjects("review-age", []string{"beta"}); err == nil {
		t.Error("an agent limited a mod")
	}
	if err := agent.SetModSetting("review-age", "beta", "warn_after_hours", int64(3)); err == nil {
		t.Error("an agent set a project's setting")
	}
}

// TestModWhereView: the Mods tab's w lists every project and every
// project as a whole; enter on a project turns the mod on or off for it,
// on every project lifts the limit after a question.
func TestModWhereView(t *testing.T) {
	src, m := modsDash(t)
	selectMod(t, m, "review-age")
	modAct(t, m, src, "w")
	out := screen(m)
	for _, want := range []string{"Review age · where it runs", "every project", "alpha", "beta", "It follows all projects: off."} {
		if !strings.Contains(out, want) {
			t.Errorf("the pick list lacks %q:\n%s", want, out)
		}
	}
	press(m, "down", "down")
	modAct(t, m, src, "enter")
	if _, ok := m.top().(*consentView); !ok {
		t.Fatalf("enter on beta: %T", m.top())
	}
	modAct(t, m, src, "Y")
	if md := modState(t, src, "review-age"); !slices.Equal(md.Projects, []string{"beta"}) {
		t.Fatalf("on for beta: %v", md.Projects)
	}
	if _, ok := m.top().(*modWhereView); !ok {
		t.Fatalf("after the consent: %T", m.top())
	}
	press(m, "up", "up")
	modAct(t, m, src, "enter")
	if c, ok := m.top().(*confirmView); !ok || c.question != "Run Review age for every project, not only beta?" {
		t.Fatalf("enter on every project: %T %+v", m.top(), m.top())
	}
	modAct(t, m, src, "Y")
	if md := modState(t, src, "review-age"); len(md.Projects) != 0 || md.State != mod.Enabled {
		t.Fatalf("every project: %s %v", md.State, md.Projects)
	}
}
