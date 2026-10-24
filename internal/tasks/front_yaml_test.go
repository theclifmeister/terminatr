package tasks

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile/mdfiletest"
)

// TestParseYAML: a board with YAML front matter keeps its next_id and
// reads like the TOML one; a parse error's line counts the fences.
func TestParseYAML(t *testing.T) {
	mdfiletest.SameYAML(t, &front{NextID: 13})
	yamlBoard := strings.Replace(specExample, "+++\nnext_id = 13\n+++\n", "---\nnext_id: 13\n---\n", 1)
	if yamlBoard == specExample {
		t.Fatal("specExample has no TOML front matter to swap")
	}
	want, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse([]byte(yamlBoard))
	if err != nil {
		t.Fatal(err)
	}
	if got.NextID != 13 || string(got.Render()) != string(want.Render()) {
		t.Fatalf("YAML board reads %d tasks, next_id %d", len(got.Tasks), got.NextID)
	}
	if _, err := Parse([]byte("---\nnext_id: 1e9999\n---\n# Tasks\n")); err == nil {
		t.Fatal("bad next_id read")
	}
}
