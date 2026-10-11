package mod

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
)

// dataMod is a data mod's manifest providing the [provides] lines given.
func dataMod(id, provides string) string {
	return "id = \"" + id + "\"\nname = \"" + id + "\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\n" + provides
}

// enabled are [mods.<id>] tables enabling ids, in that order.
func enabled(ids ...string) map[string]config.Mod {
	out := map[string]config.Mod{}
	for i, id := range ids {
		out[id] = config.Mod{Enabled: true, Order: i}
	}
	return out
}

func joined(errs []error) string {
	var s []string
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return strings.Join(s, "\n")
}

// TestProviders: only enabled mods provide, in the order they were
// enabled; Provider says why a pick can't be used.
func TestProviders(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "nord", map[string]string{"mod.toml": dataMod("nord", `theme = "theme.toml"`), "theme.toml": ""})
	install(t, mods, "abyss", map[string]string{"mod.toml": dataMod("abyss", `theme = "t.toml"`), "t.toml": ""})
	install(t, mods, "off", map[string]string{"mod.toml": dataMod("off", `theme = "t.toml"`), "t.toml": ""})
	tables := enabled("nord", "abyss")
	tables["off"] = config.Mod{}
	r, err := LoadDir(mods, tables)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range r.Providers(KindTheme) {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"nord", "abyss"}) {
		t.Fatalf("providers %v", ids)
	}
	if len(r.Providers(KindIcons)) != 0 {
		t.Fatal("icons from a theme mod")
	}
	if _, err := r.Provider(KindTheme, "off"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("off: %v", err)
	}
	if _, err := r.Provider(KindIcons, "nord"); err == nil || !strings.Contains(err.Error(), "has no icons") {
		t.Fatalf("nord icons: %v", err)
	}
	m, err := r.Provider(KindTheme, "nord")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Data(KindTheme); err != nil {
		t.Fatal(err)
	}
}

// TestReadFileChecked: a data file is read only from inside the mod's
// folder, and only up to MaxDataFile.
func TestReadFileChecked(t *testing.T) {
	mods := t.TempDir()
	dir := install(t, mods, "big", map[string]string{"mod.toml": dataMod("big", `theme = "t.toml"`), "t.toml": ""})
	r, _ := LoadDir(mods, enabled("big"))
	m, _ := r.Get("big")
	if err := os.WriteFile(filepath.Join(dir, "t.toml"), make([]byte, MaxDataFile+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Data(KindTheme); err == nil || !strings.Contains(err.Error(), "over") {
		t.Fatalf("big file: %v", err)
	}
	// Swapped for a link out of the folder after loading.
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("x"), 0o644)
	os.Remove(filepath.Join(dir, "t.toml"))
	if err := os.Symlink(outside, filepath.Join(dir, "t.toml")); err != nil {
		t.Skip(err)
	}
	if _, err := m.Data(KindTheme); err == nil || !strings.Contains(err.Error(), "leads out") {
		t.Fatalf("link out: %v", err)
	}
	if _, err := m.ReadFile("../x"); err == nil {
		t.Fatal("read ..")
	}
}

func TestParseKeymap(t *testing.T) {
	km, err := ParseKeymap([]byte("task.delegate = [\"D\"]\n\"project.next\" = [\"n\", \"]\"]\n[side]\npin = [\"P\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(km["task.delegate"], []string{"D"}) || !slices.Equal(km["project.next"], []string{"n", "]"}) || !slices.Equal(km["side.pin"], []string{"P"}) {
		t.Fatalf("%v", km)
	}
	if _, err := ParseKeymap([]byte("a.b = \"x\"\n")); err == nil {
		t.Fatal("a string, not a list")
	}
	if _, err := ParseKeymap([]byte("a.b = [1]\n")); err == nil {
		t.Fatal("a number key")
	}
}

// TestKeymapJoined: every enabled keymap applies; an action or a key
// two mods bind goes to the one enabled first, and the other is named.
func TestKeymapJoined(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "vim", map[string]string{"mod.toml": dataMod("vim", `keymap = "keys.toml"`), "keys.toml": "list.down = [\"j\"]\nproject.next = [\"L\"]\n"})
	install(t, mods, "emacs", map[string]string{"mod.toml": dataMod("emacs", `keymap = "keys.toml"`), "keys.toml": "list.down = [\"ctrl+n\"]\nproject.prev = [\"L\"]\nlist.up = [\"ctrl+p\"]\n"})
	install(t, mods, "broken", map[string]string{"mod.toml": dataMod("broken", `keymap = "keys.toml"`), "keys.toml": "x = "})
	// emacs was enabled after vim, though its id sorts first.
	r, _ := LoadDir(mods, enabled("vim", "emacs", "broken"))
	km, from, errs := r.Keymap()
	want := map[string][]string{"list.down": {"j"}, "project.next": {"L"}, "list.up": {"ctrl+p"}}
	if len(km) != len(want) {
		t.Fatalf("keymap %v", km)
	}
	for id, k := range want {
		if !slices.Equal(km[id], k) {
			t.Fatalf("%s: %v, want %v", id, km[id], k)
		}
	}
	if from["list.up"] != "emacs" || from["list.down"] != "vim" {
		t.Fatalf("from %v", from)
	}
	msg := joined(errs)
	for _, w := range []string{"emacs keymap: list.down is bound by vim", "emacs keymap: key conflict: L is vim's project.next", "broken keymap"} {
		if !strings.Contains(msg, w) {
			t.Errorf("errors lack %q:\n%s", w, msg)
		}
	}
}

