//go:build unix

package filetree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
		".envrc": {false, true}, ".dev.vars": {false, true}, ".secrets": {false, true}, "secrets": {false, true}, "SECRETS": {false, true},
		"prod.tfvars": {false, true}, "prod.tfvars.json": {false, true}, ".pgpass": {false, true}, ".docker": {false, true}, ".htpasswd": {false, true},
		"key.p8": {false, true}, "key.ppk": {false, true}, ".vault-token": {false, true}, ".boto": {false, true}, ".s3cfg": {false, true},
		"_netrc": {false, true}, ".mcp.json": {false, true}, "settings.local.json": {false, true}, "service-account.json": {false, true},
		"application_default_credentials.json": {false, true}, "token.json": {false, true}, "auth.json": {false, true}, ".azure": {false, true},
		".gcloud": {false, true}, "id_rsa.pub": {false, true}, "id_rsa.old": {false, true}, "id_rsa.bak": {false, true}, "id_dsa.old": {false, true},
		"id_ecdsa_sk": {false, true}, "id_ecdsa-sk": {false, true}, "id_ed25519_sk": {false, true}, "ID_ED25519_SK.old": {false, true},
		"id_generator.go": {false, false}, "id_work": {false, false}, "prod.env": {false, true}, "Prod.ENV.": {false, true},
		"settings.json": {false, false}, "authors.json": {false, false}, "secretary.go": {false, false}, "idle.go": {false, false}, "my.env.example": {false, false},
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
// reads nothing, nor a hard link to one.
func TestFarReadSwapped(t *testing.T) {
	if _, ok := handlePathWorks(t); !ok {
		t.Skip("this system has none")
	}
	s := far(t)
	link := filepath.Join(s.Root, "swap")
	write(t, s.Root, "swap", "plain\n")
	s.beforeOpen = func() { os.Remove(link); os.Symlink(".env", link) }
	if res, err := s.Read(context.Background(), "swap", 0, false); !errors.Is(err, ErrGone) {
		t.Errorf("through a swapped link: %q %v", res.Data, err)
	}
	os.Remove(link)
	write(t, s.Root, "swap", "plain\n")
	s.beforeOpen = func() { os.Remove(link); os.Link(filepath.Join(s.Root, ".env"), link) }
	if res, err := s.Read(context.Background(), "swap", 0, false); !errors.Is(err, ErrWithheld) {
		t.Errorf("through a swapped hard link: %q %v", res.Data, err)
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

// TestFarHardLink: a file with another name, in the root or out of it, says
// its name alone and opens for nothing; a near scope reads on.
func TestFarHardLink(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	if err := os.Link(filepath.Join(s.Root, ".env"), filepath.Join(s.Root, "plain.txt")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(s.Root), "secret.txt")
	if err := os.Link(outside, filepath.Join(s.Root, "outside.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain.txt", filepath.Join(s.Root, "tolinked")); err != nil {
		t.Fatal(err)
	}
	cs := marksFor(t, s)
	for _, p := range []string{"plain.txt", "outside.txt", "tolinked"} {
		if res, err := s.Read(ctx, p, 0, false); !errors.Is(err, ErrWithheld) || res.Data != nil {
			t.Errorf("read %s: %q %v", p, res.Data, err)
		}
		if res, err := s.Diff(ctx, p, false, cs); !errors.Is(err, ErrWithheld) {
			t.Errorf("diff %s: %+v %v", p, res, err)
		}
	}
	res, err := s.List(ctx, "", Options{}, cs)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range res.Entries {
		switch e.Name {
		case "plain.txt", "outside.txt", "tolinked":
			seen++
			if !e.Withheld || e.Size != 0 || !e.MTime.IsZero() {
				t.Errorf("%+v, want its name alone", e)
			}
		case "README.md":
			if e.Withheld {
				t.Errorf("%+v", e)
			}
		}
	}
	if seen != 3 {
		t.Errorf("listed %d of them: %s", seen, names(res.Entries))
	}
	was := s.stamp("", []string{""}, s.farIgnored(ctx))
	if err := os.WriteFile(outside, []byte("the outside, and more\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.stamp("", []string{""}, s.farIgnored(ctx)) != was {
		t.Error("the stamp moved for what a file with another name holds")
	}
	near, err := Open(ctx, s.Root, s.Root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := near.Read(ctx, "plain.txt", 0, false); err != nil || len(res.Data) == 0 {
		t.Errorf("near: %q %v", res.Data, err)
	}
}

// aKey is a private key's block as it stands in a file, not as this one says it.
func aKey(end string) string {
	return "-----BEGIN OPENSSH " + "PRIVATE KEY-----" + end + "b3BlbnNzaC1rZXktdjEAAAAABG5vbmU" + end + "-----END OPENSSH " + "PRIVATE KEY-----" + end
}

// TestFarReadKey: a private key's block under a plain name doesn't cross from
// any offset, nor as lines a diff removed; what only names the mark does, and
// a near scope reads on.
func TestFarReadKey(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	for name, text := range map[string]string{
		"plain.txt":  "intro\n" + aKey("\n"),
		"crlf.txt":   aKey("\r\n"),
		"gcp.txt":    `{"key": "` + aKey(`\n`) + `"}`,
		"late.txt":   strings.Repeat("a line\n", 40000) + aKey("\n"),
		"pgp.txt":    "-----BEGIN PGP " + "PRIVATE KEY BLOCK-----\n\nlQOYBF0aaaaaaaaaaaaaaaaaaaaaa\n",
		"locked.txt": "-----BEGIN RSA " + "PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\n",
	} {
		write(t, s.Root, name, text)
		for _, at := range []int64{0, 30, int64(len(text)) - 5} {
			if res, err := s.Read(ctx, name, at, false); !errors.Is(err, ErrWithheld) || res.Data != nil {
				t.Errorf("%s at %d: %q %v", name, at, res.Data, err)
			}
		}
	}
	write(t, s.Root, "names.go", `const mark = "-----BEGIN `+`PRIVATE KEY-----"`+"\nfunc f() {}\n")
	if res, err := s.Read(ctx, "names.go", 0, false); err != nil || len(res.Data) == 0 {
		t.Errorf("a program naming the mark: %q %v", res.Data, err)
	}
	near, err := Open(ctx, s.Root, s.Root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := near.Read(ctx, "plain.txt", 0, false); err != nil || len(res.Data) == 0 {
		t.Errorf("near: %q %v", res.Data, err)
	}

	write(t, s.Root, "was.txt", "keep\n"+aKey("\n"))
	gittest.Run(t, s.Root, "add", "was.txt")
	gittest.Run(t, s.Root, "commit", "-q", "-m", "x")
	write(t, s.Root, "was.txt", "keep\n")
	cs := marksFor(t, s)
	if res, err := s.Diff(ctx, "was.txt", false, cs); !errors.Is(err, ErrWithheld) || res.Hunks != nil {
		t.Errorf("a removed key: %+v %v", res, err)
	}
	if res, err := near.Diff(ctx, "was.txt", false, cs); err != nil || len(res.Hunks) == 0 {
		t.Errorf("near: %+v %v", res, err)
	}
}

// TestFarSpellings: a path answers only as the file system spells it. Another
// spelling lists nothing and moves no stamp, also where the system can't say
// from the open file; a link in another spelling says its name alone.
func TestFarSpellings(t *testing.T) {
	for _, withHandle := range []bool{true, false} {
		s := far(t)
		ctx := context.Background()
		if _, err := os.Lstat(filepath.Join(s.Root, "SRC")); err != nil {
			t.Skip("this file system tells case")
		}
		if !withHandle {
			was := handlePath
			handlePath = func(*os.File) (string, bool) { return "", false }
			t.Cleanup(func() { handlePath = was })
		}
		// Git needn't fold as the file system does.
		gittest.Run(t, s.Root, "config", "core.ignorecase", "false")
		write(t, s.Root, "src/kept.log", "SECRET\n")
		write(t, s.Root, "folded/kept.log", "SECRET\n")
		cs := marksFor(t, s)
		for _, p := range []string{"SRC", "\u017frc", "BUILD", "Folded"} {
			if res, err := s.List(ctx, p, Options{}, cs); !errors.Is(err, ErrGone) && !errors.Is(err, ErrIgnored) {
				t.Errorf("list %q: %s %v", p, names(res.Entries), err)
			}
			was := s.stamp("", []string{p}, s.farIgnored(ctx))
			write(t, s.Root, strings.ToLower(strings.ReplaceAll(p, "\u017f", "s"))+"/moved.txt", "more\n")
			if s.stamp("", []string{p}, s.farIgnored(ctx)) != was {
				t.Errorf("the stamp of %q moved", p)
			}
		}
		res, err := s.List(ctx, "src", Options{}, marksFor(t, s))
		if err != nil || !strings.Contains(names(res.Entries), "main.go") || strings.Contains(names(res.Entries), "kept.log") {
			t.Errorf("as it is spelled: %s %v", names(res.Entries), err)
		}
		// Whether it's there or not, another spelling of an ignored file
		// answers the same.
		_, there := s.Read(ctx, "X.LOG", 0, false)
		_, absent := s.Read(ctx, "ABSENT.LOG", 0, false)
		for _, want := range []error{ErrGone, ErrIgnored} {
			if errors.Is(there, want) != errors.Is(absent, want) {
				t.Errorf("there: %v, absent: %v", there, absent)
			}
		}
		if err := os.Symlink("BUILD/out.txt", filepath.Join(s.Root, "tospelled")); err != nil {
			t.Fatal(err)
		}
		if res, err = s.List(ctx, "", Options{}, marksFor(t, s)); err != nil {
			t.Fatal(err)
		}
		for _, e := range res.Entries {
			if e.Name == "tospelled" && (!e.Withheld || e.Size != 0 || !e.MTime.IsZero()) {
				t.Errorf("a link in another spelling: %+v", e)
			}
		}
	}
}

// TestFarIgnoredSays: an ignored path answers the same whether it is there or
// not, read or listed, also through a link; what is tracked or isn't ignored
// answers as ever, and a link's target is still looked at.
func TestFarIgnoredSays(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	write(t, s.Root, ".gitignore", "build/\n*.log\nonlydir/\n")
	write(t, s.Root, "onlydir/x", "SECRET\n")
	write(t, s.Root, "src/there.log", "SECRET\n")
	write(t, s.Root, "old.log", "kept\n")
	gittest.Run(t, s.Root, "add", "-f", "old.log")
	if err := os.Symlink("src", filepath.Join(s.Root, "lnk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("absent-target", filepath.Join(s.Root, "dangling")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"build", "build/out.txt", "build/absent", "x.log", "absent.log", "x.log/under", "absent.log/under",
		"onlydir", "onlydir/x", "onlydir/absent", "lnk/there.log", "lnk/absent.log", "tolog"} {
		if res, err := s.Read(ctx, p, 0, false); !errors.Is(err, ErrIgnored) {
			t.Errorf("read %s: %q %v", p, res.Data, err)
		}
		if res, err := s.List(ctx, p, Options{}, nil); !errors.Is(err, ErrIgnored) {
			t.Errorf("list %s: %s %v", p, names(res.Entries), err)
		}
	}
	for _, p := range []string{"absent.txt", "src/absent.txt", "lnk/absent.txt", "dangling"} {
		if res, err := s.Read(ctx, p, 0, false); !errors.Is(err, ErrGone) {
			t.Errorf("read %s: %q %v", p, res.Data, err)
		}
	}
	for p, want := range map[string]string{"old.log": "kept\n", "lnk/main.go": "package main\n"} {
		if res, err := s.Read(ctx, p, 0, false); err != nil || string(res.Data) != want {
			t.Errorf("read %s: %q %v", p, res.Data, err)
		}
	}
	if res, err := s.Read(ctx, "nested/absent", 0, false); !errors.Is(err, ErrWithheld) {
		t.Errorf("in a nested checkout: %q %v", res.Data, err)
	}
	if res, err := s.Read(ctx, "src", 0, false); !errors.Is(err, ErrNotFile) {
		t.Errorf("a folder: %q %v", res.Data, err)
	}
}

// TestFarListOneGit: however many links a listing has, git is asked once for
// all of them, each still answers for what it leads to, and git that can't say
// holds them all back.
func TestFarListOneGit(t *testing.T) {
	s := far(t)
	ctx := context.Background()
	for _, name := range []string{"plain", ":colon", "with space", "star*", "é"} {
		write(t, s.Root, "data/"+name, "text\n")
	}
	write(t, s.Root, "data/hidden.log", "SECRET\n")
	dir := filepath.Join(s.Root, "many")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, to := range []string{"plain", ":colon", "with space", "star*", "é", "hidden.log"} {
		if err := os.Symlink("../data/"+to, filepath.Join(dir, "l"+string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 200 {
		if err := os.Symlink("../README.md", filepath.Join(dir, "r"+strings.Repeat("x", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("..", filepath.Join(dir, "totop")); err != nil {
		t.Fatal(err)
	}
	asked := 0
	s.gitRan = func(args []string) {
		if slices.Contains(args, "check-ignore") {
			asked++
		}
	}
	res, err := s.List(ctx, "many", Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if asked != 3 { // the folder's own two gates, and all its links
		t.Errorf("check-ignore ran %d times", asked)
	}
	if len(res.Entries) != 207 {
		t.Fatalf("%d entries", len(res.Entries))
	}
	for _, e := range res.Entries {
		if want := e.Name == "lf"; e.Withheld != want {
			t.Errorf("%+v", e)
		}
	}
	s.tree.Dir = filepath.Join(s.Root, "absent")
	s.holdBack(ctx, &res)
	for _, e := range res.Entries {
		if !e.Withheld && e.Name != "totop" {
			t.Errorf("git can't say: %+v", e)
		}
	}
}
