package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestDesignCmd (T723): the design store, through tm design.
func TestDesignCmd(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"

	if out := h.ok(coord, "design", "set", "net-view", "--title", "Network view", "--task", "T3",
		"--artifact", "https://claude.ai/artifact/abc", "--answer", "Q1: yes", "--answer", "Q2: no"); out != "set design net-view\n" {
		t.Fatalf("set %q", out)
	}
	if out := h.ok(coord, "design", "set", "net-view", "--task", "T3"); out != "design net-view: nothing changed\n" {
		t.Fatalf("set again %q", out)
	}
	code, out, errs := h.run(human, "one\n\n  two  \n", "design", "set", "net-view", "--answers-file", "-")
	if code != 0 || out != "set design net-view\n" {
		t.Fatalf("--answers-file: exit %d %q %q", code, out, errs)
	}
	h.expect(1, "invalid-design", coord, "design", "set", "../x")
	h.expect(1, "invalid-design", coord, "design", "set", "x", "--task", "12")
	h.expect(1, "invalid-design", coord, "design", "set", "x", "--artifact", "http://claude.ai/x")
	h.expect(1, "invalid-design", coord, "design", "set", "x", "--artifact", "https://claude.ai/x)![a](https://evil)")
	h.expect(2, "not both", coord, "design", "set", "x", "--answer", "a", "--answers-file", "-")
	h.expect(1, "coordinator-only", thr, "design", "set", "x")

	if out := h.ok(thr, "design", "list"); out != "net-view · designing · Network view · T3 · 2 answers · https://claude.ai/artifact/abc\n" {
		t.Fatalf("list %q", out)
	}
	var ds []project.Design
	if err := json.Unmarshal([]byte(h.ok(coord, "design", "list", "--json")), &ds); err != nil || len(ds) != 1 || ds[0].Answers[1] != "two" {
		t.Fatalf("list --json %v %v", ds, err)
	}
	// "" clears a field.
	h.ok(coord, "design", "set", "net-view", "--artifact", "", "--answer", "")
	ds = nil
	if err := json.Unmarshal([]byte(h.ok(coord, "design", "list", "--json")), &ds); err != nil || ds[0].Artifact != "" || ds[0].Answers != nil {
		t.Fatalf("cleared %v %v", ds, err)
	}
}

// TestDesignListArchive (T872): a done design is listed only with
// --all, an archived one only with --archived; archive and unarchive
// are the coordinator's, idempotent, and take a design only a task
// names too.
func TestDesignListArchive(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"
	h.ok(coord, "task", "add", "Build it")
	h.ok(coord, "task", "add", "Other")
	h.ok(coord, "task", "edit", "T1", "--design", "shipped")
	h.ok(coord, "task", "edit", "T2", "--design", "open-one")
	h.ok(human, "task", "status", "T1", "done")
	h.ok(coord, "design", "set", "recorded")

	names := func(args ...string) string {
		t.Helper()
		var ds []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Archived bool   `json:"archived"`
		}
		if err := json.Unmarshal([]byte(h.ok(coord, append([]string{"design", "list", "--json"}, args...)...)), &ds); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range ds {
			w := d.Name + ":" + d.Status
			if d.Archived {
				w += ":archived"
			}
			out = append(out, w)
		}
		return strings.Join(out, " ")
	}
	if got := names(); got != "open-one:planned recorded:designing" {
		t.Fatalf("list %q", got)
	}
	if got := names("--all"); got != "open-one:planned recorded:designing shipped:done" {
		t.Fatalf("list --all %q", got)
	}
	h.expect(1, "coordinator-only", thr, "design", "archive", "open-one")
	h.expect(1, "unknown-design", coord, "design", "archive", "nope")
	if out := h.ok(coord, "design", "archive", "open-one"); out != "archived design open-one\n" {
		t.Fatalf("archive %q", out)
	}
	if out := h.ok(coord, "design", "archive", "open-one"); out != "design open-one: already archived\n" {
		t.Fatalf("archive again %q", out)
	}
	h.ok(coord, "design", "archive", "shipped")
	if got := names(); got != "recorded:designing" {
		t.Fatalf("list after archive %q", got)
	}
	if got := names("--archived"); got != "open-one:planned:archived recorded:designing shipped:done:archived" {
		t.Fatalf("list --archived %q", got)
	}
	if out := h.ok(coord, "design", "list", "--archived"); !strings.Contains(out, "open-one · planned · archived · 0 answers\n") {
		t.Fatalf("list --archived text %q", out)
	}
	h.ok(coord, "design", "unarchive", "open-one")
	if got := names(); got != "open-one:planned recorded:designing" {
		t.Fatalf("list after unarchive %q", got)
	}
}
