package tui

import (
	"strings"
	"testing"
	"time"

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

// TestLinkedProjectsSetting: the Settings row lists the links, its list
// steps this side through off › both › send › receive, and allowing a
// link the other side doesn't asks whether to allow it there too.
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
	// both → send: the other side already allows both, no question.
	runAll(m, press(m, "enter"))
	if len(src.links) != 1 || src.links[0] != "alpha beta send false false" {
		t.Fatalf("links %v", src.links)
	}
	// ask first on.
	runAll(m, press(m, "a"))
	if len(src.links) != 2 || src.links[1] != "alpha beta both true false" {
		t.Fatalf("ask %v", src.links)
	}
	// A link the other side has no entry for asks; y allows both sides.
	src.data.Projects[0].Links = nil
	m.setData(src.data)
	press(m, "enter")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "Also let beta message alpha?") {
		t.Fatalf("no question: %T", m.top())
	}
	runAll(m, press(m, "y"))
	if got := src.links[len(src.links)-1]; got != "alpha beta both false true" {
		t.Fatalf("mirror %v", src.links)
	}
	// n saves this side only.
	press(m, "enter")
	runAll(m, press(m, "n"))
	if got := src.links[len(src.links)-1]; got != "alpha beta both false false" {
		t.Fatalf("this side only %v", src.links)
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
