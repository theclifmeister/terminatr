package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// The secret environment of threads (docs/SPEC.md §8.6, Guard): the
// variables tm puts into a thread's environment when it starts, each from
// a source the human names, so its tools (npm, a registry client) can use
// a token the agent never sees. [defaults.env] gives every project's
// threads one; [projects.<slug>.env] a project's, a name there replacing
// all projects' of the same name:
//
//	[projects.web.env]
//	NODE_AUTH_TOKEN = { from = "gh-auth-token" }
//	NPM_TOKEN = { from = "keychain:npm-registry" }
//	ARTIFACTORY_TOKEN = { from = "env:ARTIFACTORY_TOKEN" }
//
// The value never goes into a prompt, a report, a log or the journal, and
// the guard refuses printing the variable.

// EnvSource is where one variable's value comes from: From is
// EnvFromGH (the output of gh auth token), "keychain:<item>" (the
// password of the login keychain's generic item of that service, macOS)
// or "env:<NAME>" (the server's own environment).
type EnvSource struct {
	From string `toml:"from" json:"from"`
}

// EnvFromGH is the source of GitHub's token for the logged-in gh.
const EnvFromGH = "gh-auth-token"

// Kind splits the source into its kind (gh-auth-token, keychain, env)
// and what it names.
func (e EnvSource) Kind() (kind, name string) {
	if k, n, ok := strings.Cut(e.From, ":"); ok {
		return k, n
	}
	return e.From, ""
}

var (
	envName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	keychainRef = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,200}$`)
)

// reservedEnv are names no secret may take: tm's own and what a
// session's processes need to be what they are.
var reservedEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TMPDIR", "PWD", "IFS", "ENV", "BASH_ENV", "PS4"}

// CheckEnv checks one secret variable: its name, and its source.
func CheckEnv(name string, e EnvSource) error {
	up := strings.ToUpper(name)
	switch {
	case !envName.MatchString(name):
		return fmt.Errorf("%q isn't a variable name", name)
	case slices.Contains(reservedEnv, up), strings.HasPrefix(up, "TERMINATR"), strings.HasPrefix(up, "CLAUDE"), strings.HasPrefix(up, "CODEX"),
		strings.HasPrefix(up, "LD_"), strings.HasPrefix(up, "DYLD_"):
		return fmt.Errorf("%s is a variable tm, the agent or the system sets: it can't be a secret", name)
	}
	kind, ref := e.Kind()
	switch {
	case kind == EnvFromGH && ref == "" && e.From == EnvFromGH:
	case kind == "keychain" && keychainRef.MatchString(ref):
	case kind == "env" && envName.MatchString(ref):
	default:
		return fmt.Errorf("%s: from must be %q, \"keychain:<item>\" or \"env:<NAME>\", not %q", name, EnvFromGH, e.From)
	}
	return nil
}

// checkEnv checks a table's secret variables.
func checkEnv(env map[string]EnvSource, path, table string) error {
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if err := CheckEnv(name, env[name]); err != nil {
			return fmt.Errorf("%s: %s.env: %w", path, table, err)
		}
	}
	return nil
}

// SecretEnv is the secret environment of project slug's threads: all
// projects' ([defaults.env]) with the project's own over it, by name.
// None for a nil Config.
func (c *Config) SecretEnv(slug string) (map[string]EnvSource, error) {
	if c == nil {
		return nil, nil
	}
	if err := checkEnv(c.defaults.Env, c.Path, "defaults"); err != nil {
		return nil, err
	}
	out := maps.Clone(c.defaults.Env)
	if r, ok := c.projects[slug]; ok && r.Env != nil {
		if err := checkEnv(r.Env, c.Path, "projects."+slug); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]EnvSource{}
		}
		maps.Copy(out, r.Env)
	}
	return out, nil
}

// EnvOf is one table's secret environment, names and sources only: all
// projects' ([defaults.env]) for slug "", else the project's own, without
// all projects'. None for a nil Config.
func (c *Config) EnvOf(slug string) map[string]EnvSource {
	if c == nil {
		return nil
	}
	if slug == "" {
		return maps.Clone(c.defaults.Env)
	}
	return maps.Clone(c.projects[slug].Env)
}

// envTable is the env table of table, DefaultsTable or a project's
// ("projects.<slug>").
func envTable(table string) (string, error) {
	if table != DefaultsTable && !strings.HasPrefix(table, "projects.") {
		return "", fmt.Errorf("no secret environment in [%s]", table)
	}
	return table + ".env", nil
}

// SetEnv names a secret variable in table's env table (DefaultsTable
// or "projects.<slug>"), checked as Load checks it; one of the same name
// there is replaced. Only the name and its source are written: tm never
// writes a value.
func SetEnv(table, name string, e EnvSource) error {
	if err := CheckEnv(name, e); err != nil {
		return err
	}
	t, err := envTable(table)
	if err != nil {
		return err
	}
	return Set(t, name, e)
}

// UnsetEnv removes a secret variable from table's env table; ErrForm
// when the file names it in a form the line editor doesn't change.
func UnsetEnv(table, name string) error {
	t, err := envTable(table)
	if err != nil {
		return err
	}
	return Unset(t, name)
}

// SecretEnvNames are the names SecretEnv gives, sorted: what the guard
// keeps from being printed. Every project's and all projects' when slug
// is "".
func (c *Config) SecretEnvNames(slug string) []string {
	if c == nil {
		return nil
	}
	set := map[string]bool{}
	add := func(env map[string]EnvSource) {
		for n := range env {
			set[n] = true
		}
	}
	add(c.defaults.Env)
	if slug == "" {
		for _, r := range c.projects {
			add(r.Env)
		}
	} else {
		add(c.projects[slug].Env)
	}
	return slices.Sorted(maps.Keys(set))
}
