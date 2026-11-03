package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/models/modelstest"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// progTree is a tree of a program acme (members api, then web) beside a
// standalone project zoo, its director running.
func progTree(tree view.Tree) []treeRow {
	s := config.Defaults
	ps := []ProjectData{
		{Slug: "api", Safety: &s, Threads: []ThreadRow{{Record: &thread.Record{ID: "t-1"}}, {Record: &thread.Record{ID: "t-2"}}}},
		{Slug: "web", Safety: &s, Questions: 1},
		{Slug: "zoo", Safety: &s},
	}
	sessions := []proto.SessionInfo{{ID: "s-d", Role: proto.RoleDirector, Project: "acme", State: "working"}}
	progs := []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: []string{"web", "api"}}}}
	return buildTree(ps, sessions, treeIn{tree: tree, programs: progs})
}

// openMembers unfolds acme's members web and api: they start folded.
var openMembers = view.Tree{Unfolded: []string{"web", "api"}}

// TestProgramRows: a program's row, its director's, then its members in
// its order, one level in under a │; the program counts its members'
// threads and needs you when one does.
func TestProgramRows(t *testing.T) {
	rows := progTree(openMembers)
	if keys := rowKeys(rows); !slices.Equal(keys, []string{"g:acme", "r:acme", "p:web", "c:web", "p:api", "c:api", "t:api/t-1", "t:api/t-2", "p:zoo", "c:zoo"}) {
		t.Fatalf("rows %v", keys)
	}
	g, d := rows[0], rows[1]
	if g.threads != 2 || !g.hint || d.session != "s-d" || d.last {
		t.Errorf("program %+v, director %+v", g, d)
	}
	line := func(r treeRow) string { return ansi.Strip(treeCells(r, 40, false, false)) }
	if l := line(g); !strings.Contains(l, "◆ acme") || !strings.Contains(l, "2") {
		t.Errorf("program row %q", l)
	}
	if l := line(d); !strings.Contains(l, "director") || !strings.Contains(l, "├─") {
		t.Errorf("director row %q", l)
	}
	for _, i := range []int{2, 3, 6} {
		if l := line(rows[i]); !strings.HasPrefix(l, " │ ") {
			t.Errorf("member row %d %q", i, l)
		}
	}
	if l := line(rows[8]); strings.Contains(l, "│") {
		t.Errorf("a standalone project under the program: %q", l)
	}
	// Its members start folded, the standalone project unfolded; folded,
	// a member's row still says it needs you.
	def := progTree(view.Tree{})
	if keys := rowKeys(def); !slices.Equal(keys, []string{"g:acme", "r:acme", "p:web", "p:api", "p:zoo", "c:zoo"}) || !def[2].fold || !def[2].hint {
		t.Errorf("by default %v %+v", keys, def[2])
	}
	// Folded: its row alone, with its director's state.
	folded := progTree(view.Tree{Folded: []string{"g:acme"}})
	if keys := rowKeys(folded); !slices.Equal(keys, []string{"g:acme", "p:zoo", "c:zoo"}) || folded[0].state == "" {
		t.Errorf("folded %v %+v", keys, folded[0])
	}
	// Enter on the program's row shows its dashboard (B8), on its
	// director's opens the director; a preview shows the dashboard, or
	// the director's session only while it runs.
	if tg, ok, _ := g.target(); !ok || tg.Project != "g:acme" || tg.Program != "" {
		t.Errorf("program target %+v", tg)
	}
	if tg, ok := g.preview(); !ok || tg.Project != "g:acme" {
		t.Errorf("program preview %+v %v", tg, ok)
	}
	if tg, ok, _ := d.target(); !ok || tg.Program != "acme" || tg.Project != "" {
		t.Errorf("director target %+v", tg)
	}
	if tg, ok := rows[1].preview(); !ok || tg.Session != "s-d" {
		t.Errorf("director preview %+v %v", tg, ok)
	}
	// The details and the key bar name the program.
	if a, _ := g.details(); !strings.Contains(a, "program acme") {
		t.Errorf("details %q", a)
	}
}

