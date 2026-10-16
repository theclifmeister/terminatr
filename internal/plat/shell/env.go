package shell

import (
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// SameEnvName reports whether two environment variable names are the same
// variable: equal, or on Windows equal with case folded (it names PATH
// "Path", and os.Environ returns it so).
func SameEnvName(a, b string) bool { return sameEnvName(caps.Windows, a, b) }

func sameEnvName(win bool, a, b string) bool {
	return a == b || win && strings.EqualFold(a, b)
}

// EnvValue is the value of key in env (KEY=VALUE entries), the last match
// winning, and whether there was one. Names match as SameEnvName says.
func EnvValue(env []string, key string) (string, bool) { return envValue(caps.Windows, env, key) }

func envValue(win bool, env []string, key string) (string, bool) {
	v, found := "", false
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && sameEnvName(win, k, key) {
			v, found = val, true
		}
	}
	return v, found
}

// SetEnvPath is env with the value of the PATH entry replaced by fn(old),
// keeping the entry's own spelling of the name ("Path" on Windows), and
// fn("") appended as "PATH=" when there is none. Entries of the name
// beyond the first are dropped.
func SetEnvPath(env []string, fn func(old string) string) []string {
	return setEnvPath(caps.Windows, env, fn)
}

func setEnvPath(win bool, env []string, fn func(string) string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	old, _ := envValue(win, env, "PATH")
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !sameEnvName(win, k, "PATH") {
			out = append(out, kv)
			continue
		}
		if !found {
			found = true
			out = append(out, k+"="+fn(old))
		}
	}
	if !found {
		out = append(out, "PATH="+fn(""))
	}
	return out
}
