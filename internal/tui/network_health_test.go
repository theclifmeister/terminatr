package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
)

// TestNetworkLinkHealth: a link's row ends with its health, in full
// and, for a tree under 70 cells, short: waiting on their user (in the
// warning colour), their server offline with what waits in the outbox,
// a message undelivered, half-linked here, paused.
func TestNetworkLinkHealth(t *testing.T) {
	m, src := linkNet(t, 160, 36)
	nv := netOpen(t, m)
	both := config.Link{Allow: config.LinkBoth}
	mine := func(peer string) link.Row {
		_, _, remote := config.Remote(peer)
		return link.Row{Peer: peer, Remote: remote, HasMine: true, Mine: both, Send: true, Receive: true}
	}
	waiting := mine("mini/acme-api")
	waiting.Asked = &link.Request{ID: "R-4F2A-9", Peer: "mini/acme-api", State: link.ReqWaiting}
	half := mine("web")
	paused := mine("web")
	paused.HasTheirs, paused.Theirs, paused.Paused = true, both, true
	queued := []*link.Message{{ID: "M-4F2A-7", Out: true, State: link.StateQueued}, {ID: "M-4F2A-8", Out: true, State: link.StateQueued}}
	for _, c := range []struct {
		name        string
		row         link.Row
		log         []*link.Message
		full, short string
		warn        bool
	}{
		{"waiting", waiting, nil, "waiting on mini's user", "waiting on mini", true},
		{"offline, queued", mine("nas/media"), queued, "nas offline · 2 queued in the outbox", "offline · 2 queued", false},
		{"offline", mine("nas/media"), nil, "nas offline", "nas offline", false},
		{"undelivered", mine("mini/infra"), []*link.Message{{ID: "M-4F2A-5", Out: true, State: link.StateUndelivered}}, "undelivered (see the inbox)", "undelivered", true},
		{"delivered since", mine("mini/infra"), []*link.Message{{ID: "M-4F2A-5", Out: true, State: link.StateUndelivered}, {ID: "M-4F2A-6", Out: true, State: link.StateRead}}, "takes ours", "takes ours", false},
		{"half-linked", half, nil, "half-linked: web has no entry", "half-linked", true},
		{"paused", paused, nil, "paused ⚑", "paused ⚑", true},
	} {
		src.linkLogs = map[string][]*link.Message{c.row.Peer: c.log}
		for _, short := range []bool{false, true} {
			nv.relog(m)
			got, st := nv.linkState(m, "terminatr", c.row, short)
			want := map[bool]string{false: c.full, true: c.short}[short]
			if got != want {
				t.Errorf("%s (short %v): %q, want %q", c.name, short, got, want)
			}
			if c.warn && st.Render("x") != th().Warn.Render("x") {
				t.Errorf("%s: not in the warning colour", c.name)
			}
		}
		// On the row: in full from 70 cells, short under.
		for w, want := range map[int]string{77: c.full, 57: c.short} {
			nv.relog(m)
			line := ansi.Strip(nv.linkLine(m, netRow{kind: netLink, slug: "terminatr", other: c.row.Peer, link: c.row}, w))
			if !strings.HasSuffix(line, want) || ansi.StringWidth(line) != w {
				t.Errorf("%s at %d: %q", c.name, w, line)
			}
		}
	}
}

