package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestSecretEnvNpmrc: a repo whose .npmrc reads a registry token from a
// variable the project's threads don't get is warned about, once; one
// the secret environment gives is not.
func TestSecretEnvNpmrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	repo := t.TempDir()
	os.WriteFile(filepath.Join(repo, ".npmrc"), []byte("@acme:registry=https://npm.pkg.github.com\n//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\n//r.example.com/npm/:_authToken=${NODE_AUTH_TOKEN}\n//x.example.com/:_auth=${X_AUTH}\nregistry=https://registry.npmjs.org\n"), 0o600)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetRepos("web", []string{repo}); err != nil {
		t.Fatal(err)
	}
	cs := SecretEnv(Deps{})
	if len(cs) != 2 || !strings.Contains(cs[0].Detail, "NODE_AUTH_TOKEN for npm.pkg.github.com") || !strings.Contains(cs[1].Detail, "X_AUTH") || cs[0].Status != Warn {
		t.Fatalf("%+v", cs)
	}
	f, _ := os.OpenFile(filepath.Join(home, "config.toml"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("\n[projects.web.env]\nNODE_AUTH_TOKEN = { from = \"gh-auth-token\" }\nX_AUTH = { from = \"env:X\" }\n")
	f.Close()
	if cs := SecretEnv(Deps{}); len(cs) != 0 {
		t.Errorf("with the secret environment: %+v", cs)
	}
}
