package mdfile

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in, front, body string
		err             bool
	}{
		{"# hi\n", "", "# hi\n", false},
		{"+++\na = 1\n+++\n# hi\n", "a = 1\n", "# hi\n", false},
		{"+++\n+++\n", "", "", false},
		{"+++\na = 1\n+++", "a = 1\n", "", false},
		{"+++\r\na = 1\r\n+++\r\nbody", "a = 1\r\n", "body", false},
		{"+++\na = 1\n", "", "", true},
		{"", "", "", false},
		{"---\na: 1\n---\n# hi\n", "a: 1\n", "# hi\n", false},
		{"---\r\na: 1\r\n---\r\nbody", "a: 1\r\n", "body", false},
		{"---\na: 1\n+++\nb\n---\nc", "a: 1\n+++\nb\n", "c", false},
		{"+++\na = 1\n---\nb\n", "", "", true},
		{"---\na: 1\n", "", "", true},
		{"----\na\n", "", "----\na\n", false},
	}
	for _, c := range cases {
		front, body, err := Split([]byte(c.in))
		if (err != nil) != c.err {
			t.Errorf("Split(%q) err = %v", c.in, err)
			continue
		}
		if string(front) != c.front || (!c.err && string(body) != c.body) {
			t.Errorf("Split(%q) = %q, %q; want %q, %q", c.in, front, body, c.front, c.body)
		}
	}
}

type fm struct {
	Name  string   `toml:"name" yaml:"name"`
	Repos []string `toml:"repos" yaml:"repos"`
}

func TestWriteRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "P.md")
	if err := Write(p, fm{Name: "demo", Repos: []string{"/a"}}, []byte("# Body\n")); err != nil {
		t.Fatal(err)
	}
	var got fm
	body, err := Read(p, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || len(got.Repos) != 1 || string(body) != "# Body\n" {
		t.Fatalf("got %+v %q", got, body)
	}
	raw, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(raw), "+++\nname = \"demo\"\n") {
		t.Fatalf("unexpected file:\n%s", raw)
	}
}

func TestUpdateConcurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.md")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(p, func(old []byte) ([]byte, error) {
				return append(old, 'x'), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	raw, _ := os.ReadFile(p)
	if len(raw) != 20 {
		t.Fatalf("lost updates: %q", raw)
	}
}

func TestUpdateUnchangedKeepsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.md")
	os.WriteFile(p, []byte("same"), 0o644)
	before, _ := os.Stat(p)
	if err := Update(p, func(old []byte) ([]byte, error) { return old, nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) {
		t.Fatal("unchanged update replaced the file")
	}
}

func TestUpdateUnchangedCreatesNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.md")
	if err := Update(p, func(old []byte) ([]byte, error) { return old, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("a no-op update created the file")
	}
}

// TestAppendRefusesLink: a log that is a link is refused, not followed:
// the file it points at is left as it was.
func TestAppendRefusesLink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep\n"), 0o600)
	log := filepath.Join(dir, "JOURNAL.md")
	if err := os.Symlink(victim, log); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := Append(log, []byte("line\n")); err == nil {
		t.Fatal("appended through a link")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Errorf("victim %q", b)
	}
}
