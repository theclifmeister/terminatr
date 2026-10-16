package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
)

// TestGuardAllowLoad: a project's allowances add to all projects', which
// come first marked All; guard_mode is enforce unless set.
func TestGuardAllowLoad(t *testing.T) {
	write(t, `[defaults]
guard_allow = [{ rule = "worktree-only", path = "~/src/shared/*" }]
guard_mode = "learn"

[projects.a]
guard_allow = [{ rule = "force-push", command = "git push -f origin tm/*" }, { rule = "worktree-only", path = "~/src/shared/*" }]
guard_mode = "enforce"
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	shared := GuardAllow{Rule: "worktree-only", Path: "~/src/shared/*"}
	all, _ := c.AllProjects()
	if !reflect.DeepEqual(all.GuardAllow, []GuardAllow{shared}) || all.GuardMode != GuardLearn {
		t.Errorf("all projects %+v %s", all.GuardAllow, all.GuardMode)
	}
	a, _ := c.Safety("a")
	marked := shared
	marked.All = true
	want := []GuardAllow{marked, {Rule: "force-push", Command: "git push -f origin tm/*"}}
	if !reflect.DeepEqual(a.GuardAllow, want) || a.GuardMode != GuardEnforce {
		t.Errorf("a %+v %s", a.GuardAllow, a.GuardMode)
	}
	if own := OwnAllows(a.GuardAllow); !reflect.DeepEqual(own, want[1:]) {
		t.Errorf("own %+v", own)
	}
	if b, _ := c.Safety("b"); !reflect.DeepEqual(b.GuardAllow, []GuardAllow{shared}) || b.GuardMode != GuardLearn {
		t.Errorf("b %+v", b)
	}
	write(t, "")
	if c, _ := Load(); c != nil {
		if s, _ := c.Safety("x"); s.GuardMode != GuardEnforce || s.GuardAllow != nil {
			t.Errorf("defaults %+v", s)
		}
	}
}

// TestGuardAllowCheck: what an allowance may be, in the file and to the
// writers alike.
func TestGuardAllowCheck(t *testing.T) {
	for body, want := range map[string]string{
		`guard_allow = [{ rule = "merge", command = "gh pr merge 1" }]`:                                  "can't allow merge",
		`guard_allow = [{ rule = "push", command = "git push" }]`:                                        `unknown rule "push"`,
		`guard_allow = [{ rule = "force-push" }]`:                                                        "needs a command or a path",
		`guard_allow = [{ rule = "force-push", command = "x", path = "/x" }]`:                            "needs a command or a path",
		`guard_allow = [{ rule = "credentials", command = "gh auth *" }]`:                                "exact command or path, no *",
		`guard_allow = [{ rule = "worktree-only", path = "*" }]`:                                         "allows everything",
		`guard_allow = [{ rule = "worktree-only", path = "src/x" }]`:                                     "must be absolute",
		`guard_allow = [{ rule = "force-push", command = "a\u0007b" }]`:                                  "control character",
		`guard_allow = [{ rule = "worktree-only", path = "/a\nb" }]`:                                     "one line",
		`guard_allow = [{ rule = "force-push", command = "x" }, { rule = "force-push", command = "x" }]`: "twice",
		`guard_allow = [{ rule = "force-push", command = "x", what = 1 }]`:                               "unknown setting",
		`guard_mode = "relaxed"`: `must be "enforce" or "learn"`,
	} {
		write(t, "[projects.demo]\n"+body+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
	for _, ok := range []GuardAllow{
		{Rule: "credentials", Path: "~/.npmrc"}, {Rule: "credentials", Command: "gh auth token"},
		{Rule: "mod:infra.no-destroy", Command: "terraform destroy -target=x"},
		{Rule: "worktree-only", Path: `C:\Users\me\x`},
		{Rule: "force-push", Command: "cat <<EOF | sh\n\tgit push -f\nEOF"},
	} {
		if err := CheckGuardAllow(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	if err := CheckSetting("guard_allow", []GuardAllow{{Rule: "credentials", Path: "/h/*"}}); err == nil {
		t.Error("a credentials pattern written")
	}
	if err := CheckSetting("guard_mode", "learn"); err != nil {
		t.Error(err)
	}
}

// TestGuardAllowWrite: the writers set and read back allowances, and the
// guard's other settings, in a project's table and [defaults].
func TestGuardAllowWrite(t *testing.T) {
	write(t, "[projects.demo]\nyolo = true\n")
	list := []GuardAllow{{Rule: "force-push", Command: `git push -f origin "tm/x"`}, {Rule: "worktree-only", Path: "~/src/a b/*"}, {Rule: "force-push", Command: "cat <<EOF | sh\n\tgit push -f\nEOF"}}
	if err := SetProject("demo", "guard_allow", list); err != nil {
		t.Fatal(err)
	}
	if err := SetProject("demo", "guard_off", []string{"merge"}); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaults("guard_mode", GuardLearn); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaults("guard", false); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Safety("demo")
	if !reflect.DeepEqual(s.GuardAllow, list) || s.GuardMode != GuardLearn || s.Guard || !reflect.DeepEqual(s.GuardOff, []string{"merge"}) {
		t.Errorf("%+v", s)
	}
	if own := c.Own("demo"); !reflect.DeepEqual(own, []string{"yolo", "guard_off", "guard_allow"}) {
		t.Errorf("own %v", own)
	}
	if err := SetProject("demo", "guard_allow", []GuardAllow{}); err != nil {
		t.Fatal(err)
	}
	if err := UnsetProject("demo", "guard_allow"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if s, _ := c.Safety("demo"); s.GuardAllow != nil {
		t.Errorf("after unset %+v", s.GuardAllow)
	}
}

// TestSecretEnv: a project's variables over all projects', by name; the
// names and sources it takes.
func TestSecretEnv(t *testing.T) {
	write(t, `[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }
NPM_TOKEN = { from = "env:NPM_TOKEN" }

[projects.a.env]
NPM_TOKEN = { from = "keychain:npm registry" }
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	env, err := c.SecretEnv("a")
	want := map[string]EnvSource{"NODE_AUTH_TOKEN": {From: EnvFromGH}, "NPM_TOKEN": {From: "keychain:npm registry"}}
	if err != nil || !reflect.DeepEqual(env, want) {
		t.Errorf("a %v %v", env, err)
	}
	if env, _ := c.SecretEnv("b"); env["NPM_TOKEN"].From != "env:NPM_TOKEN" || len(env) != 2 {
		t.Errorf("b %v", env)
	}
	if k, n := (EnvSource{From: "keychain:npm registry"}).Kind(); k != "keychain" || n != "npm registry" {
		t.Errorf("kind %s %s", k, n)
	}
	if names := c.SecretEnvNames(""); !reflect.DeepEqual(names, []string{"NODE_AUTH_TOKEN", "NPM_TOKEN"}) {
		t.Errorf("names %v", names)
	}
	for body, want := range map[string]string{
		"[projects.a.env]\nPATH = { from = \"env:X\" }\n":                   "can't be a secret",
		"[projects.a.env]\nTERMINATR_X = { from = \"env:X\" }\n":            "can't be a secret",
		"[projects.a.env]\nX = { from = \"vault\" }\n":                      "from must be",
		"[projects.a.env]\nX = { from = \"env:A B\" }\n":                    "from must be",
		"[defaults.env]\nX = { from = \"gh-auth-token\", value = \"s\" }\n": "unknown setting",
		"[projects.a.env]\n\"A-B\" = { from = \"env:X\" }\n":                "isn't a variable name",
	} {
		write(t, body)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// TestSetEnv: the writers add a secret variable to a project's or all
// projects' env table, replace it, refuse a bad name or source, and
// remove it, the user's comments and other tables kept.
func TestSetEnv(t *testing.T) {
	write(t, `# mine
[projects.a]
yolo = true

[projects.a.env]
# for npm
NPM_TOKEN = { from = "env:NPM_TOKEN" }
`)
	if err := SetEnv("projects.a", "NODE_AUTH_TOKEN", EnvSource{From: EnvFromGH}); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv(DefaultsTable, "REG_TOKEN", EnvSource{From: "keychain:my registry"}); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv("projects.a", "NPM_TOKEN", EnvSource{From: "keychain:npm"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.EnvOf("a"), map[string]EnvSource{"NODE_AUTH_TOKEN": {From: EnvFromGH}, "NPM_TOKEN": {From: "keychain:npm"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("a %v", got)
	}
	if got := c.EnvOf(""); len(got) != 1 || got["REG_TOKEN"].From != "keychain:my registry" {
		t.Errorf("defaults %v", got)
	}
	if b, _ := os.ReadFile(c.Path); !strings.Contains(string(b), "# for npm\nNPM_TOKEN = { from = \"keychain:npm\" }") || !strings.Contains(string(b), "# mine") {
		t.Errorf("file:\n%s", b)
	}
	for name, e := range map[string]EnvSource{"PATH": {From: EnvFromGH}, "CLAUDE_X": {From: EnvFromGH}, "A-B": {From: EnvFromGH}, "X": {From: "vault"}, "Y": {From: "env:"}} {
		if err := SetEnv("projects.a", name, e); err == nil {
			t.Errorf("%s %v written", name, e)
		}
	}
	if err := SetEnv("ui", "X", EnvSource{From: EnvFromGH}); err == nil {
		t.Error("env in [ui]")
	}
	if err := UnsetEnv("projects.a", "NPM_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := UnsetEnv(DefaultsTable, "REG_TOKEN"); err != nil {
		t.Fatal(err)
	}
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.EnvOf("a"); len(got) != 1 || got["NODE_AUTH_TOKEN"].From != EnvFromGH {
		t.Errorf("after unset a %v", got)
	}
	if got := c.EnvOf(""); len(got) != 0 {
		t.Errorf("after unset defaults %v", got)
	}
	// An inline env table isn't the line editor's: it stays.
	write(t, "[projects.b]\nenv = { X = { from = \"gh-auth-token\" } }\n")
	if err := SetEnv("projects.b", "Y", EnvSource{From: EnvFromGH}); err != ErrForm {
		t.Errorf("inline table: %v", err)
	}
}

// TestGuardKeysByHuman: the guard's settings are the writers' (the
// settings popups), never a mod's to suggest.
func TestGuardKeysByHuman(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	for _, k := range []string{"guard", "guard_off", "guard_mode", "guard_allow"} {
		if !validKey(k) {
			t.Errorf("%s isn't written by the popups", k)
		}
	}
	if err := SetDefaults("guard_mode", "learn"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	if !strings.Contains(string(b), "guard_mode = \"learn\"") {
		t.Errorf("%s", b)
	}
}
