package project

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

func linksOn(t *testing.T) {
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
}

// TestItemLinks (T717): an item's file links a task or thread subject
// and the thread its summary names; the item reads back plain ids.
func TestItemLinks(t *testing.T) {
	setup(t)
	linksOn(t)
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		subject, summary string
		want             []string
	}{
		{"t-0001", "T3 Fix it (t-0001) reported", []string{`subject: "[[demo/threads/t-0001/t-0001|t-0001]]"`}},
		{"T3", "T3 Fix it (t-0002) done", []string{`subject: "[[demo/tasks/T3|T3]]"`, `thread: "[[demo/threads/t-0002/t-0002|t-0002]]"`}},
		{"config.toml", "plain", []string{`subject: "config.toml"`}},
	} {
		it, err := p.AddItem("k", c.subject, c.summary, false)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(p.Path("inbox", it.ID+".md"))
		for _, w := range c.want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s: no %s in\n%s", c.subject, w, data)
			}
		}
		if c.subject == "t-0001" && strings.Contains(string(data), "thread:") {
			t.Errorf("thread repeats the subject:\n%s", data)
		}
		got, err := ParseItem(it.ID, data)
		if err != nil || got.Subject != c.subject || got.Thread != it.Thread {
			t.Errorf("read back %+v %v", got, err)
		}
	}
}

// TestLinkSource (T717): a note's plain source becomes a link; any
// other text, and a source already linked or not an id, stays.
func TestLinkSource(t *testing.T) {
	linksOn(t)
	p := &Project{Slug: "demo"}
	for in, want := range map[string]string{
		"---\ntitle: \"x\"\nsource: \"T481\"\n---\n## Decided\nsource: T1\n": "---\ntitle: \"x\"\nsource: \"[[demo/tasks/T481|T481]]\"\n---\n## Decided\nsource: T1\n",
		"---\r\nsource: t-0001\r\n---\r\nbody\r\n":                        "---\r\nsource: \"[[demo/threads/t-0001/t-0001|t-0001]]\"\r\n---\r\nbody\r\n",
		"---\nsource: \"[[demo/tasks/T1|T1]]\"\n---\n":                    "---\nsource: \"[[demo/tasks/T1|T1]]\"\n---\n",
		"---\nsource: \"the user\"\n---\n":                                "---\nsource: \"the user\"\n---\n",
		"---\nsource: \"T1'\n---\n":                                       "---\nsource: \"T1'\n---\n",
		"+++\nsource = \"T1\"\n+++\n":                                     "+++\nsource = \"T1\"\n+++\n",
		"# free-form\nsource: T1\n":                                       "# free-form\nsource: T1\n",
	} {
		got := string(p.LinkSource([]byte(in)))
		if got != want {
			t.Errorf("LinkSource(%q) = %q, want %q", in, got, want)
		}
		if n := ParseNote("memory/x.md", got); n.Front && strings.Contains(in, "T481") && n.Source != "T481" {
			t.Errorf("source reads %q", n.Source)
		}
	}
}
