package config

import (
	"os"
	"strings"
	"testing"
)

func TestDropTable(t *testing.T) {
	in := "# top\n[mods.nord]\nenabled = true\n\n[mods.nord.settings]\ndark = true\n[mods.nordic]\nenabled = true\n[[agents.x.models]]\nname = \"m\"\n"
	got, err := DropTable([]byte(in), "mods.nord")
	if err != nil {
		t.Fatal(err)
	}
	want := "# top\n[mods.nordic]\nenabled = true\n[[agents.x.models]]\nname = \"m\"\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got, err := DropTable([]byte("[ui]\nicons = \"ascii\"\n"), "mods.nord"); err != nil || string(got) != "[ui]\nicons = \"ascii\"\n" {
		t.Errorf("no table: %q %v", got, err)
	}
	if _, err := DropTable([]byte("mods.nord.enabled = true\n"), "mods.nord"); err != ErrForm {
		t.Errorf("dotted keys: %v", err)
	}
}

// TestProjectModSettings: [projects.<slug>.mods.<id>.settings] are read
// into the mod's ProjectSettings, a mod without its own table included;
// a table with another key is that mod's Err, nothing else's;
// [defaults.mods] is an unknown setting.
func TestProjectModSettings(t *testing.T) {
	write(t, "[mods.gates]\nenabled = true\n[mods.gates.settings]\nstrict = false\n\n"+
		"[projects.demo]\nyolo = true\n[projects.demo.mods.gates.settings]\nstrict = true\n"+
		"[projects.demo.mods.theme-x.settings]\ndark = true\n"+
		"[projects.other.mods.broken]\nenabled = true\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if g := c.Mods["gates"]; !g.Enabled || g.Settings["strict"] != false || g.ProjectSettings["demo"]["strict"] != true || g.Err != nil {
		t.Errorf("gates: %+v", g)
	}
	if x := c.Mods["theme-x"]; x.ProjectSettings["demo"]["dark"] != true || x.EnabledSet {
		t.Errorf("a mod with only a project table: %+v", x)
	}
	if b := c.Mods["broken"]; b.Err == nil || !strings.Contains(b.Err.Error(), "projects.other.mods.broken") {
		t.Errorf("a table with another key: %+v", b)
	}
	if s, _ := c.Safety("demo"); !s.Yolo {
		t.Errorf("the project's own settings: %+v", s)
	}

	write(t, "[projects.demo]\nmods = 3\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "projects.demo.mods") {
		t.Errorf("mods that isn't a table: %v", err)
	}
	write(t, "[defaults.mods.gates.settings]\nstrict = true\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "unknown setting defaults.mods") {
		t.Errorf("defaults.mods: %v", err)
	}
}

// TestProjectModSettingsWritten: a project's mod setting is written in
// its own header-line table, and RemoveModsTable drops it with the
// mod's.
func TestProjectModSettingsWritten(t *testing.T) {
	write(t, "[mods.gates]\nenabled = true\n\n[projects.demo]\nyolo = true\n")
	if err := Set(ProjectModsTable("demo", "gates"), "strict", true); err != nil {
		t.Fatal(err)
	}
	if err := Set(ProjectModsTable("other", "gates"), "mode", "light"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if g := c.Mods["gates"]; g.ProjectSettings["demo"]["strict"] != true || g.ProjectSettings["other"]["mode"] != "light" {
		t.Fatalf("read back: %+v", g)
	}
	if err := RemoveModsTable("gates"); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "gates") || !strings.Contains(string(data), "[projects.demo]\nyolo = true\n") {
		t.Errorf("after remove:\n%s", data)
	}
}
