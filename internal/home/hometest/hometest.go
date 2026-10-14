// Package hometest gives a test binary a throwaway state directory, for
// tests that reach home.Dir without caring where it is (home.Dir panics
// under go test rather than resolve the user's real one).
package hometest

import (
	"fmt"
	"os"

	"github.com/theclifmeister/terminatr/internal/home"
)

// Run is for a package's TestMain: with TERMINATR_HOME unset it points
// it at a fresh temp dir for the whole run, then calls run (m.Run, or
// whatever wraps it) and removes the dir. Tests that care still set
// their own with t.Setenv.
func Run(run func() int) int {
	if os.Getenv(home.Env) != "" {
		return run()
	}
	dir, err := os.MkdirTemp("", "tm-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	os.Setenv(home.Env, dir)
	defer os.Unsetenv(home.Env)
	return run()
}
