package server

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/shell"
)

// The secret environment of threads (docs/SPEC.md §8.6, Guard; config
// env.go): each variable the human names gets its value at the thread's
// start, from gh, the login keychain or the server's own environment, and
// goes into the session's environment only: never into its arguments, a
// prompt, a report, the log or the journal. The guard refuses printing
// it, so the thread's tools use it and the agent doesn't see it.

// secretEnvFor bounds getting one value.
const secretEnvFor = 10 * time.Second

// secretRunner runs a program for a value: its output. A variable for
// tests.
var secretRunner = func(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := shell.CLICommand(ctx, name, args...)
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}

// secretEnv is the secret environment of project slug's threads, by
// name, each value got from its source with base the server's
// environment; a value it can't get is left out and said in a line of
// fails (the name and why, never a value or a program's output).
func secretEnv(cfg *config.Config, slug string, base []string) (map[string]string, []string) {
	env, err := cfg.SecretEnv(slug)
	if err != nil {
		return nil, []string{err.Error()}
	}
	out := map[string]string{}
	var fails []string
	for _, name := range slices.Sorted(maps.Keys(env)) {
		v, err := secretValue(env[name], base)
		if err != nil {
			fails = append(fails, fmt.Sprintf("secret %s: %v", name, err))
			continue
		}
		out[name] = v
	}
	return out, fails
}

// secretValue is one source's value.
func secretValue(e config.EnvSource, base []string) (string, error) {
	kind, ref := e.Kind()
	run := func(what, name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), secretEnvFor)
		defer cancel()
		v, err := secretRunner(ctx, base, name, args...)
		v = strings.TrimSpace(v)
		switch {
		case err != nil:
			return "", fmt.Errorf("%s failed (%s)", what, firstErrLine(err))
		case v == "":
			return "", fmt.Errorf("%s gave nothing", what)
		}
		return v, nil
	}
	switch kind {
	case config.EnvFromGH:
		return run("gh auth token", "gh", "auth", "token")
	case "keychain":
		if !caps.Darwin {
			return "", fmt.Errorf("the keychain item %q: only macOS has the login keychain", ref)
		}
		return run(fmt.Sprintf("the keychain item %q", ref), "security", "find-generic-password", "-s", ref, "-w")
	case "env":
		for _, kv := range base {
			if k, v, ok := strings.Cut(kv, "="); ok && k == ref && v != "" {
				return v, nil
			}
		}
		return "", fmt.Errorf("the server's environment has no %s", ref)
	}
	return "", fmt.Errorf("unknown source %q", e.From)
}

// firstErrLine is err's first line: an exit status, never output.
func firstErrLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}

// looksSecret: a variable name an agent's own filter takes for a secret
// (Codex drops those holding KEY, SECRET or TOKEN from its commands'
// environment).
var looksSecret = regexp.MustCompile(`(?i)KEY|SECRET|TOKEN`)

// envHide are the names in env (KEY=value) that look secret but aren't
// among keep: what an agent filtering by name keeps dropping once it is
// told to pass the secret environment (LaunchSpec.EnvHide).
func envHide(env []string, keep []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k != "" && looksSecret.MatchString(k) && !slices.Contains(keep, k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
