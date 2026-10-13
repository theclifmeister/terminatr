package modsup

import "github.com/theclifmeister/terminatr/internal/plat/proc"

// hostEnv is a host's environment: proc.BareEnv, and proc.MallocEnv's one
// glibc malloc arena (on Linux) or the user's own malloc settings from
// getenv (T360).
func hostEnv(getenv func(string) string) []string {
	return append(proc.BareEnv(), proc.MallocEnv(getenv)...)
}
