package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
)

// linkNet is netDash's dashboard w × h with links: terminatr ⇄ web here
// (paused, a message of terminatr's held), web's side of it too;
// terminatr ⇄ mini/acme-api, ask first, a file offered; and nas/media
// asks to link with terminatr.
func linkNet(t *testing.T, w, h int) (*dash, *fakeSource) {
	t.Helper()
	m, src := netDash(t, w, h)
	now := time.Now()
	both := config.Link{Allow: config.LinkBoth}
	src.data.Projects[0].Links = []link.Row{
		{Peer: "mini/acme-api", Remote: true, HasMine: true, Mine: config.Link{Allow: config.LinkBoth, Ask: true}, Send: true, Receive: true, FilesWaiting: 1, Last: "M-9C1B-1"},
		{Peer: "nas/media", Remote: true, Asks: &link.Request{ID: "R-A1B2-4", Peer: "nas/media", Project: "terminatr", Allow: config.LinkSend, Created: now, State: link.ReqWaiting}},
		{Peer: "web", HasMine: true, Mine: both, HasTheirs: true, Theirs: both, Send: true, Receive: true, Held: 1, Paused: true, Last: "M-3"},
	}
	src.data.Projects[1].Links = []link.Row{
		{Peer: "terminatr", HasMine: true, Mine: config.Link{Allow: config.LinkBoth, Ask: true}, HasTheirs: true, Theirs: both, Send: true, Receive: true, Paused: true},
	}
	for i := range src.data.Projects {
		src.data.Projects[i].countLinkWaits()
	}
	src.linkLogs = map[string][]*link.Message{
		"web": {
			{ID: "M-0", Peer: "web", Created: peersAt.Add(-26 * time.Hour), Bytes: 5, State: link.StateRead, By: "coordinator", Text: "First"},
			{ID: "M-1", Peer: "web", Created: peersAt.Add(-3 * time.Hour), Bytes: 26, State: link.StateRead, By: "coordinator", Text: "Can you look at the build?"},
			{ID: "M-2", Out: true, Peer: "web", Created: peersAt.Add(-40 * time.Minute), Bytes: 5, State: link.StateRead, By: "coordinator", Text: "On it"},
			{ID: "M-3", Out: true, Peer: "web", Created: peersAt.Add(-time.Minute), Bytes: 7, State: link.StateHeld, By: "coordinator", Text: "Held one"},
		},
		"mini/acme-api": {{ID: "M-9C1B-1", Peer: "mini/acme-api", Created: now, Bytes: 9, State: link.StateRead, By: "human", Text: "The report", File: "report.pdf", FileSize: 4400000, Transfer: "X-9C1B-2"}},
	}
	src.linkFiles = []link.Transfer{{ID: "X-9C1B-2", Project: "terminatr", Peer: "mini/acme-api", Message: "M-9C1B-1", Name: "report.pdf", Size: 4400000, State: link.FileOffered}}
	m.setData(src.data)
	return m, src
}

// TestNetworkLinkRows: each project here has its links under it, each
// with this side's settings and the other side's state, and what each
// needs flagged under it; a link here shows under both projects, each
// with its own side; their project lists the links yours have to it; !
// steps through the needs.
func TestNetworkLinkRows(t *testing.T) {
	m, _ := linkNet(t, 120, 30)
	nv := netOpen(t, m)
	out := ansi.Strip(netBox(m))
	for _, want := range []string{
		"terminatr         [ ] not shared           2 links ⚑",
		"├─ ⇄ mini/acme-api  both · ask · ask me   sends here",
		"│  ⚑ report.pdf 4.2 MB offered · with M-9C1B-1",
		"├─⚑ nas/media asks to link (send only there",
		"└─ ⇄ web            both · ask me           paused ⚑",
		"⚑ M-3 waits for your OK · Held one",
		"⚑ paused: too many messages without you",
		"└─ ⇄ terminatr      both · ask · ask me     paused ⚑",
		"acme-api          ⇄ terminatr",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	var seen []string
	for range 6 {
		press(m, "!")
		seen = append(seen, nv.sel)
	}
	want := []string{
		"n:terminatr>mini/acme-api>file:X-9C1B-2", "n:terminatr>nas/media>ask", "n:terminatr>web>held:M-3",
		"n:terminatr>web>paused", "n:web>terminatr>paused", "n:terminatr>mini/acme-api>file:X-9C1B-2",
	}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Errorf("! went %v", seen)
	}
	// ← on a need goes to its project; enter there folds it.
	press(m, "left")
	if nv.sel != "p:terminatr" {
		t.Fatalf("← on a need: %q", nv.sel)
	}
	press(m, "enter")
	if out := ansi.Strip(netBox(m)); strings.Contains(out, "│  ⚑ report.pdf") || !strings.Contains(out, "⚑ terminatr ⇄ mini/acme-api      report.pdf") {
		t.Errorf("folded: the tree hides its needs, the list keeps them:\n%s", out)
	}
}

