//go:build unix

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestHandFiles: tm thread prompt --file stores a file in uploads/ by an
// upload's rules, opens an agent's only where it may name files, and leaves
// nothing behind when one of several is refused.
func TestHandFiles(t *testing.T) {
	h := newHarness(t)
	tmp := t.TempDir()
	prev := tempRoots
	tempRoots = func(*Env) ([]string, []string, bool) { return []string{tmp}, nil, false }
	t.Cleanup(func() { tempRoots = prev })
	h.ok(human, "project", "new", "demo")
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	write := func(dir, name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	stored := func() []string {
		ents, _ := os.ReadDir(p.Path("uploads"))
		var out []string
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out
	}
	e := &Env{Caller: caller.Caller{Kind: caller.Coordinator, Project: "demo"}, Cwd: tmp}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	good := write(tmp, "my notes.md", []byte("# Notes\n"))
	got, err := e.handFiles(p, []string{good, "my notes.md"}, now)
	if err != nil || len(got) != 2 {
		t.Fatalf("handFiles: %+v, %v", got, err)
	}
	if got[0].ref != "20261106-120000-my-notes.md" || got[1].ref != "20261106-120000-2-my-notes.md" {
		t.Errorf("refs: %+v", got)
	}
	if data, err := os.ReadFile(got[0].path); err != nil || string(data) != "# Notes\n" {
		t.Errorf("stored file: %q, %v", data, err)
	}
	if fi, err := os.Stat(got[0].path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("stored file's mode: %v, %v", fi, err)
	}
	got.remove()
	if s := stored(); len(s) != 0 {
		t.Fatalf("remove left %v", s)
	}

	code := func(err error) string {
		var te *tasks.Error
		if errors.As(err, &te) {
			return te.Code
		}
		return ""
	}
	secret := write(t.TempDir(), "secret.md", []byte("KEY"))
	if err := os.Symlink(secret, filepath.Join(tmp, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "dir.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, spec, code, msg string }{
		{"outside the roots", secret, "outside-roots", ""},
		{"a link out of them", "link.md", "", "escapes"},
		{"a folder", "dir.md", "invalid-file", "not a regular file"},
		{"a refused type", write(tmp, "run.sh", []byte("echo hi\n")), "invalid-file", "tm takes images"},
		{"no extension", write(tmp, "notes", []byte("x")), "invalid-file", "tm takes images"},
		{"empty", write(tmp, "empty.md", nil), "invalid-file", "1 byte to 25 MB"},
		{"too big", write(tmp, "big.txt", make([]byte, proto.MaxUpload+1)), "invalid-file", "1 byte to 25 MB"},
		{"bytes not of the name's type", write(tmp, "fake.png", []byte("not an image")), "invalid-file", "isn't a image/png"},
		{"binary as text", write(tmp, "bin.md", []byte{0, 1, 2}), "invalid-file", "isn't a text/markdown"},
	} {
		// After a good file: all or none.
		_, err := e.handFiles(p, []string{good, c.spec}, now)
		if code(err) != c.code || !strings.Contains(errString(err), c.msg) {
			t.Errorf("%s: %v, want %s %q", c.name, err, c.code, c.msg)
		}
		if s := stored(); len(s) != 0 {
			t.Errorf("%s left %v", c.name, s)
		}
	}

	// The human's open anywhere, under the same rules.
	hu := &Env{Caller: caller.Caller{Kind: caller.Human}, Cwd: tmp}
	got, err = hu.handFiles(p, []string{secret}, now)
	if err != nil || len(got) != 1 {
		t.Fatalf("the human's file: %+v, %v", got, err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
