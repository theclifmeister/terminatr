package server

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestServerStopsWhenHomeIsGone: a server whose home was removed stops on
// its own (T667).
func TestServerStopsWhenHomeIsGone(t *testing.T) {
	for _, dir := range []string{"home", "run dir"} {
		t.Run(dir, func(t *testing.T) {
			if dir == "run dir" {
				useTestHooks(t)
			}
			goneEvery = 20 * time.Millisecond
			t.Cleanup(func() { goneEvery = 5 * time.Second })
			p := testPaths(t)
			r := startServer(t, p)
			d := p.Home
			if dir == "run dir" {
				d = p.RunDir
			}
			// Moved, not removed: the server may be writing in it.
			if err := os.Rename(d, d+".gone"); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-r.done:
				if err != nil {
					t.Errorf("Run: %v", err)
				}
				r.done <- nil
			case <-time.After(10 * time.Second):
				out, _ := exec.Command("find", d).CombinedOutput()
				t.Fatalf("server still running 10s after its %s was moved away:\n%s", dir, out)
			}
		})
	}
}

// TestServerKeepsItsRunDir: without the test hooks a removed run dir
// doesn't stop the server (the system may clean /tmp under it).
func TestServerKeepsItsRunDir(t *testing.T) {
	if testHooksBuilt {
		t.Skip("built with testhooks")
	}
	goneEvery = 20 * time.Millisecond
	t.Cleanup(func() { goneEvery = 5 * time.Second })
	p := testPaths(t)
	r := startServer(t, p)
	if err := os.Rename(p.RunDir, p.RunDir+".gone"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-r.done:
		r.done <- err
		t.Fatalf("server stopped when its run dir was removed: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}
