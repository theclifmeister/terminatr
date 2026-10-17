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
