package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// The test binary is a tm for the host tests: run as `mod host …` it is
// modHost, as main dispatches it.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "mod" && os.Args[2] == "host" {
		os.Exit(modHost(os.Args[3:]))
	}
	os.Exit(m.Run())
}

// hostStats starts `tm mod host` with env, as the supervisor does (a bare
// environment), and returns its answer to stats.
func hostStats(t *testing.T, env ...string) modproto.Stats {
	t.Helper()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "main.js")
	if err := os.WriteFile(bundle, []byte(`$.on("x", () => {});`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "mod", "host", "--budget", "1s", bundle)
	cmd.Dir, cmd.Env = dir, append(proc.BareEnv(), env...)
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var c *modproto.Client
	c = modproto.NewClient(stdout, stdin, func(m *modproto.Message) { _ = c.Conn().Reply(*m.ID, nil) }, func(*modproto.Message) {})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() { _ = c.Shutdown(ctx); _ = cmd.Wait() }()
	if _, err := c.Hello(ctx, modproto.Hello{API: "1.0", Mod: "m"}); err != nil {
		t.Fatal(err)
	}
	raw, err := c.Call(ctx, modproto.MethodStats, nil)
	if err != nil {
		t.Fatal(err)
	}
	var st modproto.Stats
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// TestModHostRuntimeSettings checks tm mod host runs with the host's
// GOMAXPROCS, GC percent and soft memory limit (T360), and that the
// environment's GOMAXPROCS, GOGC and GOMEMLIMIT win.
func TestModHostRuntimeSettings(t *testing.T) {
	st := hostStats(t)
	if st.MaxProcs != modhost.MaxProcs || st.GCPercent != modhost.GCPercent || st.MemoryLimit != modhost.MemoryLimit {
		t.Errorf("default stats %+v", st)
	}
	st = hostStats(t, "GOMAXPROCS=3", "GOGC=80", "GOMEMLIMIT=64MiB")
	if st.MaxProcs != 3 || st.GCPercent != 80 || st.MemoryLimit != 64<<20 {
		t.Errorf("stats with the environment's settings %+v", st)
	}
}
