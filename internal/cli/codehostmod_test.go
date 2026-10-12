package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// TestResolveModHostMerged: a code host mod's word that a thread's PR
// merged deletes its branch only when git finds the branch's commits on
// the remote (mods design §11: the remote-has-the-commits check stays
// tm's, whoever supplies the host).
func TestResolveModHostMerged(t *testing.T) {
	t.Cleanup(func() { codehost.Provide(nil) })
	codehost.Provide(func(kind, project string) (string, codehost.ModCall, error) {
		return "gitlab", func(_ context.Context, method string, _ any) (json.RawMessage, error) {
			return json.RawMessage(`{"state":"MERGED"}`), nil // every PR merged, says the mod
		}, nil
	})
	for _, pushed := range []bool{false, true} {
		h := newHarness(t)
		h.ok(human, "project", "new", "demo")
		repo, rec := threadWithWorktree(t, h, "demo")
		p, err := project.Open("demo")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p.Path("PROJECT.md"))
		os.WriteFile(p.Path("PROJECT.md"), []byte(strings.Replace(string(b), "+++\n", "+++\ncode_host = \"gitlab\"\n", 1)), 0o644)

		// origin names gitlab.com; git reaches it in a bare repo here.
		bare := filepath.Join(t.TempDir(), "shop.git")
		git(t, repo, "init", "-q", "--bare", bare)
		git(t, repo, "remote", "add", "origin", "git@gitlab.com:acme/shop.git")
		git(t, repo, "config", "url."+bare+".insteadOf", "git@gitlab.com:acme/shop.git")
		git(t, repo, "push", "-q", "origin", "HEAD")
		os.WriteFile(filepath.Join(rec.Worktree, "work.md"), []byte("work"), 0o644)
		git(t, rec.Worktree, "add", ".")
		git(t, rec.Worktree, "commit", "-q", "-m", "work")
		if pushed {
			git(t, rec.Worktree, "push", "-q", "origin", rec.Branch)
		}

		out := h.ok(coord, "thread", "resolve", rec.ID, "--project", "demo")
		switch {
		case !pushed && (!strings.Contains(out, "kept branch "+rec.Branch+" (PR merged, but 1 unpushed commit on "+rec.Branch+")") || !worktree.BranchExists(repo, rec.Branch)):
			t.Errorf("unpushed: %q", out)
		case pushed && (!strings.Contains(out, "deleted branch "+rec.Branch+" (PR merged)") || worktree.BranchExists(repo, rec.Branch)):
			t.Errorf("pushed: %q", out)
		}
	}
}
