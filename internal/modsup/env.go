package modsup

import "github.com/theclifmeister/terminatr/internal/plat/proc"

// mallocEnv are glibc's malloc settings a host gets from the server's
// environment when the user set one.
var mallocEnv = []string{"GLIBC_TUNABLES", "MALLOC_ARENA_MAX"}

// hostEnv is a host's environment: proc.BareEnv, and on Linux one glibc
// malloc arena (T360). glibc gives each thread that mallocs (every cgo
// thread start) an arena of its own, each backed by a 2 MB transparent
// huge page: 8-12 MB of a host's 13-18 MB private memory (T349). One
// arena brings a host to 5-6 MB. It fixes the cause, works on every glibc
// since 2.10 (glibc.malloc.hugetlb=0 only keeps the arenas off huge pages
// and means different things across versions), and costs nothing: a host
// barely uses C malloc. A user's own GLIBC_TUNABLES or MALLOC_ARENA_MAX
// is passed on instead.
func hostEnv(goos string, getenv func(string) string) []string {
	env := proc.BareEnv()
	if goos != "linux" {
		return env
	}
	own := false
	for _, k := range mallocEnv {
		if v := getenv(k); v != "" {
			env, own = append(env, k+"="+v), true
		}
	}
	if !own {
		env = append(env, "MALLOC_ARENA_MAX=1")
	}
	return env
}