// TestProgramKeys: ← and → fold a program (g:<slug>), ← on its director
// or a folded member goes up to it, Z folds it with the rest.
func TestProgramKeys(t *testing.T) {
	rows := progTree(openMembers)
	st := sideKeyStep(rows, 40, "g:acme", sideOut)
	tr := view.Tree{}
	if st.refold == nil {
		t.Fatalf("← on the program: %+v", st)
	}
	st.refold(&tr)
	if !tr.IsFolded("g:acme") {
		t.Errorf("tree %+v", tr)
	}
	if st := sideKeyStep(rows, 40, "r:acme", sideOut); st.sel != "g:acme" {
		t.Errorf("← on the director: %+v", st)
	}
	folded := progTree(view.Tree{Unfolded: []string{"api"}})
	if st := sideKeyStep(folded, 40, "p:web", sideOut); st.sel != "g:acme" {
		t.Errorf("← on a folded member: %+v", st)
	}
	// → unfolds a member, ← folds it again: a fold of the user's, kept
	// if it leaves the program.
	if st := sideKeyStep(folded, 40, "p:web", sideIn); st.refold == nil {
		t.Errorf("→ on a folded member: %+v", st)
	} else {
		v := view.Tree{Unfolded: []string{"api"}}
		st.refold(&v)
		if v.MemberFolded("web") || v.IsFolded("web") {
			t.Errorf("→ unfolded %+v", v)
		}
		st = sideKeyStep(progTree(v), 40, "p:web", sideOut)
		st.refold(&v)
		if !v.MemberFolded("web") || !v.IsFolded("web") {
			t.Errorf("← folded %+v", v)
		}
	}
	// A project that leaves the program goes back to unfolded, unless
	// the user folded it.
	solo := buildTree([]ProjectData{{Slug: "web", Safety: &config.Defaults}}, nil, treeIn{tree: view.Tree{}})
	if keys := rowKeys(solo); !slices.Equal(keys, []string{"p:web", "c:web"}) {
		t.Errorf("left the program %v", keys)
	}
	if st := sideKeyStep(rows, 40, "c:web", sideFoldAll); st.sel != "g:acme" || st.refold == nil {
		t.Errorf("Z on a member's row: %+v", st)
	} else {
		var all view.Tree
		st.refold(&all)
		if !all.IsFolded("g:acme") || !all.IsFolded("zoo") || !all.MemberFolded("web") {
			t.Errorf("Z folded %+v", all)
		}
	}
	// Z from all folded unfolds the members too.
	if st := sideKeyStep(progTree(view.Tree{Folded: []string{"g:acme", "zoo"}}), 40, "g:acme", sideFoldToggle); st.refold == nil {
		t.Errorf("Z to unfold: %+v", st)
	} else {
		var all view.Tree
		st.refold(&all)
		if all.IsFolded("g:acme") || all.MemberFolded("web") || all.MemberFolded("api") {
			t.Errorf("Z unfolded %+v", all)
		}
	}
	// needs first keeps a program's rows together.
	if keys := rowKeys(needsFirstOrder(rows)); !slices.Equal(keys[:8], rowKeys(rows)[:8]) {
		t.Errorf("needs first %v", keys)
	}
	// The filter keeps a member's program.
	if keys := rowKeys(filterTree(rows, "api")); !slices.Equal(keys, []string{"g:acme", "p:api"}) {
		t.Errorf("filtered %v", keys)
	}
}

