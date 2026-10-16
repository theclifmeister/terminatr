package server

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/session"
)

const codeHostManifest = `id = "%s"
name = "GitLab"
version = "0.1.0"
api = "1.1"
[provides]
main = "main.ts"
code_host = "gitlab"
[permissions]
commands = ["glab"]
`

// codeHostMain answers from fixed words; it runs no glab.
const codeHostMain = `
$.codehost.handle("prState", (a: any) => ({state: a.remote.path === "acme/shop" && a.branch === "feat" ? "MERGED" : "OPEN"}));
`

// TestCodeHostMod: a project's code_host = "gitlab" reaches the enabled
// code mod that provides gitlab and runs for it, granted commands, and
// only one; otherwise Pick's host says why there is none.
func TestCodeHostMod(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	write := func(path, text string) {
		t.Helper()
		path = filepath.Join(tmHome, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"gl", "gl2"} {
		write("mods/"+id+"/mod.toml", strings.Replace(codeHostManifest, "%s", id, 1))
		write("mods/"+id+"/main.ts", codeHostMain)
	}
	const off = "[mods.pr-ci-column]\nenabled = false\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n"
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@gitlab.com:acme/shop.git"}} {
		gittest.Run(t, repo, args...)
	}

	s := &Server{opts: Options{Bin: os.Args[0]}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}
	codehost.Provide(s.codeHostMod)
	t.Cleanup(func() { codehost.Provide(nil) })
	cfg := codehost.Config{CodeHost: "gitlab", Project: "proj"}
	why := func() string {
		t.Helper()
		_, err := codehost.Pick(repo, cfg).PR(repo, codehost.Ref{Branch: "feat"})
		var ce *codehost.CLIError
		if !errors.As(err, &ce) {
			t.Fatalf("not a CLIError: %v", err)
		}
		return ce.Problem
	}
	if got := why(); got != "this server runs no mods" {
		t.Fatalf("without hosts: %q", got)
	}
	sup := s.newModHosts()
	t.Cleanup(sup.Close)
	s.modSup.Store(sup)
	set := func(config string) {
		t.Helper()
		write("config.toml", config+off)
		s.modBr.mu.Lock()
		s.modBr.reg = nil
		s.modBr.mu.Unlock()
		if specs, ok := s.modSpecs(); ok {
			sup.Set(specs)
		}
	}
	for _, c := range []struct{ config, want string }{
		{"", "no enabled mod serves it"},
		{"[mods.gl]\nenabled = true\ngrants = [\"commands\"]\nprojects = [\"other\"]\n", "no enabled mod serves it"},
		{"[mods.gl]\nenabled = true\n", "mod gl serves it without the commands grant"},
		{"[mods.gl]\nenabled = true\ngrants = [\"commands\"]\n[mods.gl2]\nenabled = true\ngrants = [\"commands\"]\n", "more than one mod serves it"},
	} {
		set(c.config)
		if got := why(); got != c.want {
			t.Errorf("%q: %q, want %q", c.config, got, c.want)
		}
	}

	set("[mods.gl]\nenabled = true\ngrants = [\"commands\"]\nprojects = [\"proj\"]\n")
	h := codehost.Pick(repo, cfg)
	if m, ok := h.(codehost.Mod); !ok || m.ID != "gl" || !codehost.External(h) {
		t.Fatalf("picked %#v", h)
	}
	if st := h.PRState(repo, "feat"); st != "MERGED" {
		t.Fatalf("state %q", st)
	}
	// prState is all it handles: pr is a failed call, not "no PR".
	if _, err := h.PR(repo, codehost.Ref{Branch: "feat"}); err == nil || errors.Is(err, codehost.ErrNoPR) {
		t.Fatalf("unhandled pr: %v", err)
	}
	dir, _ := home.ModLogsDir()
	b, _ := os.ReadFile(filepath.Join(dir, "gl.log"))
	if !strings.Contains(string(b), "codehost.prState for proj in ") || !strings.Contains(string(b), "codehost.pr for proj in ") {
		t.Fatalf("log:\n%s", b)
	}
	// tm doctor asks for any project.
	if got := codehost.Checks(codehost.DoctorDeps{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }}, []codehost.RepoHost{{Repo: repo, Target: codehost.Detect(repo, cfg)}}); len(got) < 1 || got[0].Name != "gitlab" || got[0].Detail != "served by mod gl" {
		t.Fatalf("doctor %+v", got)
	}
}
