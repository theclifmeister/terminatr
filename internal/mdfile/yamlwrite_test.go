package mdfile

import (
	"strings"
	"testing"
	"time"
)

// TestJoinYAML: once WriteYAML is on, Join writes YAML with every string quoted, times in UTC
// and lists as block lists, and Decode reads it back.
func TestJoinYAML(t *testing.T) {
	WriteYAML(true)
	t.Cleanup(func() { WriteYAML(false) })
	type front struct {
		Title   string    `toml:"title" yaml:"title"`
		Slug    string    `toml:"slug" yaml:"slug"`
		Touches []string  `toml:"touches,omitempty" yaml:"touches,omitempty"`
		Empty   []string  `toml:"empty" yaml:"empty"`
		Created time.Time `toml:"created" yaml:"created"`
		Due     time.Time `toml:"due,omitzero" yaml:"due,omitempty"`
		N       int       `toml:"count" yaml:"count"`
		Done    bool      `toml:"done" yaml:"done"`
	}
	in := front{Title: "a\nb: \"c\"", Slug: "yes", Touches: []string{"on", "1e3"}, Created: time.Date(2026, 10, 24, 20, 12, 56, 0, time.FixedZone("CEST", 7200)), N: 3, Done: true}
	data, err := Join(in, []byte("# Body\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntitle: \"a\\nb: \\\"c\\\"\"\nslug: \"yes\"\ntouches:\n  - \"on\"\n  - \"1e3\"\nempty: []\ncreated: 2026-10-24T18:12:56Z\ncount: 3\ndone: true\n---\n# Body\n"
	if string(data) != want {
		t.Fatalf("Join:\n%s\nwant:\n%s", data, want)
	}
	var got front
	body, err := Decode(data, &got)
	if err != nil || string(body) != "# Body\n" || got.Title != in.Title || got.Slug != "yes" || strings.Join(got.Touches, "|") != "on|1e3" ||
		!got.Created.Equal(in.Created) || !got.Due.IsZero() || got.N != 3 || !got.Done {
		t.Fatalf("Decode: %v %+v %q", err, got, body)
	}
	if _, err := Join(front{Title: "\xff"}, nil); err == nil {
		t.Fatal("invalid UTF-8 written")
	}
	if data, err := Join(struct{}{}, []byte("x")); err != nil || string(data) != "---\n---\nx" {
		t.Fatalf("empty front: %q %v", data, err)
	}
}

// TestToYAML: the migration keeps the keys' order, the full-line
// comments and the body, leaves YAML and plain files alone, and refuses
// TOML that doesn't decode.
func TestToYAML(t *testing.T) {
	in := "+++\n# Rules, not stories.\n# Each fact once.\ntitle    = \"One line\"\ntouches  = [\"path/in/repo.go\", \"repo:<name>\"]\nstanding = false\n# who\naudience = \"everyone\"\ncreated  = 2026-10-24T20:12:56+02:00\n+++\n## Decided\n+++\n"
	want := "---\n# Rules, not stories.\n# Each fact once.\ntitle: \"One line\"\ntouches:\n  - \"path/in/repo.go\"\n  - \"repo:<name>\"\nstanding: false\n# who\naudience: \"everyone\"\ncreated: 2026-10-24T18:12:56Z\n---\n## Decided\n+++\n"
	out, err := ToYAML([]byte(in))
	if err != nil || string(out) != want {
		t.Fatalf("ToYAML: %v\n%s\nwant:\n%s", err, out, want)
	}
	if !IsTOML([]byte(in)) || IsTOML(out) {
		t.Fatal("IsTOML")
	}
	for _, same := range []string{want, "# no front\n", "", "---\n---\n"} {
		if out, err := ToYAML([]byte(same)); err != nil || string(out) != same {
			t.Errorf("ToYAML(%q) = %q, %v", same, out, err)
		}
	}
	if out, err := ToYAML([]byte("+++\nnext_id = 7\n+++\n# Tasks\n")); err != nil || string(out) != "---\nnext_id: 7\n---\n# Tasks\n" {
		t.Errorf("board: %q %v", out, err)
	}
	for _, bad := range []string{"+++\ntitle = \n+++\n", "+++\na = 1\n"} {
		if _, err := ToYAML([]byte(bad)); err == nil {
			t.Errorf("ToYAML(%q) didn't fail", bad)
		}
	}
}
