package mod

import (
	"strings"
	"testing"
)

func codeModWith(id, commands string) string {
	return "id = \"" + id + "\"\nname = \"" + strings.ToUpper(id) + "\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nmain = \"main.ts\"\n" + commands
}

func TestManifestCommands(t *testing.T) {
	m, err := ParseManifest([]byte(codeModWith("lab", "[[provides.commands]]\nid = \"relabel\"\ntitle = \"Relabel tasks\"\nkey = \"L\"\n[[provides.commands]]\nid = \"quiet\"\ntitle = \"Quiet\"\nmenu = false\n")), "")
	if err != nil {
		t.Fatal(err)
	}
	c := m.Provides.Commands
	if len(c) != 2 || c[0].Key != "L" || !c[0].InMenu() || c[1].InMenu() {
		t.Fatalf("commands %+v", c)
	}
	if k := m.Kinds(); k[len(k)-1] != "commands" {
		t.Fatalf("kinds %v", k)
	}
	if got := consent(m, nil); !strings.Contains(got, "Commands you can run: Relabel tasks (L) · Quiet") {
		t.Fatalf("consent:\n%s", got)
	}
}

func TestManifestCommandErrors(t *testing.T) {
	cmd := func(body string) string { return codeModWith("lab", "[[provides.commands]]\n"+body) }
	for _, tc := range []struct{ name, toml, want string }{
		{"no main", dataMod("lab", "[[provides.commands]]\nid = \"x\"\ntitle = \"X\"\n"), "needs provides.main"},
		{"bad id", cmd("id = \"X_y\"\ntitle = \"X\"\n"), "lowercase letters, digits and dashes"},
		{"no title", cmd("id = \"x\"\n"), "has no title"},
		{"long title", cmd("id = \"x\"\ntitle = \"" + strings.Repeat("t", 49) + "\"\n"), "longer than 48"},
		{"twice", cmd("id = \"x\"\ntitle = \"X\"\n[[provides.commands]]\nid = \"x\"\ntitle = \"Y\"\n"), "listed twice"},
		{"locked key", cmd("id = \"x\"\ntitle = \"X\"\nkey = \"esc\"\n"), "esc is locked"},
		{"digit", cmd("id = \"x\"\ntitle = \"X\"\nkey = \"3\"\n"), "3 is locked"},
		{"prefix", cmd("id = \"x\"\ntitle = \"X\"\nkey = \"prefix+g\"\n"), "no prefix key"},
		{"not a key", cmd("id = \"x\"\ntitle = \"X\"\nkey = \"ctrl+ G\"\n"), "isn't a key"},
		{"key twice", cmd("id = \"x\"\ntitle = \"X\"\nkey = \"g\"\n[[provides.commands]]\nid = \"y\"\ntitle = \"Y\"\nkey = \"g\"\n"), "key g is x's already"},
	} {
		_, err := ParseManifest([]byte(tc.toml), "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

// TestRegistryCommands: the enabled code mods' commands, in the order
// the mods were enabled; a key two mods bind goes to the one enabled
// first, and the other's command keeps no key and says why.
func TestRegistryCommands(t *testing.T) {
	mods := t.TempDir()
	install(t, mods, "beta", map[string]string{"main.ts": "", "mod.toml": codeModWith("beta", "[[provides.commands]]\nid = \"sync\"\ntitle = \"Sync\"\nkey = \"g\"\n")})
	install(t, mods, "alpha", map[string]string{"main.ts": "", "mod.toml": codeModWith("alpha", "[[provides.commands]]\nid = \"go\"\ntitle = \"Go\"\nkey = \"g\"\n[[provides.commands]]\nid = \"stop\"\ntitle = \"Stop\"\nkey = \"G\"\n")})
	install(t, mods, "off", map[string]string{"main.ts": "", "mod.toml": codeModWith("off", "[[provides.commands]]\nid = \"x\"\ntitle = \"X\"\n")})
	// beta was enabled before alpha, though alpha's id sorts first.
	r, _ := LoadDir(mods, enabled("beta", "alpha"))
	var got []string
	for _, c := range r.Commands() {
		if c.Builtin {
			continue
		}
		got = append(got, CommandName(c.Mod, c.ID)+"="+c.Key+"|"+c.Conflict)
	}
	want := "beta:sync=g| alpha:go=|key conflict: g is beta:sync's alpha:stop=G|"
	if strings.Join(got, " ") != want {
		t.Fatalf("commands\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}
