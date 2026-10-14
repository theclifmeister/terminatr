package config

import (
	"os"
	"strings"
	"testing"
)

func TestLinks(t *testing.T) {
	write(t, `[projects.a.links]
b = "both"
c = { allow = "send", ask = true }

[projects.b.links]
a = "both"

[projects.c]
yolo = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	links, err := c.Links("a")
	if err != nil || len(links) != 2 || links["b"] != (Link{Allow: LinkBoth}) || links["c"] != (Link{Allow: LinkSend, Ask: true}) {
		t.Fatalf("links %v %v", links, err)
	}
	for _, tc := range []struct{ from, to, code string }{
		{"a", "b", ""}, {"b", "a", ""},
		{"a", "c", "not-linked-back"}, // c doesn't accept
		{"c", "a", "no-link"},         // c has no entry
		{"b", "c", "no-link"},
		{"a", "a", "self-link"},
	} {
		if code, msg := c.LinkRefusal(tc.from, tc.to); code != tc.code || (code != "" && msg == "") {
			t.Errorf("%s → %s: %q %q, want %q", tc.from, tc.to, code, msg, tc.code)
		}
	}
	if got := strings.Join(c.LinkPeers("c"), ","); got != "a" {
		t.Errorf("peers of c %q", got)
	}
	if got := strings.Join(c.LinkPeers("a"), ","); got != "b,c" {
		t.Errorf("peers of a %q", got)
	}
}

func TestLinksInvalid(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"[defaults.links]\nb = \"both\"\n", "links are per project"},
		{"[projects.a.links]\na = \"both\"\n", "can't link to itself"},
		{"[projects.a.links]\nb = \"sometimes\"\n", `must be "both"`},
		{"[projects.a.links]\nb = { allow = \"both\", ask = \"yes\" }\n", "ask must be true or false"},
		{"[projects.a.links]\nb = { ask = true }\n", "needs allow"},
		{"[projects.a.links]\nb = { allow = \"both\", mute = true }\n", "unknown key"},
		{"[projects.a.links]\n\"B C\" = \"both\"\n", "not a project slug"},
		{"[projects.a.links]\nb = 3\n", "must be"},
	} {
		write(t, tc.file)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want %q", tc.file, err, tc.want)
		}
	}
}

func TestSetLink(t *testing.T) {
	write(t, "# mine\n[projects.a]\nyolo = false\n")
	if err := SetLink("a", "b", Link{Allow: LinkBoth}); err != nil {
		t.Fatal(err)
	}
	if err := SetLink("a", "c", Link{Allow: LinkSend, Ask: true}); err != nil {
		t.Fatal(err)
	}
	if err := SetLink("b", "a", Link{Allow: LinkReceive}); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	data, _ := os.ReadFile(path)
	want := "# mine\n[projects.a]\nyolo = false\n\n[projects.a.links]\nb = \"both\"\nc = { allow = \"send\", ask = true }\n\n[projects.b.links]\na = \"receive\"\n"
	if string(data) != want {
		t.Fatalf("file\n%s\nwant\n%s", data, want)
	}
	// Changing one keeps its place; off removes it.
	if err := SetLink("a", "b", Link{Allow: LinkBoth, Ask: true}); err != nil {
		t.Fatal(err)
	}
	if err := SetLink("a", "c", Link{Allow: LinkOff}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "[projects.a.links]\nb = { allow = \"both\", ask = true }\n\n") {
		t.Fatalf("after change\n%s", data)
	}
	if err := SetLink("a", "a", Link{Allow: LinkBoth}); err == nil {
		t.Error("linked a project to itself")
	}
	if err := SetLink("a", "x", Link{Allow: "sometimes"}); err == nil {
		t.Error("took a bad direction")
	}
	if Mirror(LinkSend) != LinkReceive || Mirror(LinkReceive) != LinkSend || Mirror(LinkBoth) != LinkBoth {
		t.Error("Mirror")
	}
}

func TestRenameLinks(t *testing.T) {
	in := "[projects.demo]\npaused = true\n\n[projects.other.links]\ndemo = \"both\" # pair\nweb = \"send\"\n"
	got, err := RenameTable([]byte(in), "demo", "d2")
	want := "[projects.d2]\npaused = true\n\n[projects.other.links]\nd2 = \"both\" # pair\nweb = \"send\"\n"
	if err != nil || string(got) != want {
		t.Fatalf("got %q %v\nwant %q", got, err, want)
	}
	// Linked to, with no table of its own.
	in = "[projects.other.links]\ndemo = { allow = \"send\", ask = true }\n"
	got, err = RenameTable([]byte(in), "demo", "d2")
	if err != nil || string(got) != "[projects.other.links]\nd2 = { allow = \"send\", ask = true }\n" {
		t.Fatalf("got %q %v", got, err)
	}
}

// TestSetLinkPair: one write links both sides, the other's entry the
// mirror with its own ask kept; off removes both.
func TestSetLinkPair(t *testing.T) {
	write(t, "[projects.b.links]\na = { allow = \"both\", ask = true }\n")
	if err := SetLinkPair("a", "b", Link{Allow: LinkSend}, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Links("a")
	b, _ := c.Links("b")
	if a["b"] != (Link{Allow: LinkSend}) || b["a"] != (Link{Allow: LinkReceive, Ask: true}) {
		t.Fatalf("a %v b %v", a, b)
	}
	if err := SetLinkPair("a", "b", Link{Allow: LinkOff}, false); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if a, _ := c.Links("a"); len(a) != 0 {
		t.Fatalf("a after off %v", a)
	}
	if b, _ := c.Links("b"); len(b) != 0 {
		t.Fatalf("b after off %v", b)
	}
}

func TestHalf(t *testing.T) {
	both, send, recv := Link{Allow: LinkBoth}, Link{Allow: LinkSend}, Link{Allow: LinkReceive}
	for _, tc := range []struct {
		mine, theirs       Link
		hasMine, hasTheirs bool
		half               bool
	}{
		{both, both, true, true, false},
		{send, recv, true, true, false},
		{both, send, true, true, true},
		{both, Link{}, true, false, true},
		{Link{}, both, false, true, true},
		{Link{}, Link{}, false, false, false},
	} {
		if got := Half(tc.mine, tc.theirs, tc.hasMine, tc.hasTheirs); got != tc.half {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
