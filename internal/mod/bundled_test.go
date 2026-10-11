package mod

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod/bundlegen"
)

// TestBundled: the bundled mods (mods design §11) load from the binary,
// valid, enabled and built in, after the installed ones; every data
// file of the data mods loads; the defaults tm needs are required, the
// templates aren't.
func TestBundled(t *testing.T) {
	want := []string{"ascii-icons", "default-keymap", "default-layout", "default-policy", "default-templates", "default-theme", "memory-tab", "nerd-icons", "unicode-icons"}
	if got := BundledIDs(); !slices.Equal(got, want) {
		t.Fatalf("bundled %q", got)
	}
	for _, id := range want {
		if slices.Contains(bundlegen.Code, id) {
			continue // TestBundledCodeMod
		}
		m := Bundled(id, config.Mod{})
		if m.State != Enabled || !m.Builtin || m.FS == nil || m.Dir != "" || m.Err != nil {
			t.Errorf("%s: %+v", id, m)
			continue
		}
		if m.Manifest.ID != id || len(m.Manifest.Permissions.Requested()) > 0 || m.Manifest.IsCode() {
			t.Errorf("%s: manifest %+v", id, m.Manifest)
		}
		if errs := m.CheckData(); len(errs) > 0 {
			t.Errorf("%s: %v", id, errs)
		}
		if m.Required != (id != BundledTemplates) {
			t.Errorf("%s: required %v", id, m.Required)
		}
	}
	// A table turns the templates off, never a required one.
	off := config.Mod{EnabledSet: true}
	if Bundled(BundledTemplates, off).State != Disabled || Bundled(BundledTheme, off).State != Enabled {
		t.Error("enabled = false")
	}
	if Bundled(BundledTemplates, config.Mod{Grants: []string{}}).State != Enabled {
		t.Error("a table without enabled turned the templates off")
	}
	ts, errs := Bundled(BundledTemplates, config.Mod{}).templates()
	var names []string
	for _, tpl := range ts {
		names = append(names, tpl.Name)
		if !tpl.Builtin || tpl.Mod != BundledTemplates {
			t.Errorf("template %+v", tpl)
		}
	}
	if len(errs) > 0 || !slices.Equal(names, []string{"bug", "chore", "spike"}) {
		t.Errorf("templates %q %v", names, errs)
	}
	for _, role := range []string{"coordinator", "thread"} {
		if text, ok := BundledPolicyText(role); !ok || strings.TrimSpace(text) == "" {
			t.Errorf("no %s policy", role)
		}
	}
}

// TestLoadBundled: Load lists the installed mods, then the bundled ones;
// a bundled mod's table is no orphan; an installed folder named as one
// is shadowed, not loaded.
func TestLoadBundled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	mods := filepath.Join(home, "mods")
	install(t, mods, "nord", map[string]string{"mod.toml": dataMod("nord", `theme = "theme.toml"`), "theme.toml": ""})
	install(t, mods, "default-theme", map[string]string{"mod.toml": dataMod("default-theme", `theme = "theme.toml"`), "theme.toml": ""})
	r, err := Load(&config.Config{Mods: map[string]config.Mod{"default-templates": {EnabledSet: true}, "gone": {}}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range r.Mods {
		ids = append(ids, m.ID)
	}
	if want := append([]string{"nord"}, BundledIDs()...); !slices.Equal(ids, want) {
		t.Fatalf("mods %q, want %q", ids, want)
	}
	if m, _ := r.Get("default-theme"); !m.Builtin {
		t.Error("the installed default-theme took the built-in's place")
	}
	if !slices.Equal(r.Shadowed, []string{"default-theme"}) || !slices.Equal(r.Orphans, []string{"gone"}) {
		t.Errorf("shadowed %q, orphans %q", r.Shadowed, r.Orphans)
	}
	if m, _ := r.Get(BundledTemplates); m.State != Disabled {
		t.Errorf("templates %s", m.State)
	}
	// The built-in providers come after the user's, in Providers' order.
	if p := r.Providers(KindTheme); len(p) != 1 || p[0].ID != BundledTheme {
		t.Errorf("providers %v (nord is off)", p)
	}
}

// TestBundledWrites: a required built-in mod can't be turned off, no
// built-in mod is removed, and no mod installs under a built-in id.
func TestBundledWrites(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	var req ErrRequired
	if err := SetEnabled(human, BundledKeymap, false); !errors.As(err, &req) || !strings.Contains(err.Error(), "enable a keymap mod") {
		t.Fatalf("disable the keymap: %v", err)
	}
	if err := SetEnabled(human, BundledTemplates, false); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load()
	r, _ := Load(c)
	if m, _ := r.Get(BundledTemplates); m.State != Disabled {
		t.Fatalf("templates %s after disable", m.State)
	}
	if err := Remove(human, BundledTemplates, false); err == nil || !strings.Contains(err.Error(), "built in") {
		t.Fatalf("remove: %v", err)
	}
	if err := Install(human, &Staged{Manifest: &Manifest{ID: BundledTheme}}); err == nil || !strings.Contains(err.Error(), "built-in mod's id") {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("TERMINATR_HOME"), "mods", BundledTheme)); err == nil {
		t.Fatal("installed")
	}
}

