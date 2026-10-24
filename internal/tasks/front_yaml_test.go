package tasks

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile/mdfiletest"
)

// TestParseYAML: a board with TOML front matter, as an older tm wrote
// it, keeps its next_id and reads like the YAML one, which Render writes.
func TestParseYAML(t *testing.T) {
	mdfiletest.SameYAML(t, &front{NextID: 13})
	tomlBoard := strings.Replace(specExample, "---\nnext_id: 13\n---\n", "+++\nnext_id = 13\n+++\n", 1)
	if tomlBoard == specExample {
		t.Fatal("specExample has no YAML front matter to swap")
	}
	want, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse([]byte(tomlBoard))
	if err != nil {
		t.Fatal(err)
	}
	if got.NextID != 13 || string(got.Render()) != specExample || string(want.Render()) != specExample {
		t.Fatalf("TOML board reads %d tasks, next_id %d", len(got.Tasks), got.NextID)
	}
	if _, err := Parse([]byte("---\nnext_id: 1e9999\n---\n# Tasks\n")); err == nil {
		t.Fatal("bad next_id read")
	}
}
