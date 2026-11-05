//go:build unix

package filetree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkout is a repo with a commit and uncommitted changes, ignored files,
// and a secret outside it, opened as a Scope.
func checkout(t *testing.T) (s *Scope, secret string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	secret = filepath.Join(root, "secret.txt")
	write(t, root, "secret.txt", "the outside\n")
	repo := filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "-q", "-b", "main", repo)
	write(t, repo, ".gitignore", "build/\n*.log\n.env\n")
	write(t, repo, "README.md", "one\ntwo\n")
	write(t, repo, "src/main.go", "package main\n")
	write(t, repo, "src/gone.go", "package gone\n")
	write(t, repo, "old/only.go", "package only\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	write(t, repo, "README.md", "one\ntwo\nthree\n")
	write(t, repo, "src/new.go", "package new\n")
	os.Remove(filepath.Join(repo, "src/gone.go"))
	os.RemoveAll(filepath.Join(repo, "old"))
	write(t, repo, "build/out.bin", "built\n")
	write(t, repo, "debug.log", "log\n")
	write(t, repo, ".env", "TOKEN=x\n")
	s, err := Open(context.Background(), repo, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, secret
}

func marksFor(t *testing.T, s *Scope) *changes.Changeset {
	t.Helper()
	var c Cache
	cs, err := c.Changes(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// TestPathRules: every way out of the folder or into .git, as a table:
// by the path itself and through a link, for a read and for a listing.
func TestPathRules(t *testing.T) {
	s, secret := checkout(t)
	root := s.Root
	outside := filepath.Dir(secret)
	for name, to := range map[string]string{
		"out-abs":     secret,
		"out-rel":     "../secret.txt",
		"out-dir":     outside,
		"out-dir-rel": "..",
		"out-chain":   "out-rel",
		"to-git":      ".git/config",
		"to-git-dir":  ".git",
		"to-git-up":   "src/../.git/HEAD",
		"dangling":    "nothing-here",
		"loop":        "loop",
		"in-link":     "README.md",
		"in-dir-link": "src",
		"to-ignored":  ".env",
	} {
		if err := os.Symlink(to, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(root, "src", ".git")
	os.WriteFile(gitFile, []byte("gitdir: x\n"), 0o644)

	for _, c := range []struct {
		path string
		want error // nil: reads
	}{
		{"README.md", nil},
		{"src/main.go", nil},
		{"in-link", nil},
		{"in-dir-link/main.go", nil},
		// By the path.
		{"..", ErrPath},
		{"../secret.txt", ErrPath},
		{"src/../../secret.txt", ErrPath},
		{"src/../README.md", ErrPath},
		{"./README.md", ErrPath},
		{"src//main.go", ErrPath},
		{"src/", ErrPath},
		{"/etc/passwd", ErrPath},
		{secret, ErrPath},
		{"", ErrNotFile},
		{".", ErrNotFile},
		{`..\secret.txt`, ErrPath},
		{`src\..\..\secret.txt`, ErrPath},
		{`.git\config`, ErrPath},
		{"a\x00b", ErrPath},
		{"a\x1b[2Jb", ErrPath},
		{"RE\u202eADME.md", ErrPath},
		{"\xff\xfe", ErrPath},
		{".git/config", ErrPath},
		{".GIT/config", ErrPath},
		{".Git/HEAD", ErrPath},
		{"src/.git", ErrPath},
		{"src/.git/x", ErrPath},
		{".git./config", ErrPath},
		{".git /config", ErrPath},
		{"git~1/config", ErrPath},
		// Through a link.
		{"out-abs", ErrLinkOut},
		{"out-rel", ErrLinkOut},
		{"out-chain", ErrLinkOut},
		{"out-dir/secret.txt", ErrLinkOut},
		{"out-dir-rel/secret.txt", ErrLinkOut},
		{"to-git", ErrLinkOut},
		{"to-git-dir/config", ErrLinkOut},
		{"to-git-up", ErrLinkOut},
		{"dangling", ErrGone},
		{"missing.txt", ErrGone},
		{"..%2f..%2fsecret.txt", ErrGone}, // a name, not decoded
		{"loop", ErrPath},
		// Not files.
		{"src", ErrNotFile},
		{"pipe", ErrNotFile},
		// Ignored, until asked.
		{".env", ErrIgnored},
		{"build/out.bin", ErrIgnored},
		{"debug.log", ErrIgnored},
		{"to-ignored", ErrIgnored},
	} {
		res, err := s.Read(context.Background(), c.path, 0, false)
		if c.want == nil && err != nil {
			t.Errorf("read %q: %v", c.path, err)
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("read %q = %v (%q), want %v", c.path, err, res.Data, c.want)
		}
		if strings.Contains(string(res.Data), "the outside") {
			t.Errorf("read %q reached the secret", c.path)
		}
	}
	for _, c := range []struct {
		path string
		want error
	}{
		{"", nil},
		{".", nil},
		{"src", nil},
		{"in-dir-link", nil},
		{"..", ErrPath},
		{"src/..", ErrPath},
		{"../repo", ErrPath},
		{outside, ErrPath},
		{".git", ErrPath},
		{".GIT/refs", ErrPath},
		{"out-dir", ErrLinkOut},
		{"out-dir-rel", ErrLinkOut},
		{"to-git-dir", ErrLinkOut},
		{"build", ErrIgnored},
		{"README.md", ErrNotFile},
		{"missing", ErrGone},
	} {
		_, err := s.List(context.Background(), c.path, Options{}, nil)
		if c.want == nil && err != nil {
			t.Errorf("list %q: %v", c.path, err)
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("list %q = %v, want %v", c.path, err, c.want)
		}
	}
	// Asked for, an ignored file reads, and a link out still doesn't.
	if res, err := s.Read(context.Background(), ".env", 0, true); err != nil || string(res.Data) != "TOKEN=x\n" {
		t.Errorf("read .env, asked: %q %v", res.Data, err)
	}
	if _, err := s.Read(context.Background(), "out-rel", 0, true); !errors.Is(err, ErrLinkOut) {
		t.Errorf("read out-rel, ignored asked: %v", err)
	}
	if res, err := s.List(context.Background(), "build", Options{Ignored: true}, nil); err != nil || len(res.Entries) != 1 {
		t.Errorf("list build, asked: %+v %v", res, err)
	}
}

// TestLinkSwapped: a file swapped for a link after it was resolved doesn't
// lead the open out of the root or into .git.
func TestLinkSwapped(t *testing.T) {
	s, secret := checkout(t)
	for _, to := range []string{secret, filepath.Join(s.Root, ".git", "config")} {
		p := filepath.Join(s.Root, "swap")
		os.Remove(p)
		if err := os.Symlink(to, p); err != nil {
			t.Fatal(err)
		}
		// "swap" stands for the path resolved a moment before.
		if f, _, err := s.open("swap"); !errors.Is(err, ErrLinkOut) {
			if err == nil {
				f.Close()
			}
			t.Errorf("open through a link to %s: %v", to, err)
		}
	}
}

// TestSwapAfterGate: a link swapped in after the ignored check reads
// nothing the check would have refused, with the kernel's answer and
// without one; asked for, the ignored file opens.
func TestSwapAfterGate(t *testing.T) {
	for _, handle := range []bool{true, false} {
		name := "HandlePath"
		if !handle {
			name = "no HandlePath"
		}
		t.Run(name, func(t *testing.T) {
			if !handle {
				was := handlePath
				handlePath = func(*os.File) (string, bool) { return "", false }
				t.Cleanup(func() { handlePath = was })
			} else if _, ok := handlePathWorks(t); !ok {
				t.Skip("this system has none")
			}
			s, _ := checkout(t)
			ctx := context.Background()
			write(t, s.Root, ".env", "SECRET=1\n")
			link := filepath.Join(s.Root, "swap")
			put := func(to string) {
				t.Helper()
				os.Remove(link)
				if to == "" {
					write(t, s.Root, "swap", "plain\n")
				} else if err := os.Symlink(to, link); err != nil {
					t.Fatal(err)
				}
			}
			put("")
			if res, err := s.Read(ctx, "swap", 0, false); err != nil || string(res.Data) != "plain\n" {
				t.Fatalf("no swap: %q %v", res.Data, err)
			}
			s.beforeOpen = func() { put(".env") }
			if res, err := s.Read(ctx, "swap", 0, false); err == nil {
				t.Errorf("read an ignored file through a swapped link: %q", res.Data)
			}
			s.beforeOpen = nil
			put(".env")
			if res, err := s.Read(ctx, "swap", 0, true); err != nil || string(res.Data) != "SECRET=1\n" {
				t.Errorf("asked for: %q %v", res.Data, err)
			}
			// A folder too: an ignored one swapped in isn't listed.
			write(t, s.Root, "build/out.txt", "x")
			os.Remove(link)
			os.Mkdir(link, 0o755)
			s.beforeOpen = func() { os.Remove(link); os.Symlink("build", link) }
			if res, err := s.List(ctx, "swap", Options{}, nil); err == nil {
				t.Errorf("listed an ignored folder through a swapped link: %s", names(res.Entries))
			}
			s.beforeOpen = nil
			os.Remove(link)
			// A file replaced outright: not the file the path leads to now.
			if !handle {
				write(t, s.Root, "plain.txt", "one\n")
				s.beforeOpen = nil
				f, fi, err := s.open("plain.txt")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				os.Remove(filepath.Join(s.Root, "plain.txt"))
				write(t, s.Root, "plain.txt", "two\n")
				if _, err := s.openedAt(f, fi, "plain.txt"); !errors.Is(err, ErrGone) {
					t.Errorf("a replaced file: %v", err)
				}
			}
		})
	}
}

func handlePathWorks(t *testing.T) (string, bool) {
	t.Helper()
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return handlePath(f)
}

// TestIgnoredInsideFolder: what's listed inside an ignored folder is marked
// ignored too, for the viewer to say so.
func TestIgnoredInsideFolder(t *testing.T) {
	s, _ := checkout(t)
	write(t, s.Root, "build/out.txt", "x")
	res, err := s.List(context.Background(), "build", Options{Ignored: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(res.Entries); !strings.Contains(got, "build/out.txt!") {
		t.Errorf("listed %s", got)
	}
}

// TestSubmoduleInScope: a submodule's folder planted in the root runs
// nothing it configures, for a listing, a read or a stamp.
func TestSubmoduleInScope(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	mark := filepath.Join(t.TempDir(), "ran")
	sub := filepath.Join(s.Root, "sub")
	write(t, sub, ".gitattributes", "* filter=evil\n")
	write(t, sub, "f", "x\n")
	g := gittest.In(t, sub)
	g("init", "-q")
	g("config", "user.email", "a@b.c")
	g("config", "user.name", "a")
	g("add", ".")
	g("commit", "-qm", "s")
	gittest.Run(t, s.Root, "update-index", "--add", "--cacheinfo", "160000,"+g("rev-parse", "HEAD")+",sub")
	g("config", "filter.evil.clean", "touch "+mark+"; cat")
	write(t, sub, "f", "y\n")
	var c Cache
	if _, err := c.Changes(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, "", Options{}, marksFor(t, s)); err != nil {
		t.Fatal(err)
	}
	s.Stamp(ctx, []string{""})
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the submodule's filter ran")
	}
}

// TestCacheStampPaced: a slow repo's git status is asked for no more than
// its share of the time, while the folders' entries stay live; a quick
// repo's every time.
func TestCacheStampPaced(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	var c Cache
	first := c.Stamp(ctx, s, nil)
	if first != s.Stamp(ctx, nil) {
		t.Fatal("not the scope's stamp")
	}
	seen := c.stamps[s.Root]
	seen.took = time.Hour // as slow as it gets
	c.stamps[s.Root] = seen
	write(t, s.Root, "src/main.go", "package main // edit\n")
	if got := c.Stamp(ctx, s, nil); got != first {
		t.Error("a slow repo's status was asked again at once")
	}
	if got := c.Stamp(ctx, s, []string{""}); got == s.stamp(seen.fp, nil) {
		t.Error("the folders didn't count")
	}
	seen.at = time.Now().Add(-maxStampRest - time.Second)
	c.stamps[s.Root] = seen
	if got := c.Stamp(ctx, s, nil); got == first {
		t.Error("the stamp never caught up")
	}
	// A quick one isn't held back: its time is well under filesEvery's.
	seen = c.stamps[s.Root]
	seen.took = time.Millisecond
	seen.at = time.Now().Add(-time.Second)
	c.stamps[s.Root] = seen
	write(t, s.Root, "src/main.go", "package main // again\n")
	if got := c.Stamp(ctx, s, nil); got != s.Stamp(ctx, nil) {
		t.Error("a quick repo's stamp lagged")
	}
}

func names(es []proto.FileEntry) string {
	var out []string
	for _, e := range es {
		l := e.Path
		if e.Status != "" {
			l += ":" + e.Status
		}
		if e.Changed {
			l += "*"
		}
		if e.Ignored {
			l += "!"
		}
		if e.LinkOut {
			l += ">"
		}
		out = append(out, l)
	}
	return strings.Join(out, " ")
}

// TestList: folders first, .git never, ignored only when asked, marks and
// what git has deleted, totals of the whole checkout; a link out listed
// but marked.
func TestList(t *testing.T) {
	s, secret := checkout(t)
	os.Symlink(secret, filepath.Join(s.Root, "lnk"))
	cs := marksFor(t, s)
	ctx := context.Background()
	res, err := s.List(ctx, "", Options{}, cs)
	if err != nil {
		t.Fatal(err)
	}
	want := "old:D* src* .gitignore lnk:A> README.md:M"
	if got := names(res.Entries); got != want {
		t.Errorf("top = %s\nwant %s", got, want)
	}
	if !res.Git || res.Root != s.Root || res.Files != cs.Totals.Files || res.Added == 0 || res.Deleted == 0 {
		t.Errorf("totals: %+v", res)
	}
	res, err = s.List(ctx, "", Options{Ignored: true}, cs)
	if err != nil {
		t.Fatal(err)
	}
	want = "build! old:D* src* .env! .gitignore debug.log! lnk:A> README.md:M"
	if got := names(res.Entries); got != want {
		t.Errorf("top with ignored = %s\nwant %s", got, want)
	}
	res, err = s.List(ctx, "src", Options{}, cs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(res.Entries), "src/gone.go:D src/main.go src/new.go:A"; got != want {
		t.Errorf("src = %s\nwant %s", got, want)
	}
	res, err = s.List(ctx, "old", Options{}, cs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(res.Entries), "old/only.go:D"; got != want {
		t.Errorf("a deleted folder = %s, want %s", got, want)
	}
}

// TestFind: a search by a path's words, a changed-files list, under a
// folder, ignored only when asked, in a plain folder too.
func TestFind(t *testing.T) {
	s, _ := checkout(t)
	cs := marksFor(t, s)
	ctx := context.Background()
	for _, c := range []struct {
		path string
		o    Options
		want string
	}{
		{"", Options{Query: "MAIN"}, "src/main.go"},
		{"", Options{Query: "env"}, ""},
		{"", Options{Query: "env", Ignored: true}, ".env!"},
		{"", Options{Query: "src/n"}, "src/new.go:A"},
		{"old", Options{Changed: true}, "old/only.go:D"},
		{"", Options{Changed: true}, "README.md:M old/only.go:D src/gone.go:D src/new.go:A"},
		{"", Options{Changed: true, Query: "new"}, "src/new.go:A"},
	} {
		res, err := s.List(ctx, c.path, c.o, cs)
		if err != nil {
			t.Errorf("%+v: %v", c, err)
			continue
		}
		if got := names(res.Entries); got != c.want {
			t.Errorf("%+v = %q, want %q", c, got, c.want)
		}
	}
	plain := t.TempDir()
	write(t, plain, "a/b.txt", "text")
	write(t, plain, ".git/config", "hidden")
	p, err := Open(ctx, plain, "", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.List(ctx, "", Options{Query: "b"}, nil)
	if err != nil || names(res.Entries) != "a/b.txt" {
		t.Errorf("plain search: %s %v", names(res.Entries), err)
	}
	res, err = p.List(ctx, "", Options{}, nil)
	if err != nil || names(res.Entries) != "a" || res.Git {
		t.Errorf("plain list: %s %+v %v", names(res.Entries), res, err)
	}
	if _, err := p.Read(ctx, ".git/config", 0, true); !errors.Is(err, ErrPath) {
		t.Errorf("plain .git: %v", err)
	}
}

// TestOpenPinned: a folder that isn't its repo's checkout has no scope,
// however its own .git reads; config the worktree carries starts nothing.
func TestOpenPinned(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	other := t.TempDir()
	gittest.Run(t, other, "init", "-q")
	if _, err := Open(ctx, other, s.Root, ""); !errors.Is(err, ErrNoRoot) {
		t.Errorf("another repo's checkout: %v", err)
	}
	if _, err := Open(ctx, filepath.Join(s.Root, "nowhere"), "", ""); !errors.Is(err, ErrNoRoot) {
		t.Errorf("a missing folder: %v", err)
	}
	if _, err := Open(ctx, filepath.Join(s.Root, "README.md"), "", ""); !errors.Is(err, ErrNoRoot) {
		t.Errorf("a file: %v", err)
	}
	mark := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "mon.sh")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch "+mark+"\n"), 0o755)
	gittest.Run(t, s.Root, "config", "core.fsmonitor", script)
	if _, err := s.List(ctx, "", Options{}, marksFor(t, s)); err != nil {
		t.Fatal(err)
	}
	s.Stamp(ctx, nil)
	if _, err := os.Stat(mark); err == nil {
		t.Error("the repo's core.fsmonitor ran")
	}
}

// TestRead: chunks, a size cap with Truncated, binary as name and size.
func TestRead(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	big := strings.Repeat("x", proto.LibraryChunk+10)
	write(t, s.Root, "big.txt", big)
	res, err := s.Read(ctx, "big.txt", 0, false)
	if err != nil || len(res.Data) != proto.LibraryChunk || res.Size != int64(len(big)) || res.Truncated || res.Binary {
		t.Fatalf("first chunk: %d %+v %v", len(res.Data), res.Size, err)
	}
	res, err = s.Read(ctx, "big.txt", proto.LibraryChunk, false)
	if err != nil || len(res.Data) != 10 {
		t.Fatalf("last chunk: %d %v", len(res.Data), err)
	}
	if res, err = s.Read(ctx, "big.txt", 1<<40, false); err != nil || len(res.Data) != 0 {
		t.Fatalf("past the end: %d %v", len(res.Data), err)
	}
	if _, err = s.Read(ctx, "big.txt", -1, false); !errors.Is(err, ErrPath) {
		t.Fatalf("negative offset: %v", err)
	}
	huge := filepath.Join(s.Root, "huge.txt")
	f, _ := os.Create(huge)
	f.WriteString("text")
	f.Truncate(proto.MaxFilesRead + 1)
	f.Close()
	os.WriteFile(huge, append([]byte("text"), []byte(strings.Repeat("y", proto.MaxFilesRead))...), 0o644)
	if res, err = s.Read(ctx, "huge.txt", 0, false); err != nil || !res.Truncated || len(res.Data) != proto.LibraryChunk {
		t.Fatalf("over the cap: %d %v %v", len(res.Data), res.Truncated, err)
	}
	if res, err = s.Read(ctx, "huge.txt", proto.MaxFilesRead, false); err != nil || len(res.Data) != 0 || !res.Truncated {
		t.Fatalf("at the cap: %d %v", len(res.Data), err)
	}
	write(t, s.Root, "pic.png", "\x89PNG\x00\x00")
	if res, err = s.Read(ctx, "pic.png", 0, false); err != nil || !res.Binary || res.Data != nil || res.Size != 6 || res.Name != "pic.png" {
		t.Fatalf("binary: %+v %v", res, err)
	}
	if res, err = s.ReadBytes(ctx, "pic.png", 0, false); err != nil || !res.Binary || string(res.Data) != "\x89PNG\x00\x00" {
		t.Fatalf("a binary file's bytes: %+v %v", res, err)
	}
	if _, err = s.ReadBytes(ctx, "debug.log", 0, false); !errors.Is(err, ErrIgnored) {
		t.Fatalf("asking for bytes passes the ignore rule: %v", err)
	}
	write(t, s.Root, "latin.txt", "caf\xe9 au lait")
	if res, err = s.Read(ctx, "latin.txt", 0, false); err != nil || !res.Binary {
		t.Fatalf("not UTF-8: %+v %v", res, err)
	}
	// A multi-byte character cut by the sniff is still text.
	write(t, s.Root, "cut.txt", strings.Repeat("a", sniff-1)+"é")
	if res, err = s.Read(ctx, "cut.txt", 0, false); err != nil || res.Binary {
		t.Fatalf("a cut character: %v %v", res.Binary, err)
	}
}

// TestStamp: the stamp moves with what a listing would show, in a folder
// named or by git's status anywhere, and only then.
func TestStamp(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	folders := []string{"", "src", "../x", "gone"}
	st := s.Stamp(ctx, folders)
	if again := s.Stamp(ctx, folders); again != st {
		t.Fatal("the stamp moved by itself")
	}
	step := func(what string, f func()) {
		t.Helper()
		f()
		next := s.Stamp(ctx, folders)
		if next == st {
			t.Errorf("%s: the stamp stayed", what)
		}
		st = next
	}
	step("a tracked file edited", func() { write(t, s.Root, "src/main.go", "package main // edit\n") })
	step("a new file deep down", func() { write(t, s.Root, "deep/er/file.txt", "x") })
	step("an ignored file in a folder named", func() { write(t, s.Root, "other.log", "x") })
	step("a commit", func() {
		gittest.Run(t, s.Root, "add", ".")
		gittest.Run(t, s.Root, "commit", "-q", "-m", "more")
	})
	if res := s.Stamp(ctx, nil); res == "" {
		t.Error("no stamp")
	}
}

// TestCacheFollows: the cached changes last until the fingerprint moves.
func TestCacheFollows(t *testing.T) {
	s, _ := checkout(t)
	var c Cache
	ctx := context.Background()
	a, err := c.Changes(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.Changes(ctx, s)
	if a != b {
		t.Error("read again though nothing changed")
	}
	write(t, s.Root, "another.txt", "x")
	d, _ := c.Changes(ctx, s)
	if d == a || d.Totals.Files != a.Totals.Files+1 {
		t.Errorf("not followed: %+v", d.Totals)
	}
}
