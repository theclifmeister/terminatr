package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
)

func TestTrustDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	a := load(t)
	tr, ok := a.(agent.Truster)
	if !ok {
		t.Fatal("the claude agent is no Truster")
	}
	home := t.TempDir()
	real := filepath.Join(t.TempDir(), "wt")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	real, _ = filepath.EvalSymlinks(real)
	cfg := filepath.Join(home, ".claude.json")

	// No config yet: it is created with the entry, private.
	if err := tr.TrustDir(home, link); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(cfg)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config %v %v", fi, err)
	}

	// Other keys, other projects and other fields of the entry stay.
	os.WriteFile(cfg, []byte(`{"numStartups": 12345678901234567890, "oauthAccount": {"x": 1},
  "projects": {"/other": {"hasTrustDialogAccepted": false, "allowedTools": ["a"]},
               "`+real+`": {"lastCost": 1.5}}}`), 0o600)
	os.Chmod(cfg, 0o640)
	if err := tr.TrustDir(home, link); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	d := json.NewDecoder(mustOpen(t, cfg))
	d.UseNumber()
	if err := d.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["numStartups"].(json.Number).String() != "12345678901234567890" || got["oauthAccount"] == nil {
		t.Errorf("top-level keys changed: %v", got)
	}
	projects := got["projects"].(map[string]any)
	other := projects["/other"].(map[string]any)
	if other["hasTrustDialogAccepted"] != false || other["allowedTools"] == nil {
		t.Errorf("other project changed: %v", other)
	}
	for _, p := range []string{link, real} {
		e, _ := projects[p].(map[string]any)
		if e["hasTrustDialogAccepted"] != true {
			t.Errorf("%s not trusted: %v", p, projects)
		}
	}
	if e := projects[real].(map[string]any); e["lastCost"] == nil {
		t.Errorf("entry fields lost: %v", e)
	}
	if fi, _ := os.Stat(cfg); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}

	// Already trusted: the file isn't rewritten.
	before, _ := os.Stat(cfg)
	os.Chtimes(cfg, before.ModTime().Add(-1e9), before.ModTime().Add(-1e9))
	before, _ = os.Stat(cfg)
	if err := tr.TrustDir(home, link); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(cfg); !after.ModTime().Equal(before.ModTime()) {
		t.Error("rewrote an already trusted config")
	}

	// A broken config is left alone.
	os.WriteFile(cfg, []byte("{broken"), 0o600)
	if err := tr.TrustDir(home, filepath.Join(home, "new")); err == nil {
		t.Error("no error on a broken config")
	}
	if b, _ := os.ReadFile(cfg); string(b) != "{broken" {
		t.Errorf("broken config rewritten: %q", b)
	}

	// CLAUDE_CONFIG_DIR moves the file; no lock is left behind.
	cd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cd)
	if err := tr.TrustDir(home, real); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cd, ".claude.json")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(cd, ".claude.json.lock")); err == nil {
		t.Error("lock left behind")
	}
}

func mustOpen(t *testing.T, p string) *os.File {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestProjectKeyForwardSlash(t *testing.T) {
	// Claude keys projects by forward-slash path; on Windows that is
	// ToSlash of the backslash path (a no-op elsewhere).
	if got, want := projectKey(filepath.Join("a", "b")), "a/b"; got != want {
		t.Fatalf("projectKey = %q, want %q", got, want)
	}
}

func TestEditProjectsKeepsText(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude.json")
	in := `{"cmd": "a && b <c>", "projects": {}}`
	os.WriteFile(cfg, []byte(in), 0o600)
	err := editProjects(home, func(p map[string]json.RawMessage, _ string) (bool, error) {
		p["/x"] = json.RawMessage(`{"n": "<&>"}`)
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "a && b <c>") || !strings.Contains(string(b), `"<&>"`) {
		t.Errorf("escaped: %s", b)
	}
	if bak, _ := os.ReadFile(cfg + ".tm-backup"); string(bak) != in {
		t.Errorf("backup %q", bak)
	}
}

func TestLockConfig(t *testing.T) {
	setup := func(t *testing.T) (path, lock string) {
		path = filepath.Join(t.TempDir(), ".claude.json")
		return path, path + ".lock"
	}
	t.Run("free", func(t *testing.T) {
		path, lock := setup(t)
		unlock := lockConfig(path)
		if !exists(lock) {
			t.Fatal("no lock taken")
		}
		unlock()
		if exists(lock) {
			t.Error("lock left behind")
		}
	})
	t.Run("held then released", func(t *testing.T) {
		path, lock := setup(t)
		os.Mkdir(lock, 0o700)
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.Remove(lock)
		}()
		start := time.Now()
		unlock := lockConfig(path)
		if d := time.Since(start); d < 150*time.Millisecond || d > lockWait {
			t.Errorf("waited %v", d)
		}
		if !exists(lock) {
			t.Error("lock not taken after the other writer let go")
		}
		unlock()
		if exists(lock) {
			t.Error("lock left behind")
		}
	})
	t.Run("held past lockWait", func(t *testing.T) {
		path, lock := setup(t)
		os.Mkdir(lock, 0o700)
		start := time.Now()
		unlock := lockConfig(path)
		if d := time.Since(start); d < lockWait {
			t.Errorf("gave up after %v", d)
		}
		// Not ours: unlocking must not remove the other writer's lock.
		unlock()
		if !exists(lock) {
			t.Error("removed a lock it doesn't hold")
		}
	})
	t.Run("stale", func(t *testing.T) {
		path, lock := setup(t)
		os.Mkdir(lock, 0o700)
		old := time.Now().Add(-time.Minute)
		os.Chtimes(lock, old, old)
		start := time.Now()
		unlock := lockConfig(path)
		if d := time.Since(start); d > time.Second {
			t.Errorf("waited %v on a stale lock", d)
		}
		unlock()
		if !exists(lock) {
			t.Error("removed a stale lock it doesn't hold")
		}
	})
	t.Run("can't be created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "no", "such", ".claude.json")
		start := time.Now()
		lockConfig(path)()
		if d := time.Since(start); d > time.Second {
			t.Errorf("waited %v", d)
		}
	})
}
