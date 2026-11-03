package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func tabText(m *dash) string { return ansi.Strip(m.View().Content) }

// TestInviteRows (B14): a paired program's invite is a NEEDS YOU row of
// the Messages tab; A opens the accept dialog with share and needs view
// ticked and answer not, enter asks, then accepts with them; X declines
// after a confirm. A membership row leaves with L, after a confirm.
func TestInviteRows(t *testing.T) {
	src, m := linkDash(t)
	src.invites = map[string][]link.Invite{"alpha": {{ID: "P-4F2A-7", Project: "alpha", Peer: "studio/apps", State: link.ReqWaiting,
		Features: []string{"tasks", "messages", "status", "starts"}}}}
	m.openTab("alpha", tabMessages, 0)
	view := tabText(m)
	for _, want := range []string{"1 program invite", "invites it into program apps on studio (P-4F2A-7)", "A accept the invite"} {
		if !strings.Contains(view, want) {
			t.Fatalf("no %q in\n%s", want, view)
		}
	}
	press(m, "A")
	iv, ok := m.top().(*inviteView)
	if !ok {
		t.Fatalf("A opened %T", m.top())
	}
	if !iv.o.Share || !iv.o.NeedsView || iv.o.NeedsAnswer || iv.o.Starts || !iv.o.Status || iv.o.Tasks != config.ProgramTasksFile {
		t.Fatalf("defaults %+v", iv.o)
	}
	// Starts asks first; N leaves it off.
	iv.sel = invStarts
	press(m, "space")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("starts on didn't ask: %T", m.top())
	}
	press(m, "N")
	if iv.o.Starts {
		t.Fatal("starts on after N")
	}
	iv.sel = invTasks
	press(m, "space")
	drain(t, m, press(m, "enter"))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "proposes to you") || !strings.Contains(cv.question, "sees its needs in full.") {
		t.Fatalf("accept confirm: %T %+v", m.top(), cv)
	}
	drain(t, m, press(m, "Y"))
	if len(src.programActs) != 1 || !strings.HasPrefix(src.programActs[0], "alpha accept P-4F2A-7 ") || !strings.Contains(src.programActs[0], "Tasks:propose") ||
		!strings.Contains(src.programActs[0], "NeedsView:true NeedsAnswer:false") {
		t.Fatalf("acts %v", src.programActs)
	}

	press(m, "X")
	drain(t, m, press(m, "Y"))
	if len(src.programActs) != 2 || !strings.HasPrefix(src.programActs[1], "alpha decline P-4F2A-7") {
		t.Fatalf("decline: %v", src.programActs)
	}

	src.invites["alpha"] = []link.Invite{{ID: "P-4F2A-7", Project: "alpha", Peer: "studio/apps", State: link.ReqAccepted, Granted: []string{"tasks", "messages"}}}
	m.reloadMessages()
	if view := tabText(m); !strings.Contains(view, "member of program apps on studio: tasks, messages") || strings.Contains(view, "program invite") {
		t.Fatalf("membership row:\n%s", view)
	}
	press(m, "L")
	drain(t, m, press(m, "Y"))
	if len(src.programActs) != 3 || !strings.HasPrefix(src.programActs[2], "alpha leave studio/apps") {
		t.Fatalf("leave: %v", src.programActs)
	}

	// Programs off: the source shows none, and the tab has no row.
	src.invites = nil
	m.reloadMessages()
	if view := tabText(m); strings.Contains(view, "program") {
		t.Fatalf("a program row while off:\n%s", view)
	}
}

// TestProgramLinkSettings (B14): a project in a paired program has a
// Program section in its settings, each key changed after its own
// confirm; one that isn't (or with programs off) has none.
func TestProgramLinkSettings(t *testing.T) {
	src, m := linkDash(t)
	if rows := m.projectSettingRows("alpha"); slices.ContainsFunc(rows, func(r setting) bool { return r.section == "Program" }) {
		t.Fatal("a Program section without a membership")
	}
	src.data.ProgramLinks = map[string]config.ProgramLink{"alpha": {Program: "studio/apps", Tasks: config.ProgramTasksFile, Status: true}}
	m.setData(src.data)
	rows := m.projectSettingRows("alpha")
	i := slices.IndexFunc(rows, func(r setting) bool { return r.label == "Director starts threads" })
	if i < 0 || rows[len(rows)-1].label != "Mods" || rows[i].value(m) != "off" {
		t.Fatalf("rows: %d, last %q", i, rows[len(rows)-1].label)
	}
	rows[i].change(m)
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "caps, pause and start rules hold") {
		t.Fatalf("starts confirm: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	j := slices.IndexFunc(rows, func(r setting) bool { return r.label == "Director's tasks" })
	rows[j].change(m)
	drain(t, m, press(m, "Y"))
	k := slices.IndexFunc(rows, func(r setting) bool { return r.label == "Status to the program" })
	rows[k].change(m)
	drain(t, m, press(m, "Y"))
	if want := []string{"alpha set program_starts true", "alpha set program_tasks propose", "alpha set program_status false"}; !slices.Equal(src.programActs, want) {
		t.Fatalf("acts %v, want %v", src.programActs, want)
	}
	rows[0+slices.IndexFunc(rows, func(r setting) bool { return r.label == "Program" })].change(m)
	drain(t, m, press(m, "Y"))
	if got := src.programActs[len(src.programActs)-1]; !strings.HasPrefix(got, "alpha leave studio/apps") {
		t.Fatalf("leave from settings: %v", src.programActs)
	}
}

// TestMembersPaired (B14): the members view lists the projects a paired
// server with programs on shares, with their invite's state; one with
// programs off is named, its projects not listed; saving a new paired
// member says it is invited.
func TestMembersPaired(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"mini/old\"]\n")
	src := &fakeSource{data: testData()}
	src.data.Peers = []proto.PeerMirror{
		{Alias: "mini", Program: true, Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "api"}}, {PeerProject: proto.PeerProject{Project: "old"}}}},
		{Alias: "nas", Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "files"}}}},
	}
	src.data.Programs = []ProgramData{{Settings: config.Program{Slug: "acme", Projects: []string{"web", "mini/old"}}}}
	src.data.Programs[0].Slug = "acme"
	src.invites = map[string][]link.Invite{"acme": {{ID: "P-1", Out: true, Project: "acme", Peer: "mini/old", State: link.ReqWaiting}}}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	m.setData(src.data)
	mv := newMembersView(m, "acme")
	text := ansi.Strip(strings.Join(mv.lines(m, 110), "\n"))
	for _, want := range []string{"[x] mini/old · invited, no answer yet", "[ ] mini/api · on a paired server", "nas has programs off"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, "nas/files") {
		t.Fatalf("a project of a server with programs off:\n%s", text)
	}
	mv.sel = slices.Index(mv.all, "mini/api")
	mv.key(m, keyMsg("space"))
	if !strings.Contains(ansi.Strip(strings.Join(mv.lines(m, 110), "\n")), "mini/api · invited when you save") {
		t.Fatal("no note on the new member")
	}
	m.push(mv)
	drain(t, m, press(m, "enter"))
	if !strings.Contains(m.msg, "invited mini/api; their users accept there") || !slices.Contains(src.settings, "programs.acme.projects=[web mini/old mini/api]") {
		t.Fatalf("msg %q, settings %v", m.msg, src.settings)
	}
}
