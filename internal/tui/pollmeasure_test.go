//go:build unix

package tui

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// countWriter counts the frames a console writes.
type countWriter struct{ frames atomic.Int64 }

func (w *countWriter) Write(b []byte) (int, error) {
	w.frames.Add(1)
	return len(b), nil
}

// TestPollStateIdleCost measures an idle attach console: the CPU its
// state poll and its renderer use, and the frames it draws, with a
// sidebar over 4 projects of 20 threads each and no server.
// TERMINATR_MEASURE=1 runs it.
func TestPollStateIdleCost(t *testing.T) {
	if os.Getenv("TERMINATR_MEASURE") == "" {
		t.Skip("TERMINATR_MEASURE=1 measures")
	}
	t.Setenv("TERMINATR_HOME", t.TempDir())
	sock, err := os.MkdirTemp("", "tmt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sock)
	t.Setenv("TERMINATR_SOCKET", filepath.Join(sock, "tm.sock"))
	t.Setenv(caller.EnvSession, "")
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
	paths, err := server.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	c, err := newClient(paths, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := emu.New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c.vc = &ViewConn{}
	c.side = &sidebar{}
	c.v = view.View{Name: view.Main, Mode: view.ModeLayout, Focus: "s-1", Cols: 200, Rows: 50}
	c.v.Normalize()
	c.setWindow(200, 50)
	c.relayout()
	r, err := emu.NewRenderer(200, 50)
	if err != nil {
		t.Fatal(err)
	}
	c.focus = &pane{mirror: mirror, r: r, info: proto.SessionInfo{ID: "s-1"}}
	c.panes["s-1"] = c.focus
	var out countWriter
	var draws atomic.Int64
	go func() {
		for range c.wake {
			draws.Add(1)
			c.drawFrame(&out)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.pollState(ctx)
	time.Sleep(2 * time.Second) // settle
	cpu := func() time.Duration {
		var s syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &s)
		return time.Duration(s.Utime.Nano() + s.Stime.Nano())
	}
	const window = 10 * time.Second
	c0, d0, f0 := cpu(), draws.Load(), out.frames.Load()
	time.Sleep(window)
	used, d, frames := cpu()-c0, draws.Load()-d0, out.frames.Load()-f0
	t.Logf("idle console, 4 projects of 20 threads: CPU %.2f%% (%v per s), %.1f redraws/s, %d frames written",
		100*used.Seconds()/window.Seconds(), used/time.Duration(window.Seconds()), float64(d)/window.Seconds(), frames)
}
