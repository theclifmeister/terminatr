package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/models/modelstest"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
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

// TestProgramRows: a program's row, its director's, then its members in
// its order, one level in under a │; the program counts its members'
// threads and needs you when one does.
func TestProgramRows(t *testing.T) {
	rows := progTree(view.Tree{})
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
	// Folded: its row alone, with its director's state.
	folded := progTree(view.Tree{Folded: []string{"g:acme"}})
	if keys := rowKeys(folded); !slices.Equal(keys, []string{"g:acme", "p:zoo", "c:zoo"}) || folded[0].state == "" {
		t.Errorf("folded %v %+v", keys, folded[0])
	}
	// Enter on either row opens the director; a preview shows its
	// session only while it runs.
	for _, r := range rows[:2] {
		if tg, ok, _ := r.target(); !ok || tg.Program != "acme" || tg.Project != "" {
			t.Errorf("%s target %+v", r.key(), tg)
		}
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
	rows := progTree(view.Tree{})
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
	folded := progTree(view.Tree{Folded: []string{"web"}})
	if st := sideKeyStep(folded, 40, "p:web", sideOut); st.sel != "g:acme" {
		t.Errorf("← on a folded member: %+v", st)
	}
	if st := sideKeyStep(rows, 40, "c:web", sideFoldAll); st.sel != "g:acme" || st.refold == nil {
		t.Errorf("Z on a member's row: %+v", st)
	} else {
		var all view.Tree
		st.refold(&all)
		if !all.IsFolded("g:acme") || !all.IsFolded("zoo") {
			t.Errorf("Z folded %+v", all)
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