// TestProgramSettings: the popup opens from the program's row; delegate
// asks first and writes only on Y, propose takes one key; the members
// picker keeps a project another program has out.
func TestProgramSettings(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\"]\n[programs.other]\nprojects = [\"zoo\"]\n")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	data := Data{Projects: []ProjectData{{Slug: "api"}, {Slug: "web"}, {Slug: "zoo"}}}
	for _, slug := range []string{"acme", "other"} {
		g, _ := cfg.Program(slug)
		data.Programs = append(data.Programs, ProgramData{SideProgram: server.SideProgram{Slug: slug, Projects: g.Projects, Authority: g.Authority}, Settings: g})
	}
	src := &fakeSource{data: data}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	m.setData(data)
	m.focus, m.sideSel = areaSide, "g:acme"
	drain(t, m, press(m, "A"))
	pv, ok := m.top().(*programView)
	if !ok {
		t.Fatalf("popup %T", m.top())
	}
	if out := screen(m); !strings.Contains(out, "acme · program settings") || !strings.Contains(out, "Authority") {
		t.Fatalf("popup:\n%s", out)
	}
	pv.list.sel = slices.IndexFunc(pv.list.rows, func(r setting) bool { return r.label == "Authority" })
	drain(t, m, press(m, "enter"))
	if _, ok := m.top().(*confirmView); !ok || len(src.settings) != 0 {
		t.Fatalf("delegate didn't ask first: %T %v", m.top(), src.settings)
	}
	drain(t, m, press(m, "N"))
	if len(src.settings) != 0 {
		t.Fatalf("N wrote %v", src.settings)
	}
	drain(t, m, press(m, "enter"))
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.settings, []string{"programs.acme.authority=delegate"}) {
		t.Fatalf("settings %v", src.settings)
	}
	if c, _ := config.Load(); func() string { g, _ := c.Program("acme"); return g.Authority }() != config.AuthorityDelegate {
		t.Error("delegate not written")
	}
	// Members: zoo is other's, api ticks; enter saves in order.
	mv := newMembersView(m, "acme")
	mv.sel = slices.Index(mv.all, "zoo")
	mv.key(m, keyMsg("space"))
	if mv.on["zoo"] {
		t.Error("ticked a project of another program")
	}
	mv.sel = slices.Index(mv.all, "api")
	mv.key(m, keyMsg("space"))
	if got := mv.members(); !slices.Equal(got, []string{"web", "api"}) {
		t.Errorf("members %v", got)
	}
}

// TestProgramDirectorParity: the program popup has a Director model row
// (its agent's models; a change syncs the running director) and a
// Remote control row with the coordinator's note; prefix+r and prefix+C
// on a program's row act on its director (T962).
func TestProgramDirectorParity(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\"]\nremote_control = true\n")
	modelstest.Answer(t, "claude", "alpha", "beta")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	g, _ := cfg.Program("acme")
	data := Data{Projects: []ProjectData{{Slug: "web"}},
		Programs: []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: g.Projects}, Settings: g}},
		Sessions: []proto.SessionInfo{{ID: "s-d", Role: proto.RoleDirector, Project: "acme", Agent: "claude", State: "idle"}}}
	src := &fakeSource{data: data}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	m.setData(data)
	m.focus, m.sideSel = areaSide, "g:acme"
	drain(t, m, press(m, "A"))
	pv, ok := m.top().(*programView)
	if !ok {
		t.Fatalf("popup %T", m.top())
	}
	row := func(label string) setting {
		i := slices.IndexFunc(pv.list.rows, func(r setting) bool { return r.label == label })
		if i < 0 {
			t.Fatalf("no %s row", label)
		}
		pv.list.sel = i
		return pv.list.rows[i]
	}
	if r := row("Director model"); r.value(m) != "the coordinator's" {
		t.Errorf("model default %q", r.value(m))
	}
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.settings, []string{"programs.acme.director_model=alpha", "sync s-d"}) {
		t.Fatalf("model change: %v", src.settings)
	}
	r := row("Remote control")
	if r.value(m) != "on" {
		t.Errorf("remote %q", r.value(m))
	}
	if note := ansi.Strip(strings.Join(r.note(m), " ")); !strings.Contains(note, "the running director has it off; tm turns it on once the director is idle") {
		t.Errorf("remote note %q", note)
	}
	m.pop()
	// prefix+r on the program's row asks about its director.
	m.askRemote()
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.remote, []string{"director:acme on"}) {
		t.Fatalf("prefix+r: %v", src.remote)
	}
	drain(t, m, m.prefixCommand("C"))
	if !slices.Equal(src.saveCleared, []string{"director:acme"}) || !strings.Contains(m.msg, "asked acme's director to save") {
		t.Fatalf("prefix+C: %v %q", src.saveCleared, m.msg)
	}
}

