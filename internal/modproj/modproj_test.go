package modproj

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
)

func manifest(desc string, perms mod.Permissions) *mod.Manifest {
	return &mod.Manifest{ID: "ci", Name: "CI", Version: "1.0.0", Description: desc, Permissions: perms}
}

// TestToggleFor: the Mods tab's space, in each of its cases.
func TestToggleFor(t *testing.T) {
	off := mod.Mod{ID: "ci", Manifest: manifest("", mod.Permissions{}), State: mod.Disabled}
	every := mod.Mod{ID: "ci", Manifest: off.Manifest, State: mod.Enabled}
	limited := mod.Mod{ID: "ci", Manifest: off.Manifest, State: mod.Enabled, Projects: []string{"web"}}
	more := mod.Mod{ID: "ci", Manifest: off.Manifest, State: mod.Enabled, Projects: []string{"web"}, WantsMore: []string{mod.PermNet}}
	req := mod.Mod{ID: "look", Manifest: off.Manifest, State: mod.Enabled, Required: true}
	bad := mod.Mod{ID: "ci", State: mod.Invalid}
	for _, c := range []struct {
		md     mod.Mod
		slug   string
		locals []string
		do     string
		ask    string
	}{
		{off, "demo", nil, DoConsent, "Enable CI for demo?"},
		{every, "demo", []string{"demo", "web"}, DoLimit, "CI runs for every project. Run it only for web instead? A project made later won't run it until you turn it on there."},
		{every, "demo", []string{"demo"}, DoOffAll, "CI runs for every project, and demo is the only one. Turn it off?"},
		{limited, "web", nil, DoOff, ""},
		{limited, "demo", nil, DoOn, ""},
		{more, "demo", nil, DoConsent, "Grant CI what it wants more?"},
		{req, "demo", nil, DoRefuse, ""},
		{bad, "demo", nil, DoRefuse, ""},
	} {
		got := ToggleFor(c.md, c.slug, c.locals)
		if got.Do != c.do || got.Question != c.ask {
			t.Errorf("%+v for %s: %+v", c.md, c.slug, got)
		}
		if k := KeyOf(c.md, got); (k == "") != (c.ask == "") {
			t.Errorf("%s: key %q", c.do, k)
		}
	}
}

// TestKey: the key changes with anything the consent shows, and with
// the question.
func TestKey(t *testing.T) {
	a := mod.Mod{ID: "ci", Manifest: manifest("Checks.", mod.Permissions{}), State: mod.Disabled}
	b := mod.Mod{ID: "ci", Manifest: manifest("Checks!", mod.Permissions{}), State: mod.Disabled}
	c := mod.Mod{ID: "ci", Manifest: manifest("Checks.", mod.Permissions{Net: []string{"api.example.com"}}), State: mod.Disabled}
	ka := KeyOf(a, ToggleFor(a, "demo", nil))
	if ka == "" || ka != KeyOf(a, ToggleFor(a, "demo", nil)) {
		t.Fatalf("key %q", ka)
	}
	for _, other := range []string{KeyOf(b, ToggleFor(b, "demo", nil)), KeyOf(c, ToggleFor(c, "demo", nil)), KeyOf(a, ToggleFor(a, "web", nil))} {
		if other == ka {
			t.Error("a different consent has the same key")
		}
	}
	var text []string
	for _, l := range Consent(c, "Enable CI?") {
		text = append(text, l.Text())
	}
	if all := strings.Join(text, "\n"); !strings.Contains(all, "It may:\n  • ") || !strings.HasSuffix(all, "\nEnable CI?") {
		t.Errorf("consent:\n%s", all)
	}
}
