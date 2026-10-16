package server

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// fakeSecrets makes secretRunner answer gh auth token with out (or fail
// with err), recording what it ran.
func fakeSecrets(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var ran []string
	old := secretRunner
	secretRunner = func(_ context.Context, _ []string, name string, args ...string) (string, error) {
		ran = append(ran, strings.Join(append([]string{name}, args...), " "))
		return out, err
	}
	t.Cleanup(func() { secretRunner = old })
	return &ran
}

// TestSecretEnvSources: each source gives its value; one that fails is
// left out and named, with no output of the program.
func TestSecretEnvSources(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.env]
GH_TOKEN_X = { from = "gh-auth-token" }
FROM_ENV = { from = "env:SRV_SECRET" }
MISSING = { from = "env:NOT_SET" }
`)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ran := fakeSecrets(t, "gho_abc\n", nil)
	got, fails := secretEnv(cfg, "demo", []string{"SRV_SECRET=s3cr3t", "PATH=/bin"})
	if got["GH_TOKEN_X"] != "gho_abc" || got["FROM_ENV"] != "s3cr3t" || len(got) != 2 {
		t.Errorf("values %v", got)
	}
	if !slices.Equal(*ran, []string{"gh auth token"}) {
		t.Errorf("ran %v", *ran)
	}
	if len(fails) != 1 || !strings.Contains(fails[0], "MISSING") || !strings.Contains(fails[0], "NOT_SET") {
		t.Errorf("fails %v", fails)
	}
	fakeSecrets(t, "token-in-output", errors.New("exit status 1"))
	_, fails = secretEnv(cfg, "demo", nil)
	if j := strings.Join(fails, "\n"); !strings.Contains(j, "gh auth token failed (exit status 1)") || strings.Contains(j, "token-in-output") {
		t.Errorf("fails %v", fails)
	}
	if got := envHide([]string{"A_TOKEN=1", "PATH=/bin", "NPM_TOKEN=2", "my_key=3", "A_TOKEN=4"}, []string{"NPM_TOKEN"}); !slices.Equal(got, []string{"A_TOKEN", "my_key"}) {
		t.Errorf("hide %v", got)
	}
}

// TestSecretEnvLaunch: a thread's launch puts its secret variables in
// the session's environment only: not in its arguments or the log, and
// Codex is told to pass them; a coordinator gets none.
func TestSecretEnvLaunch(t *testing.T) {
	p := testPaths(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	newWatchProject(t)
	writeConfig(t, "[projects.demo.env]\nNPM_TOKEN = { from = \"env:SRV_NPM\" }\n")
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	var logged lockedBuffer
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin", "SRV_NPM=s3cr3t", "OTHER_TOKEN=x"}},
		log: log.New(&logged, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	wt := t.TempDir()
	rec := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "codex", Project: "demo", Thread: "t-0001", Cwd: wt}
	pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(pl.abandon)
	if !slices.Contains(pl.env, "NPM_TOKEN=s3cr3t") {
		t.Errorf("env lacks the secret: %q", pl.env)
	}
	argv := strings.Join(pl.argv, " ")
	if strings.Contains(argv, "s3cr3t") || !strings.Contains(argv, `shell_environment_policy.ignore_default_excludes=true`) ||
		!strings.Contains(argv, `shell_environment_policy.exclude=["OTHER_TOKEN"]`) {
		t.Errorf("argv %s", argv)
	}
	files, _ := os.ReadDir(pl.rt)
	for _, f := range files {
		if b, _ := os.ReadFile(filepath.Join(pl.rt, f.Name())); strings.Contains(string(b), "s3cr3t") {
			t.Errorf("%s holds the secret", f.Name())
		}
	}
	if strings.Contains(logged.String(), "s3cr3t") {
		t.Errorf("logged: %s", logged.String())
	}
	coord := SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Agent: "claude", Project: "demo", Cwd: t.TempDir()}
	pc, perr := s.prepareLaunch(agentLaunch{rec: coord, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(pc.abandon)
	if slices.ContainsFunc(pc.env, func(kv string) bool { return strings.HasPrefix(kv, "NPM_TOKEN=") }) {
		t.Errorf("a coordinator got the thread's secret")
	}
}

// TestSecretTest: secret.test gets the variable from its source in the
// server's environment and answers ok, or why not, never the value; a
// project's table, or all projects' for "", names it.
func TestSecretTest(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }

[projects.demo.env]
FROM_ENV = { from = "env:SRV_SECRET" }
MISSING = { from = "env:NOT_SET" }
`)
	s := &Server{opts: Options{Env: []string{"SRV_SECRET=s3cr3t"}}}
	fakeSecrets(t, "gho_abc\n", nil)
	for _, c := range []struct {
		p    proto.SecretTestParams
		ok   bool
		want string
	}{
		{proto.SecretTestParams{Name: "NODE_AUTH_TOKEN"}, true, ""},
		{proto.SecretTestParams{Project: "demo", Name: "FROM_ENV"}, true, ""},
		{proto.SecretTestParams{Project: "demo", Name: "MISSING"}, false, "the server's environment has no NOT_SET"},
	} {
		res, perr := s.secretTest(c.p)
		r, _ := res.(proto.SecretTestResult)
		if perr != nil || r.OK != c.ok || !strings.Contains(r.Error, c.want) {
			t.Errorf("%+v: %+v %v", c.p, r, perr)
		}
	}
	// Only the tables' own: demo's test doesn't reach all projects'.
	if _, perr := s.secretTest(proto.SecretTestParams{Project: "demo", Name: "NODE_AUTH_TOKEN"}); perr == nil {
		t.Error("demo tested all projects' variable")
	}
	fakeSecrets(t, "token-in-output", errors.New("exit status 1"))
	res, _ := s.secretTest(proto.SecretTestParams{Name: "NODE_AUTH_TOKEN"})
	if r := res.(proto.SecretTestResult); r.OK || r.Error != "gh auth token failed (exit status 1)" {
		t.Errorf("%+v", r)
	}
}
