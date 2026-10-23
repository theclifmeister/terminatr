package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// sets both sides (no question), and A turns this side's ask first.
func TestLinkedProjectsSetting(t *testing.T) {
	src, m := linkDash(t)
	m.openTab("alpha", tabSettings, 0)
	pv := m.projectPopupView()
	pv.settings.sel = len(pv.settings.rows) - 1
	if out := screen(m); !strings.Contains(out, "Linked projects") || !strings.Contains(out, "beta ⇄") {
		t.Fatalf("no Linked projects row:\n%s", out)
	}
	m.push(&linksView{slug: "alpha"})
	if out := screen(m); !strings.Contains(out, "linked both ways") || !strings.Contains(out, "A ask first") {
		t.Fatalf("list:\n%s", out)
	}
	// both → send, at once: no question.
	drain(t, m, press(m, "enter"))
	if _, asked := m.top().(*confirmView); asked || len(src.links) != 1 || src.links[0] != "alpha beta send" {
		t.Fatalf("links %v (%T)", src.links, m.top())
	}
	drain(t, m, press(m, "A"))
	if len(src.links) != 2 || src.links[1] != "alpha beta ask true" {
		t.Fatalf("ask %v", src.links)
	}
	// No link yet: enter links both ways; A says to link first.
	src.data.Projects[0].Links = nil
	m.setData(src.data)
	press(m, "A")
	if len(src.links) != 2 || !strings.Contains(m.msg, "link beta first") {
		t.Fatalf("ask without a link: %v %q", src.links, m.msg)
	}
	drain(t, m, press(m, "enter"))
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

// TestMessagesTab: the Messages tab lists every message, newest first,
// the held one marked under a NEEDS YOU line; enter opens its dialog,
// where A sends it; NEEDS YOU on the dashboard has a row for it that
// opens the tab.
func TestMessagesTab(t *testing.T) {
	src, m := linkDash(t)
	if out := screen(m); !strings.Contains(out, "1 message waits") {
		t.Fatalf("no NEEDS YOU row:\n%s", out)
	}
	m.openTab("alpha", tabMessages, 0)
	out := screen(m)
	for _, want := range []string{"9 messages 1", "⚑ NEEDS YOU 1 · 1 message waits for your OK", "⚑ → beta", "M-3", "Held one", "Hello", "enter open", "M write as you"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "linked both ways") {
		t.Errorf("the list shows the link's state:\n%s", out)
	}
	if strings.Index(out, "Held one") > strings.Index(out, "Hello") {
		t.Errorf("not newest first:\n%s", out)
	}
	press(m, "enter")
	mv, ok := m.top().(*msgView)
	if !ok || mv.e.msg.ID != "M-3" {
		t.Fatalf("enter opened %T", m.top())
	}
	out = screen(m)
	for _, want := range []string{"M-3 · alpha → beta", "From      alpha", "To        beta", "State     waits for your OK", "Held one", "A send · X drop", "esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog lacks %q:\n%s", want, out)
		}
	}
	drain(t, m, press(m, "A"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha approve M-3" {
		t.Fatalf("acts %v", src.linkActs)
	}
	// On a message that isn't held, A says why.
	press(m, "esc")
	keyPress(m, "down")
	press(m, "enter")
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.ID != "M-1" {
		t.Fatalf("down, enter opened %T", m.top())
	}
	press(m, "A")
	if len(src.linkActs) != 1 || !strings.Contains(m.msg, "only a message that waits") {
		t.Fatalf("A on a read message: %v %q", src.linkActs, m.msg)
	}
	press(m, "esc")
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("esc left %T", m.top())
	}
}

