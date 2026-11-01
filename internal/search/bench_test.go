package search

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestBenchProject times the index on a real project, read only, the
// index in a temp folder: TM_SEARCH_PROJECT=~/.terminatr/projects/<slug>
// go test ./internal/search -run BenchProject -v. Skipped without it.
func TestBenchProject(t *testing.T) {
	dir := os.Getenv("TM_SEARCH_PROJECT")
	if dir == "" {
		t.Skip("TM_SEARCH_PROJECT not set")
	}
	idx := t.TempDir()
	x := Open(dir, idx)
	st, err := x.Update()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first build: %v, %d files, %d chunks, %d terms, %d segments, %d errors", st.Took, st.Files, st.Chunks, st.Terms, st.Segments, len(st.Errors))
	for _, e := range st.Errors[:min(len(st.Errors), 5)] {
		t.Logf("  error: %s", e)
	}
	start := time.Now()
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("save: %v, %s on disk", time.Since(start), du(t, idx))
	start = time.Now()
	x = Open(dir, idx)
	st, err = x.Update()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cold load + update: %v (read %d)", time.Since(start), st.Read)
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	t.Logf("heap in use with the index loaded: %s", formatMB(int64(ms.HeapInuse)))
	for _, q := range []string{"additionalContext cap", "libghostty", "flaky TestColors OSC 11 timeout", "App Store review notes", "azure devops sign in fallback", "the"} {
		var took []time.Duration
		var r Result
		for range 5 {
			s := time.Now()
			r, err = x.Search(Query{Words: q, Coordinator: true, Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			took = append(took, time.Since(s))
		}
		t.Logf("%-35q %d hits, warm %v (update %v); first: %v", q, r.Total, took[len(took)-1], r.Stats.Took, first(r))
	}
}

func first(r Result) string {
	if len(r.Hits) == 0 {
		return "-"
	}
	h := r.Hits[0]
	return h.ID + " " + h.Title
}

func du(t *testing.T, dir string) string {
	ents, _ := os.ReadDir(dir)
	var n int64
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			n += fi.Size()
		}
	}
	return formatMB(n)
}

func formatMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) }