// TestAfterText (the programs design §6.3): a member's task shows the
// refs to other projects' tasks with their state; a project in no
// program shows its refs plain.
func TestAfterText(t *testing.T) {
	data := Data{
		Projects: []ProjectData{{Slug: "web", Program: "acme"}, {Slug: "zoo"}},
		Programs: []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: []string{"web", "api"}},
			Deps: map[string]program.Dep{"api/T12": {State: program.DepOpen, Status: "review"}}}},
	}
	m := newDash(DashOptions{Source: &fakeSource{data: data}, Width: 120 + sideDefault, Height: 40})
	m.setData(data)
	tk := &tasks.Task{ID: 40, Title: "Login", Status: tasks.Ready, After: []int{10}, AfterX: []string{"api/T12", "ios/T7"}}
	if got := m.afterText("web", tk); got != "T10, api/T12 (review), ios/T7" {
		t.Errorf("member: %q", got)
	}
	if got := m.afterText("zoo", tk); got != "T10, api/T12, ios/T7" {
		t.Errorf("standalone: %q", got)
	}
	d := &panel{w: 60}
	taskPanelWith(d, tk, nil, m.afterText("web", tk), "", thread.Usage{}, nil)
	if out := ansi.Strip(strings.Join(d.lines, "\n")); !strings.Contains(out, "api/T12 (review)") {
		t.Errorf("panel:\n%s", out)
	}
}

// TestProgramRenameDelete (the programs design §3.4): the program
// popup's Rename asks for the new name and renames through the source,
// refusing a name that isn't one; Delete asks first and deletes only on
// Y.
func TestProgramRenameDelete(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\"]\n")
	cfg, _ := config.Load()
	g, _ := cfg.Program("acme")
	data := Data{Projects: []ProjectData{{Slug: "web"}},
		Programs: []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: g.Projects}, Settings: g}}}
	src := &fakeSource{data: data}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	m.setData(data)
	m.focus, m.sideSel = areaSide, "g:acme"
	open := func(label string) {
		t.Helper()
		drain(t, m, press(m, "A"))
		pv, ok := m.top().(*programView)
		if !ok {
			t.Fatalf("popup %T", m.top())
		}
		pv.list.sel = slices.IndexFunc(pv.list.rows, func(r setting) bool { return r.label == label })
		if pv.list.sel < 0 {
			t.Fatalf("no %s row", label)
		}
		drain(t, m, press(m, "enter"))
	}
	open("Rename")
	in, ok := m.top().(*inputView)
	if !ok {
		t.Fatalf("rename asked nothing: %T", m.top())
	}
	in.lineEdit = newLine("Bad Name")
	drain(t, m, press(m, "enter"))
	if len(src.programActs) != 0 || !strings.Contains(m.msg, "not a program name") {
		t.Fatalf("bad name: %v, %q", src.programActs, m.msg)
	}
	for m.top() != nil {
		m.pop()
	}
	open("Rename")
	m.top().(*inputView).lineEdit = newLine("studio")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.programActs, []string{"rename acme studio"}) || m.top() != nil {
		t.Fatalf("rename: %v, top %T", src.programActs, m.top())
	}

	open("Delete")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("delete didn't ask: %T", m.top())
	}
	drain(t, m, press(m, "N"))
	if len(src.programActs) != 1 {
		t.Fatalf("N deleted: %v", src.programActs)
	}
	for m.top() != nil {
		m.pop()
	}
	open("Delete")
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.programActs, []string{"rename acme studio", "delete acme"}) {
		t.Fatalf("delete: %v", src.programActs)
	}
}