// TestMessagesTabRemote (T683, T699): a paired server's project's
// messages are rows like one here's, in the same list, its
// "<server>/<slug>" whole; the dialog tells who wrote it, the paired
// user's message isn't called yours, and a queued or undelivered
// message says so.
func TestMessagesTabRemote(t *testing.T) {
	src, m := linkDash(t)
	now := time.Now()
	src.data.Projects[0].Links = append(src.data.Projects[0].Links,
		link.Row{Peer: "del-02-0468-lt/gatehouse", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkBoth},
			Send: true, Receive: true, Unread: 2, Last: "M-8F49-29", LastAt: now})
	m.setData(src.data)
	src.linkLogs = map[string][]*link.Message{
		"beta": {{ID: "M-1", Peer: "beta", Created: now.Add(-150 * time.Second), State: link.StateRead, By: "coordinator", Text: "From beta"}},
		"del-02-0468-lt/gatehouse": {
			{ID: "M-8F49-28", Peer: "del-02-0468-lt/gatehouse", Created: now.Add(-3 * time.Minute), State: link.StateRead, By: "human", Text: "From their user"},
			{ID: "M-F885-28", Out: true, Peer: "del-02-0468-lt/gatehouse", Created: now.Add(-2 * time.Minute), State: link.StateRead, By: "coordinator", Text: "Read there", Re: "M-8F49-28"},
			{ID: "M-8F49-29", Peer: "del-02-0468-lt/gatehouse", Created: now.Add(-time.Minute), State: link.StateSent, By: "coordinator", Text: "Their coordinator"},
			{ID: "M-F885-29", Out: true, Peer: "del-02-0468-lt/gatehouse", Created: now, State: link.StateQueued, By: "human", Text: "Waits in the outbox"},
			{ID: "M-F885-30", Out: true, Peer: "del-02-0468-lt/gatehouse", Created: now, State: link.StateUndelivered, By: "coordinator", Text: "Refused there"},
		}}
	m.openTab("alpha", tabMessages, 0)
	out := screen(m)
	order := []string{"M-F885-30", "M-F885-29", "M-8F49-29", "M-F885-28", "M-1 ", "M-8F49-28"}
	for i, want := range order {
		if !strings.Contains(out, want) {
			t.Fatalf("tab lacks %q:\n%s", want, out)
		}
		if i > 0 && strings.Index(out, want) < strings.Index(out, order[i-1]) {
			t.Errorf("%s before %s:\n%s", want, order[i-1], out)
		}
	}
	if !strings.Contains(out, "→ del-02-0468-lt/gatehouse") {
		t.Errorf("the name is cut:\n%s", out)
	}
	dialog := func(i int) string {
		t.Helper()
		m.projectPopupView().sel[tabMessages] = i
		press(m, "enter")
		if _, ok := m.top().(*msgView); !ok {
			t.Fatalf("enter on %d opened %T", i, m.top())
		}
		out := screen(m)
		press(m, "esc")
		return out
	}
	for i, wants := range map[int][]string{
		5: {"From      del-02-0468-lt/gatehouse (its user)", "To        alpha", "State     read", "From their user"},
		3: {"From      alpha", "Re        M-8F49-28", "State     read"},
		2: {"From      del-02-0468-lt/gatehouse", "State     sent"},
		1: {"From      alpha (you)", "queued until del-02-0468-lt takes it"},
		0: {"undelivered (see the inbox)"},
	} {
		out := dialog(i)
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("dialog %d lacks %q:\n%s", i, want, out)
			}
		}
		if strings.Contains(out, "gatehouse (you)") {
			t.Errorf("the paired user's message is called yours:\n%s", out)
		}
	}
}

