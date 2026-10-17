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
	h.expect(1, "human-only", coord, "project", "link", "demo", "mods")
	h.expect(1, "self-link", human, "project", "link", "demo", "demo")
	h.expect(1, "unknown-project", human, "project", "link", "demo", "nope")
	h.expect(2, "usage", human, "project", "link", "demo", "mods", "sometimes")
	h.expect(2, "usage", human, "project", "link", "demo")

	// One side only, as only a hand edit of config.toml makes it: the
	// coordinator is refused with the other side's code, and the list
	// says it is half-linked.
	if err := config.SetLink("demo", "mods", config.Link{Allow: config.LinkBoth}); err != nil {
		t.Fatal(err)
	}
	if out := h.ok(coord, "link", "list", "--project", "demo"); !strings.Contains(out, "not linked, half-linked: mods has no entry") {
		t.Fatalf("one side %q", out)
	}
	h.expect(1, "not-linked-back", coord, "link", "send", "mods", "hello", "--project", "demo")
	h.expect(1, "no-link", modsCoord, "link", "send", "demo", "hello", "--project", "mods")
	h.expect(1, "no-link", coord, "link", "send", "web", "hello", "--project", "demo")
	h.expect(1, "self-link", coord, "link", "send", "demo", "hello", "--project", "demo")
	h.expect(1, "unknown-project", coord, "link", "send", "nope", "hello", "--project", "demo")

	// One act links both sides; the other side journals it too.
	if out := h.ok(human, "project", "link", "mods", "demo"); out != "mods ⇄ demo: linked both ways\n" {
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
	os.WriteFile(thread.Path(p, "t-0001", "library", "notes.md"), []byte("the notes\x1b]52;c;eA==\a\n\tindented"), 0o644)
	// a name that isn't plain text isn't attached
	os.WriteFile(thread.Path(p, "t-0001", "library", "x\x1b]0;t\a.md"), []byte("x"), 0o644)
	h.expect(1, "bad-attachment", coord, "link", "send", "mods", "hi", "--attach", "t-0001/x\x1b]0;t\a.md", "--project", "demo")

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
		!strings.Contains(read, "The API landed.[31m\nSee SPEC.") || !strings.Contains(read, "attached: notes.md") {
		t.Fatalf("read %q", read)
	}
	// the file is another project's: only its text reaches the terminal
	if f := h.ok(modsCoord, "link", "read", "M-1", "--file", "notes.md", "--project", "mods"); !strings.Contains(f, "the notes]52;c;eA==\n\tindented\n(characters that aren't text") || strings.ContainsAny(f, "\x1b\a") {
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
	h.ok(human, "project", "link", "demo", "mods", "both", "--ask")
	if cfg, _ := config.Load(); func() bool { l, _ := cfg.Links("mods"); return l["demo"].Ask }() {
		t.Fatal("--ask reached the other side")
	}
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

	// Off removes both sides; the history stays.
	h.ok(human, "project", "link", "demo", "mods", "off")
	h.expect(1, "no-link", coord, "link", "send", "mods", "hi", "--project", "demo")
	cfg, _ := config.Load()
	if a, _ := cfg.Links("demo"); len(a) != 0 {
		t.Fatalf("demo's links after off %v", a)
	}
	if b, _ := cfg.Links("mods"); len(b) != 0 {
		t.Fatalf("mods' links after off %v", b)
	}
	if out := h.ok(coord, "link", "list", "--project", "demo"); !strings.Contains(out, "mods") || !strings.Contains(out, "off") {
		t.Fatalf("list after off %q", out)
	}
	j, _ := os.ReadFile(mp.Path("JOURNAL.md"))
	for _, w := range []string{"human settings.links mods demo both (demo both)", "coordinator link.receive demo M-1 (", "coordinator link.send demo M-2"} {
		if !strings.Contains(string(j), w) {
			t.Errorf("mods journal lacks %q:\n%s", w, j)
		}
	}
}

// TestProjectLinkMirror: tm project link writes both entries in one
// call, the other side's the mirror: send here is receive there.
func TestProjectLinkMirror(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "mods")
	if out := h.ok(human, "project", "link", "demo", "mods", "send"); !strings.Contains(out, "linked, send only") {
		t.Fatalf("out %q", out)
	}
	cfg, _ := config.Load()
	a, _ := cfg.Links("demo")
	b, _ := cfg.Links("mods")
	if a["mods"] != (config.Link{Allow: config.LinkSend}) || b["demo"] != (config.Link{Allow: config.LinkReceive}) {
		t.Fatalf("entries %v %v", a, b)
	}
	// Changing it on the other side changes both.
	h.ok(human, "project", "link", "mods", "demo", "send")
	cfg, _ = config.Load()
	a, _ = cfg.Links("demo")
	if a["mods"] != (config.Link{Allow: config.LinkReceive}) {
		t.Fatalf("demo after mods' change %v", a)
	}
}

// TestRenameLinked: renaming a project carries its links and the
// messages other projects keep with it.
func TestRenameLinked(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "mods")
	h.ok(human, "project", "link", "demo", "mods")
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
