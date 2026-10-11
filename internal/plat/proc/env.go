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