// TestNetworkLinkDialog: L on a project opens the picker, grouped by
// server, a server not paired yet last; a project here opens the link
// dialog, which writes nothing until Y, then both sides (one act); a
// paired server's project gets this side only, and the request when
// asked.
func TestNetworkLinkDialog(t *testing.T) {
	m, src := linkNet(t, 120, 30)
	src.data.Projects[0].Links = src.data.Projects[0].Links[:2]
	src.data.Projects[1].Links = nil
	m.setData(src.data)
	nv := netOpen(t, m)
	nv.sel = "p:terminatr"
	press(m, "L")
	if _, ok := m.top().(*linkPickView); !ok {
		t.Fatalf("L: %T", m.top())
	}
	out := ansi.Strip(screen(m))
	for _, want := range []string{"Link terminatr with", "studio · this server", "› web", "mini · paired server", "infra", "nas · paired server", "media · stale", "not paired yet", "+ a server not paired yet · pair it here"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "acme-api") {
		t.Errorf("a linked project is offered:\n%s", out)
	}
	press(m, "enter")
	dv, ok := m.top().(*linkDialogView)
	if !ok {
		t.Fatalf("enter on web: %T", m.top())
	}
	out = ansi.Strip(screen(m))
	for _, want := range []string{"Link terminatr with web", "terminatr and web, both on this server.", "this side         [both ways] · send only · receive only",
		"ask first         on · [off]", "files they send   [ask me] · take them · refuse", "WHAT CROSSES", "✓ messages both ways, text only, up to 8 KB each",
		"✓ files they send, up to 25 MB each, each after your OK", "✕ never the files, paths, repos or worktrees of either project", "One act sets both sides", "Y link"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "'s user") {
		t.Errorf("a link here offers to ask its user:\n%s", out)
	}
	// The arrows change the fields; nothing is written until Y.
	press(m, "right", "down", "right")
	if len(src.links) != 0 || dv.link() != (config.Link{Allow: config.LinkSend, Ask: true, Files: config.FilesAsk}) {
		t.Fatalf("before Y: %v %+v", src.links, dv.link())
	}
	if out := ansi.Strip(screen(m)); !strings.Contains(out, "✕ no files from them: this side only sends") || !strings.Contains(out, "each of terminatr's after your OK") {
		t.Errorf("what crosses doesn't follow:\n%s", out)
	}
	drain(t, m, press(m, "Y"))
	if got := strings.Join(src.links, ";"); got != "terminatr web save send ask true files ask" || !strings.Contains(m.msg, "on both sides") {
		t.Fatalf("Y: %v %q", src.links, m.msg)
	}

	// mini/infra, a paired server's: ask first on by default, and
	// whether to ask its user; Y writes this side, then asks.
	src.links = nil
	press(m, "L")
	pk := m.top().(*linkPickView)
	for i, it := range pk.items {
		if it.other == "mini/infra" {
			pk.sel = i
		}
	}
	press(m, "enter")
	out = ansi.Strip(screen(m))
	for _, want := range []string{"terminatr on this server and mini/infra on mini.", "ask first         [on] · off", "ask mini's user   [yes] · no", "This writes only this side; mini's user sets theirs. Y also asks them to link back."} {
		if !strings.Contains(out, want) {
			t.Errorf("remote dialog lacks %q:\n%s", want, out)
		}
	}
	press(m, "esc")
	if len(src.links) != 0 || m.msg != "link unchanged" {
		t.Fatalf("esc: %v %q", src.links, m.msg)
	}
	press(m, "L")
	pk = m.top().(*linkPickView)
	for i, it := range pk.items {
		if it.other == "mini/infra" {
			pk.sel = i
		}
	}
	press(m, "enter")
	drain(t, m, press(m, "Y"))
	if got := strings.Join(src.links, ";"); got != "terminatr mini/infra save both ask true files ask" || len(src.linkActs) != 1 || src.linkActs[0] != "terminatr request mini/infra both" {
		t.Fatalf("remote Y: %v %v", src.links, src.linkActs)
	}

	// Take them asks before the dialog writes.
	src.links = nil
	nv.sel = "l:terminatr>mini/acme-api"
	press(m, "enter")
	dv = m.top().(*linkDialogView)
	dv.sel = 2
	press(m, "right")
	press(m, "Y")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "Take files from mini/acme-api without asking?") || !strings.Contains(cv.question, "This takes 1 waiting file") {
		t.Fatalf("take them: %T %+v", m.top(), cv)
	}
	drain(t, m, press(m, "Y"))
	if got := strings.Join(src.links, ";"); got != "terminatr mini/acme-api save both ask true files auto" {
		t.Fatalf("take them saved %v", src.links)
	}
}

