// Package hometest gives a test binary a throwaway state directory, for
// tests that reach home.Dir without caring where it is (home.Dir panics
// under go test rather than resolve the user's real one).
package hometest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
)

// Run is for a package's TestMain: with TERMINATR_HOME unset, or set to
// the user's real ~/.terminatr (as every tm session's children inherit
// it, T472), it points it at a fresh temp dir for the whole run, then
// calls run (m.Run, or whatever wraps it) and removes the dir. Tests
// that care still set their own with t.Setenv.
func Run(run func() int) int {
	if d := os.Getenv(home.Env); d != "" && !home.IsReal(d) {
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

// CleanThreadTemps removes, when t ends, the own temp folders
// (home.ThreadTempDir) of the threads of the home t runs with: they are
// outside it, in the shared /tmp, so its removal leaves them behind.
// Call it after setting TERMINATR_HOME.
func CleanThreadTemps(t testing.TB) {
	t.Cleanup(func() {
		if d, err := home.Dir(); err == nil {
			RemoveThreadTemps(d)
		}
	})
}

// RemoveThreadTemps removes the own temp folders of the threads of the
// state directory dir.
func RemoveThreadTemps(dir string) {
	threads, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "threads", "*"))
	for _, th := range threads {
		slug := filepath.Base(filepath.Dir(filepath.Dir(th)))
		if tmp, err := home.ThreadTempDirOf(dir, slug, filepath.Base(th)); err == nil {
			os.RemoveAll(tmp)
		}
	}
}
