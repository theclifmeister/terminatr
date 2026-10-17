package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// npmAuth: a line of an .npmrc giving a registry's credential from an
// environment variable (//npm.example.com/:_authToken=${NPM_TOKEN}).
var npmAuth = regexp.MustCompile(`^\s*//([^\s:/]+(?::\d+)?)[^\s]*:(_authToken|_auth|_password)\s*=\s*\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// npmNeed is a variable an .npmrc reads for a registry's credential.
type npmNeed struct{ name, host string }

// npmrcNeeds are the variables the .npmrc at path reads for registries'
// credentials; none when it has none or can't be read.
func npmrcNeeds(path string) []npmNeed {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []npmNeed
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := npmAuth.FindStringSubmatch(sc.Text()); m != nil {
			out = append(out, npmNeed{name: m[3], host: m[1]})
		}
	}
	return out
}

// SecretEnv warns about a project repository whose .npmrc reads a
// registry's token from a variable the project's threads don't get from
// tm's secret environment (config env): its threads' npm can't log in
// to the private registry unless the server's own environment has it.
func SecretEnv(d Deps) []Check {
	c, err := config.Load()
	if err != nil {
		return nil
	}
	list, _ := project.List()
	var out []Check
	for _, sum := range list {
		env, err := c.SecretEnv(sum.Slug)
		if err != nil {
			continue
		}
		for _, repo := range sum.Repos {
			var missing []string
			for _, n := range npmrcNeeds(filepath.Join(repo, ".npmrc")) {
				src, have := env[n.name]
				switch {
				case !have:
					if !slices.Contains(missing, n.name) {
						missing = append(missing, n.name)
						out = append(out, Check{Group: "settings", Name: "secret environment", Status: Warn,
							Detail: fmt.Sprintf("%s: %s/.npmrc reads %s for %s, which tm doesn't give the project's threads; add it to the project's secret environment (env in its settings), %s",
								sum.Slug, repo, n.name, n.host, envAdvice(n))})
					}
				case src.From == config.EnvFromGH && !isGitHubPackages(n.host):
					out = append(out, Check{Group: "settings", Name: "secret environment", Status: Warn,
						Detail: fmt.Sprintf("%s: %s/.npmrc sends %s, which is your GitHub token (gh-auth-token), to %s, which isn't GitHub's package registry; remove it from the secret environment unless you trust %s with your GitHub token",
							sum.Slug, repo, n.name, n.host, n.host)})
				}
			}
		}
	}
	return out
}

// isGitHubPackages: host is GitHub's npm package registry, the one host
// a GitHub token belongs to.
func isGitHubPackages(host string) bool {
	return strings.EqualFold(host, "npm.pkg.github.com")
}

// envAdvice is how to give the variable an .npmrc names for host: the
// GitHub token only for GitHub's registry, else a credential for that
// host, which the .npmrc (repository content) names and so isn't trusted.
func envAdvice(n npmNeed) string {
	if isGitHubPackages(n.host) {
		return fmt.Sprintf("e.g. %s = { from = \"gh-auth-token\" }", n.name)
	}
	return fmt.Sprintf("with a token for %s only if you trust that host, e.g. %s = { from = \"keychain:<item>\" } or { from = \"env:<NAME>\" }; not your GitHub token", n.host, n.name)
}