// TestNetworkLinkPicker: the picker's last entry pairs a new server;
// from their project it lists only the projects here not linked to it.
func TestNetworkLinkPicker(t *testing.T) {
	m, _ := linkNet(t, 120, 30)
	nv := netOpen(t, m)
	nv.sel = "p:web"
	press(m, "L")
	pk := m.top().(*linkPickView)
	pk.sel = len(pk.items) - 1
	press(m, "enter")
	if in, ok := m.top().(*inputView); !ok || in.title != "Pair a server" {
		t.Fatalf("a server not paired yet: %T", m.top())
	}
	press(m, "esc")
	nv.sel = "t:mini/acme-api"
	press(m, "L")
	if _, ok := m.top().(*linkPickView); !ok {
		t.Fatalf("L on theirs: %T", m.top())
	}
	out := ansi.Strip(screen(m))
	if !strings.Contains(out, "Link one of yours with mini/acme-api") || !strings.Contains(out, "web") || strings.Contains(out, "terminatr\n") || strings.Contains(out, "not paired yet") {
		t.Fatalf("picker from theirs:\n%s", out)
	}
	press(m, "enter")
	if dv, ok := m.top().(*linkDialogView); !ok || dv.slug != "web" || dv.other != "mini/acme-api" {
		t.Fatalf("enter: %T", m.top())
	}
}

