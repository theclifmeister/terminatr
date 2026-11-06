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
	"github.com/theclifmeister/terminatr/internal/modaction"
)

// openHistory opens the Tasks tab's History at 80 columns and waits for what.
func openHistory(t *testing.T, env *Env, what string) (*Window, string) {
	t.Helper()
	w := env.Window(80, 40)
	w.WaitFor("SESSIONS", wait)
	w.Type("T")
	w.WaitFor("h history", wait)
	w.Type("h")
	return w, w.WaitFor(what, wait)
}

func closeHistory(w *Window) {
	w.Key(keyEsc)
	w.Quit()
	w.WaitExit(wait)
}

// TestHistoryPlain: a plain folder with no designs and no tags has the curve,
// its days and the facts, and not a word of releases or chapters; h goes back.
func TestHistoryPlain(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	seedDone(t, dir, 20, 20)
	w, s := openHistory(t, env, "back to the board")
	t.Logf("plain:\n%s", s)
	for _, want := range []string{"HISTORY", "20 days", "20 done · 0 open", "best day"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q:\n%s", want, s)
		}
	}
	for _, not := range []string{"release", " rel ", "CHAPTERS"} {
		if strings.Contains(s, not) {
			t.Errorf("%q in a plain folder:\n%s", not, s)
		}
	}
	w.Type("h")
	w.WaitFor("h history", wait)
	closeHistory(w)
}

// TestHistoryThreeTasks: a project too young for the Pulse has a way in, and
// the curve with what it has.
func TestHistoryThreeTasks(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	seedDone(t, dir, 3, 3)
	w, s := openHistory(t, env, "back to the board")
	t.Logf("3 tasks:\n%s", s)
	if !strings.Contains(s, "3 done · 0 open") || !strings.Contains(s, "3 days") {
		t.Errorf("the curve with what it has:\n%s", s)
	}
	closeHistory(w)
}

// TestHistoryByMonth: a quiet project over two years goes by month.
func TestHistoryByMonth(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	seedDone(t, dir, 14, 800)
	w, s := openHistory(t, env, "per month")
	t.Logf("by month:\n%s", s)
	if !strings.Contains(s, "2 years") || !strings.Contains(s, "best month") {
		t.Errorf("by month:\n%s", s)
	}
	closeHistory(w)
}

// TestHistoryRepos: several repos with tags have a tick row each, named, and
// their counts listed, never added up.
func TestHistoryRepos(t *testing.T) {
	env := New(t)
	one, git1 := gittest.Repo(t)
	git1("tag", "v1.2.0")
	two, git2 := gittest.Repo(t)
	git2("tag", "v2.0.0")
	var p struct{ Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("project", "new", "demo", "--repo", one, "--repo", two, "--json")), &p); err != nil {
		t.Fatal(err)
	}
	seedDone(t, p.Dir, 12, 12)
	w, s := openHistory(t, env, "back to the board")
	t.Logf("two repos:\n%s", s)
	if !strings.Contains(s, " 1 · ") || !strings.Contains(s, "1 release") || strings.Contains(s, "2 releases") {
		t.Errorf("the counts, each its own:\n%s", s)
	}
	if strings.Count(s, "•") < 2 {
		t.Errorf("a tick row each:\n%s", s)
	}
	closeHistory(w)
}

// TestHistoryChapters: a design is a chapter with its progress, one with all
// its tasks open too, and enter on it opens it.
func TestHistoryChapters(t *testing.T) {
	env := New(t)
	_, dir := newProject(env, "demo")
	var b strings.Builder
	fmt.Fprintf(&b, "---\nnext_id: 13\n---\n# Tasks\n\n")
	for i := range 10 {
		d := time.Now().AddDate(0, 0, -i).Format(time.DateOnly)
		design := ""
		if i < 3 {
			design = " · design: built"
		}
		fmt.Fprintf(&b, "### T%d Task number %d\nstatus: done%s · created: %s · updated: %s\npriority: should\n\ndone (%s): by hand\n\n", i+1, i+1, design, d, d, d)
	}
	for i := 10; i < 12; i++ {
		d := time.Now().Format(time.DateOnly)
		fmt.Fprintf(&b, "### T%d Task number %d\nstatus: open · design: ahead · created: %s · updated: %s\npriority: should\n\n", i+1, i+1, d, d)
	}
	if err := os.WriteFile(filepath.Join(dir, "TASKS.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	w, s := openHistory(t, env, "CHAPTERS")
	t.Logf("chapters:\n%s", s)
	for _, want := range []string{"3 of 10 done tasks belong to one", "ahead", "0/2", "built", "3/3", "enter opens a chapter"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q:\n%s", want, s)
		}
	}
	// A row's bindings count once they have stood still (modaction.Stable).
	time.Sleep(modaction.Stable + 200*time.Millisecond)
	w.Key(keyEnter)
	s = w.WaitFor("T11", wait)
	t.Logf("a chapter opened:\n%s", s)
	closeHistory(w)
}
