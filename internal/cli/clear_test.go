package cli

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// TestClearIsCoordinators: tm clear is the human's and the coordinator's;
// a thread is refused before the server is asked, and a stray argument
// is a usage error.
func TestClearIsCoordinators(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"
	h.expect(1, "coordinator-only", thr, "clear")
	h.expect(2, "usage: tm clear", coord, "clear", "extra")
	if out := h.ok(human, "clear", "--help"); out == "" {
		t.Error("no help")
	}
}

// TestClearProgram: tm clear --program is the human's and the program's
// director's; a director's plain tm clear names its own program, another
// program is refused, as are a coordinator and both flags at once.
func TestClearProgram(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	dir := caller.Caller{Kind: caller.Director, Project: "acme"}
	h.expect(1, "other-program", dir, "clear", "--program", "beta")
	h.expect(1, "director-only", coord, "clear", "--program", "acme")
	h.expect(2, "usage: tm clear", human, "clear", "--project", "demo", "--program", "acme")
}
