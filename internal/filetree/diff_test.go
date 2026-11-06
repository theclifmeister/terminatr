//go:build unix

package filetree

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestDiff: what each kind of file answers, against what its mark counts
// from.
func TestDiff(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	repo := s.Root
	write(t, repo, "edit.txt", "a\nb\nc\nd\ne\n")
	write(t, repo, "name.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	write(t, repo, "big.txt", "x\n")
	write(t, repo, "shrunk.txt", strings.Repeat("y\n", proto.MaxFilesDiff))
	write(t, repo, "pic.bin", "\x00\x01")
	write(t, repo, "bigdel.txt", strings.Repeat("z\n", proto.MaxFilesDiff))
	gittest.Run(t, repo, "add", "edit.txt", "name.txt", "big.txt", "shrunk.txt", "pic.bin", "bigdel.txt")
	gittest.Run(t, repo, "commit", "-q", "-m", "more")
	write(t, repo, "edit.txt", "a\nB\nc\ne\nf\n")
	gittest.Run(t, repo, "mv", "name.txt", "renamed.txt")
	write(t, repo, "renamed.txt", "one\ntwo\nthree\nfour\nfive\nSIX\n")
	write(t, repo, "big.txt", strings.Repeat("x\n", proto.MaxFilesDiff))
	write(t, repo, "shrunk.txt", "y\n")
	write(t, repo, "pic.bin", "\x00\x02")
	os.Remove(filepath.Join(repo, "bigdel.txt"))
	cs := marksFor(t, s)

	for name, c := range map[string]struct {
		want proto.FilesDiffResult
	}{
		"edit.txt": {proto.FilesDiffResult{Status: "M", Added: 2, Deleted: 3, Hunks: []proto.FilesHunk{
			{Old: 2, New: 2, Added: 1, Removed: []string{"b"}},
			{Old: 4, New: 4, Removed: []string{"d"}},
			{Old: 6, New: 5, Added: 1},
		}}},
		"README.md":   {proto.FilesDiffResult{Status: "M", Added: 1, Hunks: []proto.FilesHunk{{Old: 3, New: 3, Added: 1}}}},
		"src/main.go": {},
		"src/new.go":  {proto.FilesDiffResult{Status: "A", Added: 1}},
		"src/gone.go": {proto.FilesDiffResult{Status: "D", Deleted: 1, Hunks: []proto.FilesHunk{{Old: 1, New: 1, Removed: []string{"package gone"}}}}},
		"renamed.txt": {proto.FilesDiffResult{Status: "M", From: "name.txt", Added: 1, Deleted: 1, Hunks: []proto.FilesHunk{{Old: 6, New: 6, Added: 1, Removed: []string{"six"}}}}},
		"name.txt":    {proto.FilesDiffResult{Status: "D", Hunks: []proto.FilesHunk{{Old: 1, New: 1, Removed: []string{"one", "two", "three", "four", "five", "six"}}}}},
		"big.txt":     {proto.FilesDiffResult{Status: "M", TooBig: true}},
		"shrunk.txt":  {proto.FilesDiffResult{Status: "M", TooBig: true}},
		"bigdel.txt":  {proto.FilesDiffResult{Status: "D", TooBig: true}},
		"pic.bin":     {proto.FilesDiffResult{Status: "M", Binary: true}},
	} {
		got, err := s.Diff(ctx, name, false, cs)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got.Name, got.Path, got.Size, got.Base, got.MTime = "", "", 0, "", c.want.MTime
		if name == "edit.txt" || name == "big.txt" || name == "shrunk.txt" || name == "bigdel.txt" {
			got.Added, got.Deleted = c.want.Added, c.want.Deleted // git's choice of pairing, or a size's
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
	if d, err := s.Diff(ctx, "bigdel.txt", false, cs); err != nil || d.Size != 2*proto.MaxFilesDiff {
		t.Errorf("a deleted file's size is the base's: %d %v", d.Size, err)
	}
}

// TestDiffRules: the path rules are Read's, a deleted file's too, and an
// unmarked path that isn't there is gone.
func TestDiffRules(t *testing.T) {
	s, secret := checkout(t)
	ctx := context.Background()
	if err := os.Symlink(secret, filepath.Join(s.Root, "out")); err != nil {
		t.Fatal(err)
	}
	cs := marksFor(t, s)
	for rel, want := range map[string]error{
		"":            ErrNotFile,
		"../x":        ErrPath,
		".git/config": ErrPath,
		"out":         ErrLinkOut,
		"debug.log":   ErrIgnored,
		"src":         ErrNotFile,
		"nope.txt":    ErrGone,
	} {
		if _, err := s.Diff(ctx, rel, false, cs); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", rel, err, want)
		}
	}
	got, err := s.Diff(ctx, "debug.log", true, cs)
	if err != nil || got.Status != "" {
		t.Errorf("an ignored file, asked for: %+v %v", got, err)
	}

	// Inside a link to a file of the folder, the change is the target's.
	if err := os.Symlink("README.md", filepath.Join(s.Root, "again")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Diff(ctx, "again", false, marksFor(t, s)); err != nil || got.Status != "M" || len(got.Hunks) != 1 {
		t.Errorf("a link's target: %+v %v", got, err)
	}
}

// TestDiffPlainFolder: a folder with no repo has no marks.
func TestDiffPlainFolder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", "a\n")
	s, err := Open(context.Background(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Diff(context.Background(), "a.txt", false, nil); err != nil || got.Status != "" || got.Size != 2 {
		t.Errorf("%+v %v", got, err)
	}
	if _, err := s.Diff(context.Background(), "no.txt", false, nil); !errors.Is(err, ErrGone) {
		t.Errorf("gone: %v", err)
	}
}

// TestDiffAgainstBase: changes count from the merge base with the thread's
// base, committed ones too, as the marks do.
func TestDiffAgainstBase(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "-q", "-b", "main", repo)
	write(t, repo, "a.txt", "one\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	wt := filepath.Join(root, "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/x", wt)
	write(t, wt, "a.txt", "two\n")
	gittest.Run(t, wt, "add", ".")
	gittest.Run(t, wt, "commit", "-q", "-m", "on the branch")
	s, err := Open(context.Background(), wt, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Diff(context.Background(), "a.txt", false, marksFor(t, s))
	if err != nil || got.Status != "M" || got.Base != "main" || len(got.Hunks) != 1 || got.Hunks[0].Removed[0] != "one" {
		t.Errorf("%+v %v", got, err)
	}
}

// TestDiffText: a removed line is data, and the answer stays within a line.
func TestDiffText(t *testing.T) {
	s, _ := checkout(t)
	ctx := context.Background()
	write(t, s.Root, "evil.txt", "ok\nbad\x1b[2J\u202e\ttabbed\r\nnot \xff utf8\nlast")
	write(t, s.Root, "many.txt", strings.Repeat(`"<>&"`+"\n", proto.MaxFilesDiff/6))
	gittest.Run(t, s.Root, "add", ".")
	gittest.Run(t, s.Root, "commit", "-q", "-m", "more")
	write(t, s.Root, "evil.txt", "ok\n")
	write(t, s.Root, "many.txt", "")
	cs := marksFor(t, s)
	got, err := s.Diff(ctx, "evil.txt", false, cs)
	if err != nil || len(got.Hunks) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if want := []string{"bad[2J\ttabbed", "not ? utf8", "last"}; !reflect.DeepEqual(got.Hunks[0].Removed, want) {
		t.Errorf("removed %q, want %q", got.Hunks[0].Removed, want)
	}
	got, err = s.Diff(ctx, "many.txt", false, cs)
	if err != nil || !got.Truncated || len(got.Hunks) != 1 || len(got.Hunks[0].Removed) == 0 {
		t.Fatalf("over the wire's bound: %v %d %v", got.Truncated, len(got.Hunks), err)
	}
	if enc, _ := json.Marshal(got); len(enc) > 900<<10 {
		t.Errorf("%d bytes on the wire", len(enc))
	}
}

// TestParseHunks: git's forms, a cut, and text that looks like git's.
func TestParseHunks(t *testing.T) {
	patch := "diff --git a/f b/f\nindex 1..2 100644\n--- a/f\n+++ b/f\n" +
		"@@ -1,2 +0,0 @@\n--- a/f\n-@@ -9 +9 @@\n" +
		"@@ -5 +3 @@ func x\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n" +
		"@@ -9,0 +8,2 @@\n+a\n+b\n"
	got, binary, cut := parseHunks(patch, false, maxDiffWire)
	want := []proto.FilesHunk{
		{Old: 1, New: 1, Removed: []string{"-- a/f", "@@ -9 +9 @@"}},
		{Old: 5, New: 3, Added: 1, Removed: []string{"old"}},
		{Old: 10, New: 8, Added: 2},
	}
	if binary || cut || !reflect.DeepEqual(got, want) {
		t.Errorf("%+v %v %v", got, binary, cut)
	}
	got, _, cut = parseHunks("@@ -1,2 +1 @@\n-a\n-b\n+c\n@@ -9,3 +9 @@\n-d\n-e", true, maxDiffWire)
	if !cut || len(got) != 1 || got[0].Added != 1 {
		t.Errorf("a cut in a hunk: %+v %v", got, cut)
	}
	if _, binary, _ := parseHunks("diff --git a/f b/f\nBinary files a/f and b/f differ\n", false, maxDiffWire); !binary {
		t.Error("binary")
	}
	if got, binary, cut := parseHunks("", false, maxDiffWire); got != nil || binary || cut {
		t.Errorf("empty: %+v", got)
	}
}
