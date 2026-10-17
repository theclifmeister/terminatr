package mod

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/modaction"
)

// reviewAge is the design's example manifest (mods design §5), with
// unknown keys a newer API might add.
const reviewAge = `
id          = "review-age"
name        = "Review age"
version     = "0.3.1"
api         = "1.0"
min_tm      = "0.20.0"
description = "How long each task has waited in review."
authors     = ["Ada"]
homepage    = "https://github.com/ada/tm-review-age"
future_key  = "ignored"

[provides]
main      = "main.ts"
columns   = [{ id = "age", title = "age", width = 4, where = ["sidebar.thread", "dashboard.task"] }]
views     = [{ id = "queue", title = "Review queue", slot = "project.tab" }]

[permissions]
project_read = true
views        = true
net          = ["api.example.com"]

[settings]
warn_after_hours = { type = "int", default = 24, min = 1, max = 720, label = "Warn after (hours)" }
`

// install writes a mod folder under mods with files (name → content).
func install(t *testing.T, mods, id string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(mods, id)
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestManifestExample(t *testing.T) {
	dir := install(t, t.TempDir(), "review-age", map[string]string{"mod.toml": reviewAge, "main.ts": ""})
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "review-age" || !m.IsCode() || m.Provides.Columns[0].Width != 4 || m.Provides.Views[0].Slot != SlotProjectTab {
		t.Fatalf("%+v", m)
	}
	if got := m.Permissions.Requested(); !slices.Equal(got, []string{PermProjectRead, PermViews, PermNet}) {
		t.Fatalf("requested %v", got)
	}
	// The consent names the panels it draws and how they are marked.
	if c := Consent(m, dir); !strings.Contains(c, `Panels it draws: a project-popup tab "Review queue"`) ||
		!strings.Contains(c, "draws its own panels") {
		t.Fatalf("consent:\n%s", c)
	}
}

// TestManifestErrors: each broken manifest is refused, saying why.
func TestManifestErrors(t *testing.T) {
	base := "name = \"X\"\nversion = \"1.0.0\"\napi = \"1.0\"\n"
	base17 := strings.Replace(base, "1.0\"\n", "1.7\"\n", 1)
	for _, tc := range []struct{ name, toml, want string }{
		{"no id", base, "has no id"},
		{"bad id", "id = \"Review_Age\"\n" + base, "lowercase letters"},
		{"reserved id", "id = \"band\"\n" + base, "reserved"},
		{"bad version", "id = \"x\"\nname = \"X\"\nversion = \"v1\"\napi = \"1.0\"\n", "isn't MAJOR.MINOR.PATCH"},
		{"bad api", "id = \"x\"\nname = \"X\"\nversion = \"1.0.0\"\napi = \"1\"\n", "api \"1\""},
		{"no name", "id = \"x\"\nversion = \"1.0.0\"\napi = \"1.0\"\n", "has no name"},
		{"main not ts", "id = \"x\"\n" + base + "[provides]\nmain = \"main.py\"\n", "isn't a .ts or .js"},
		{"dotdot", "id = \"x\"\n" + base + "[provides]\ntheme = \"../theme.toml\"\n", "has a .. in it"},
		{"absolute", "id = \"x\"\n" + base + "[provides]\ntheme = \"/etc/passwd\"\n", "inside the mod's folder"},
		{"column without code", "id = \"x\"\n" + base + "[provides]\ncolumns = [{ id = \"a\", width = 2, where = [\"sidebar.thread\"] }]\n", "needs provides.main"},
		{"wide column", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\ncolumns = [{ id = \"a\", width = 13, where = [\"sidebar.thread\"] }]\n", "width must be"},
		{"bad slot", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"pane\" }]\n[permissions]\nviews = true\n", "slot \"pane\""},
		{"sidebar slot on 1.6", "id = \"x\"\n" + strings.Replace(base, "1.0", "1.6", 1) + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"sidebar.section\" }]\n[permissions]\nviews = true\n", `slot sidebar.section needs api = "1.7"`},
		{"opens unknown", "id = \"x\"\n" + base17 + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"sidebar.section\", opens = \"w\" }]\n[permissions]\nviews = true\n", `opens "w", which isn't one of its views`},
		{"opens a section", "id = \"x\"\n" + base17 + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"sidebar.section\", opens = \"w\" }, { id = \"w\", title = \"W\", slot = \"details.section\" }]\n[permissions]\nviews = true\n", `opens "w", a details.section`},
		{"opens from a card", "id = \"x\"\n" + base17 + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"dashboard.card\", opens = \"w\" }, { id = \"w\", title = \"W\", slot = \"full\" }]\n[permissions]\nviews = true\n", "opens is a section's"},
		{"opens from a panel section on 1.6", "id = \"x\"\n" + strings.Replace(base, "1.0", "1.6", 1) + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"coordinator.section\", opens = \"w\" }, { id = \"w\", title = \"W\", slot = \"full\" }]\n[permissions]\nviews = true\n", `opens needs api = "1.7"`},
		{"three sidebar views", "id = \"x\"\n" + base17 + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"a\", title = \"A\", slot = \"sidebar.section\" }, { id = \"b\", title = \"B\", slot = \"sidebar.project.section\" }, { id = \"c\", title = \"C\", slot = \"sidebar.section\" }]\n[permissions]\nviews = true\n", "at most 2 sidebar sections"},
		{"long description", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"full\", description = \"" + strings.Repeat("d", 81) + "\" }]\n[permissions]\nviews = true\n", "description is longer than 80"},
		{"views ungranted", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\nviews = [{ id = \"v\", title = \"V\", slot = \"full\" }]\n", "needs permissions.views"},
		{"data mod perms", "id = \"x\"\n" + base + "[permissions]\ninbox = true\n", "are for code mods"},
		{"net url", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nnet = [\"https://api.x.com/v1\"]\n", "isn't a host name"},
		{"net ip", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nnet = [\"10.0.0.1\"]\n", "isn't a host name"},
		{"command path", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\ncommands = [\"/bin/sh\"]\n", "isn't a program name"},
		{"files relative", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nfiles = { read = [\"notes\"] }\n", "absolute or start with ~/"},
		{"action unknown", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nproject_read = true\nactions = [\"thread.merge\"]\n", "not an action a mod may bind"},
		{"action needs no grant", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nproject_read = true\nactions = [\"task.accept\"]\n", "needs no grant of its own"},
		{"action twice", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nproject_read = true\nactions = [\"session.clear\", \"session.clear\"]\n", "session.clear twice"},
		{"actions without project_read", "id = \"x\"\n" + base + "[provides]\nmain = \"m.ts\"\n[permissions]\nactions = [\"session.clear\"]\n", "needs permissions.project_read"},
		{"actions on a data mod", "id = \"x\"\n" + base + "[permissions]\nactions = [\"session.clear\"]\n", "are for code mods"},
		{"setting default", "id = \"x\"\n" + base + "[settings]\nn = { type = \"int\", default = 0, min = 1 }\n", "default must be at least 1"},
		{"choice no options", "id = \"x\"\n" + base + "[settings]\nc = { type = \"choice\", default = \"a\" }\n", "needs options"},
		{"rules role", "id = \"x\"\n" + base + "[provides]\nrules = { human = \"r.md\" }\n", "coordinator or thread"},
		{"not toml", "id = ", "doesn't parse"},
	} {
		_, err := ParseManifest([]byte(tc.toml), "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestConsentActions: the actions a mod binds only with a grant of
// their own are listed in its consent, each in tm's words, and Binds
// holds only with the grant and the listing.
func TestConsentActions(t *testing.T) {
	toml := strings.Replace(reviewAge, `net          = ["api.example.com"]`, `actions = ["session.clear", "question.answer"]`, 1)
	dir := install(t, t.TempDir(), "review-age", map[string]string{"mod.toml": toml, "main.ts": ""})
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Permissions.Requested(); !slices.Equal(got, []string{PermProjectRead, PermViews, PermActions}) {
		t.Fatalf("requested %v", got)
	}
	c := Consent(m, dir)
	for _, want := range []string{"clear an agent session's conversation (session.clear)", "answer an agent's question with one of its options (question.answer)", "tm asks you before each one runs"} {
		if !strings.Contains(c, want) {
			t.Errorf("consent lacks %q:\n%s", want, c)
		}
	}
	clear, _ := modaction.Lookup("session.clear")
	attach, _ := modaction.Lookup("session.attach")
	md := Mod{Manifest: m, Grants: []string{PermProjectRead, PermViews}}
	if md.Binds(clear) || !md.Binds(attach) {
		t.Error("without the actions grant: binds session.clear, or not session.attach")
	}
	md.Grants = append(md.Grants, PermActions)
	if !md.Binds(clear) {
		t.Error("with the grant, session.clear isn't bound")
	}
	m.Permissions.Actions = []string{"question.answer"}
	if md.Binds(clear) {
		t.Error("binds session.clear, which its manifest doesn't list")
	}
	// Listing another action asks again on update.
	old, _ := ReadManifest(dir)
	if ch := Diff(old, dir, m, dir); !slices.Equal(ch.NewPerms, []string{PermActions}) {
		t.Errorf("diff: %+v", ch)
	}
}

// TestManifestFiles: the files a manifest names must be in its folder,
// of the right kind, and a symlink may not lead out of it.
func TestManifestFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "secret.toml")
	os.WriteFile(outside, nil, 0o644)
	head := "id = \"x\"\nname = \"X\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\n"
	dir := install(t, filepath.Join(root, "mods"), "x", map[string]string{"theme.toml": "", "templates/a.md": ""})
	if err := os.Symlink(outside, filepath.Join(dir, "link.toml")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ provides, want string }{
		{"theme = \"theme.toml\"\ntemplates = \"templates\"\n", ""},
		{"theme = \"missing.toml\"\n", "isn't there"},
		{"theme = \"link.toml\"\n", "leads out"},
		{"theme = \"templates\"\n", "isn't a file"},
		{"templates = \"theme.toml\"\n", "isn't a folder"},
	} {
		_, err := ParseManifest([]byte(head+tc.provides), dir)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%q: %v, want %q", tc.provides, err, tc.want)
		}
	}
}

func TestCheckAPI(t *testing.T) {
	for _, tc := range []struct{ api, minTM, tm, want string }{
		{"1.4", "", "v0.19.0", ""},
		{"1.0", "", "v0.19.0", ""},
		{"1.5", "", "v0.19.0", "review-age needs mod API 1.5; this tm has 1.4: run tm update"},
		{"2.0", "", "v0.19.0", "needs mod API 2.0"},
		{"0.9", "", "v0.19.0", "was written for mod API 0.9"},
		{"1.1", "0.20.0", "v0.19.0", "needs tm 0.20.0; this tm is v0.19.0"},
		{"1.1", "0.19.0", "v0.19.0", ""},
		{"1.1", "0.20.0", "dev", ""}, // a source build passes min_tm
	} {
		err := checkAPI(&Manifest{ID: "review-age", API: tc.api, MinTM: tc.minTM}, 1, 4, tc.tm)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("api %s min_tm %q on %s: %v, want %q", tc.api, tc.minTM, tc.tm, err, tc.want)
		}
	}
}

// TestRegistry: installed mods joined with their tables; a broken one is
// listed, not fatal.
func TestRegistry(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "review-age", map[string]string{"mod.toml": reviewAge, "main.ts": ""})
	install(t, mods, "nord", map[string]string{"mod.toml": "id = \"nord\"\nname = \"Nord\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\ntheme = \"t.toml\"\n", "t.toml": ""})
	install(t, mods, "wrong-folder", map[string]string{"mod.toml": "id = \"other\"\nname = \"O\"\nversion = \"1.0.0\"\napi = \"1.0\"\n"})
	install(t, mods, "future", map[string]string{"mod.toml": "id = \"future\"\nname = \"F\"\nversion = \"1.0.0\"\napi = \"1.99\"\n"})
	install(t, mods, "empty", map[string]string{"README": ""})
	os.WriteFile(filepath.Join(mods, "stray.txt"), nil, 0o644)

	r, err := LoadDir(mods, map[string]config.Mod{
		"review-age": {Enabled: true, Grants: []string{"project_read", "notify"}, Projects: []string{"demo"},
			Settings: map[string]any{"warn_after_hours": int64(48), "nope": true}},
		"nord":    {Enabled: false},
		"removed": {Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range r.Mods {
		ids = append(ids, m.ID+":"+string(m.State))
	}
	if got := strings.Join(ids, " "); got != "empty:invalid future:incompatible nord:disabled review-age:enabled wrong-folder:invalid" {
		t.Fatalf("mods %s", got)
	}
	if !slices.Equal(r.Orphans, []string{"removed"}) {
		t.Fatalf("orphans %v", r.Orphans)
	}
	ra, _ := r.Get("review-age")
	if !slices.Equal(ra.Grants, []string{PermProjectRead}) || !slices.Equal(ra.WantsMore, []string{PermViews, PermNet}) {
		t.Fatalf("grants %v, wants more %v", ra.Grants, ra.WantsMore)
	}
	if ra.Settings["warn_after_hours"] != int64(48) || len(ra.BadSettings) != 1 || !strings.Contains(ra.BadSettings[0], "nope") {
		t.Fatalf("settings %v, bad %v", ra.Settings, ra.BadSettings)
	}
	if !ra.Runs("demo") || ra.Runs("site") {
		t.Fatal("review-age runs for demo only")
	}
	if got := r.For("demo"); len(got) != 1 || got[0].ID != "review-age" {
		t.Fatalf("for demo %v", got)
	}
	wf, _ := r.Get("wrong-folder")
	if !strings.Contains(wf.Err.Error(), `its id is "other"`) {
		t.Fatal(wf.Err)
	}
	// A bad setting value keeps the default, and says so.
	r, _ = LoadDir(mods, map[string]config.Mod{"review-age": {Settings: map[string]any{"warn_after_hours": int64(0)}}})
	ra, _ = r.Get("review-age")
	if ra.Settings["warn_after_hours"] != int64(24) || len(ra.BadSettings) != 1 {
		t.Fatalf("settings %v, bad %v", ra.Settings, ra.BadSettings)
	}
	// A table that didn't read makes the mod invalid.
	r, _ = LoadDir(mods, map[string]config.Mod{"nord": {Err: errors.New("mods.nord: bad")}})
	if n, _ := r.Get("nord"); n.State != Invalid {
		t.Fatalf("nord %v", n.State)
	}
	// No folder: no mods.
	if r, err := LoadDir(filepath.Join(mods, "none"), nil); err != nil || len(r.Mods) != 0 {
		t.Fatal(r, err)
	}
}

// TestWritesHumanOnly: an agent can't enable, grant, limit or set a mod;
// the human's calls edit [mods.<id>] and the registry reads them back.
func TestWritesHumanOnly(t *testing.T) {
	h := t.TempDir()
	t.Setenv(home.Env, h)
	mods := filepath.Join(h, "mods")
	install(t, mods, "review-age", map[string]string{"mod.toml": reviewAge, "main.ts": ""})
	install(t, mods, "future", map[string]string{"mod.toml": "id = \"future\"\nname = \"F\"\nversion = \"1.0.0\"\napi = \"9.0\"\n"})
	os.WriteFile(filepath.Join(h, "config.toml"), []byte("# mine\n[ui]\nicons = \"ascii\"\n"), 0o600)

	human := caller.Caller{Kind: caller.Human}
	for _, c := range []caller.Caller{{Kind: caller.Coordinator, Project: "demo"}, {Kind: caller.Thread, Project: "demo", Thread: "t-0001"}} {
		for _, err := range []error{
			SetEnabled(c, "review-age", true), SetGrants(c, "review-age", []string{"views"}),
			SetProjects(c, "review-age", []string{"demo"}), SetSetting(c, "review-age", "warn_after_hours", 2),
		} {
			if !errors.Is(err, ErrHumanOnly) {
				t.Fatalf("%s: %v", c, err)
			}
		}
	}
	if err := SetEnabled(human, "future", true); err == nil || !strings.Contains(err.Error(), "needs mod API 9.0") {
		t.Fatal(err)
	}
	if err := SetEnabled(human, "missing", true); err == nil {
		t.Fatal("enabled a mod that isn't installed")
	}
	if err := SetGrants(human, "review-age", []string{"inbox"}); err == nil || !strings.Contains(err.Error(), "doesn't ask for inbox") {
		t.Fatal(err)
	}
	if err := SetGrants(human, "review-age", []string{"credentials"}); err == nil {
		t.Fatal("granted credentials")
	}
	if err := SetSetting(human, "review-age", "warn_after_hours", 0); err == nil {
		t.Fatal("set a value below min")
	}
	for _, err := range []error{
		SetEnabled(human, "review-age", true),
		SetGrants(human, "review-age", []string{"views", "project_read"}),
		SetProjects(human, "review-age", []string{"demo", "site"}),
		SetSetting(human, "review-age", "warn_after_hours", 48),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(h, "config.toml"))
	want := "# mine\n[ui]\nicons = \"ascii\"\n\n[mods.review-age]\nenabled = true\ngrants = [\"project_read\", \"views\"]\nprojects = [\"demo\", \"site\"]\n\n[mods.review-age.settings]\nwarn_after_hours = 48\n"
	if string(data) != want {
		t.Fatalf("config.toml:\n%s\nwant:\n%s", data, want)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Load(c)
	ra, _ := r.Get("review-age")
	if ra.State != Enabled || !ra.Runs("site") || ra.Runs("other") || !slices.Equal(ra.WantsMore, []string{PermNet}) || ra.Settings["warn_after_hours"] != int64(48) {
		t.Fatalf("%+v", ra)
	}
	// Clearing the project list and the setting goes back to all and the default.
	if err := SetProjects(human, "review-age", nil); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(human, "review-age", "warn_after_hours", nil); err != nil {
		t.Fatal(err)
	}
	c, _ = config.Load()
	r, _ = Load(c)
	ra, _ = r.Get("review-age")
	if !ra.Runs("other") || ra.Settings["warn_after_hours"] != int64(24) {
		t.Fatalf("%+v", ra)
	}
}

func TestSectionViews(t *testing.T) {
	const toml = `id = "notes"
name = "Notes"
version = "0.1.0"
api = "1.0"

[provides]
main  = "main.ts"
views = [
  { id = "review", title = "Review", slot = "details.section" },
  { id = "digest", title = "Digest", slot = "coordinator.section" },
]

[permissions]
views = true
`
	dir := install(t, t.TempDir(), "notes", map[string]string{"mod.toml": toml, "main.ts": ""})
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSection(m.Provides.Views[0].Slot) || !IsSection(m.Provides.Views[1].Slot) || IsSection(SlotDashboardCard) {
		t.Fatalf("%+v", m.Provides.Views)
	}
	if got := m.ViewWords(); !slices.Equal(got, []string{`a details-panel section "Review" (on the dashboard and beside a thread's pane)`,
		`a coordinator-panel section "Digest" (beside the coordinator's pane)`}) {
		t.Fatalf("%q", got)
	}
}

// TestSidebarViews: the sidebar slots need mod API 1.7, are sections,
// name their companion view, as the right pane's do from 1.7, and the
// consent says where they go.
func TestSidebarViews(t *testing.T) {
	const toml = `id = "ci-status"
name = "CI status"
version = "0.1.0"
api = "1.7"

[provides]
main  = "main.ts"
views = [
  { id = "ticker", title = "Ticker", slot = "sidebar.section", description = "this project's PR polls" },
  { id = "ci", title = "ci status", slot = "sidebar.project.section", opens = "prs" },
  { id = "prs", title = "Open PRs", slot = "dashboard.card" },
  { id = "checks", title = "Checks", slot = "details.section", opens = "prs" },
  { id = "glance", title = "Glance", slot = "coordinator.section", opens = "prs" },
]

[permissions]
views = true
`
	dir := install(t, t.TempDir(), "ci-status", map[string]string{"mod.toml": toml, "main.ts": ""})
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := m.Provides.Views
	if !IsSection(v[0].Slot) || !IsSection(v[1].Slot) || !IsSidebar(v[1].Slot) || IsSidebar(SlotDetailsSection) ||
		v[0].Description != "this project's PR polls" || v[1].Opens != "prs" || v[3].Opens != "prs" || !IsPanel(v[3].Slot) || IsPanel(v[0].Slot) {
		t.Fatalf("%+v", v)
	}
	want := []string{`a sidebar section "Ticker" (above or below your projects, on every screen)`,
		`a section under each project "ci status" (in the sidebar)`, `a dashboard card "Open PRs"`,
		`a details-panel section "Checks" (on the dashboard and beside a thread's pane)`,
		`a coordinator-panel section "Glance" (beside the coordinator's pane)`}
	if got := m.ViewWords(); !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}
