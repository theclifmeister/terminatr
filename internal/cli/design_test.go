package cli

import (
	"encoding/json"
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

	if out := h.ok(thr, "design", "list"); out != "net-view · Network view · T3 · 2 answers · https://claude.ai/artifact/abc\n" {
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
