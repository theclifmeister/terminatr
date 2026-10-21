package config

import (
	"strings"
	"testing"
)

func TestProtectionLevels(t *testing.T) {
	for _, c := range []struct {
		level                   string
		rules, sandbox, escapes bool
	}{
		{ProtectOpen, false, false, true},
		{ProtectGuarded, true, false, true},
		{ProtectSandboxed, true, true, true},
		{ProtectStrict, true, true, false},
	} {
		s := Safety{Protection: c.level, Guard: true}
		if s.GuardRulesOn() != c.rules || s.ThreadSandbox() != c.sandbox || s.SandboxEscapes() != c.escapes {
			t.Errorf("%s: rules %v sandbox %v escapes %v", c.level, s.GuardRulesOn(), s.ThreadSandbox(), s.SandboxEscapes())
		}
		// The guard off lifts its rules at every level.
		if s.Guard = false; s.GuardRulesOn() {
			t.Errorf("%s: guard rules with the guard off", c.level)
		}
	}
}

func TestProtectionSetting(t *testing.T) {
	write(t, `
[defaults]
sandbox_escapes = false

[projects.demo]
protection = "guarded"

[projects.old]
sandbox_escapes = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for slug, want := range map[string]string{"demo": ProtectGuarded, "old": ProtectStrict, "new": ProtectStrict} {
		if s, _ := c.Safety(slug); s.Protection != want {
			t.Errorf("%s: protection %q, want %q", slug, s.Protection, want)
		}
	}
	write(t, "")
	if c, _ = Load(); c != nil {
		if s, _ := c.Safety("demo"); s.Protection != ProtectOpen {
			t.Errorf("default protection %q, want open", s.Protection)
		}
	}
	write(t, "[defaults]\nprotection = \"loose\"\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), `"open", "guarded", "sandboxed" or "strict"`) {
		t.Errorf("a bad level: %v", err)
	}
	if err := CheckSetting("protection", "strict"); err != nil {
		t.Error(err)
	}
}

func TestMigrateProtection(t *testing.T) {
	for _, c := range []struct {
		name, in   string
		existing   bool
		out, level string
	}{
		{"new install", "", false, "", ""},
		{"projects, no file", "", true, "[defaults]\nprotection = \"sandboxed\"\n", ProtectSandboxed},
		{"settings", "[defaults]\nyolo = false\n", false, "[defaults]\nyolo = false\nprotection = \"sandboxed\"\n", ProtectSandboxed},
		{"escapes off", "[defaults]\nsandbox_escapes = false\n\n[projects.demo]\nsandbox_escapes = true\n\n[projects.other]\nmerge_method = \"squash\"\n",
			true, "[defaults]\nprotection = \"strict\"\n\n[projects.demo]\nprotection = \"sandboxed\"\n\n[projects.other]\nmerge_method = \"squash\"\n", ProtectStrict},
		{"set already", "[projects.demo]\nprotection = \"open\"\n", true, "[projects.demo]\nprotection = \"open\"\n", ""},
	} {
		out, level, err := migrateProtection([]byte(c.in), c.existing)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(out) != c.out || level != c.level {
			t.Errorf("%s: got %q (%q), want %q (%q)", c.name, out, level, c.out, c.level)
		}
	}
}

func TestResetAccess(t *testing.T) {
	write(t, `[defaults]
protection = "sandboxed"
merge_method = "squash"

[projects.demo]
yolo = true
guard_mode = "learn"
guard_allow = [{ rule = "push-default", command = "git push origin main" }]
sandbox_escapes = false
parallel_threads = 3

[projects.demo.sandbox]
hosts = ["registry.npmjs.org"]
`)
	reset, err := ResetAccess("projects.demo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reset, " ") != "protection yolo guard_mode" {
		t.Errorf("reset %q", reset)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Safety("demo")
	// It follows all projects' sandboxed again; the rest is kept.
	if s.Protection != ProtectSandboxed || s.Yolo || s.GuardMode != GuardEnforce || len(s.GuardAllow) != 1 || s.ParallelThreads != 3 ||
		len(c.ThreadSandbox("demo").Given(SandboxHosts)) != 1 {
		t.Errorf("demo after reset %+v", s)
	}
	if reset, _ = ResetAccess("defaults"); strings.Join(reset, " ") != "protection" {
		t.Errorf("defaults reset %q", reset)
	}
	c, _ = Load()
	if s, _ := c.AllProjects(); s.Protection != ProtectOpen || s.MergeMethod != MethodSquash {
		t.Errorf("all projects after reset %+v", s)
	}
	if reset, err = ResetAccess("defaults"); err != nil || len(reset) != 0 {
		t.Errorf("a second reset: %q %v", reset, err)
	}
}
