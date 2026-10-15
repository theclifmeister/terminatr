package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestProjectCacheSeesThreadMidCreate: a look between thread.Create's
// mkdir and its thread.toml lists no thread; the record landing after
// must still make the next look read again, though it changes only the
// thread's folder, not the threads folder (T457).
func TestProjectCacheSeesThreadMidCreate(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	rec, err := thread.Create(p, thread.Record{Title: "work"})
	if err != nil {
		t.Fatal(err)
	}
	path := thread.Path(p, rec.ID, "thread.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var c projectCache
	ts := filepath.Join(t.TempDir(), "ticker.json")
	for range 2 { // the first read has no stamp and reads once more
		snap, err := c.get(p.Slug, ts, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.recs) != 0 {
			t.Fatalf("a folder without a record: threads %d, want 0", len(snap.recs))
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := c.get(p.Slug, ts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.recs) != 1 || snap.recs[0].ID != rec.ID {
		t.Fatalf("after the record landed: threads %d, want %s", len(snap.recs), rec.ID)
	}
}
