package mdfile

import (
	"strings"
	"testing"
	"time"
)

type yfm struct {
	Title    string    `toml:"title" yaml:"title"`
	Touches  []string  `toml:"touches" yaml:"touches"`
	Standing bool      `toml:"standing" yaml:"standing"`
	Created  time.Time `toml:"created" yaml:"created"`
	NextID   int       `toml:"next_id" yaml:"next_id"`
}

// TestDecodeYAML: YAML front matter reads into the same struct as TOML,
// as tm will write it and as Obsidian's Properties panel rewrites it.
func TestDecodeYAML(t *testing.T) {
	created := time.Date(2026, 10, 24, 18, 12, 56, 0, time.UTC)
	want := yfm{Title: "CI jobs", Touches: []string{".github/workflows/", "internal/e2e/"}, Standing: true, Created: created, NextID: 716}
	for _, in := range []string{
		"+++\ntitle = \"CI jobs\"\ntouches = [\".github/workflows/\", \"internal/e2e/\"]\nstanding = true\ncreated = 2026-10-24T18:12:56Z\nnext_id = 716\n+++\nbody\n",
		"---\ntitle: \"CI jobs\"\ntouches:\n  - \".github/workflows/\"\n  - \"internal/e2e/\"\nstanding: true\ncreated: \"2026-10-24T18:12:56Z\"\nnext_id: 716\n---\nbody\n",
		// Obsidian: unquoted strings, flow lists, its own keys, comments.
		"---\n# tm's note\ntitle: CI jobs\ntouches: [.github/workflows/, internal/e2e/]\nstanding: true\ncreated: 2026-10-24T18:12:56Z\nnext_id: 716\ntags: [decision, gotcha]\ncssclasses:\n  - tm-owned\naliases: []\n---\nbody\n",
	} {
		var got yfm
		body, err := Decode([]byte(in), &got)
		if err != nil {
			t.Errorf("Decode(%q): %v", in, err)
			continue
		}
		if !got.Created.Equal(want.Created) || got.Title != want.Title || strings.Join(got.Touches, "|") != strings.Join(want.Touches, "|") ||
			got.Standing != want.Standing || got.NextID != want.NextID || string(body) != "body\n" {
			t.Errorf("Decode(%q) = %+v %q, want %+v", in, got, body, want)
		}
	}
}

// TestDecodeYAMLStrings: a quoted or YAML 1.2 plain word stays a string,
// so a slug like yes or on never turns into a boolean.
func TestDecodeYAMLStrings(t *testing.T) {
	for in, want := range map[string]string{"yes": "yes", "on": "on", "no": "no", `"1e3"`: "1e3", "0x10": "0x10", `""`: "", `"a #b"`: "a #b"} {
		var got yfm
		if _, err := Decode([]byte("---\ntitle: "+in+"\n---\n"), &got); err != nil || got.Title != want {
			t.Errorf("title: %s = %q, %v; want %q", in, got.Title, err, want)
		}
	}
}

// TestDecodeYAMLRefuses: what the strict reader refuses, with the reason.
func TestDecodeYAMLRefuses(t *testing.T) {
	big := "---\ntitle: \"" + strings.Repeat("x", maxYAML) + "\"\n---\n"
	cases := map[string]string{
		"anchor":         "---\ntitle: &a CI\n---\n",
		"alias":          "---\nx: &a CI\ntitle: *a\n---\n",
		"alias only":     "---\ntitle: *a\n---\n",
		"custom tag":     "---\ntitle: !secret CI\n---\n",
		"binary":         "---\ntitle: !!binary Q0k=\n---\n",
		"merge":          "---\n<<: {title: CI}\n---\n",
		"two documents":  "---\ntitle: CI\n...\n--- \nnext_id: 1\n---\n",
		"not a mapping":  "---\n- CI\n---\n",
		"a scalar":       "---\nCI\n---\n",
		"duplicate keys": "---\ntitle: a\ntitle: b\n---\n",
		"wrong type":     "---\nnext_id: many\n---\n",
		"too big":        big,
		"bad syntax":     "---\ntitle: [a\n---\n",
	}
	for name, in := range cases {
		var got yfm
		if _, err := Decode([]byte(in), &got); err == nil {
			t.Errorf("%s: decoded %+v from %.80q", name, got, in)
		} else if !strings.HasPrefix(err.Error(), "front matter: ") {
			t.Errorf("%s: error %q", name, err)
		}
	}
}

// TestDecodeYAMLEmpty: front matter with nothing in it, or only
// comments, decodes to nothing.
func TestDecodeYAMLEmpty(t *testing.T) {
	for _, in := range []string{"---\n---\nbody", "---\n# a comment\n\n---\nbody", "---\n~\n---\nbody"} {
		got := yfm{Title: "kept"}
		body, err := Decode([]byte(in), &got)
		if err != nil || got.Title != "kept" || string(body) != "body" {
			t.Errorf("Decode(%q) = %+v %q %v", in, got, body, err)
		}
	}
}