// TestProgramPairedMember: a paired member shows under its program,
// named alias/slug, and under its server's group, tagged with its
// program; each place folds apart, ← on it folded goes to its program
// or its server, and enter opens the same dashboard from both. An
// invited member the mirror doesn't have is a dim row saying so.
func TestProgramPairedMember(t *testing.T) {
	peers := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Projects: []proto.PeerMirrorProject{
		{PeerProject: proto.PeerProject{Project: "ios", NeedsYou: 1}, Watch: &proto.ProjectWatch{Project: "ios"}}}}}
	progs := []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: []string{"mini/ios", "mini/mac"}},
		Invites: map[string]link.Invite{"mini/mac": {ID: "P-1", Out: true, Peer: "mini/mac", State: link.ReqWaiting}}}}
	build := func(tr view.Tree) []treeRow {
		return buildTree(nil, nil, treeIn{tree: tr, peers: peers, host: "here", programs: progs})
	}
	if keys := rowKeys(build(view.Tree{})); slices.Contains(keys, "g:acme/rc:mini/ios") || !slices.Contains(keys, "rc:mini/ios") {
		t.Errorf("by default: folded under its program alone %v", keys)
	}
	rows := build(view.Tree{Unfolded: []string{"g:acme/@mini/ios"}})
	want := []string{"g:", "g:acme", "r:acme", "g:acme/r:mini/ios", "g:acme/rc:mini/ios", "g:acme/r:mini/mac", "g:mini", "r:mini/ios", "rc:mini/ios"}
	if keys := rowKeys(rows); !slices.Equal(keys, want) {
		t.Fatalf("rows %v", keys)
	}
	line := func(key string) string {
		r, _ := sideRowOf(rows, key, false)
		return ansi.Strip(treeCells(r, 50, false, false))
	}
	if l := line("g:acme/r:mini/ios"); !strings.Contains(l, "│") || !strings.Contains(l, "mini/ios") {
		t.Errorf("under the program %q", l)
	}
	if l := line("g:acme/r:mini/mac"); !strings.Contains(l, "mini/mac") || !strings.Contains(l, "invited") {
		t.Errorf("invited %q", l)
	}
	if l := line("r:mini/ios"); !strings.Contains(l, "ios · acme") {
		t.Errorf("under its server %q", l)
	}
	if g, _ := sideRowOf(rows, "g:acme", false); !g.hint {
		t.Errorf("the program row doesn't say its paired member needs you")
	}
	// Folding it under its program leaves it unfolded under its server.
	st := sideKeyStep(rows, 50, "g:acme/r:mini/ios", sideOut)
	var tr view.Tree
	if st.refold == nil {
		t.Fatalf("← under the program: %+v", st)
	}
	st.refold(&tr)
	if !tr.MemberFolded("g:acme/@mini/ios") || tr.IsFolded("@mini/ios") {
		t.Fatalf("tree %+v", tr)
	}
	rows = build(tr)
	if keys := rowKeys(rows); slices.Contains(keys, "g:acme/rc:mini/ios") || !slices.Contains(keys, "rc:mini/ios") {
		t.Errorf("folded under the program: %v", keys)
	}
	if st := sideKeyStep(rows, 50, "g:acme/r:mini/ios", sideOut); st.sel != "g:acme" {
		t.Errorf("← on it folded: %+v", st)
	}
	a, _ := sideRowOf(rows, "g:acme/r:mini/ios", false)
	b, _ := sideRowOf(rows, "r:mini/ios", false)
	ta, _, _ := a.target()
	tb, _, _ := b.target()
	if ta != tb || ta.Project != "@mini/ios" {
		t.Errorf("targets %+v %+v", ta, tb)
	}
}
