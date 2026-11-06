//go:build unix

package filetree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// far is checkout's repo opened for a paired server, with a tracked .env,
// .aws folder and renamed file, a nested checkout and links beside them.
func far(t *testing.T) *Scope {
	t.Helper()
	near, _ := checkout(t)
	repo := near.Root
	write(t, repo, ".gitignore", "build/\n*.log\n")
	write(t, repo, ".env", "TOKEN=SECRET\n")
	write(t, repo, ".aws/credentials", "SECRET\n")
	write(t, repo, ".aws/config", "SECRET\n")
	write(t, repo, "deleted.pem", "SECRET\n")
	write(t, repo, "old.key", strings.Repeat("SECRET line\n", 40))
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "secrets")
	gittest.Run(t, repo, "mv", "old.key", "renamed.txt")
	write(t, repo, "README.md", "one\nthree\n")
	os.Remove(filepath.Join(repo, "deleted.pem"))
	write(t, repo, "build/out.txt", "SECRET\n")
	write(t, repo, "x.log", "SECRET\n")
	write(t, repo, "nested/inner.txt", "SECRET\n")
	gittest.Run(t, repo, "init", "-q", filepath.Join(repo, "nested"))
	for link, to := range map[string]string{"notes.txt": ".env", "cfg": ".aws", "id_rsa": "README.md", "tolog": "x.log"} {
		if err := os.Symlink(to, filepath.Join(repo, link)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := OpenFar(context.Background(), repo, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestWithheldNames: the local rule, and the far list beside it, however
// the name is spelled.
func TestWithheldNames(t *testing.T) {
	for name, want := range map[string][2]bool{ // SecretName, withheldName
		".env": {true, true}, ".ENV": {true, true}, ".env.local": {true, true}, ".env.": {true, true}, ".env ": {false, true},
		".env.example": {false, false}, "id_rsa": {true, true}, "ID_RSA": {true, true}, "a.pem": {true, true}, "secrets.yml": {true, true},
		".ssh": {false, true}, ".AWS": {false, true}, ".gnupg": {false, true}, ".kube": {false, true},
		"terraform.tfstate": {false, true}, "terraform.tfstate.backup": {false, true},
		"main.go": {false, false}, "environment": {false, false}, "README.md": {false, false},
	} {
		if got := SecretName(name); got != want[0] {
			t.Errorf("SecretName(%q) = %v", name, got)
		}
		if got := withheldName(name); got != want[1] {
			t.Errorf("withheldName(%q) = %v", name, got)
		}
	}
}

// TestOpenFarChecksOnly: a plain folder doesn't open for a paired server.
func TestOpenFarChecksOnly(t *testing.T) {
	if _, err := OpenFar(context.Background(), t.TempDir(), "", ""); !errors.Is(err, ErrNoRoot) {
		t.Errorf("plain folder: %v", err)
	}
}

// TestFarRead: what a read refuses, by name and by where a link leads,
// whether or not the file is there, and however the caller asks.
func TestFarRead(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	for _, tc := range []struct {
		path string
		want error
	}{
		{".env", ErrWithheld},
		{".env.absent", ErrWithheld},
		{"notes.txt", ErrWithheld}, // a link to .env
		{"id_rsa", ErrWithheld},    // a link by that name, to a plain file
		{".aws/config", ErrWithheld},
		{"cfg/config", ErrWithheld}, // through a link to .aws
		{"nested/inner.txt", ErrWithheld},
		{"build/out.txt", ErrIgnored},
		{"x.log", ErrIgnored},
		{"tolog", ErrIgnored},
		{".git/config", ErrPath},
	} {
		for _, read := range []func(context.Context, string, int64, bool) (proto.FilesReadResult, error){s.Read, s.ReadBytes} {
			// A far scope can't ask for what's ignored.
			if res, err := read(ctx, tc.path, 0, true); !errors.Is(err, tc.want) {
				t.Errorf("read %s: %q %v, want %v", tc.path, res.Data, err, tc.want)
			}
		}
	}
	if res, err := s.Read(ctx, "README.md", 0, false); err != nil || !bytes.HasPrefix(res.Data, []byte("one")) {
		t.Errorf("a plain file: %q %v", res.Data, err)
	}
}

// TestFarReadSwapped: a link to a withheld file swapped in after the gate
// reads nothing.
func TestFarReadSwapped(t *testing.T) {
	if _, ok := handlePathWorks(t); !ok {
		t.Skip("this system has none")
	}
	s := far(t)
	link := filepath.Join(s.Root, "swap")
	write(t, s.Root, "swap", "plain\n")
	s.beforeOpen = func() { os.Remove(link); os.Symlink(".env", link) }
	if res, err := s.Read(context.Background(), "swap", 0, false); !errors.Is(err, ErrWithheld) {
		t.Errorf("through a swapped link: %q %v", res.Data, err)
	}
}

// TestFarReadText: text only and at most 1 MiB, also when the head looks
// like text; a near scope still reads on.
func TestFarReadText(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	write(t, s.Root, "bin.dat", "\x00\x01\x02SECRET")
	for _, read := range []func(context.Context, string, int64, bool) (proto.FilesReadResult, error){s.Read, s.ReadBytes} {
		if res, err := read(ctx, "bin.dat", 0, false); err != nil || !res.Binary || res.Data != nil {
			t.Errorf("binary: %+v %v", res, err)
		}
	}
	write(t, s.Root, "late.dat", strings.Repeat("a", sniff+10)+"\x00SECRET")
	if res, err := s.Read(ctx, "late.dat", 0, false); err != nil || !res.Binary || res.Data != nil {
		t.Errorf("binary past its head: %+v %v", res, err)
	}
	write(t, s.Root, "latin.dat", strings.Repeat("a", sniff+10)+"caf\xe9 SECRET")
	if res, err := s.Read(ctx, "latin.dat", 0, false); err != nil || !res.Binary || res.Data != nil {
		t.Errorf("not UTF-8 past its head: %+v %v", res, err)
	}
	// A chunk can start or stop inside a character, and is still text.
	write(t, s.Root, "cut.txt", "héllo wörld é")
	if res, err := s.Read(ctx, "cut.txt", 2, false); err != nil || res.Binary || len(res.Data) == 0 {
		t.Errorf("a chunk from inside a character: %+v %v", res, err)
	}
	write(t, s.Root, "big.txt", strings.Repeat("a", proto.PeerWorktreeMaxRead+10))
	res, err := s.Read(ctx, "big.txt", proto.PeerWorktreeMaxRead-5, false)
	if err != nil || !res.Truncated || len(res.Data) != 5 {
		t.Errorf("the last chunk: %d bytes, truncated %v, %v", len(res.Data), res.Truncated, err)
	}
	if res, err := s.Read(ctx, "big.txt", proto.PeerWorktreeMaxRead, false); err != nil || len(res.Data) != 0 {
		t.Errorf("past the most: %d bytes, %v", len(res.Data), err)
	}
	near, err := Open(ctx, s.Root, s.Root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := near.Read(ctx, "big.txt", proto.PeerWorktreeMaxRead, false); err != nil || len(res.Data) != 10 {
		t.Errorf("near, past a paired server's most: %d bytes, %v", len(res.Data), err)
	}
	if res, err := near.Read(ctx, ".env", 0, false); err != nil || len(res.Data) == 0 {
		t.Errorf("near, .env: %q %v", res.Data, err)
	}
}

// TestFarList: a listing shows a withheld name alone, ignored entries not at
// all, and no folder that's withheld opens; a search finds nothing inside
// one.
func TestFarList(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	cs := marksFor(t, s)
	res, err := s.List(ctx, "", Options{Ignored: true}, cs)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]proto.FileEntry{}
	for _, e := range res.Entries {
		got[e.Name] = e
	}
	for _, name := range []string{"build", "x.log"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s is ignored, and listed", name)
		}
	}
	for _, name := range []string{".env", ".aws", "nested", "notes.txt", "cfg", "id_rsa", "tolog", "deleted.pem"} {
		e, ok := got[name]
		if !ok || !e.Withheld || e.Size != 0 || !e.MTime.IsZero() {
			t.Errorf("%s: %+v, want withheld, its name alone", name, e)
		}
	}
	for _, name := range []string{"README.md", "src", ".gitignore"} {
		if e, ok := got[name]; !ok || e.Withheld {
			t.Errorf("%s: %+v", name, e)
		}
	}
	for _, p := range []string{".aws", "cfg", "nested", "build", ".env"} {
		if res, err := s.List(ctx, p, Options{}, cs); err == nil {
			t.Errorf("opened %s: %s", p, names(res.Entries))
		}
	}
	found, err := s.List(ctx, "", Options{Query: "config"}, cs)
	if err != nil {
		t.Fatal(err)
	}
	if n := names(found.Entries); strings.Contains(n, ".aws") {
		t.Errorf("a search looked in .aws: %s", n)
	}
	found, err = s.List(ctx, "", Options{Query: "nested", Ignored: true}, cs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range found.Entries {
		if !e.Withheld || strings.Contains(e.Path, "inner") || strings.HasSuffix(e.Path, "/") {
			t.Errorf("a search of nested: %+v", e)
		}
	}
	changed, err := s.List(ctx, "", Options{Changed: true}, cs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range changed.Entries {
		if e.Name == "deleted.pem" && !e.Withheld {
			t.Errorf("changed: %+v", e)
		}
	}
}

// TestFarDiff: a diff refuses what a read does, a file deleted too, and what
// a file was renamed from, whose old lines would cross. Hunks stop at their
// bound.
func TestFarDiff(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	cs := marksFor(t, s)
	for _, p := range []string{".env", "notes.txt", "deleted.pem", "renamed.txt", "old.key", "x.log", "nested/inner.txt"} {
		if res, err := s.Diff(ctx, p, true, cs); err == nil {
			t.Errorf("diff of %s: %+v", p, res)
		}
	}
	if _, err := s.Diff(ctx, "renamed.txt", false, cs); !errors.Is(err, ErrWithheld) {
		t.Errorf("renamed from old.key: %v", err)
	}
	if res, err := s.Diff(ctx, "README.md", false, cs); err != nil || res.Status != proto.FileModified {
		t.Errorf("a plain file: %+v %v", res, err)
	}

	big := strings.Repeat(strings.Repeat("x", 1000)+"\n", 900)
	write(t, s.Root, "big.txt", big)
	gittest.Run(t, s.Root, "add", "big.txt")
	gittest.Run(t, s.Root, "commit", "-q", "-m", "big")
	write(t, s.Root, "big.txt", "")
	cs = marksFor(t, s)
	res, err := s.Diff(ctx, "big.txt", false, cs)
	if err != nil || !res.Truncated {
		t.Fatalf("a big deletion: truncated %v, %v", res.Truncated, err)
	}
	wire := 0
	for _, h := range res.Hunks {
		for _, l := range h.Removed {
			wire += len(l)
		}
	}
	if wire > proto.PeerWorktreeMaxHunks {
		t.Errorf("%d bytes of hunks", wire)
	}
	near, err := Open(ctx, s.Root, s.Root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := near.Diff(ctx, "big.txt", false, cs); err != nil || len(res.Hunks) == 0 {
		t.Errorf("near: %v", err)
	}
}

// TestFarStamp: an ignored or withheld folder counts as one that doesn't
// answer, what's ignored in a folder doesn't move its stamp, and a withheld
// file counts by its name; a near scope sees them all, also from one Cache.
func TestFarStamp(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	near, err := Open(ctx, s.Root, s.Root, "")
	if err != nil {
		t.Fatal(err)
	}
	// Fingerprints aside: what's under test is what the folders add.
	digest := func(sc *Scope, folders ...string) string { return sc.stamp("", folders, s.farIgnored(ctx)) }
	touch := func(name string) { t.Helper(); write(t, s.Root, name, "SECRET and more\n") }

	for _, tc := range []struct{ folder, file string }{
		{"build", "build/new.txt"},
		{".aws", ".aws/new"},
		{"cfg", ".aws/new2"},
		{"nested", "nested/new.txt"},
	} {
		was := digest(s, tc.folder)
		touch(tc.file)
		if digest(s, tc.folder) != was {
			t.Errorf("the stamp of %s moved as %s changed", tc.folder, tc.file)
		}
	}
	was := digest(s, "")
	touch("y.log") // a new entry, ignored
	touch(".env")  // a withheld one's size
	if digest(s, "") != was {
		t.Error("the top's stamp moved for an ignored file or a withheld file's size")
	}
	touch("plain.txt")
	if digest(s, "") == was {
		t.Error("the top's stamp didn't move for a new file")
	}

	nearWas := near.stamp("", []string{"build"}, nil)
	touch("build/new2.txt")
	if near.stamp("", []string{"build"}, nil) == nearWas {
		t.Error("a near stamp no longer looks in an ignored folder")
	}

	// Git that can't say answers for no folder.
	if a, b := s.stamp("", []string{""}, nil), s.stamp("", []string{"src"}, nil); a == digest(s, "") || b == digest(s, "src") {
		t.Error("a far stamp without git's word still looked")
	}

	// One cache serves both, and a near stamp's nil isn't a far one's word.
	var c Cache
	c.Stamp(ctx, near, []string{""})
	f1 := c.Stamp(ctx, s, []string{""})
	touch("build/new3.txt")
	if c.Stamp(ctx, s, []string{""}) != f1 {
		t.Error("a cached far stamp moved for an ignored file")
	}
}
