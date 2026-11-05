//go:build unix

package server

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestFilesScope: what a session's folder is, by its role: a thread's
// worktree, a coordinator's main checkout; a director, a shell and a
// paired server's session have none and say so.
func TestFilesScope(t *testing.T) {
	testPaths(t) // a home of its own
	repo := filepath.Join(t.TempDir(), "repo")
	gittest.Run(t, filepath.Dir(repo), "init", "-q", "-b", "main", repo)
	write := func(dir, name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(repo, "README.md", "main checkout\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	wt := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/x", wt)
	write(wt, "new.txt", "from the thread\n")

	p, err := project.New(project.Options{Slug: "demo", Repos: []string{repo}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := thread.Create(p, thread.Record{Title: "x", Agent: "claude", Repo: repo, Base: "main", Branch: "tm/x", Worktree: wt, State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := project.New(project.Options{Slug: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	strayFolder := t.TempDir()
	stray, err := thread.Create(plain, thread.Record{Title: "y", Agent: "claude", Worktree: strayFolder, State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{log: log.New(os.Stderr, "", 0), sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}}
	start := func(id, role, slug, th string) {
		t.Helper()
		sess, err := session.Start(session.Config{ID: id, Role: role, Project: slug, Thread: th, Argv: []string{testSh(t), "-c", "exec cat"},
			Cwd: t.TempDir(), Env: []string{"PATH=/usr/bin:/bin"}, Cols: 80, Rows: 24, Logf: t.Logf})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sess.Stop(time.Second) })
		s.mu.Lock()
		s.sessions[id] = sess
		s.mu.Unlock()
	}
	start("s-t", proto.RoleThread, "demo", r.ID)
	start("s-c", proto.RoleCoordinator, "demo", "")
	start("s-d", proto.RoleDirector, "prog", "")
	start("s-x", proto.RoleShell, "", "")
	start("s-p", proto.RoleCoordinator, "plain", "")
	start("s-s", proto.RoleThread, "plain", stray.ID)

	code := func(perr *proto.Error) string {
		if perr == nil {
			return ""
		}
		return perr.Code
	}
	for _, c := range []struct{ id, want string }{
		{"s-t", ""}, {"s-c", ""},
		{"s-d", proto.ErrNoFiles}, {"s-x", proto.ErrNoFiles},
		{"s-p", proto.ErrNoFiles}, // a project without a repo
		{"s-s", proto.ErrNoFiles}, // a plain folder tm didn't make
		{"pal/s-1", proto.ErrNoFiles},
		{"s-none", proto.ErrUnknownSession},
	} {
		if _, perr := s.filesScope(context.Background(), c.id); code(perr) != c.want {
			t.Errorf("%s: %v, want %q", c.id, perr, c.want)
		}
	}

	got, perr := s.filesList(proto.FilesListParams{Session: "s-t"})
	if perr != nil {
		t.Fatal(perr)
	}
	res := got.(proto.FilesListResult)
	real, _ := filepath.EvalSymlinks(wt)
	if res.Root != real || !res.Git || res.Files != 1 || len(res.Entries) != 2 || res.Entries[0].Name != "new.txt" || res.Entries[0].Status != proto.FileAdded {
		t.Errorf("the thread's: %+v", res)
	}
	got, perr = s.filesList(proto.FilesListParams{Session: "s-c"})
	if perr != nil {
		t.Fatal(perr)
	}
	real, _ = filepath.EvalSymlinks(repo)
	if res := got.(proto.FilesListResult); res.Root != real || len(res.Entries) != 1 || res.Entries[0].Name != "README.md" {
		t.Errorf("the coordinator's: %+v", res)
	}
	got, perr = s.filesRead(proto.FilesReadParams{Session: "s-t", Path: "new.txt"})
	if perr != nil || string(got.(proto.FilesReadResult).Data) != "from the thread\n" {
		t.Errorf("read: %+v %v", got, perr)
	}
	for path, want := range map[string]string{"../x": proto.ErrInvalidPath, ".git/config": proto.ErrInvalidPath, "nope": proto.ErrFileGone} {
		if _, perr := s.filesRead(proto.FilesReadParams{Session: "s-t", Path: path}); code(perr) != want {
			t.Errorf("read %s: %v, want %s", path, perr, want)
		}
	}
	a, perr := s.filesStamp(proto.FilesStampParams{Session: "s-t", Folders: []string{""}})
	if perr != nil {
		t.Fatal(perr)
	}
	write(wt, "more.txt", "x")
	b, _ := s.filesStamp(proto.FilesStampParams{Session: "s-t", Folders: []string{""}})
	if a.(proto.FilesStampResult).Stamp == b.(proto.FilesStampResult).Stamp {
		t.Error("the stamp stayed")
	}
	if _, perr := s.filesStamp(proto.FilesStampParams{Session: "s-t", Folders: make([]string, proto.MaxFilesStamped+1)}); code(perr) != proto.ErrBadParams {
		t.Errorf("too many folders: %v", perr)
	}
}
