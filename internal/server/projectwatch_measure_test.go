package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestProjectWatchIdleCost measures an idle project watch: CPU and git
// execs per second with 4 watchers on a project of 3 repos and 20
// threads, at the default poll. TERMINATR_MEASURE=1 runs it.
func TestProjectWatchIdleCost(t *testing.T) {
	if os.Getenv("TERMINATR_MEASURE") == "" {
		t.Skip("TERMINATR_MEASURE=1 measures")
	}
	paths := testPaths(t)
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "git.log")
	real, _ := exec.LookPath("git")
	os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho x >> "+log+"\nexec "+real+" \"$@\"\n"), 0o755)
	var repos []string
	for i := range 3 {
		r := t.TempDir()
		exec.Command(real, "-C", r, "init", "-q").Run()
		exec.Command(real, "-C", r, "remote", "add", "origin", fmt.Sprintf("https://github.com/o/r%d.git", i)).Run()
		repos = append(repos, r)
	}
	p, err := project.New(project.Options{Slug: "demo", Repos: repos})
	if err != nil {
		t.Fatal(err)
	}
	var nt []tasks.NewTask
	for i := range 20 {
		nt = append(nt, tasks.NewTask{Title: fmt.Sprintf("task %d", i), Status: "started", Steps: []string{"a", "b"}})
	}
	if _, err := p.Tasks().Add(human, nt); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		if _, err := thread.Create(p, thread.Record{Title: "work", Task: fmt.Sprintf("T%d", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	startServer(t, paths)
	for range 4 {
		st, _, err := WatchProject(paths, p.Slug)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
	}
	time.Sleep(1500 * time.Millisecond) // settle
	os.Remove(log)
	cpu := func() time.Duration {
		var s, c syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &s)
		syscall.Getrusage(syscall.RUSAGE_CHILDREN, &c)
		tv := func(v syscall.Timeval) time.Duration { return time.Duration(v.Nano()) }
		return tv(s.Utime) + tv(s.Stime) + tv(c.Utime) + tv(c.Stime)
	}
	const window = 10 * time.Second
	c0 := cpu()
	time.Sleep(window)
	used := cpu() - c0
	b, _ := os.ReadFile(log)
	execs := strings.Count(string(b), "x")
	t.Logf("idle, 4 watchers, 3 repos, 20 threads: CPU %.1f%% (%v per s), git execs %.1f/s",
		100*used.Seconds()/window.Seconds(), used/time.Duration(window.Seconds()), float64(execs)/window.Seconds())
}
