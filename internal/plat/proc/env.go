package proc

import (
	"os"
	"runtime"
)

// BareEnv is the environment of a process that must see none of this
// one's (a mod host: no tokens, mods design §7): empty, but for what the
// OS needs to start a program at all (SystemRoot on Windows).
func BareEnv() []string {
	env := []string{}
	if runtime.GOOS == "windows" {
		if r := os.Getenv("SystemRoot"); r != "" {
			env = append(env, "SystemRoot="+r)
		}
	}
	return env
}

// MallocEnv is what a BareEnv adds so a cgo child keeps one glibc malloc
// arena (T360): on Linux, MALLOC_ARENA_MAX=1, or the user's own
// GLIBC_TUNABLES and MALLOC_ARENA_MAX from getenv when either is set;
// elsewhere nothing. glibc gives each thread that mallocs (every cgo
// thread start) an arena of its own, each backed by a 2 MB transparent
// huge page: 8-12 MB of a mod host's 13-18 MB private memory (T349), 5-6
// MB with one arena. One arena fixes the cause and works on every glibc
// since 2.10 (glibc.malloc.hugetlb=0 only keeps the arenas off huge pages
// and means different things across versions); a mod host barely uses C
// malloc, so it costs nothing.
func MallocEnv(getenv func(string) string) []string { return mallocEnv(runtime.GOOS, getenv) }

func mallocEnv(goos string, getenv func(string) string) []string {
	if goos != "linux" {
		return nil
	}
	var env []string
	for _, k := range []string{"GLIBC_TUNABLES", "MALLOC_ARENA_MAX"} {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if env == nil {
		env = []string{"MALLOC_ARENA_MAX=1"}
	}
	return env
}