func TestParseLayout(t *testing.T) {
	l, err := ParseLayout([]byte(`sidebar = ["count", "review-age.age", "state", "needs"]`))
	if err != nil || !slices.Equal(l.Sidebar, []string{"count", "review-age.age", "state", "needs"}) {
		t.Fatalf("%v %v", l, err)
	}
	for _, tc := range []struct{ toml, want string }{
		{`sidebar = ["count", "bogus"]`, `no column "bogus"`},
		{`sidebar = ["count", "count"]`, "listed twice"},
		{"sidebar = []\ndashboard = []", "unknown key dashboard"},
		{`sidebar = "count"`, "sidebar"},
	} {
		if _, err := ParseLayout([]byte(tc.toml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.toml, err, tc.want)
		}
	}
}

// TestRules: every addendum of an enabled mod running for the project,
// by id; the first enabled policy for the role; a mod limited to other
// projects adds nothing.
func TestRules(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "commits", map[string]string{"mod.toml": dataMod("commits", `rules = { coordinator = "rules/c.md", thread = "rules/t.md" }`),
		"rules/c.md": "Conventional commits.\n", "rules/t.md": "Write feat: and fix:.\n"})
	install(t, mods, "careful", map[string]string{"mod.toml": dataMod("careful", `rules = { coordinator_policy = "p.md" }`), "p.md": "Propose before starting.\n"})
	install(t, mods, "eager", map[string]string{"mod.toml": dataMod("eager", `rules = { coordinator_policy = "p.md", coordinator = "c.md" }`), "p.md": "Start at once.\n", "c.md": "Eager.\n"})
	install(t, mods, "elsewhere", map[string]string{"mod.toml": dataMod("elsewhere", `rules = { thread = "t.md" }`), "t.md": "Not here.\n"})
	tables := enabled("careful", "eager", "commits", "elsewhere")
	tables["elsewhere"] = config.Mod{Enabled: true, Projects: []string{"site"}}
	r, _ := LoadDir(mods, tables)

	policy, add, errs := r.Rules("coordinator", "demo")
	if policy == nil || policy.Mod != "careful" {
		t.Fatalf("policy %+v", policy)
	}
	if len(add) != 2 || add[0].Mod != "commits" || add[0].Text != "Conventional commits." || add[1].Mod != "eager" {
		t.Fatalf("addenda %+v", add)
	}
	if !strings.Contains(joined(errs), "eager: its coordinator policy is not used: careful's came first") {
		t.Fatalf("errs %v", errs)
	}
	_, add, _ = r.Rules("thread", "demo")
	if len(add) != 1 || add[0].Mod != "commits" {
		t.Fatalf("thread addenda in demo %+v", add)
	}
	_, add, _ = r.Rules("thread", "site")
	if len(add) != 2 {
		t.Fatalf("thread addenda in site %+v", add)
	}
	if _, err := ParseManifest([]byte(dataMod("x", `rules = { reviewer = "r.md" }`)), ""); err == nil {
		t.Fatal("rules for an unknown role")
	}
}

