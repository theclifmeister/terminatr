package hometest

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
)

// TestRunReplacesRealHome: a test binary started from a tm session
// inherits TERMINATR_HOME=~/.terminatr; Run must swap it for a temp dir,
// and keep one that is already somewhere else (T472).
func TestRunReplacesRealHome(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		t.Skip("no account home")
	}
	real := filepath.Join(u.HomeDir, ".terminatr")
	t.Setenv(home.Env, real)
	var got string
	Run(func() int { got = os.Getenv(home.Env); return 0 })
	if got == "" || got == real {
		t.Fatalf("Run kept %q", got)
	}
	other := t.TempDir()
	t.Setenv(home.Env, other)
	Run(func() int { got = os.Getenv(home.Env); return 0 })
	if got != other {
		t.Fatalf("Run moved a temp home: %q", got)
	}
}
