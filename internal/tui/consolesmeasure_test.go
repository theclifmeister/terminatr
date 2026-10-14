//go:build unix

package tui

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestConsolesIdleCost measures idle attach consoles with a server:
// the CPU of the server and of 1, 4 and 8 consoles' state polls (their
// session.list and sidebar data each tick), over 4 projects of 20
// threads each. TERMINATR_MEASURE=1 runs it.
func TestConsolesIdleCost(t *testing.T) {
	if os.Getenv("TERMINATR_MEASURE") == "" {
		t.Skip("TERMINATR_MEASURE=1 measures")
	}
	t.Setenv("TERMINATR_HOME", t.TempDir())
	human := caller.Caller{Kind: caller.Human}
	for i := range 4 {
		p, err := project.New(project.Options{Slug: fmt.Sprintf("proj%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		var nt []tasks.NewTask
		for j := range 20 {
			nt = append(nt, tasks.NewTask{Title: fmt.Sprintf("task %d", j), Status: "started", Steps: []string{"a", "b"}})
		}
		if _, err := p.Tasks().Add(human, nt); err != nil {
			t.Fatal(err)
		}
		for j := range 20 {
			if _, err := thread.Create(p, thread.Record{Title: "work", Task: fmt.Sprintf("T%d", j+1)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	paths := startServer(t)
	cpu := func() time.Duration {
		var s syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &s)
		return time.Duration(s.Utime.Nano() + s.Stime.Nano())
	}
	const window = 10 * time.Second
	measure := func(what string) {
		time.Sleep(2 * time.Second) // settle
		c0 := cpu()
		time.Sleep(window)
		used := cpu() - c0
		t.Logf("%s: CPU %.2f%% (%v per s)", what, 100*used.Seconds()/window.Seconds(), used/time.Duration(window.Seconds()))
	}
	measure("server alone")
	running := 0
	for _, n := range []int{1, 4, 8} {
		for ; running < n; running++ {
			c, err := newClient(paths, log.New(io.Discard, "", 0))
			if err != nil {
				t.Fatal(err)
			}
			c.side = &sidebar{}
			go func() {
				for range c.wake {
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go c.pollState(ctx)
		}
		measure(fmt.Sprintf("server and %d consoles, 4 projects of 20 threads", n))
	}
}

// TestDashboardLoadCost measures the dashboard's poll (ServerSource.Load,
// once a second) with a server, 4 projects of 20 threads and 6 mods
// installed: the CPU of one Load. TERMINATR_MEASURE=1 runs it.
func TestDashboardLoadCost(t *testing.T) {
	if os.Getenv("TERMINATR_MEASURE") == "" {
		t.Skip("TERMINATR_MEASURE=1 measures")
	}
	t.Setenv("TERMINATR_HOME", t.TempDir())
	human := caller.Caller{Kind: caller.Human}
	for i := range 4 {
		p, err := project.New(project.Options{Slug: fmt.Sprintf("proj%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		for j := range 20 {
			if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: fmt.Sprintf("task %d", j), Status: "started"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := thread.Create(p, thread.Record{Title: "work", Task: fmt.Sprintf("T%d", j+1)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range 6 {
		modFolderAt(t, fmt.Sprintf("look%d", i), map[string]string{
			"mod.toml":    fmt.Sprintf("id = \"look%d\"\nname = \"Look\"\nversion = \"1.0.0\"\napi = \"1.5\"\n[provides]\nlayout = \"layout.toml\"\n", i),
			"layout.toml": "[sidebar]\nside = \"left\"\n",
		})
	}
	paths := startServer(t)
	src := &ServerSource{Paths: paths, Caller: human}
	defer src.Close()
	src.Load() // connect, settle
	cpu := func() time.Duration {
		var s syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &s)
		return time.Duration(s.Utime.Nano() + s.Stime.Nano())
	}
	const n = 50
	c0 := cpu()
	for range n {
		src.Load()
	}
	t.Logf("dashboard Load, 4 projects of 20 threads, 6 mods: %v CPU per Load", (cpu()-c0)/n)
}

// modFolderAt installs mod id in the mods folder with files.
func modFolderAt(t *testing.T, id string, files map[string]string) {
	t.Helper()
	dir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(dir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
