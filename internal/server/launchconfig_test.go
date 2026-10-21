package server

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/home"
)

// nowhere is a PATH entry that holds nothing, with its list separator.
var nowhere = filepath.FromSlash("/nowhere") + string(os.PathListSeparator)

func versionServer(t *testing.T, version string) (*Server, agent.Agent, *int) {
	t.Helper()
	t.Setenv(home.Env, t.TempDir())
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "claude"+testExe), []byte("#!/bin/sh\n"), 0o700)
	m, err := agent.ParseManifest([]byte("manifest_version = 1\nname = \"claude\"\n[identify]\nversion_args = [\"--version\"]\n[launch]\ncommand = \"claude\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	runs := new(int)
	s := &Server{opts: Options{Env: []string{"PATH=" + nowhere + bin}}, log: log.New(io.Discard, "", 0)}
	s.versions = &agent.Versions{Run: func(_ context.Context, path string, args, _ []string) ([]byte, error) {
		*runs++
		if path != filepath.Join(bin, "claude"+testExe) || strings.Join(args, " ") != "--version" {
			t.Errorf("ran %s %v", path, args)
		}
		return []byte(version + " (Claude Code)\n"), nil
	}}
	return s, agent.FromManifest(m), runs
}

// TestVersionsSeeUpgrade: a changed binary is asked again.
func TestVersionsSeeUpgrade(t *testing.T) {
	s, a, runs := versionServer(t, "2.1.291")
	if v, err := s.versionOf(a, s.baseEnv()); err != nil || v != "2.1.291" {
		t.Fatalf("version %q, %v", v, err)
	}
	path := filepath.Join(strings.TrimPrefix(s.opts.Env[0], "PATH="+nowhere), "claude"+testExe)
	os.WriteFile(path, []byte("#!/bin/sh\n# upgraded\n"), 0o700)
	os.Chtimes(path, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	s.versionOf(a, s.baseEnv())
	if *runs != 2 {
		t.Fatalf("runs %d, want 2", *runs)
	}
}
