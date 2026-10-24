package link

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// TestMessageLinks (T717): each side's file links the message answered
// and the sender (vaultlink), and reads back the plain ids.
func TestMessageLinks(t *testing.T) {
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	a, b, _ := setup(t)
	ca, cb := coord, coord
	ca.Project, cb.Project = "a", "b"
	m1, err := Send(ca, a, "b", Opts{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Send(cb, b, "a", Opts{Text: "back", Re: m1.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		file string
		want []string
	}{
		{b.Path("messages", "a", m2.ID+".md"), []string{`re: "[[b/messages/a/` + m1.ID + `|` + m1.ID + `]]"`, `from: "[[b/PROJECT|b]]"`}},
		{a.Path("messages", "b", m2.ID+".md"), []string{`re: "[[a/messages/b/` + m1.ID + `|` + m1.ID + `]]"`, `from: "[[b/PROJECT|b]]"`}},
		{a.Path("messages", "b", m1.ID+".md"), []string{`from: "[[a/PROJECT|a]]"`}},
	} {
		data, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range c.want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s has no %s:\n%s", c.file, w, data)
			}
		}
	}
	got, err := Read(a, m2.ID)
	if err != nil || got.Re != m1.ID || got.Sender != "" || got.Text != "back" {
		t.Fatalf("%+v %v", got, err)
	}
	// An update (the receiver reads it) keeps the links.
	if err := update(a, "b", m2.ID, func(m *Message) { m.State = StateRead }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(a.Path("messages", "b", m2.ID+".md"))
	if !strings.Contains(string(data), `re: "[[a/messages/b/`+m1.ID) || !strings.Contains(string(data), `state: "read"`) {
		t.Fatalf("after update:\n%s", data)
	}
	// The receiver's inbox item links the sender and the message.
	items, _ := a.Inbox()
	if len(items) == 0 || items[len(items)-1].Message != m2.ID || items[len(items)-1].Subject != "b" {
		t.Fatalf("items %+v", items)
	}
	data, _ = os.ReadFile(a.Path("inbox", items[len(items)-1].ID+".md"))
	if !strings.Contains(string(data), `subject: "[[b/PROJECT|b]]"`) || !strings.Contains(string(data), `message: "[[a/messages/b/`+m2.ID+`|`+m2.ID+`]]"`) {
		t.Fatalf("item:\n%s", data)
	}
}

// TestMessageFenced (T718): a message's file marks it as data, and its
// text is fenced so the vault shows it as text; it reads back whole,
// through an update too, and a file from before reads as it was.
func TestMessageFenced(t *testing.T) {
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	text := "<img src=\"https://x.y/a.png\">\n~~~~~ text\n![[x]]\n```dataviewjs\nx\n```"
	m, err := Send(ca, a, "b", Opts{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	in, _ := os.ReadFile(b.Path("messages", "a", m.ID+".md"))
	for _, w := range []string{"tm: \"data\"", "- \"from-link\"", "- \"tm-data\"", "\n~~~~~~text\n" + text + "\n~~~~~~\n"} {
		if !strings.Contains(string(in), w) {
			t.Errorf("received file has no %q:\n%s", w, in)
		}
	}
	if out, _ := os.ReadFile(a.Path("messages", "b", m.ID+".md")); strings.Contains(string(out), "from-link") || !strings.Contains(string(out), "~~~~~~text\n") {
		t.Errorf("sent file:\n%s", out)
	}
	if err := update(b, "a", m.ID, func(m *Message) { m.State = StateRead }); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(b, m.ID); err != nil || got.Text != text || got.Vault != "data" {
		t.Fatalf("%+v %v", got, err)
	}
	// A file tm wrote before the vault: its text as it is, fenced once
	// it is updated.
	old := b.Path("messages", "a", "M-99.md")
	os.WriteFile(old, []byte("---\nid: \"M-99\"\nstate: \"new\"\n---\n~~~text\nplain\n~~~\n"), 0o644)
	if got, err := Read(b, "M-99"); err != nil || got.Text != "~~~text\nplain\n~~~" {
		t.Fatalf("old file: %+v %v", got, err)
	}
	if err := update(b, "a", "M-99", func(m *Message) { m.State = StateRead }); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(b, "M-99"); got.Text != "~~~text\nplain\n~~~" {
		t.Fatalf("old file after update: %q", got.Text)
	}
}