// TestLinkedProjectsRemote: the list has a group per paired server with
// the projects it shares and any this side links; enter and a write
// this side only (SetLink with the qualified key, which asks first when
// new), and the row says that server decides its side and what the
// messages tell of it. It matches testdata/linked-projects.golden (go
// test -run TestLinkedProjectsRemote -update rewrites it).
func TestLinkedProjectsRemote(t *testing.T) {
	freshConsole(t)
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
	// asks first for a new remote entry); A on mini/acme-api turns its
	// ask off.
	v.sel = 2
	drain(t, m, press(m, "enter"))
	if got := src.links[len(src.links)-1]; got != "alpha nas/media both" || !strings.Contains(m.msg, "nas decides its side") || !strings.Contains(m.msg, "ask first") {
		t.Fatalf("link %v %q", src.links, m.msg)
	}
	v.sel = 1
	drain(t, m, press(m, "A"))
	if got := src.links[len(src.links)-1]; got != "alpha mini/acme-api ask false" {
		t.Fatalf("ask %v", src.links)
	}
}

// TestLinkedProjectsUnshared (T701): a paired server's group lists only
// the projects it shares now. One it stopped sharing drops out, though
// messages or an answered request of this side's name it; one this side
// still links stays, marked, and enter sets it off; one a request waits
// on stays as it is. Without a peer.projects answer yet nothing is
// marked.
func TestLinkedProjectsUnshared(t *testing.T) {
	src, m := linkDash(t)
	src.data.Peers = remotePeers()
	src.data.Peers[0].Listed = true
	now := time.Now()
	src.data.Projects[0].Links = []link.Row{
		{Peer: "mini/billing", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkBoth, Ask: true}, Send: true, Receive: true},
		{Peer: "mini/docs", Remote: true, Last: "M-4F2A-3", LastAt: now,
			Asked: &link.Request{ID: "R-4F2A-2", Out: true, Peer: "mini/docs", Allow: "both", Created: now, State: link.ReqDeclined}},
		{Peer: "mini/infra", Remote: true,
			Asks: &link.Request{ID: "R-9C1B-5", Peer: "mini/infra", Project: "alpha", Allow: "send", Created: now, State: link.ReqWaiting}},
	}
	m.setData(src.data)
	v := &linksView{slug: "alpha"}
	m.push(v)
	items, _ := v.items(m)
	var keys []string
	for _, it := range items {
		keys = append(keys, it.key)
	}
	if got := strings.Join(keys, " "); got != "beta mini/acme-api mini/billing mini/infra nas/media" {
		t.Fatalf("items %s", got)
	}
	for _, it := range items {
		if it.gone != (it.key == "mini/billing") {
			t.Errorf("%s gone %v", it.key, it.gone)
		}
	}
	out := screen(m)
	if !strings.Contains(out, "not shared any more") || strings.Count(out, "billing") != 1 {
		t.Fatalf("list:\n%s", out)
	}

	v.sel = 2
	drain(t, m, press(m, "enter"))
	if got := src.links[len(src.links)-1]; got != "alpha mini/billing off" {
		t.Fatalf("enter on an unshared project: %v %q", src.links, m.msg)
	}
	src.data.Projects[0].Links = src.data.Projects[0].Links[1:]
	m.setData(src.data)
	if items, _ := v.items(m); slices.ContainsFunc(items, func(it linkItem) bool { return it.slug == "billing" }) {
		t.Fatalf("billing set off still listed: %+v", items)
	}

	// Before the first peer.projects answer, a link is no sign of an
	// unshared project.
	src.data.Peers[0].Listed = false
	src.data.Projects[0].Links = append(src.data.Projects[0].Links,
		link.Row{Peer: "mini/billing", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkSend}, Send: true})
	m.setData(src.data)
	items, _ = v.items(m)
	if i := slices.IndexFunc(items, func(it linkItem) bool { return it.key == "mini/billing" }); i < 0 || items[i].gone {
		t.Fatalf("not listed yet: %+v", items)
	}
}

