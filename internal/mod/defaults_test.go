package mod

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// policyMod is a data mod that suggests defaults for all projects (mods
// design §7, Tighten only).
const policyMod = `
id = "policy"
name = "Policy"
version = "1.0.0"
api = "1.1"

[defaults]
auto_close = "days"
auto_close_days = 3
pr_followup = true
`

func TestManifestDefaults(t *testing.T) {
	m, err := ParseManifest([]byte(policyMod), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Kinds(), ",") != "defaults" || len(m.Permissions.Requested()) != 0 {
		t.Fatalf("kinds %v", m.Kinds())
	}
	c := Consent(m, "")
	for _, want := range []string{"Provides: defaults", DefaultsNote + ":", `  - auto_close = "days"`, "  - auto_close_days = 3", "  - pr_followup = true"} {
		if !strings.Contains(c, want) {
			t.Errorf("consent lacks %q:\n%s", want, c)
		}
	}
	// tm's own defaults: auto_close merged, 7 days, PR follow-up on.
	sgs := m.Suggestions(nil)
	if len(sgs) != 3 || sgs[0].Key != "auto_close" || sgs[0].Now != config.CloseMerged || sgs[0].Value != "days" || !sgs[0].Applies() ||
		sgs[1].Value != 3 || sgs[1].Now != 7 || !sgs[1].Applies() || sgs[2].Applies() {
		t.Fatalf("suggestions %+v", sgs)
	}
}

// TestManifestDefaultsRefused: a mod suggests only DefaultKeys, each
// with a value config.toml would take; a safety setting makes it
// invalid with loosens's reason, once.
func TestManifestDefaultsRefused(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`merge = "thread"`, "defaults.merge: a mod can't set merge: safety settings are the human's"},
		{`yolo = true`, "defaults.yolo: a mod can't set yolo"},
		{`guard_off = ["merge"]`, "defaults.guard_off: a mod can't set guard_off"},
		{`thread_agent = "codex"`, "defaults.thread_agent: a mod may suggest only auto_close,"},
		{`paused = true`, "defaults.paused: a mod may suggest only"},
		{`auto_close = "sometimes"`, `defaults.auto_close: auto-close must be "off", "merged" or "days"`},
		{`auto_close_days = 9999`, "defaults.auto_close_days: auto-close days must be 1 to 365"},
		{`pr_followup = "yes"`, "defaults.pr_followup: pr_followup must be true or false"},
		{`pr_poll_seconds = 5`, "defaults.pr_poll_seconds: PR poll seconds must be 30 to 3600"},
	} {
		_, err := ParseManifest([]byte("id = \"p\"\nname = \"P\"\nversion = \"1.0.0\"\napi = \"1.1\"\n[defaults]\n"+tc.line+"\n"), "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.line, err, tc.want)
		} else if strings.Count(err.Error(), "defaults.") != 1 {
			t.Errorf("%s: more than one error: %v", tc.line, err)
		}
	}
}

// TestDefaultKeys: a mod suggests only settings config.toml has for all
// projects, never a safety one.
func TestDefaultKeys(t *testing.T) {
	for _, k := range DefaultKeys {
		if slices.Contains(SafetyKeys, k) || !slices.Contains(config.ProjectKeys, k) || slices.Contains(config.ProjectOnly, k) {
			t.Errorf("%s can't be a suggested default", k)
		}
		if config.Defaults.Value(k) == nil {
			t.Errorf("%s has no value in config.Safety", k)
		}
	}
}
