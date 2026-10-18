package home

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
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

// TestDirPanicsOnRealHomeFromEnv is the guard for T472: a tm session
// hands its children TERMINATR_HOME=~/.terminatr, so a thread's go test
// inherits it; resolving it from a test must fail, not write the live
// config (a tui test turned the user's share setting off).
func TestDirPanicsOnRealHomeFromEnv(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		t.Skip("no account home")
	}
	t.Setenv(Env, filepath.Join(u.HomeDir, ".terminatr"))
	defer func() {
		r, _ := recover().(string)
		if !strings.Contains(r, "real state directory") {
			t.Fatalf("Dir did not panic on TERMINATR_HOME=~/.terminatr, recovered %q", r)
		}
	}()
	Dir()
}

// TestThreadTempDir (T587): one folder per home, project and thread, in
// ThreadTempRoot, short enough for Claude's sandbox to keep its TMPDIR
// there (44 bytes with /claude-<uid>); only plain names.
func TestThreadTempDir(t *testing.T) {
	t.Setenv(Env, t.TempDir())
	a, err := ThreadTempDir("demo", "t-0001")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(a) != ThreadTempRoot() {
		t.Errorf("%s is not in %s", a, ThreadTempRoot())
	}
	if again, _ := ThreadTempDir("demo", "t-0001"); again != a {
		t.Errorf("not stable: %s, %s", a, again)
	}
	for _, o := range [][2]string{{"demo", "t-0002"}, {"other", "t-0001"}} {
		if b, _ := ThreadTempDir(o[0], o[1]); b == a {
			t.Errorf("%v shares %s", o, a)
		}
	}
	t.Setenv(Env, t.TempDir())
	if b, _ := ThreadTempDir("demo", "t-0001"); b == a {
		t.Errorf("another home shares %s", a)
	}
	if runtime.GOOS != "windows" {
		if n := len(filepath.Join(a, "claude-"+strconv.Itoa(os.Getuid()))); n > 44 {
			t.Errorf("%s/claude-<uid> is %d bytes, over Claude's 44", a, n)
		}
	}
	for _, bad := range [][2]string{{"", "t-1"}, {"demo", ""}, {"..", "t-1"}, {"demo", "../x"}, {"a/b", "t-1"}, {"demo", "."}} {
		if _, err := ThreadTempDir(bad[0], bad[1]); err == nil {
			t.Errorf("ThreadTempDir(%q, %q) took it", bad[0], bad[1])
		}
	}
}
