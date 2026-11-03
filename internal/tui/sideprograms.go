package tui

import (
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Programs in the sidebar (the programs design §11.1): a program's row,
// its director's under it, and its member projects one level in, each
// with its own rows as a standalone project has them:
//
//	" ◆ acme                 3 ●        ⚑"  the program: its members' open
//	                                         threads, its director's state
//	                                         while folded, ⚑ when something
//	                                         in it needs you
//	" ├─ director          31% ●         "  its director, as a coordinator
//	" │ ■ acme-api           2 ●        ⚑"  a member project, its rows as
//	" │ └─ coordinator     22% ●         "  ever, under the program's │
//
// A program sorts with the projects that aren't pinned, by slug, and
// folds as a project does; its fold key is g:<slug> (programKey), so a
// program and a project never share one. With programs off the server
// sends no programs, and the tree is today's. Members on paired servers
// come with B16: only local members show here.

// ProgramData is one program as the sidebar shows it: the server's
// (server.SidePrograms), with its settings read from the settings file,
// for its settings popup.
type ProgramData struct {
	server.SideProgram
	Settings config.Program
	// Deps are its members' cross-project dependency states, as the
	// ticker last computed them (program.ReadDeps), by ref.
	Deps map[string]program.Dep
	// Invites are its invites to paired projects, by member
	// ("mini/acme-ios"): the newest of each (link.Invites).
	Invites map[string]link.Invite
}

// programsOf are cfg's programs as the sidebar shows them; none while
// programs are off.
func programsOf(cfg *config.Config) []ProgramData {
	var out []ProgramData
	for _, sp := range server.SidePrograms(cfg) {
		t, _ := cfg.Program(sp.Slug)
		deps, _ := program.ReadDeps(sp.Slug)
		g := ProgramData{SideProgram: sp, Settings: t, Deps: deps}
		for _, r := range link.Invites(sp.Slug) {
			if r.Out {
				if g.Invites == nil {
					g.Invites = map[string]link.Invite{}
				}
				g.Invites[r.Peer] = r
			}
		}
		out = append(out, g)
	}
	return out
}

// programKey is program slug's row key, and its fold's in view.Tree.
func programKey(slug string) string { return "g:" + slug }

// programMembers maps the projects of ps that a program of progs holds
// to that program: local members only, the first program that lists one.
func programMembers(progs []ProgramData, ps []ProjectData) map[string]string {
	if len(progs) == 0 {
		return nil
	}
	here := map[string]bool{}
	for _, p := range ps {
		here[p.Slug] = true
	}
	out := map[string]string{}
	for _, g := range progs {
		for _, m := range g.Projects {
			if here[m] && out[m] == "" && !strings.Contains(m, "/") {
				out[m] = g.Slug
			}
		}
	}
	return out
}

// programRows are program g's rows in the tree: its own, and unless it
// is folded its director's and its members', in its table's order, each
// member's as projectRows has them, one level in.
func programRows(g ProgramData, ps []ProjectData, sessions []proto.SessionInfo, byID map[string]proto.SessionInfo, in treeIn) []treeRow {
	pr := treeRow{kind: treeProgram, slug: g.Slug, prog: g.Slug, pct: -1, paused: g.Paused, hint: g.Questions > 0,
		current: in.current == programKey(g.Slug)}
	dir := treeRow{kind: treeDirector, slug: g.Slug, prog: g.Slug, pct: -1, ctx: -1, last: true, hint: g.Questions > 0, asks: g.Questions}
	for _, s := range sessions {
		if s.Role == proto.RoleDirector && s.Project == g.Slug {
			dir.session, dir.state, dir.remote, dir.ctx = s.ID, stateWord(s), s.RemoteControl, s.ContextPercent()
			dir.ctxHint = dir.ctx >= 0 && in.ctxHint > 0 && dir.ctx >= in.ctxHint
			break
		}
	}
	pr.session, pr.state = dir.session, dir.state
	bySlug := map[string]ProjectData{}
	for _, p := range ps {
		bySlug[p.Slug] = p
	}
	var kids []treeRow
	for _, m := range g.Projects {
		p, ok := bySlug[m]
		if !ok || strings.Contains(m, "/") {
			continue
		}
		rows := projectRows(p, sessions, byID, in)
		for i := range rows {
			rows[i].prog = g.Slug
		}
		if !p.inactive() {
			pr.threads += len(p.Threads)
		}
		pr.hint = pr.hint || rows[0].hint
		kids = append(kids, rows...)
	}
	if len(kids) > 0 {
		dir.last = false
	}
	if in.tree.IsFolded(programKey(g.Slug)) {
		pr.fold = true
		return []treeRow{pr}
	}
	return append([]treeRow{pr, dir}, kids...)
}

// programGlyph marks a program's row, before its name.
func programGlyph() string {
	if ic().name == IconsASCII {
		return "#"
	}
	return "◆"
}

// memberBar is the │ a program's member rows hang from, two cells.
func memberBar() string {
	if ic().name == IconsASCII {
		return "| "
	}
	return "│ "
}

// afterText is task t of project slug's after refs as its dashboard
// shows them (the programs design §6.3): "T10, acme-api/T12 (review)",
// each ref to another project's task with its state while programs are
// on and the project is in a program; plain refs otherwise.
func (m *dash) afterText(slug string, t *tasks.Task) string {
	if p := m.projectData(slug); p != nil && p.Program != "" && len(t.AfterX) > 0 {
		for _, g := range m.data.Programs {
			if g.Slug == p.Program {
				return program.AfterText(t, g.Deps)
			}
		}
	}
	return strings.Join(t.AfterRefs(), ", ")
}
