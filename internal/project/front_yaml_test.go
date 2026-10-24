package project

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile/mdfiletest"
)

// TestFrontYAML: PROJECT.md's, inbox items' and memory notes' front
// matter read the same from YAML as from TOML.
func TestFrontYAML(t *testing.T) {
	at := time.Date(2026, 10, 24, 18, 12, 56, 0, time.UTC)
	mdfiletest.SameYAML(t, &Meta{Goal: "g", Created: at, CodeHost: "azure", AzureURL: "https://dev.azure.com/o"})
	mdfiletest.SameYAML(t, &Item{ID: "i", Kind: "k", Subject: "s", Created: at, Summary: "x", NeedsUser: true, Message: "m", Thread: "t-0001"})
	mdfiletest.SameYAML(t, &NoteFront{Title: "t", Touches: []string{"a/", "repo:api"}, Standing: true, Audience: "coordinator", Source: "T1"})
}

// TestParseNoteYAML: a note Obsidian's Properties panel rewrote reads as
// the TOML one does, its own keys left alone.
func TestParseNoteYAML(t *testing.T) {
	n := ParseNote("memory/retries.md", "---\ntitle: Retries stay at 4\ntouches:\n  - internal/ticker/\n  - \"repo:api\"\nstanding: true\naudience: coordinator\ntags: [decision]\n---\n## Decided\nKeep 4.\n")
	if !n.Front || n.Title != "Retries stay at 4" || len(n.Touches) != 2 || n.Touches[1] != "repo:api" || !n.Standing || n.ForThreads() || n.Body != "## Decided\nKeep 4.\n" {
		t.Fatalf("%+v", n)
	}
	// Front matter the strict reader refuses: read as free-form text.
	bad := "---\ntitle: &a X\n---\n# Heading\n"
	if n = ParseNote("memory/x.md", bad); n.Front || n.Body != bad {
		t.Fatalf("%+v", n)
	}
}
