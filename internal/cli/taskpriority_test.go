package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

func TestTaskPriority(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"

	h.ok(coord, "task", "add", "Plain", "--status", "ready")
	h.ok(coord, "task", "add", "Nice", "--status", "ready", "--priority", "could")
	h.ok(coord, "task", "add", "Idea", "--priority", "must")
	h.expect(1, "invalid-priority", coord, "task", "add", "Bad", "--priority", "urgent")
	code, out, _ := h.run(coord, `[{"title":"Planned","status":"ready","priority":"should"}]`, "task", "add", "--json")
	if code != 0 || !strings.Contains(out, `"created"`) {
		t.Fatalf("plan: exit %d %s", code, out)
	}
	h.ok(coord, "task", "edit", "T1", "--priority", "must")
	h.expect(1, "invalid-priority", coord, "task", "edit", "T1", "--priority", "high")

	want := "ON DECK\n  T1   ready    must    Plain\n  T4   ready    should  Planned\n  T2   ready    could   Nice\nBACKLOG\n  T3   open     must    Idea\n"
	if list := h.ok(coord, "task", "list"); list != want {
		t.Fatalf("list:\n%s\nwant:\n%s", list, want)
	}
	if list := h.ok(coord, "task", "list", "--priority", "must,could"); !strings.Contains(list, "T1") || !strings.Contains(list, "T2") || strings.Contains(list, "T4") {
		t.Fatalf("filtered:\n%s", list)
	}
	h.expect(2, "unknown priority", coord, "task", "list", "--priority", "high")
	if show := h.ok(coord, "task", "show", "T1"); !strings.Contains(show, "status: ready · priority: must") {
		t.Fatalf("show:\n%s", show)
	}
	var tj []map[string]any
	json.Unmarshal([]byte(h.ok(coord, "task", "list", "--json")), &tj)
	if len(tj) != 4 || tj[0]["priority"] != "must" || tj[2]["priority"] != "could" {
		t.Fatalf("json %v", tj)
	}
	h.ok(coord, "task", "edit", "T1", "--priority", "none")
	if list := h.ok(coord, "task", "list", "--priority", "none"); !strings.Contains(list, "T1") || strings.Contains(list, "T2") {
		t.Fatalf("none:\n%s", list)
	}
}
