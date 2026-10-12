package modsup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// envMeasureBin names a built tm binary for TestMeasurePRCI.
const envMeasureBin = "TERMINATR_MEASURE_BIN"

// TestMeasurePRCI measures the bundled PR/CI column (mods design §12,
// "measure: time to first cell, frame cost, RSS") in a real `tm mod
// host`, as the supervisor runs it: the time from starting the host to
// its hello answer and to its first cell, the time to draw a snapshot of
// 10 projects of 8 threads, the round trip of one pr.changed, and the
// host's RSS once idle. The frame cost is BenchmarkSidebarCI in
// internal/tui. It runs only when TERMINATR_MEASURE_BIN names a tm
// binary (go build -o /tmp/tm ./cmd/tm), and logs what it measured.
func TestMeasurePRCI(t *testing.T) {
	bin := os.Getenv(envMeasureBin)
	if bin == "" {
		t.Skip("set " + envMeasureBin + " to a built tm to measure the PR/CI column")
	}
	m := mod.Bundled(mod.BundledPRCI, config.Mod{})
	script, err := m.ReadFile(m.Manifest.Provides.Main)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "main.js")
	if err := os.WriteFile(bundle, script, 0o600); err != nil {
		t.Fatal(err)
	}
	var ready []map[string]any
	for p := range 10 {
		var threads []map[string]any
		for i := range 8 {
			threads = append(threads, map[string]any{"id": fmt.Sprintf("t-%04d", i+1), "title": "x",
				"pr_checks": []string{"pass", "fail", "pending", ""}[i%4]})
		}
		ready = append(ready, map[string]any{"project": fmt.Sprintf("p%d", p), "threads": threads})
	}
	readyJSON, _ := json.Marshal(map[string]any{"projects": ready})

	const runs = 10
	var helloT, firstT, allT, prT []time.Duration
	var rss []uint64
	for range runs {
		h, f, a, p, r := measureOnce(t, bin, bundle, dir, readyJSON)
		helloT, firstT, allT, prT, rss = append(helloT, h), append(firstT, f), append(allT, a), append(prT, p), append(rss, r)
	}
	med := func(ds []time.Duration) time.Duration {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[len(ds)/2]
	}
	sort.Slice(rss, func(i, j int) bool { return rss[i] < rss[j] })
	t.Logf("pr-ci-column, %d runs, medians: start→hello %s, start→first cell %s, start→60 cells %s, pr.changed→cell %s, idle RSS %.1f MiB (min %.1f, max %.1f)",
		runs, med(helloT), med(firstT), med(allT), med(prT),
		float64(rss[len(rss)/2])/(1<<20), float64(rss[0])/(1<<20), float64(rss[len(rss)-1])/(1<<20))
}

// measureOnce runs one host: start to hello, to its first cell and to
// the 60 cells of the snapshot; one pr.changed's round trip; its RSS
// once idle.
func measureOnce(t *testing.T, bin, bundle, dir string, ready []byte) (hello, first, all, pr time.Duration, rss uint64) {
	t.Helper()
	const cells = 60 // 10 projects × 8 threads, 6 of each 8 with checks
	cellc := make(chan time.Time, 200)
	began := time.Now()
	cmd := exec.Command(bin, "mod", "host", "--budget", "500ms", bundle)
	cmd.Dir, cmd.Env = dir, proc.BareEnv()
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var c *modproto.Client
	c = modproto.NewClient(stdout, stdin, func(m *modproto.Message) {
		if m.Method == "ui.cell.set" {
			cellc <- time.Now()
		}
		if m.Method == "store.get" {
			_ = c.Conn().Reply(*m.ID, nil)
			return
		}
		_ = c.Conn().Reply(*m.ID, struct{}{})
	}, func(*modproto.Message) {})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = c.Shutdown(ctx)
		_ = cmd.Wait()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := c.Hello(ctx, modproto.Hello{API: mod.APIVersion(), Mod: mod.BundledPRCI, Grants: []string{mod.PermProjectRead}}); err != nil {
		t.Fatal(err)
	}
	hello = time.Since(began)
	go func() { _, _ = c.Event(ctx, modproto.Event{Name: "mod.ready", Data: ready}) }()
	for i := range cells {
		select {
		case at := <-cellc:
			if i == 0 {
				first = at.Sub(began)
			}
			all = at.Sub(began)
		case <-ctx.Done():
			t.Fatalf("%d cells of %d", i, cells)
		}
	}
	time.Sleep(200 * time.Millisecond) // settle: the store write, GC
	sent := time.Now()
	go func() {
		_, _ = c.Event(ctx, modproto.Event{Name: "pr.changed", Project: "p0",
			Data: json.RawMessage(`{"project":"p0","thread":"t-0004","number":4,"state":"OPEN","checks":"pass"}`)})
	}()
	select {
	case at := <-cellc:
		pr = at.Sub(sent)
	case <-ctx.Done():
		t.Fatal("no cell for pr.changed")
	}
	time.Sleep(500 * time.Millisecond)
	got, err := proc.RSS([]int{cmd.Process.Pid})
	if err != nil {
		t.Fatal(err)
	}
	return hello, first, all, pr, got[cmd.Process.Pid]
}
