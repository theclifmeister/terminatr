package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
)

func linkDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	d := testData()
	d.Projects[0].Links = []link.Row{
		{Peer: "beta", HasMine: true, Mine: config.Link{Allow: config.LinkBoth}, HasTheirs: true, Theirs: config.Link{Allow: config.LinkBoth},
			Send: true, Receive: true, Unread: 1, Held: 1, Last: "M-3", LastAt: time.Now()},
	}
	d.Projects[0].LinkWaits = 1
	src := &fakeSource{data: d, linkLog: []*link.Message{
		{ID: "M-1", Out: false, Peer: "beta", Created: time.Now(), Bytes: 5, State: link.StateRead, By: "coordinator", Text: "Hello"},
		{ID: "M-3", Out: true, Peer: "beta", Created: time.Now(), Bytes: 7, State: link.StateHeld, By: "coordinator", Text: "Held one"},
	}}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	return src, m
}

// TestLinkedProjectsSetting: the Settings row lists the links; its list
// steps the link through off › both › send › receive, each one act that
// sets both sides (no question), and a turns this side's ask first.
func TestLinkedProjectsSetting(t *testing.T) {
	src, m := linkDash(t)
	m.openTab("alpha", tabSettings, 0)
	pv := m.projectPopupView()
	pv.settings.sel = len(pv.settings.rows) - 1
	if out := screen(m); !strings.Contains(out, "Linked projects") || !strings.Contains(out, "beta ⇄") {
		t.Fatalf("no Linked projects row:\n%s", out)
	}
	m.push(&linksView{slug: "alpha"})
	if out := screen(m); !strings.Contains(out, "linked both ways") || !strings.Contains(out, "a ask first") {
		t.Fatalf("list:\n%s", out)
	}
	// both → send, at once: no question.
	runAll(m, press(m, "enter"))
	if _, asked := m.top().(*confirmView); asked || len(src.links) != 1 || src.links[0] != "alpha beta send" {
		t.Fatalf("links %v (%T)", src.links, m.top())
	}
	runAll(m, press(m, "a"))
	if len(src.links) != 2 || src.links[1] != "alpha beta ask true" {
		t.Fatalf("ask %v", src.links)
	}
	// No link yet: enter links both ways; a says to link first.
	src.data.Projects[0].Links = nil
	m.setData(src.data)
	press(m, "a")
	if len(src.links) != 2 || !strings.Contains(m.msg, "link beta first") {
		t.Fatalf("ask without a link: %v %q", src.links, m.msg)
	}
	runAll(m, press(m, "enter"))
	if got := src.links[len(src.links)-1]; got != "alpha beta both" {
		t.Fatalf("link %v", src.links)
	}
	// A half-linked pair (a hand edit) says so.
	src.data.Projects[0].Links = []link.Row{{Peer: "beta", HasMine: true, Mine: config.Link{Allow: config.LinkBoth}}}
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "half-linked: beta has no entry") {
		t.Fatalf("half:\n%s", out)
	}
}

// TestMessagesTab: the Messages tab lists the links; enter opens the
// conversation, where A approves a held message; NEEDS YOU has a row for
// it that opens the tab.
func TestMessagesTab(t *testing.T) {
	src, m := linkDash(t)
	if out := screen(m); !strings.Contains(out, "1 message waits") {
		t.Fatalf("no NEEDS YOU row:\n%s", out)
	}
	m.openTab("alpha", tabMessages, 0)
	out := screen(m)
	if !strings.Contains(out, "8 messages 1") || !strings.Contains(out, "beta") || !strings.Contains(out, "1 waits for you") {
		t.Fatalf("tab:\n%s", out)
	}
	press(m, "enter")
	cv, ok := m.top().(*convView)
	if !ok || cv.other != "beta" {
		t.Fatalf("enter opened %T", m.top())
	}
	out = screen(m)
	for _, want := range []string{"Hello", "Held one", "waits for your OK", "A approve", "m write"} {
		if !strings.Contains(out, want) {
			t.Errorf("conversation lacks %q:\n%s", want, out)
		}
	}
	runAll(m, press(m, "A"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha approve M-3" {
		t.Fatalf("acts %v", src.linkActs)
	}
	// On a message that isn't held, A says why.
	press(m, "k")
	press(m, "A")
	if len(src.linkActs) != 1 || !strings.Contains(m.msg, "only a message that waits") {
		t.Fatalf("A on a sent message: %v %q", src.linkActs, m.msg)
	}
}

// TestLinkedProjectsRemote: the list has a group per paired server with
// the projects it shares and any this side links; enter and a write
// this side only (SetLink with the qualified key, which asks first when
// new), and the row says that server decides its side and what the
// messages tell of it. It matches testdata/linked-projects.golden (go
// test -run TestLinkedProjectsRemote -update rewrites it).
func TestLinkedProjectsRemote(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src, m := linkDash(t)
	src.data.Peers = remotePeers()
	src.data.Projects[0].Links = append(src.data.Projects[0].Links,
		link.Row{Peer: "mini/acme-api", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkBoth, Ask: true}, Send: true, Receive: true},
		link.Row{Peer: "nas/old", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkSend}, Send: true})
	m.setData(src.data)
	src.linkLogs = map[string][]*link.Message{
		"mini/acme-api": {
			{ID: "M-4F2A-1", Out: true, State: link.StateRead},
			{ID: "M-9C1B-4", Out: false, State: link.StateRead},
		},
		"nas/old": {{ID: "M-4F2A-2", Out: true, State: link.StateUndelivered, Refused: "not-linked"}},
	}
	v := &linksView{slug: "alpha"}
	m.push(v)
	items, groups := v.items(m)
	var keys []string
	for _, it := range items {
		keys = append(keys, it.key)
	}
	if got := strings.Join(keys, " "); got != "beta mini/acme-api nas/media nas/old" {
		t.Fatalf("items %s", got)
	}
	if len(groups) != 2 || groups[0].alias != "mini" || groups[1].state != "offline" {
		t.Fatalf("groups %+v", groups)
	}

	lines, _ := v.lines(m, 110)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
	}
	path := filepath.Join("testdata", "linked-projects.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil {
		t.Fatalf("%v (go test -run TestLinkedProjectsRemote -update writes it)", err)
	} else if b.String() != string(want) {
		t.Errorf("the list differs from %s:\n%s", path, b.String())
	}

	// nas/media, unlinked: enter links this side both ways (link.Set
	// asks first for a new remote entry); a on mini/acme-api turns its
	// ask off.
	v.sel = 2
	runAll(m, press(m, "enter"))
	if got := src.links[len(src.links)-1]; got != "alpha nas/media both" || !strings.Contains(m.msg, "nas decides its side") || !strings.Contains(m.msg, "ask first") {
		t.Fatalf("link %v %q", src.links, m.msg)
	}
	v.sel = 1
	runAll(m, press(m, "a"))
	if got := src.links[len(src.links)-1]; got != "alpha mini/acme-api ask false" {
		t.Fatalf("ask %v", src.links)
	}
}