func TestParseTemplate(t *testing.T) {
	tpl, err := ParseTemplate("bug", []byte("\n# Bug: {title}\n\nWhat happens, what should.\n\n## Steps\n\n- Reproduce it in a test\n- [ ] Fix it\n* Ship\n"))
	if err != nil {
		t.Fatal(err)
	}
	title, notes, steps := tpl.Apply("crash on start")
	if title != "Bug: crash on start" || notes != "What happens, what should." || !slices.Equal(steps, []string{"Reproduce it in a test", "Fix it", "Ship"}) {
		t.Fatalf("%q %q %q", title, notes, steps)
	}
	for _, tc := range []struct{ name, body, want string }{
		{"Bad", "# {title}\n", "lowercase"},
		{"x", "no title\n", "first line is the title"},
		{"x", "# Bug\n", "needs {title} once"},
		{"x", "# {title}\n## Steps\nnot an item\n", "isn't a list item"},
		{"x", "# {title}\n## Steps\n" + strings.Repeat("- s\n", MaxTemplateSteps+1), "at most"},
	} {
		if _, err := ParseTemplate(tc.name, []byte(tc.body)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want %q", tc.body, err, tc.want)
		}
	}
}

// TestTemplates: an enabled mod's templates, for its projects; a name
// two mods have needs its mod.
func TestTemplates(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "basics", map[string]string{"mod.toml": dataMod("basics", `templates = "templates"`),
		"templates/bug.md": "# Bug: {title}\n", "templates/spike.md": "# Spike: {title}\n", "templates/broken.md": "nope"})
	install(t, mods, "more", map[string]string{"mod.toml": dataMod("more", `templates = "t"`), "t/bug.md": "# Defect: {title}\n"})
	tables := enabled("basics", "more")
	tables["more"] = config.Mod{Enabled: true, Projects: []string{"site"}}
	r, _ := LoadDir(mods, tables)
	ts, errs := r.Templates("demo")
	if len(ts) != 2 || !strings.Contains(joined(errs), "basics: template broken") {
		t.Fatalf("%+v %v", ts, errs)
	}
	if tpl, err := FindTemplate(ts, "bug"); err != nil || tpl.Mod != "basics" {
		t.Fatalf("%+v %v", tpl, err)
	}
	ts, _ = r.Templates("site")
	if _, err := FindTemplate(ts, "bug"); err == nil || !strings.Contains(err.Error(), "basics:bug, more:bug") {
		t.Fatalf("ambiguous: %v", err)
	}
	if tpl, err := FindTemplate(ts, "more:bug"); err != nil || tpl.Title != "Defect: {title}" {
		t.Fatalf("%+v %v", tpl, err)
	}
	if _, err := FindTemplate(ts, "chore"); err == nil || !strings.Contains(err.Error(), "there are basics:bug") {
		t.Fatalf("missing: %v", err)
	}
}

// TestAgentDirs and the order of the [mods.<id>] tables, read from
// config.toml.
func TestAgentDirs(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	mods, _ := home.ModsDir()
	install(t, mods, "pi", map[string]string{"mod.toml": dataMod("pi", `agents = "agents"`), "agents/pi.toml": ""})
	install(t, mods, "aider", map[string]string{"mod.toml": dataMod("aider", `agents = "agents"`), "agents/aider.toml": ""})
	path, _ := config.Path()
	os.WriteFile(path, []byte("[mods.pi]\nenabled = true\n[mods.aider]\nenabled = true\n"), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	r, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	dirs := r.AgentDirs()
	if len(dirs) != 2 || dirs[0].Mod != "pi" || dirs[1].Mod != "aider" || filepath.Base(dirs[0].Dir) != "agents" {
		t.Fatalf("%+v", dirs)
	}
}
