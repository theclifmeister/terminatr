package ticker

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
)

func TestRenameProjectState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ticker.json")
	if err := RenameProjectState(path, "a", "b"); err != nil {
		t.Fatalf("no file: %v", err)
	}
	os.WriteFile(path, []byte(`{"threads":{"a/t-0001":{"reports":3},"ab/t-0001":{"reports":1}},"projects":{"a":{"completed":{"T1":"c1"}},"ab":{}}}`), 0o600)
	if err := RenameProjectState(path, "a", "b"); err != nil {
		t.Fatal(err)
	}
	tk := New(Options{State: path})
	st := tk.st
	if st.Threads["b/t-0001"] == nil || st.Threads["b/t-0001"].Reports != 3 || st.Threads["a/t-0001"] != nil || st.Threads["ab/t-0001"] == nil {
		t.Fatalf("threads %+v", st.Threads)
	}
	if st.Projects["b"] == nil || st.Projects["b"].Completed["T1"] != "c1" || st.Projects["a"] != nil || st.Projects["ab"] == nil {
		t.Fatalf("projects %+v", st.Projects)
	}

	// In a running ticker, between sweeps; a failed move keeps the memos.
	if err := tk.RenameProject("b", "c", func() error { return os.ErrExist }); err == nil || tk.st.Projects["b"] == nil {
		t.Fatalf("failed move: %v %+v", err, tk.st.Projects)
	}
	if err := tk.RenameProject("b", "c", func() error { return nil }); err != nil || tk.st.Projects["c"] == nil || tk.st.Threads["c/t-0001"] == nil {
		t.Fatalf("move: %v %+v", err, tk.st)
	}
}

// TestRenameDuringSlowSweep: a rename doesn't wait for a sweep stuck on
// another project's code host (PERF-07); renaming the project the sweep
// works on waits for that project only, and its memos survive the
// sweep.
func TestRenameDuringSlowSweep(t *testing.T) {
	r := newRig(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	let := func() { releaseOnce.Do(func() { close(release) }) }
	run := r.run
	r.run = func(dir string, args ...string) ([]byte, error) {
		once.Do(func() {
			close(entered)
			<-release
		})
		return run(dir, args...)
	}
	// Every goroutine ends before the test's folders go, failed or not:
	// this cleanup runs before t.TempDir's.
	var wg sync.WaitGroup
	t.Cleanup(func() {
		let()
		wg.Wait()
	})
	goT := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}
	swept := make(chan struct{})
	goT(func() {
		r.tk.Sweep()
		close(swept)
	})
	<-entered // the sweep waits on gh, in demo

	done := make(chan error, 1)
	goT(func() { done <- r.tk.RenameProject("other", "other2", func() error { return nil }) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renaming another project waited for the sweep")
	}

	moved := make(chan error, 1)
	goT(func() { moved <- r.tk.RenameProject("demo", "demo2", func() error { return nil }) })
	select {
	case <-moved:
		t.Fatal("demo was renamed while its sweep ran")
	case <-time.After(100 * time.Millisecond):
	}
	let()
	if err := <-moved; err != nil {
		t.Fatal(err)
	}
	<-swept
	wg.Wait()
	r.tk.mu.Lock()
	defer r.tk.mu.Unlock()
	if r.tk.st.Threads["demo2/t-0001"] == nil || r.tk.st.Threads["demo/t-0001"] != nil {
		t.Fatalf("threads %+v", r.tk.st.Threads)
	}
}
