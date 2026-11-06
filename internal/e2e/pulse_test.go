package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/gittest"
)

// seedDone writes n done tasks to the project's task list, the newest
// today, spread back over days days.
func seedDone(t *testing.T, dir string, n, days int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "---\nnext_id: %d\n---\n# Tasks\n\n", n+1)
	for i := range n {
		d := time.Now().AddDate(0, 0, -(i * days / n)).Format(time.DateOnly)
		fmt.Fprintf(&b, "### T%d Task number %d\nstatus: done · created: %s · updated: %s\npriority: should\n\ndone (%s): by hand\n\n", i+1, i+1, d, d, d)
	}
	if err := os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPulseBand: in an 80-column window the Tasks tab tops the list with
// the Pulse, from tm's records in a plain folder: bars, no dots and no word
// of releases; [ filters to the newest day with work, and ] past it ends
// the filter.
func TestPulseBand(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	seedDone(t, dir, 20, 20)

	w := env.Window(80, 30)
	w.WaitFor("SESSIONS", wait)
	w.Type("T")
	s := w.WaitFor("20 done in 20 days", wait)
	if strings.Contains(s, "each dot") || strings.Contains(s, "release") {
		t.Fatalf("a plain folder has no releases to speak of:\n%s", s)
	}
	t.Logf("the band:\n%s", s)
	w.WaitFor("[ filter by day", wait)
	w.Type("[")
	s = w.WaitFor("DONE · 1  ", wait)
	if !strings.Contains(s, "T1 ") || strings.Contains(s, "T2 ") || !strings.Contains(s, ": 1 done") {
		t.Fatalf("the newest day alone, and its numbers:\n%s", s)
	}
	t.Logf("filtered:\n%s", s)
	w.Type("]")
	w.WaitFor("[ filter by day", wait)
	w.Key(keyEsc)
	w.Quit()
	w.WaitExit(wait)
}

// TestPulseBandReleases: a repo's tag adds the dots and their words, read
// locally, with no remote.
func TestPulseBandReleases(t *testing.T) {
	env := New(t)
	repo, git := gittest.Repo(t)
	git("tag", "v1.2.0")
	var p struct{ Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("project", "new", "demo", "--repo", repo, "--json")), &p); err != nil {
		t.Fatal(err)
	}
	seedDone(t, p.Dir, 20, 20)

	w := env.Window(80, 30)
	w.WaitFor("SESSIONS", wait)
	w.Type("T")
	s := w.WaitFor("1 release", wait)
	if !strings.Contains(s, "each dot a day") || !strings.Contains(s, "•") {
		t.Fatalf("no dots:\n%s", s)
	}
	t.Logf("with a release:\n%s", s)
	w.Key(keyEsc)
	w.Quit()
	w.WaitExit(wait)
}

// TestPulseBandHidden: 9 done tasks show no band.
func TestPulseBandHidden(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	seedDone(t, dir, 9, 20)

	w := env.Window(80, 30)
	w.WaitFor("SESSIONS", wait)
	w.Type("T")
	s := w.WaitFor("9 done (b shows them)", wait)
	if strings.Contains(s, "done in") || strings.Contains(s, "filter by day") {
		t.Fatalf("a band at 9 done:\n%s", s)
	}
	w.Key(keyEsc)
	w.Quit()
	w.WaitExit(wait)
}
