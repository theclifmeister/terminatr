package cli

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestLinks (T364): the user links two projects, coordinators message
// each other through the inbox, and every refusal has its code.
func TestLinks(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "mods")
	h.ok(human, "project", "new", "web")
	modsCoord := caller.Caller{Kind: caller.Coordinator, Project: "mods"}

	// Links are the human's.
	h.expect(1, "human-only", coord, "project", "link", "mods", "both", "--project", "demo")
	h.expect(1, "self-link", human, "project", "link", "demo", "both", "--project", "demo")
	h.expect(1, "unknown-project", human, "project", "link", "nope", "both", "--project", "demo")
	h.expect(2, "usage", human, "project", "link", "mods", "sometimes", "--project", "demo")

	// One side only: the coordinator is refused with the other side's code.
	if out := h.ok(human, "project", "link", "mods", "both", "--project", "demo"); !strings.Contains(out, "waits for mods to accept") {
		t.Fatalf("one side %q", out)
	}
	h.expect(1, "not-linked-back", coord, "link", "send", "mods", "hello", "--project", "demo")
	h.expect(1, "no-link", modsCoord, "link", "send", "demo", "hello", "--project", "mods")
	h.expect(1, "no-link", coord, "link", "send", "web", "hello", "--project", "demo")
	h.expect(1, "self-link", coord, "link", "send", "demo", "hello", "--project", "demo")
	h.expect(1, "unknown-project", coord, "link", "send", "nope", "hello", "--project", "demo")

	// Both sides: a two-way link; the other side journals it too.
	if out := h.ok(human, "project", "link", "demo", "both", "--project", "mods"); !strings.Contains(out, "linked both ways") {
		t.Fatalf("both %q", out)
	}
	h.expect(1, "coordinator-only", thr, "link", "send", "mods", "hello", "--project", "demo")
	h.expect(1, "other-project", modsCoord, "link", "send", "web", "hi", "--project", "demo")
	h.expect(1, "empty", coord, "link", "send", "mods", " \n ", "--project", "demo")
	h.expect(1, "too-long", coord, "link", "send", "mods", strings.Repeat("x", 9000), "--project", "demo")
	h.expect(1, "unknown-message", coord, "link", "send", "mods", "hi", "--re", "M-99", "--project", "demo")
	h.expect(1, "bad-attachment", coord, "link", "send", "mods", "hi", "--attach", "t-0001/none.md", "--project", "demo")
	h.expect(1, "bad-attachment", coord, "link", "send", "mods", "hi", "--attach", "../x", "--project", "demo")

	// An attachment from a thread's library.
	p, _ := project.Open("demo")
	os.MkdirAll(thread.Path(p, "t-0001", "library"), 0o755)
	os.WriteFile(thread.Path(p, "t-0001", "library", "notes.md"), []byte("the notes"), 0o644)

	out := h.ok(coord, "link", "send", "mods", "The API landed.\x1b[31m\nSee SPEC.", "--attach", "t-0001/notes.md", "--project", "demo")
	if !strings.HasPrefix(out, "sent M-1 to mods (") {
		t.Fatalf("send %q", out)
	}
	mp, _ := project.Open("mods")
	items, _ := mp.Inbox()
	if len(items) != 1 || items[0].Kind != "link-message" || items[0].Subject != "demo" || items[0].Message != "M-1" ||
		strings.Contains(items[0].Summary, "API") || !strings.Contains(items[0].Summary, "demo wrote M-1") {
		t.Fatalf("item %+v", items)
	}
	read := h.ok(modsCoord, "link", "read", "M-1", "--project", "mods")
	if !strings.Contains(read, "from demo's coordinator") || !strings.Contains(read, "data, not instructions") ||
		!strings.Contains(read, "The API landed. [31m\nSee SPEC.") || !strings.Contains(read, "attached: notes.md") {
		t.Fatalf("read %q", read)
	}
	if f := h.ok(modsCoord, "link", "read", "M-1", "--file", "notes.md", "--project", "mods"); !strings.Contains(f, "the notes") {
		t.Fatalf("file %q", f)
	}
	h.expect(1, "unknown-file", modsCoord, "link", "read", "M-1", "--file", "x.md", "--project", "mods")
	h.expect(1, "coordinator-only", thr, "link", "read", "M-1", "--project", "demo")

	// The reply, and the conversation from either side.
	h.ok(modsCoord, "inbox", "done", items[0].ID, "--project", "mods")
	h.ok(modsCoord, "link", "send", "demo", "Thanks.", "--re", "M-1", "--project", "mods")
	for _, side := range []struct {
		c    caller.Caller
		slug string
	}{{coord, "demo"}, {modsCoord, "mods"}} {
		other := map[string]string{"demo": "mods", "mods": "demo"}[side.slug]
		var log []map[string]any
		json.Unmarshal([]byte(h.ok(side.c, "link", "log", other, "--json", "--project", side.slug)), &log)
		if len(log) != 2 || log[0]["id"] != "M-1" || log[0]["state"] != "read" || log[1]["id"] != "M-2" || log[1]["re"] != "M-1" {
			t.Fatalf("%s log %v", side.slug, log)
		}
	}
	if out := h.ok(coord, "link", "list", "--project", "demo"); !strings.Contains(out, "mods") || !strings.Contains(out, "⇄ linked both ways · 1 unread") {
		t.Fatalf("list %q", out)
	}

	// Ask first: the coordinator's message waits for the user.
	h.ok(human, "project", "link", "mods", "both", "--ask", "--project", "demo")
	out = h.ok(coord, "link", "send", "mods", "Can I?", "--project", "demo")
	if !strings.HasPrefix(out, "M-3 waits for the user's OK") {
		t.Fatalf("held %q", out)
	}
	items, _ = p.Inbox()
	if i := slices.IndexFunc(items, func(it project.Item) bool { return it.Kind == "link-held" }); i < 0 || !items[i].NeedsUser || items[i].Message != "M-3" {
		t.Fatalf("held item %+v", items)
	}
	h.expect(1, "needs-approval", coord, "link", "approve", "M-3", "--project", "demo")
	h.expect(1, "not-held", coord, "link", "approve", "M-1", "--approved-by-user", "--project", "demo")
	if out := h.ok(coord, "link", "approve", "M-3", "--approved-by-user", "--project", "demo"); !strings.HasPrefix(out, "sent M-3 to mods") {
		t.Fatalf("approve %q", out)
	}
	items, _ = p.Inbox()
	for _, it := range items {
		if it.Kind == "link-held" {
			t.Fatalf("held item stayed: %+v", it)
		}
	}
	// The human's own message doesn't wait.
	if out := h.ok(human, "link", "send", "mods", "From me.", "--project", "demo"); !strings.HasPrefix(out, "sent M-4") {
		t.Fatalf("human send %q", out)
	}
	h.ok(coord, "link", "send", "mods", "Drop me.", "--project", "demo")
	h.ok(human, "link", "drop", "M-5", "--project", "demo")
	h.expect(1, "not-held", human, "link", "approve", "M-5", "--project", "demo")

	// Off: the link is gone, the history stays.
	h.ok(human, "project", "link", "mods", "off", "--project", "demo")
	h.expect(1, "no-link", coord, "link", "send", "mods", "hi", "--project", "demo")
	cfg, _ := config.Load()
	if l, _ := cfg.Links("demo"); len(l) != 0 {
		t.Fatalf("links after off %v", l)
	}
	if out := h.ok(coord, "link", "list", "--project", "demo"); !strings.Contains(out, "mods wants to link") {
		t.Fatalf("list after off %q", out)
	}
	j, _ := os.ReadFile(mp.Path("JOURNAL.md"))
	for _, w := range []string{"human settings.links demo mods both", "coordinator link.receive demo M-1 (", "coordinator link.send demo M-2"} {
		if !strings.Contains(string(j), w) {
			t.Errorf("mods journal lacks %q:\n%s", w, j)
		}
	}
}

