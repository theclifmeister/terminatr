package mdfile

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzSplit checks that Split never panics and never loses bytes: the
// input is exactly fence + front + fence + body, both fences +++ (TOML)
// or both --- (YAML).
func FuzzSplit(f *testing.F) {
	for _, s := range []string{
		"", "# hi\n", "+++\na = 1\n+++\nbody", "+++\r\n+++\r\n", "+++", "+++\n+++", "++\n", "+++\nx\n+++ \n",
		"---\na: 1\n---\nbody", "---\r\n---\r\n", "---", "---\n---", "--\n", "---\nx\n--- \n", "---\na\n+++\n", "+++\na\n---\n+++\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		front, body, err := Split(data)
		if err != nil {
			if !bytes.HasPrefix(data, []byte(fence)) && !bytes.HasPrefix(data, []byte(yamlFence)) {
				t.Fatalf("error without an opening fence: %v", err)
			}
			return
		}
		if front == nil {
			if !bytes.Equal(body, data) {
				t.Fatalf("no front matter, but body %q != input %q", body, data)
			}
			return
		}
		if !bytes.HasSuffix(data, body) {
			t.Fatalf("body %q is not the tail of %q", body, data)
		}
		head := data[:len(data)-len(body)]
		first, rest, _ := bytes.Cut(head, []byte("\n"))
		open := string(bytes.TrimRight(first, "\r"))
		if open != fence && open != yamlFence || !bytes.HasPrefix(rest, front) {
			t.Fatalf("head %q doesn't start with fence + front %q", head, front)
		}
		closing := strings.TrimRight(strings.TrimSuffix(string(rest[len(front):]), "\n"), "\r")
		if closing != open {
			t.Fatalf("closing fence %q, opening %q", closing, open)
		}
	})
}

// FuzzDecodeYAML checks that the YAML reader never panics, never reads
// more than maxYAML bytes, and decodes the same input the same twice.
func FuzzDecodeYAML(f *testing.F) {
	for _, s := range []string{
		"title: \"CI\"\ntouches:\n  - \"a/\"\nstanding: true\ncreated: 2026-10-24T18:12:56Z\n",
		"title: CI\ntouches: [a/, \"repo:api\"]\ntags: [x]\n",
		"# only a comment\n", "~\n", "- a\n", "title: &a x\nsource: *a\n", "title: !x y\n", "<<: {title: a}\n",
		"a: &a [x, x]\nb: &b [*a, *a]\nc: [*b, *b]\n", "title: a\n...\ntitle: b\n", "title: [a\n", "next_id: 0x10\n",
		"title: !!binary Q0k=\n", "created: 2026-10-24\n", "touches: [[a]]\n", "title: {a: b}\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, front []byte) {
		var a, b yfm
		errA := decodeYAML(front, &a)
		errB := decodeYAML(front, &b)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("decoded once, not twice: %v, %v", errA, errB)
		}
		if errA != nil {
			return
		}
		if len(front) > maxYAML {
			t.Fatalf("read %d bytes", len(front))
		}
		if a.Title != b.Title || a.NextID != b.NextID || a.Standing != b.Standing || !a.Created.Equal(b.Created) || strings.Join(a.Touches, "\x00") != strings.Join(b.Touches, "\x00") {
			t.Fatalf("two decodes differ: %+v, %+v", a, b)
		}
	})
}

// FuzzJoinDecode checks that any front matter value and body survive a
// Join and a Decode unchanged.
func FuzzJoinDecode(f *testing.F) {
	f.Add("demo", "/a", "# Body\n")
	f.Add("", "", "")
	f.Add("+++", "\"quoted\"\n", "+++\nnot front\n+++\n")
	f.Add("ünï\tcode", "a\\b", "\r\n")
	f.Fuzz(func(t *testing.T, name, repo, body string) {
		data, err := Join(fm{Name: name, Repos: []string{repo}}, []byte(body))
		if err != nil {
			return // TOML can't encode it (e.g. invalid UTF-8); nothing written
		}
		var got fm
		b, err := Decode(data, &got)
		if err != nil {
			t.Fatalf("decode own output: %v\n%q", err, data)
		}
		if got.Name != name || len(got.Repos) != 1 || got.Repos[0] != repo || string(b) != body {
			t.Fatalf("lost content: %+v %q from %q %q %q", got, b, name, repo, body)
		}
	})
}
