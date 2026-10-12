package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestModNewAndTest(t *testing.T) {
	h := newHarness(t)
	// An agent may scaffold and test a mod: it writes only there.
	out := h.ok(thr, "mod", "new", "my-col", "--kind", "column")
	for _, want := range []string{"made my-col, a column mod", "types/tm.d.ts", "tests/waiting.json", "tm mod test"} {
		if !strings.Contains(out, want) {
			t.Errorf("new output lacks %q:\n%s", want, out)
		}
	}
	dir := filepath.Join(h.cwd, "my-col")
	h.expect(1, "isn't empty", human, "mod", "new", "my-col", "--dir", dir)
	h.expect(2, "no kind", human, "mod", "new", "x", "--kind", "widget")
	h.ok(human, "mod", "new", "my-theme", "--kind", "theme")

	out = h.ok(thr, "mod", "test", "my-col", "my-theme")
	for _, want := range []string{"ok    my-col tests/waiting.json  a thread that needs you shows red", "my-theme  a data mod"} {
		if !strings.Contains(out, want) {
			t.Errorf("test output lacks %q:\n%s", want, out)
		}
	}
	// A fixture that fails exits 1 and shows the calls made.
	bad := `{"events":[{"name":"thread.state","project":"p","data":{"project":"p","thread":"t-1","state":"idle"}}],"expect":{"calls":[{"method":"ui.toast"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "tests", "bad.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := h.run(human, "", "mod", "test", dir)
	if code != 1 || !strings.Contains(out, "FAIL  my-col tests/bad.json") || !strings.Contains(out, "no call ui.toast()") ||
		!strings.Contains(out, `ui.cell.set {"column":"state"`) {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	code, out, _ = h.run(human, "", "mod", "test", dir, "--json")
	var rows []testRow
	if code != 1 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 || rows[0].OK || len(rows[0].Results) != 2 {
		t.Fatalf("--json: exit %d %s", code, out)
	}
	code, out, _ = h.run(human, "", "mod", "test", t.TempDir())
	if code != 1 || !strings.Contains(out, "FAIL") {
		t.Fatalf("no mod: exit %d %s", code, out)
	}
}

func TestModLogs(t *testing.T) {
	h := newHarness(t)
	code, _, errs := h.run(human, "", "mod", "logs", "age")
	if code != 0 || !strings.Contains(errs, "no log yet") {
		t.Fatalf("no log: %d %q", code, errs)
	}
	h.expect(2, "", human, "mod", "logs", "Bad_Id")
	dir, err := home.ModLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "age.log"), []byte("1\n2\n3\n"), 0o600)
	if out := h.ok(coord, "mod", "logs", "age", "-n", "2"); out != "2\n3\n" {
		t.Fatalf("logs -n 2: %q", out)
	}
	h.expect(2, "takes a number", human, "mod", "logs", "age", "--lines", "x")
}

func TestModDev(t *testing.T) {
	h := newHarness(t)
	withTTY(t)
	h.ok(human, "mod", "new", "my-hook")
	dir := filepath.Join(h.cwd, "my-hook")
	h.expect(1, "human-only", thr, "mod", "dev", dir, "--yes")

	restarts := make(chan string, 10)
	oldRestart, oldInt, oldEvery := restartMod, interrupted, devEvery
	restartMod = func(id string) (proto.ModHostsResult, error) {
		restarts <- id
		return proto.ModHostsResult{Hosts: []proto.ModHost{{ID: id, State: "running"}}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	interrupted = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
	devEvery = 5 * time.Millisecond
	t.Cleanup(func() { restartMod, interrupted, devEvery = oldRestart, oldInt, oldEvery })

	type ran struct {
		code      int
		out, errs string
	}
	done := make(chan ran, 1)
	go func() {
		code, out, errs := h.run(human, "y\n", "mod", "dev", dir)
		done <- ran{code, out, errs}
	}()
	next := func(what string) {
		t.Helper()
		select {
		case id := <-restarts:
			if id != "my-hook" {
				t.Fatalf("restarted %q", id)
			}
		case r := <-done:
			t.Fatalf("dev ended before %s: %+v", what, r)
		case <-time.After(5 * time.Second):
			t.Fatalf("no restart: %s", what)
		}
	}
	next("the start")
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "● my-hook") {
		t.Errorf("not enabled while dev runs:\n%s", out)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte("$.on(\"clock.minute\", () => {});\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next("a save")
	cancel()
	r := <-done
	if r.code != 0 {
		t.Fatalf("dev: exit %d %s %s", r.code, r.out, r.errs)
	}
	for _, want := range []string{"linked my-hook to", "My hook", "── my-hook: restarted (running)", "my-hook disabled; it stays linked"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("dev output lacks %q:\n%s", want, r.out)
		}
	}
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "○ my-hook") {
		t.Errorf("still on after dev:\n%s", out)
	}
	// The link it left is found again (a junction on Windows).
	real, _ := filepath.EvalSymlinks(dir)
	if made, err := devLink("my-hook", real); made || err != nil {
		t.Fatalf("devLink again: made %v, %v", made, err)
	}
	// Another folder with the same id is refused; remove unlinks, keeping
	// the folder.
	other := filepath.Join(h.cwd, "other")
	h.ok(human, "mod", "new", "my-hook", "--dir", other)
	h.expect(1, "is linked to", human, "mod", "dev", other, "--yes")
	h.ok(human, "mod", "remove", "my-hook", "--yes")
	if _, err := os.Stat(filepath.Join(dir, "main.ts")); err != nil {
		t.Fatalf("remove touched the work folder: %v", err)
	}
}