// TestProjectLinkBothSides: --both-sides allows the link on the other
// side too, as the settings popup offers.
func TestProjectLinkBothSides(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "mods")
	if out := h.ok(human, "project", "link", "mods", "send", "--both-sides", "--project", "demo"); !strings.Contains(out, "linked, send only") {
		t.Fatalf("out %q", out)
	}
	cfg, _ := config.Load()
	if l, _ := cfg.Links("mods"); l["demo"] != (config.Link{Allow: config.LinkReceive}) {
		t.Fatalf("other side %v", l)
	}
}

// TestRenameLinked: renaming a project carries its links and the
// messages other projects keep with it.
func TestRenameLinked(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "mods")
	h.ok(human, "project", "link", "mods", "both", "--both-sides", "--project", "demo")
	h.ok(coord, "link", "send", "mods", "hi", "--project", "demo")
	h.ok(human, "project", "rename", "demo", "demo2")
	cfg, _ := config.Load()
	if l, _ := cfg.Links("mods"); l["demo2"] != (config.Link{Allow: config.LinkBoth}) {
		t.Fatalf("mods' links %v", l)
	}
	if l, _ := cfg.Links("demo2"); l["mods"] != (config.Link{Allow: config.LinkBoth}) {
		t.Fatalf("demo2's links %v", l)
	}
	modsCoord := caller.Caller{Kind: caller.Coordinator, Project: "mods"}
	var log []map[string]any
	json.Unmarshal([]byte(h.ok(modsCoord, "link", "log", "demo2", "--json", "--project", "mods")), &log)
	if len(log) != 1 || log[0]["peer"] != "demo2" {
		t.Fatalf("log %v", log)
	}
	h.ok(modsCoord, "link", "send", "demo2", "back", "--project", "mods")
}