// TestNetworkNeedsList: from an inner width of netWide every need is
// listed over the tree, one row each, folded or not; ↑ from the tree
// goes into it, a click selects a row, and its rows answer with the
// tree rows' handlers (A, X, R); ← goes to the need in the tree. Under
// netWide there is no list.
func TestNetworkNeedsList(t *testing.T) {
	m, src := linkNet(t, 120, 30)
	nv := netOpen(t, m)
	out := ansi.Strip(netBox(m))
	for _, want := range []string{
		"⚑ NEEDS YOU 5 · 1 message waits for your OK · 1 file waits for you · 2 paused links · 1 link request",
		"⚑ terminatr ⇄ mini/acme-api      report.pdf 4.2 MB offered · with M-9C1B-1",
		"⚑ terminatr · nas/media          asks to link (send only there, R-A1B2-4)",
		"⚑ terminatr ⇄ web                M-3 waits for your OK · Held one",
		"⚑ web ⇄ terminatr                paused: too many messages without you",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	nv.sel = "g:"
	press(m, "up")
	if nv.sel != "!n:web>terminatr>paused" {
		t.Fatalf("↑ from the server row: %q", nv.sel)
	}
	// ! steps through the tree's needs, not the list's.
	press(m, "!")
	if nv.sel != "n:terminatr>mini/acme-api>file:X-9C1B-2" {
		t.Fatalf("! from the list: %q", nv.sel)
	}
	clickOn(t, m, "⚑ terminatr ⇄ web                M-3")
	if nv.sel != "!n:terminatr>web>held:M-3" {
		t.Fatalf("a click: %q", nv.sel)
	}
	if out := ansi.Strip(netBox(m)); !strings.Contains(out, "enter read · A send · X drop") {
		t.Errorf("keys on a listed held message:\n%s", out)
	}
	drain(t, m, press(m, "A"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr approve M-3" {
		t.Fatalf("A on the list's held message: %v", src.linkActs)
	}
	m.pop() // the message's dialog
	nv.sel = "!n:terminatr>nas/media>ask"
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr decline R-A1B2-4" {
		t.Fatalf("X on the list's request: %v", src.linkActs)
	}
	nv.sel = "!n:web>terminatr>paused"
	drain(t, m, press(m, "R"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "web resume terminatr" {
		t.Fatalf("R on the list's paused link: %v", src.linkActs)
	}
	// ← to it in the tree, its project unfolded.
	nv.folded["p:web"] = true
	press(m, "left")
	if nv.sel != "n:web>terminatr>paused" || nv.folded["p:web"] {
		t.Fatalf("← from the list: %q %v", nv.sel, nv.folded)
	}
	if _, i, ok := nv.selected(m, nv.rows(m)); !ok || nv.rows(m)[i].listed {
		t.Fatalf("← selected no tree row")
	}

	m, _ = linkNet(t, 80, 24)
	netOpen(t, m)
	if out := ansi.Strip(netBox(m)); strings.Contains(out, "NEEDS YOU") {
		t.Errorf("a list under netWide:\n%s", out)
	}
}

// TestNetworkLinkMap: a project's panel draws its links as a map from
// netMapWide, a line style for each state with a legend; a link's panel
// shows its last three messages, newest first.
func TestNetworkLinkMap(t *testing.T) {
	m, _ := linkNet(t, 120, 30)
	nv := netOpen(t, m)
	nv.sel = "p:terminatr"
	out := ansi.Strip(netBox(m))
	for _, want := range []string{
		"LINKS 2",
		" terminatr ┄┄⇄┄┄ mini/acme-api ⚑ ask · file…",
		"           ━━⇄━━ web           ⚑ files: ask…",
		"━ linked  ┄ waiting  ─ offline  ━ paused",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if c, st := nv.mapLine(m, "terminatr", link.Row{Peer: "nas/media", Remote: true, HasMine: true}); c != "─" || st.Render("x") != th().Faint.Render("x") {
		t.Errorf("offline: %q", c)
	}
	if c, _ := nv.mapLine(m, "terminatr", link.Row{Peer: "web", HasMine: true, HasTheirs: true}); c != "━" {
		t.Errorf("linked here: %q", c)
	}
	if c, _ := nv.mapLine(m, "terminatr", link.Row{Peer: "web", HasMine: true}); c != "┄" {
		t.Errorf("half-linked: %q", c)
	}

	nv.sel = "l:terminatr>web"
	out = ansi.Strip(netBox(m))
	for _, want := range []string{"CONVERSATION · all of it in the Messages tab", "→ M-3 Held one", "→ M-2 On it", "← M-1 Can you look at the build?"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "M-0") || strings.Index(out, "M-3 Held") > strings.Index(out, "M-1 Can") {
		t.Errorf("not the last three, newest first:\n%s", out)
	}
}

// TestNetworkNeedsListShares: a paired server's share request is listed
// and counted too, and its row answers with the tree row's handlers: X
// declines after Y, A picks a project, then the share confirm; ← goes
// to it under its server.
func TestNetworkNeedsListShares(t *testing.T) {
	m, src := shareDash(t, 120, 30, true)
	nv := netOpen(t, m)
	out := ansi.Strip(netBox(m))
	for _, want := range []string{"⚑ NEEDS YOU 1 · 1 share request", `⚑ mini                           asks you to share a project (for its acme) · "the API docs"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	clickOn(t, m, "⚑ mini   ")
	if nv.sel != "!s:mini:S-91C3-7" {
		t.Fatalf("a click: %q", nv.sel)
	}
	press(m, "X")
	if cv, ok := m.top().(*confirmView); !ok || cv.question != "Decline mini's request (S-91C3-7)? Nothing is shared; mini hears of it." {
		t.Fatalf("X on the list's request: %T", m.top())
	}
	press(m, "N")
	src.data.Projects[1].Safety = nil
	press(m, "A")
	if title, _ := menuLabels(t, m); title != "Share a project with mini? · pick one of yours" {
		t.Fatalf("A on the list's request: %q", title)
	}
	press(m, "esc")
	nv.sel = "!s:mini:S-91C3-7"
	nv.folded["g:mini"] = true
	press(m, "left")
	if nv.sel != "s:mini:S-91C3-7" || nv.folded["g:mini"] {
		t.Fatalf("← from the list: %q", nv.sel)
	}
}