// TestLinkRequests: R on a paired server's project asks its user to
// link back (after a Y; with no entry here it links both ways first),
// once while one waits, and the other side's column says the request's
// state. A request from there is a NEEDS YOU row; in the Messages tab A
// offers the directions, the request's mirror first, and X declines
// after a Y.
func TestLinkRequests(t *testing.T) {
	src, m := linkDash(t)
	src.data.Peers = remotePeers()
	now := time.Now()
	src.data.Projects[0].Links = []link.Row{
		{Peer: "mini/acme-api", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkSend, Ask: true}, Send: true,
			Asked: &link.Request{ID: "R-4F2A-7", Out: true, Peer: "mini/acme-api", Allow: "send", Created: now, State: link.ReqWaiting}},
		{Peer: "nas/old", Remote: true,
			Asks: &link.Request{ID: "R-9C1B-3", Peer: "nas/old", Project: "alpha", Allow: "send", Created: now, State: link.ReqWaiting}},
	}
	src.data.Projects[0].countLinkWaits()
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "1 link request waits") {
		t.Fatalf("no NEEDS YOU row:\n%s", out)
	}

	v := &linksView{slug: "alpha"}
	m.push(v)
	if out := screen(m); !strings.Contains(out, "asked, no answer yet") || !strings.Contains(out, "asks to link") || !strings.Contains(out, "R ask") {
		t.Fatalf("list:\n%s", out)
	}
	items, _ := v.items(m)
	for i, it := range items {
		if it.key == "mini/acme-api" {
			v.sel = i
		}
	}
	press(m, "R")
	if len(src.linkActs) != 0 || !strings.Contains(m.msg, "asked already (R-4F2A-7)") {
		t.Fatalf("R while one waits: %v %q", src.linkActs, m.msg)
	}
	for i, it := range items {
		if it.key == "nas/media" {
			v.sel = i
		}
	}
	press(m, "R")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "both ways on this side") {
		t.Fatalf("R without an entry: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha request nas/media both" {
		t.Fatalf("request %v", src.linkActs)
	}
	m.pop()

	m.openTab("alpha", tabMessages, 0)
	if out := screen(m); !strings.Contains(out, "A accept the link") || !strings.Contains(out, "⚑ nas/old") || !strings.Contains(out, "asks to link (R-9C1B-3)") {
		t.Fatalf("tab:\n%s", out)
	}
	press(m, "A")
	mv, ok := m.top().(*menuView)
	if !ok || mv.items[0].label != "receive only, ask first" || len(mv.items) != 6 {
		t.Fatalf("A: %T %+v", m.top(), mv)
	}
	drain(t, m, press(m, "enter"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "alpha accept R-9C1B-3 receive ask" {
		t.Fatalf("accept %v", src.linkActs)
	}
	press(m, "X")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("X: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "alpha decline R-9C1B-3" {
		t.Fatalf("decline %v", src.linkActs)
	}
}

// TestQueuedState: the dialog says when a message over the rate goes,
// and tells it from one waiting for a paired server.
func TestQueuedState(t *testing.T) {
	due := time.Now().Add(12 * time.Minute)
	if got := messageState(&link.Message{State: link.StateQueued, Due: due}, "beta"); !strings.HasPrefix(got, "queued over the rate, goes about ") || !strings.Contains(got, due.Local().Format("15:04")) {
		t.Errorf("over the rate: %q", got)
	}
	if got := messageState(&link.Message{State: link.StateQueued}, "mini/api"); got != "queued until mini takes it" {
		t.Errorf("outbox: %q", got)
	}
}

// TestNeedsYouLine: the line over the list counts all that waits for
// the user; a paused link is a row of its own, where R resumes it.
func TestNeedsYouLine(t *testing.T) {
	links := []link.Row{{Peer: "beta", Held: 2, FilesWaiting: 1, Paused: true},
		{Peer: "mini/api", Asks: &link.Request{ID: "R-4F2A-1"}}}
	if got := ansi.Strip(needsLine(links)); got != "⚑ NEEDS YOU 5 · 2 messages wait for your OK · 1 file waits for you · 1 paused link · 1 link request" {
		t.Errorf("line %q", got)
	}
	if needsLine([]link.Row{{Peer: "beta", Unread: 3}}) != "" {
		t.Error("a line with nothing waiting")
	}
	src, m := linkDash(t)
	src.data.Projects[0].Links[0].Paused = true
	src.data.Projects[0].countLinkWaits()
	m.setData(src.data)
	m.openTab("alpha", tabMessages, 0)
	if out := screen(m); !strings.Contains(out, "⚑ beta") || !strings.Contains(out, "link paused") || !strings.Contains(out, "R resume the link") {
		t.Fatalf("paused row:\n%s", out)
	}
	drain(t, m, press(m, "R"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha resume beta" {
		t.Fatalf("acts %v", src.linkActs)
	}
}

// TestDropQueuedKey: X drops a message queued over the rate, after a Y,
// as it does a held one; A doesn't apply to it, and a message queued
// for a paired server (no due time: it has left) or sent has no X.
func TestDropQueuedKey(t *testing.T) {
	src, m := linkDash(t)
	now := time.Now()
	src.linkLog = append(src.linkLog,
		&link.Message{ID: "M-4", Out: true, Peer: "beta", Created: now.Add(time.Second), Bytes: 3, State: link.StateQueued, Due: now.Add(10 * time.Minute), By: "coordinator", Text: "Over the rate"},
		&link.Message{ID: "M-5", Out: true, Peer: "beta", Created: now.Add(2 * time.Second), Bytes: 3, State: link.StateQueued, By: "coordinator", Text: "In the outbox"})
	m.openTab("alpha", tabMessages, 0)
	keyPress(m, "down")
	press(m, "enter")
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.ID != "M-4" {
		t.Fatalf("enter opened %T", m.top())
	}
	if out := screen(m); !strings.Contains(out, "X drop") || strings.Contains(out, "A send") {
		t.Fatalf("keys on a queued message:\n%s", out)
	}
	press(m, "A")
	if len(src.linkActs) != 0 || !strings.Contains(m.msg, "only a message") {
		t.Fatalf("A on a queued one: %v %q", src.linkActs, m.msg)
	}
	press(m, "X")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("X: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha drop M-4" {
		t.Fatalf("acts %v", src.linkActs)
	}
	press(m, "esc")
	keyPress(m, "up")
	press(m, "enter")
	press(m, "X")
	if _, ok := m.top().(*confirmView); ok || len(src.linkActs) != 1 {
		t.Fatalf("X on one in the outbox: %T %v", m.top(), src.linkActs)
	}
}

// TestMessagesGolden (T699): the Messages tab's list, a link here and
// one to a paired server's project in one list with the same columns,
// and a message's dialog, as they read. It matches
// testdata/messages-tab.golden (go test -run TestMessagesGolden -update
// rewrites it).
func TestMessagesGolden(t *testing.T) {
	src, m := linkDash(t)
	at := func(min int) time.Time { return time.Date(2026, 10, 14, 15, min, 0, 0, time.Local) }
	src.data.Projects[0].Links = []link.Row{
		{Peer: "beta", Send: true, Receive: true, Held: 1},
		{Peer: "mini/acme-api", Remote: true, Send: true, Receive: true, FilesWaiting: 1, Paused: true},
		{Peer: "nas/old", Remote: true, Asks: &link.Request{ID: "R-9C1B-3", Peer: "nas/old", Allow: "send", State: link.ReqWaiting}},
	}
	src.data.Projects[0].countLinkWaits()
	m.setData(src.data)
	src.linkLogs = map[string][]*link.Message{
		"beta": {
			{ID: "M-1", Peer: "beta", Created: at(2), Bytes: 31, State: link.StateRead, By: "coordinator", Text: "\nThe API landed.\nSecond line, not in the list."},
			{ID: "M-3", Out: true, Peer: "beta", Created: at(9), Bytes: 120, State: link.StateHeld, By: "coordinator", Re: "M-1",
				Attachments: []string{"t-0042/notes.md"}, Text: "Thanks: a long answer that goes on and on, past what the title column can hold in the list, so it is cut there."},
		},
		"mini/acme-api": {
			{ID: "M-4F2A-7", Out: true, Peer: "mini/acme-api", Created: at(4), Bytes: 12, State: link.StateSent, By: "human", Text: "From me"},
			{ID: "M-9C1B-8", Peer: "mini/acme-api", Created: at(6), Bytes: 20, State: link.StateSent, By: "human", Text: "The report, attached",
				File: "report.pdf", FileSize: 4400000, Transfer: "X-9C1B-2"},
		},
	}
	src.linkFiles = []link.Transfer{{ID: "X-9C1B-2", Peer: "mini/acme-api", Message: "M-9C1B-8", Name: "report.pdf", Size: 4400000, State: link.FileOffered}}
	m.openTab("alpha", tabMessages, 0)
	pv := m.projectPopupView()
	needs, es := pv.messages(m)
	lines, _, _ := msgLines(src.data.Projects[0].Links, es, len(needs), 110)
	var b strings.Builder
	write := func(lines []string) {
		for _, l := range lines {
			b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
	}
	write(lines)
	for _, i := range []int{0, 1} {
		v := &msgView{slug: "alpha", other: es[i].other, e: es[i], loaded: time.Now()}
		b.WriteString("== " + ansi.Strip(v.box(m).title) + " · " + v.keys(m) + "\n")
		write(v.lines(m, 110))
	}
	path := filepath.Join("testdata", "messages-tab.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil {
		t.Fatalf("%v (go test -run TestMessagesGolden -update writes it)", err)
	} else if b.String() != string(want) {
		t.Errorf("the tab differs from %s:\n%s", path, b.String())
	}
}

// TestMessagesList: the list shows the newest 200 and says how many
// more there are; the arrows, PgDn and the wheel move the selection,
// a click opens a message; M writes as the user to the selected
// message's project, with a menu when more than one link sends.
func TestMessagesList(t *testing.T) {
	src, m := linkDash(t)
	src.linkLog = nil
	start := time.Now().Add(-time.Hour)
	for i := range 230 {
		src.linkLog = append(src.linkLog, &link.Message{ID: fmt.Sprintf("M-%d", i+1), Peer: "beta", Created: start.Add(time.Duration(i) * time.Second),
			State: link.StateRead, By: "coordinator", Text: fmt.Sprintf("message %d", i+1)})
	}
	m.openTab("alpha", tabMessages, 0)
	pv := m.projectPopupView()
	if n := pv.count(m); n != 200 {
		t.Fatalf("count %d", n)
	}
	body := pv.box(m).body
	if text := ansi.Strip(strings.Join(body, "\n")); !strings.Contains(text, "30 older messages not shown") || !strings.Contains(text, "message 230") || strings.Contains(text, "message 30\n") {
		t.Fatalf("body:\n%s", text)
	}
	keyPress(m, "pgdown")
	pv.wheel(m, 1)
	keyPress(m, "down")
	if pv.sel[tabMessages] != 12 {
		t.Fatalf("sel %d", pv.sel[tabMessages])
	}
	drain(t, m, pv.click(m, 3, 4, false))
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.ID != "M-227" {
		t.Fatalf("a click opened %T", m.top())
	}
	press(m, "esc")

	press(m, "M")
	iv, ok := m.top().(*inputView)
	if !ok || iv.title != "Write to beta" {
		t.Fatalf("M opened %T", m.top())
	}
	drain(t, m, iv.submit("Hello there"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "alpha send beta Hello there" {
		t.Fatalf("acts %v", src.linkActs)
	}
	src.data.Projects[0].Links = append(src.data.Projects[0].Links, link.Row{Peer: "gamma", Send: true})
	m.setData(src.data)
	m.pop()
	press(m, "M")
	if mv, ok := m.top().(*menuView); !ok || len(mv.items) != 2 || mv.items[0].label != "beta" {
		t.Fatalf("M with two links: %T", m.top())
	}
}