// TestNetworkLinkKeys: K turns ask first, F steps the files (take them
// asks first), R resumes a paused link or asks their user to link back,
// M writes as you, X unlinks after Y; in the panel the arrows change a
// switch.
func TestNetworkLinkKeys(t *testing.T) {
	m, src := linkNet(t, 160, 36)
	nv := netOpen(t, m)
	nv.sel = "l:terminatr>mini/acme-api"
	if out := ansi.Strip(netBox(m)); !strings.Contains(out, "enter edit · K ask first · F files · R ask back · M write · X unlink") {
		t.Errorf("keys:\n%s", out)
	}
	drain(t, m, press(m, "K"))
	if got := src.links[len(src.links)-1]; got != "terminatr mini/acme-api ask false" {
		t.Fatalf("K: %v", src.links)
	}
	press(m, "F")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "takes 1 waiting file") {
		t.Fatalf("F to take them: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if got := src.links[len(src.links)-1]; got != "terminatr mini/acme-api files auto" {
		t.Fatalf("F: %v", src.links)
	}
	press(m, "R")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Ask mini's user to link mini/acme-api back with terminatr?") {
		t.Fatalf("R ask back: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr request mini/acme-api both" {
		t.Fatalf("R: %v", src.linkActs)
	}
	nv.sel = "l:terminatr>web"
	drain(t, m, press(m, "R"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr resume web" {
		t.Fatalf("R paused: %v", src.linkActs)
	}
	press(m, "M")
	in, ok := m.top().(*inputView)
	if !ok || in.title != "Write to web" {
		t.Fatalf("M: %T", m.top())
	}
	press(m, "h", "i")
	drain(t, m, press(m, "enter"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr send web hi" {
		t.Fatalf("M: %v", src.linkActs)
	}
	press(m, "X")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "This removes both sides") {
		t.Fatalf("X: %T", m.top())
	}
	press(m, "N")
	if strings.Contains(src.links[len(src.links)-1], "off") {
		t.Fatalf("N unlinked: %v", src.links)
	}
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if got := src.links[len(src.links)-1]; got != "terminatr web save off ask false files ask" {
		t.Fatalf("X: %v", src.links)
	}

	// The panel: tab, then → on this side's direction writes it.
	tab(m)
	if !nv.panel {
		t.Fatal("tab: no panel")
	}
	drain(t, m, press(m, "right"))
	if got := src.links[len(src.links)-1]; got != "terminatr web save send ask false files ask" {
		t.Fatalf("panel →: %v", src.links)
	}
}

// TestNetworkNeeds: a held message and a file go through their
// message's dialog (enter opens it, A and X act there); a request's A
// offers this side's directions, its mirror first, and X declines after
// Y; a paused link's R resumes it.
func TestNetworkNeeds(t *testing.T) {
	m, src := linkNet(t, 120, 30)
	nv := netOpen(t, m)
	nv.sel = "n:terminatr>web>held:M-3"
	press(m, "enter")
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.ID != "M-3" {
		t.Fatalf("enter on held: %T", m.top())
	}
	press(m, "esc")
	drain(t, m, press(m, "A"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr approve M-3" {
		t.Fatalf("A on held: %v", src.linkActs)
	}
	press(m, "esc")
	nv.sel = "n:terminatr>web>held:M-3"
	press(m, "X")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("X on held: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr drop M-3" {
		t.Fatalf("X on held: %v", src.linkActs)
	}
	m.pop() // the message's dialog

	nv.sel = "n:terminatr>mini/acme-api>file:X-9C1B-2"
	drain(t, m, press(m, "A"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr accept-file X-9C1B-2" {
		t.Fatalf("A on a file: %v", src.linkActs)
	}
	if mv, ok := m.top().(*msgView); !ok || mv.e.msg.Text != "The report" {
		t.Fatalf("the file's message: %T", m.top())
	}
	m.pop()

	nv.sel = "n:terminatr>nas/media>ask"
	press(m, "A")
	mv, ok := m.top().(*menuView)
	if !ok || mv.items[0].label != "receive only, ask first" || len(mv.items) != 6 {
		t.Fatalf("A on a request: %T", m.top())
	}
	drain(t, m, press(m, "enter"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr accept R-A1B2-4 receive ask" {
		t.Fatalf("accept: %v", src.linkActs)
	}
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "terminatr decline R-A1B2-4" {
		t.Fatalf("decline: %v", src.linkActs)
	}
	nv.sel = "n:web>terminatr>paused"
	drain(t, m, press(m, "R"))
	if got := src.linkActs[len(src.linkActs)-1]; got != "web resume terminatr" {
		t.Fatalf("R: %v", src.linkActs)
	}
}

// TestNetworkLinksGolden: the popup with links at 80×24 (two detail
// lines), 120×30 and 160×36 (the panel and the NEEDS YOU list over the
// tree), on a link to a paired server's project, on one here with its
// last messages, on a need, on a project with its links' map and on a
// listed need, matching
// testdata/network-links-<w>x<h>.golden (go test -run
// TestNetworkLinksGolden -update rewrites them).
func TestNetworkLinksGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 36}} {
		m, _ := linkNet(t, size[0], size[1])
		nv := netOpen(t, m)
		var b strings.Builder
		sels := []string{"l:terminatr>mini/acme-api", "l:terminatr>web", "n:terminatr>web>held:M-3", "p:terminatr"}
		if size[0] >= 120 {
			sels = append(sels, "!n:terminatr>mini/acme-api>file:X-9C1B-2")
		}
		for _, sel := range sels {
			nv.sel = sel
			b.WriteString("== " + sel + "\n" + ansi.Strip(netBox(m)) + "\n")
		}
		got := b.String()
		path := filepath.Join("testdata", fmt.Sprintf("network-links-%dx%d.golden", size[0], size[1]))
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (go test -run TestNetworkLinksGolden -update writes it)", err)
		}
		if got != string(want) {
			t.Errorf("the Network popup differs from %s:\n%s", path, got)
		}
	}
}