// TestCheckData: CheckData says which data file doesn't load, by kind,
// with the console's checks when they are set.
func TestCheckData(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "bad", map[string]string{
		"mod.toml":    dataMod("bad", "layout = \"l.toml\"\ntheme = \"t.toml\"\ntemplates = \"tpl\"\n[provides.rules]\nthread = \"r.md\""),
		"l.toml":      `sidebar = ["nope"]`,
		"t.toml":      "",
		"r.md":        "Be kind.",
		"tpl/oops.md": "no title line",
	})
	r, _ := LoadDir(mods, nil)
	m, _ := r.Get("bad")
	defer func(old func([]byte) error) { DataChecks[KindTheme] = old }(DataChecks[KindTheme])
	DataChecks[KindTheme] = func([]byte) error { return errors.New("bad colour") }
	var got []string
	for _, err := range m.CheckData() {
		got = append(got, err.Error())
	}
	want := []string{"theme: bad colour", `layout: sidebar: no column "nope"`, "templates: bad: template oops: the first line"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("got %q, want %q…", got[i], want[i])
		}
	}
}

// TestBundledCodeCurrent: each bundled code mod's embedded files are what
// go generate makes of its source in examples/mods.
func TestBundledCodeCurrent(t *testing.T) {
	for _, id := range bundlegen.Code {
		want, err := bundlegen.Files(filepath.Join("..", "..", "examples", "mods", id), id)
		if err != nil {
			t.Fatal(err)
		}
		for name, b := range want {
			got, err := fs.ReadFile(bundledFS, "bundled/"+id+"/"+name)
			if err != nil || !bytes.Equal(got, b) {
				t.Errorf("bundled/%s/%s is stale: run go generate ./internal/mod", id, name)
			}
		}
	}
}

// TestBundledMemoryTab: the Memory tab is a built-in code mod, on by
// default, with what it asks for granted, its bundle embedded, and one
// project tab; a table turns it off.
func TestBundledMemoryTab(t *testing.T) {
	m := Bundled(BundledMemory, config.Mod{})
	if m.State != Enabled || !m.Builtin || m.Required || m.Manifest == nil || !m.Manifest.IsCode() {
		t.Fatalf("memory-tab: %+v", m)
	}
	if !slices.Equal(m.Grants, []string{PermProjectRead, PermViews}) {
		t.Errorf("grants %v", m.Grants)
	}
	if b, err := m.ReadFile(m.Manifest.Provides.Main); err != nil || len(b) == 0 {
		t.Errorf("its bundle: %v", err)
	}
	if v := m.Manifest.Provides.Views; len(v) != 1 || v[0].Slot != SlotProjectTab || v[0].ID != MemoryView {
		t.Errorf("views %+v", v)
	}
	if m := Bundled(BundledMemory, config.Mod{EnabledSet: true}); m.State != Disabled {
		t.Errorf("turned off: %s", m.State)
	}
}
