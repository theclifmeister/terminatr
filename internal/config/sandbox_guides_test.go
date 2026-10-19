package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGuidesWritten: a guide's text goes into config.toml as it is, its
// quotes, backslashes and tabs too, and reads back the same; a change
// rewrites it in place, a removal takes it out, and a project's drop of
// all projects' guide takes it by its title. A guide written by hand as
// an array of tables loads too.
func TestGuidesWritten(t *testing.T) {
	write(t, "")
	body := "Run \"make fixtures\" first.\n\n\tNever edit testdata\\ by hand: \"\"\" quotes \"\""
	if err := EditSandbox(DefaultsTable, func(t *SandboxTable) error {
		t.SetGuide(Guide{"Test data", body})
		t.SetGuide(Guide{"Style", "Short lines."})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []Guide{{"Test data", body}, {"Style", "Short lines."}}
	if got := c.ThreadSandbox("web").Guides(); !slices.Equal(got, want) {
		t.Fatalf("read back %q", got)
	}
	data, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(data), "\n\tNever edit") || !strings.Contains(string(data), `{ title = "Style", body = """`) {
		t.Errorf("file:\n%s", data)
	}
	if err := EditSandbox(DefaultsTable, func(t *SandboxTable) error {
		t.SetGuide(Guide{"Style", "Lines of 79."})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error {
		t.Add(SandboxDrop, "Test data")
		t.SetGuide(Guide{"Own", "Its own."})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("web").Guides(); !slices.Equal(got, []Guide{{"Style", "Lines of 79."}, {"Own", "Its own."}}) {
		t.Errorf("web: %q", got)
	}
	if got := c.ThreadSandbox("other").Guides(); !slices.Equal(got, []Guide{{"Test data", body}, {"Style", "Lines of 79."}}) {
		t.Errorf("other: %q", got)
	}
	if err := EditSandbox(DefaultsTable, func(t *SandboxTable) error { t.Remove(SandboxGuides, "Style"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("other").Guides(); !slices.Equal(got, []Guide{{"Test data", body}}) {
		t.Errorf("after rm: %q", got)
	}

	write(t, "[[projects.web.sandbox.guides]]\ntitle = \"Test data\"\nbody = \"\"\"\nRun make fixtures.\n\"\"\"\n")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("web").Guides(); !slices.Equal(got, []Guide{{"Test data", "Run make fixtures."}}) {
		t.Errorf("by hand: %q", got)
	}
	for _, bad := range []string{"[defaults.sandbox]\nguides = [{ title = \"x\" }]\n", "[defaults.sandbox]\nguides = [{ title = \"x\", body = \"y\", z = 1 }]\n", "[defaults.sandbox]\nguides = [1]\n"} {
		write(t, bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q loaded", bad)
		}
	}
}

// TestPresetGuides: a preset's guide is worded from the rules threads
// have once it is added: local ports, the download hosts.
func TestPresetGuides(t *testing.T) {
	p, _ := PresetNamed(PresetBrowserTesting)
	r := WordRules(p.Rules, ThreadSandbox{})
	if len(r) != 1 || !strings.Contains(r[0].Body, "--single-process") || !strings.Contains(r[0].Body, "hasn't opened local ports") || strings.Contains(r[0].Body, "Downloads:") {
		t.Errorf("testing: %q", r)
	}
	if r = WordRules(p.Rules, ThreadSandbox{LocalPorts: true}); !strings.Contains(r[0].Body, "The\nhuman also opened local ports") {
		t.Errorf("testing, ports open: %q", r[0].Body)
	}
	p, _ = PresetNamed(PresetBrowserDownloads)
	r = WordRules(p.Rules, ThreadSandbox{})
	if b := r[len(r)-1].Body; !strings.Contains(b, "the human opened registry.npmjs.org") || strings.Contains(b, ", not ") {
		t.Errorf("downloads: %q", b)
	}
	// Left out, a host is named closed.
	r = WordRules(p.Rules[1:], ThreadSandbox{})
	if b := r[len(r)-1].Body; !strings.Contains(b, "not registry.npmjs.org") {
		t.Errorf("downloads without the registry: %q", b)
	}
	for _, g := range []Guide{BrowserTestingGuide(false), BrowserDownloadsGuide(nil)} {
		if err := CheckGuide(g); err != nil || strings.Contains(g.Body, "%!") || len(g.Body) > MaxGuideBody {
			t.Errorf("%s: %v", g.Title, err)
		}
	}
}

// TestMigrateGuides: tm 0.38's names become their texts written out, as
// each project's threads got them: all projects' worded from all
// projects' rules, a project worded otherwise (its own hosts, a drop)
// getting its own in place of theirs; a name tm never had goes. Every
// project's guidance and rules stay the same, and a file without names
// has nothing to move.
func TestMigrateGuides(t *testing.T) {
	file := `sandbox_lists = "add"

[defaults.sandbox]
hosts = ["registry.npmjs.org"]
guides = ["browser-testing", "browser-downloads"]

[projects.a.sandbox]
hosts = ["cdn.playwright.dev"]

[projects.b.sandbox]
drop = ["browser-testing"]

[projects.c.sandbox]
guides = ["nope"]
local_ports = true

[projects.d.sandbox]
hosts = ["x.dev"]
`
	old, err := parse("config.toml", []byte(file))
	if err != nil {
		t.Fatal(err)
	}
	m, err := MigrateGuides([]byte(file))
	if err != nil || m == nil || m.Differs != "" || !m.Guides || m.Access {
		t.Fatalf("%+v, %v", m, err)
	}
	nu, err := parse("config.toml", m.Data)
	if err != nil {
		t.Fatalf("%v:\n%s", err, m.Data)
	}
	for _, slug := range []string{"", "a", "b", "c", "d", "other"} {
		if got, was := nu.ThreadSandbox(slug).Guides(), old.ThreadSandbox(slug).Guides(); !slices.Equal(got, was) {
			t.Errorf("%s: %q, was %q", slug, got, was)
		}
	}
	text := string(m.Data)
	if strings.Contains(text, `"browser-testing"`) || strings.Contains(text, "nope") || !strings.Contains(text, `title = "Sandbox: browser testing"`) {
		t.Errorf("file:\n%s", text)
	}
	if g := nu.ThreadSandbox("b").Guides(); len(g) != 1 || g[0].Title != GuideBrowserDownloads {
		t.Errorf("b: %q", g)
	}
	if g := nu.ThreadSandbox("c").Guides(); len(g) != 1 || !strings.Contains(g[0].Body, "human also opened local ports") || !nu.SandboxOf("c").Has(SandboxDrop, GuideBrowserTesting) {
		t.Errorf("c, worded with its local ports: %q", g)
	}
	if nu.SandboxOf("d").Has(SandboxDrop, GuideBrowserTesting) {
		t.Errorf("d needs no guide of its own (x.dev isn't a download host): %+v", nu.SandboxOf("d"))
	}
	if !slices.ContainsFunc(m.Changes[""], func(s string) bool { return strings.Contains(s, "written out") }) ||
		!slices.ContainsFunc(m.Changes["c"], func(s string) bool { return strings.Contains(s, "nope") }) {
		t.Errorf("changes: %q", m.Changes)
	}
	if m, err := MigrateGuides(m.Data); m != nil || err != nil {
		t.Errorf("again: %+v, %v", m, err)
	}
}

// TestMigrateFileGuides: one start moves a file of tm 0.37 to the access
// rules and writes out its guides in the same write, one backup kept.
func TestMigrateFileGuides(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	write(t, "[defaults.sandbox]\nbrowser_testing = true\n")
	m, backup, err := MigrateFile("0.39")
	if err != nil || m == nil || !m.Access || !m.Guides || backup == "" {
		t.Fatalf("%+v %q %v", m, backup, err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if g := c.ThreadSandbox("web").Guides(); len(g) != 1 || g[0].Title != GuideBrowserTesting || !strings.Contains(g[0].Body, "Downloads: your commands may not reach") {
		t.Errorf("guides: %q", g)
	}
	if bak, _ := filepath.Glob(filepath.Join(filepath.Dir(c.Path), "config.toml.bak-*")); len(bak) != 1 {
		t.Errorf("backups: %q", bak)
	}
	if m, _, err := MigrateFile("0.39"); m != nil || err != nil {
		t.Errorf("again: %+v, %v", m, err)
	}
}
