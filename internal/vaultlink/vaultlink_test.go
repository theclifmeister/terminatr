package vaultlink

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// on turns links on for a test: they are written only once front
// matter is YAML.
func on(t *testing.T) {
	t.Helper()
	was := mdfile.WritesYAML()
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(was) })
}

func TestLink(t *testing.T) {
	on(t)
	for _, c := range []struct {
		k         Kind
		id, alias string
		want      string
	}{
		{Task, "T709", "", "[[terminatr/tasks/T709|T709]]"},
		{Thread, "t-0651", "", "[[terminatr/threads/t-0651/t-0651|t-0651]]"},
		{ThreadFile, "t-0651/task.md", "task.md", "[[terminatr/threads/t-0651/task|task.md]]"},
		{Memory, "memory/ci.md", "CI jobs", "[[terminatr/memory/ci|CI jobs]]"},
		{Memory, "ci", "", "[[terminatr/memory/ci|ci]]"},
		{Message, "foodperfect/M-16", "M-16", "[[terminatr/messages/foodperfect/M-16|M-16]]"},
		{Message, "mini/api/M-1", "M-1", "[[terminatr/messages/@mini/api/M-1|M-1]]"},
		{Project, "foodperfect", "", "[[foodperfect/PROJECT|foodperfect]]"},
		{Project, "mini/api", "", "[[_peers/mini/api|mini/api]]"},
		// An alias can't end or split the link.
		{Memory, "x", "a [b] | c\nd", "[[terminatr/memory/x|a (b) / c d]]"},
		// Ids that can't be a note stay plain.
		{Task, "../T1", "T1", "T1"},
		{Thread, "a/b", "", "a/b"},
		{Memory, "memory/", "m", "m"},
		{Task, "T1|x", "T1", "T1"},
		{Task, "", "x", "x"},
		{Message, "M-1", "M-1", "M-1"},
	} {
		if got := Link("terminatr", c.k, c.id, c.alias); got != c.want {
			t.Errorf("Link(%v, %q, %q) = %q, want %q", c.k, c.id, c.alias, got, c.want)
		}
	}
	if got := Link("", Task, "T1", ""); got != "T1" {
		t.Errorf("no slug: %q", got)
	}
}

// TestLinkOff: until front matter is YAML, Link writes the alias alone.
func TestLinkOff(t *testing.T) {
	mdfile.WriteYAML(false)
	if got := Link("terminatr", Task, "T709", ""); got != "T709" {
		t.Errorf("got %q", got)
	}
	if got := Ref("terminatr", "t-0001"); got != "t-0001" {
		t.Errorf("got %q", got)
	}
}

func TestRef(t *testing.T) {
	on(t)
	for in, want := range map[string]string{
		"T12":      "[[p/tasks/T12|T12]]",
		"t-0001":   "[[p/threads/t-0001/t-0001|t-0001]]",
		"T012":     "T012",
		"mini/api": "mini/api",
		"":         "",
	} {
		if got := Ref("p", in); got != want {
			t.Errorf("Ref(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlain(t *testing.T) {
	for in, want := range map[string]string{
		"t-0001":                              "t-0001",
		"[[terminatr/tasks/T709|T709]]":       "T709",
		"[[terminatr/tasks/T709]]":            "T709",
		"[[terminatr/tasks/T709#Steps]]":      "T709",
		"[[terminatr/tasks/T709|]]":           "T709",
		"status: done · thread: [[a/b|t-1]]":  "status: done · thread: t-1",
		"- [[a/memory/x|Title]]: holds [[y]]": "- Title: holds y",
		"[[unclosed":                          "[[unclosed",
		"[[a|b]] and [[c|d]]":                 "b and d",
	} {
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPlainLink: what Link writes, Plain reads back as the alias.
func TestPlainLink(t *testing.T) {
	on(t)
	for _, id := range []string{"T1", "t-0001"} {
		if got := Plain(Ref("p", id)); got != id {
			t.Errorf("%s: %q", id, got)
		}
	}
}

func TestRename(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[[old/tasks/T1|T1]]", "[[new/tasks/T1|T1]]"},
		{"[[old/PROJECT|old]]", "[[new/PROJECT|new]]"},
		{"[[old/PROJECT|The old one]]", "[[new/PROJECT|The old one]]"},
		{"[[other/messages/old/M-1|M-1]]", "[[other/messages/new/M-1|M-1]]"},
		{"[[old/messages/old/M-1|M-1]]", "[[new/messages/new/M-1|M-1]]"},
		{"[[old/tasks/T1]]", "[[new/tasks/T1]]"},
		{"[[old/tasks/T1|]]", "[[new/tasks/T1|]]"},
		// Not old's: another project, a prefix, plain text.
		{"[[older/tasks/T1|T1]]", "[[older/tasks/T1|T1]]"},
		{"[[other/tasks/old|old]]", "[[other/tasks/old|old]]"},
		{"old/tasks/T1 and [[_peers/m/old|m/old]]", "old/tasks/T1 and [[_peers/m/old|m/old]]"},
	} {
		if got := Rename(c.in, "old", "new"); got != c.want {
			t.Errorf("Rename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEach(t *testing.T) {
	var got []string
	Each("a [[x/y|z]] b [[w]]", func(target, alias string) { got = append(got, target+"="+alias) })
	if len(got) != 2 || got[0] != "x/y=z" || got[1] != "w=" {
		t.Errorf("got %q", got)
	}
}
