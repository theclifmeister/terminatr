package tui

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// A paired server's pane marks that server's project as current, not a
// local project of the same name.
func TestSideMarkPairedSameName(t *testing.T) {
	c := &client{v: view.View{Current: "demo"}}
	if got := c.sideMark(); got != "demo" {
		t.Fatalf("no focus: %q", got)
	}
	c.focus = &pane{info: proto.SessionInfo{ID: "lab/s1", Project: "demo", Server: "lab"}}
	if got := c.sideMark(); got != "@lab/demo" {
		t.Fatalf("paired pane marks %q, want @lab/demo", got)
	}
	if got := c.sideCurrent(); got != "demo" {
		t.Fatalf("band project %q, want the local slug", got)
	}
	c.focus = &pane{info: proto.SessionInfo{ID: "s2", Project: "demo"}}
	if got := c.sideMark(); got != "demo" {
		t.Fatalf("local pane marks %q", got)
	}
}
