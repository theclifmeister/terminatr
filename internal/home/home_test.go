package home

import (
	"os/user"
	"strings"
	"testing"
)

func TestDirHonorsEnv(t *testing.T) {
	d := t.TempDir()
	t.Setenv(Env, d)
	got, err := Dir()
	if err != nil || got != d {
		t.Fatalf("Dir = %q, %v; want %q", got, err, d)
	}
}

func TestDirUnderTempHomeIsAllowed(t *testing.T) {
	t.Setenv(Env, "")
	t.Setenv("HOME", t.TempDir())
	if u, err := user.Current(); err == nil && u.HomeDir == t.TempDir() {
		t.Skip("temp dir is the account home")
	}
	if _, err := Dir(); err != nil {
		t.Fatal(err)
	}
}

// TestDirPanicsOnRealHome is the guard for T391: a test that leaves
// TERMINATR_HOME unset with the real HOME must fail loudly instead of
// writing the user's live state.
func TestDirPanicsOnRealHome(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		t.Skip("no account home")
	}
	t.Setenv(Env, "")
	t.Setenv("HOME", u.HomeDir)
	defer func() {
		r, _ := recover().(string)
		if !strings.Contains(r, "real state directory") {
			t.Fatalf("Dir did not panic on the real home, recovered %q", r)
		}
	}()
	Dir()
}
