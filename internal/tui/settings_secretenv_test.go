package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// secretEnvData is popupData with a secret environment for all projects
// and alpha, as Load reads it.
func secretEnvData(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src, m := popupData(t)
	os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(`[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }
NPM_TOKEN = { from = "env:NPM_TOKEN" }

[projects.alpha.env]
NPM_TOKEN = { from = "keychain:npm registry" }
`), 0o600)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m.data.Env = map[string]map[string]config.EnvSource{"": c.EnvOf(""), "alpha": c.EnvOf("alpha")}
	return src, m
}

// TestSecretEnvView: a project's list has its own variables and all
// projects', marked, by name and source; T tests the selected one in its
// own table, X removes it there.
func TestSecretEnvView(t *testing.T) {
	src, m := secretEnvData(t)
	row := guardRow(t, "alpha", "Secret environment")
	if v := row.value(m); v != "2 (1 of all projects)" {
		t.Errorf("value %q", v)
	}
	if v := guardRow(t, "", "Secret environment").value(m); v != "2" {
		t.Errorf("all projects' value %q", v)
	}
	drain(t, m, row.change(m))
	out := screen(m)
	for _, want := range []string{"Secret environment for alpha", `from the keychain item "npm registry"`,
		"NODE_AUTH_TOKEN · all projects", "from gh auth token", "NPM_TOKEN · all projects, replaced here", "from the server's NPM_TOKEN",
		"A add · X remove · T test"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	drain(t, m, keyPress(m, "T"))
	keyPress(m, "down")
	drain(t, m, keyPress(m, "T"))
	if !slices.Equal(src.secretTests, []string{"alpha NPM_TOKEN", " NODE_AUTH_TOKEN"}) {
		t.Errorf("tests %v", src.secretTests)
	}
	if !strings.Contains(m.msg, "NODE_AUTH_TOKEN: ok") {
		t.Errorf("msg %q", m.msg)
	}
	// X on an all-projects variable removes it from all projects'.
	drain(t, m, keyPress(m, "X"))
	if got := src.secrets[len(src.secrets)-1]; got != "defaults NODE_AUTH_TOKEN removed" {
		t.Errorf("removed %v", src.secrets)
	}
	if _, ok := m.data.Env[""]["NODE_AUTH_TOKEN"]; ok {
		t.Error("still shown")
	}
	c, _ := config.Load()
	if _, ok := c.EnvOf("")["NODE_AUTH_TOKEN"]; ok {
		t.Error("still in the file")
	}
}

// TestSecretEnvAdd: A asks the name, refusing one tm sets, then the
// source; keychain and env sources ask what they name; the variable is
// written to the view's table.
func TestSecretEnvAdd(t *testing.T) {
	src, m := secretEnvData(t)
	drain(t, m, guardRow(t, "alpha", "Secret environment").change(m))
	enter := func(text string) {
		t.Helper()
		in, ok := m.top().(*inputView)
		if !ok {
			t.Fatalf("no input: %T", m.top())
		}
		in.lineEdit = newLine(text)
		drain(t, m, keyPress(m, "enter"))
	}
	keyPress(m, "A")
	enter("PATH")
	if len(src.secrets) != 0 || !strings.Contains(m.msg, "can't be a secret") {
		t.Errorf("PATH taken: %v %q", src.secrets, m.msg)
	}
	if _, ok := m.top().(*secretEnvView); !ok {
		t.Fatalf("not back on the list: %T", m.top())
	}
	keyPress(m, "A")
	enter("REG_TOKEN")
	if _, ok := m.top().(*menuView); !ok {
		t.Fatalf("no source picker: %T", m.top())
	}
	keyPress(m, "down")
	keyPress(m, "down")
	drain(t, m, keyPress(m, "enter"))
	enter("REG_SECRET")
	if got := src.secrets; !slices.Equal(got, []string{"projects.alpha REG_TOKEN=env:REG_SECRET"}) {
		t.Errorf("added %v", got)
	}
	if !strings.Contains(m.msg, "REG_TOKEN added for alpha, from the server's REG_SECRET") {
		t.Errorf("msg %q", m.msg)
	}
	// gh auth token needs nothing more; a bad env name is refused.
	keyPress(m, "A")
	enter("NODE_AUTH_TOKEN")
	drain(t, m, keyPress(m, "enter"))
	keyPress(m, "A")
	enter("OTHER")
	keyPress(m, "down")
	keyPress(m, "down")
	drain(t, m, keyPress(m, "enter"))
	enter("A B")
	if got := src.secrets; len(got) != 2 || got[1] != "projects.alpha NODE_AUTH_TOKEN=gh-auth-token" || !strings.Contains(m.msg, "from must be") {
		t.Errorf("%v %q", got, m.msg)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.EnvOf("alpha"); len(got) != 3 || got["REG_TOKEN"].From != "env:REG_SECRET" || got["NODE_AUTH_TOKEN"].From != config.EnvFromGH {
		t.Errorf("written %v", got)
	}
	if v := guardRow(t, "alpha", "Secret environment").value(m); v != "3" {
		t.Errorf("value %q", v)
	}
}
