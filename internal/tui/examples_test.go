package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// TestExampleMods: every mod of terminatr's examples/mods validates with
// the console's data checks (mods design §13: kept honest by CI), one
// folder for each of mod.Examples and no other, asking for the
// permissions it lists; a keymap applies with nothing refused, also as
// the console loads it enabled, and an icon set or theme loads as the
// dashboard reads it. Their code runs in internal/modtest's TestExamples.
func TestExampleMods(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "mods")
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]mod.Example{}
	for _, e := range mod.Examples {
		listed[e.ID] = e
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		n++
		dir := filepath.Join(root, d.Name())
		e, ok := listed[d.Name()]
		if !ok {
			t.Errorf("%s isn't in mod.Examples", d.Name())
		}
		m, err := mod.Validate(dir)
		if err != nil {
			t.Errorf("%s: %v", d.Name(), err)
			continue
		}
		if m.ID != d.Name() {
			t.Errorf("%s: id %q isn't its folder's name", d.Name(), m.ID)
		}
		if asks := m.Permissions.Requested(); ok && !slices.Equal(asks, e.Perms()) {
			t.Errorf("%s asks for %q; mod.Examples says %q", m.ID, asks, e.Perms())
		}
		// In the consent screen's words: gitlab "runs glab", not "runs "
		// (T301 B6).
		if want := m.Permissions.Words(m.Permissions.Requested()); ok && !slices.Equal(e.Words(), want) {
			t.Errorf("%s: tm mod examples says %q; its consent says %q", m.ID, e.Words(), want)
		}
		x := mod.Mod{ID: m.ID, Dir: dir, Manifest: m}
		if x.Has(mod.KindKeymap) {
			data, _ := x.Data(mod.KindKeymap)
			km, err := mod.ParseKeymap(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplyKeymap(km, DefaultPrefixKey); err != nil {
				t.Errorf("%s keymap: %v", m.ID, err)
			}
			// As a console loads it, enabled: vim-keys's pgdown in both
			// the sidebar and the lists was a conflict there (T301 B3).
			x.State = mod.Enabled
			if err := loadKeymap(&mod.Registry{Mods: []mod.Mod{x}}); err != nil {
				t.Errorf("%s keymap enabled: %v", m.ID, err)
			}
			if err := ApplyKeymap(nil, DefaultPrefixKey); err != nil {
				t.Fatal(err)
			}
		}
		if x.Has(mod.KindLayout) {
			// CheckData parses it and gates its keys on the manifest's api.
			if errs := x.CheckData(); len(errs) > 0 {
				t.Errorf("%s layout: %v", m.ID, errs)
			}
			data, _ := x.Data(mod.KindLayout)
			if l, err := mod.ParseLayout(data); err != nil || !l.V2 {
				t.Errorf("%s layout: %v, v2 %v", m.ID, err, l.V2)
			}
		}
		if x.Has(mod.KindIcons) {
			data, _ := x.Data(mod.KindIcons)
			if s, err := parseIcons(data); err != nil || s.name != m.ID {
				t.Errorf("%s icons: %v, named %q", m.ID, err, s.name)
			}
		}
	}
	if n != len(mod.Examples) {
		t.Errorf("%d example folders, %d in mod.Examples", n, len(mod.Examples))
	}
}
