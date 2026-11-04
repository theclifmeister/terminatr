package tui

import (
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
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
//	                                         while folded (working while
//	                                         anything it hides works), ⚑ when
//	                                         something in it needs you
//	" ├─ director          31% ●         "  its director, as a coordinator
//	" │ ■ acme-api           2 ●        ⚑"  a member project, its rows as
//	" │ └─ coordinator     22% ●         "  ever, under the program's │
//
// A program sorts with the projects that aren't pinned, by slug, and
// folds as a project does; its fold key is g:<slug> (programKey), so a
// program and a project never share one. With programs off the server
// sends no programs, and the tree is today's.
//
// A member on a paired server shows twice (the programs design §11.1):
// under its program, named alias/slug, and under its server's group as
// ever, tagged with its program ("acme-ios · acme"). Both are built from
// the one mirror entry (peer.list), so they never drift; their keys and
// folds are each place's own ("g:acme/r:mini/acme-ios" and
// "r:mini/acme-ios"), and enter opens the same from either. A member
// the mirror doesn't have (invited, or not shared with this server) is
// a dim row under its program alone, saying so. Need counts come from
// the data, not the rows, so the same need is never counted twice.

// ProgramData is one program as the sidebar shows it: the server's
// (server.SidePrograms), with its settings read from the settings file,
// for its settings popup.
type ProgramData struct {
	server.SideProgram
	Settings config.Program
	// Deps are its members' cross-project dependency states and its
	// umbrellas' parts', as the ticker last computed them
	// (program.ReadSnapshot), by ref.
	Deps map[string]program.Dep
	// Waits are its members' open tasks that come after other members'
	// tasks, as the ticker last saw them: the dashboard's DEPENDENCIES.
	Waits []program.Wait
	// Umbrellas are its board's open umbrella and sweep tasks, in board
	// order: the dashboard's UMBRELLAS.
	Umbrellas []*tasks.Task
	// Tasks are the local members' tasks the umbrellas' parts and the
	// waits name, by ref ("acme-api/T12"), for the details panel.
	Tasks map[string]*tasks.Task
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
		snap, _ := program.ReadSnapshot(sp.Slug)
		g := ProgramData{SideProgram: sp, Settings: t, Deps: snap.States, Waits: snap.Waits}
		g.Umbrellas, g.Tasks = umbrellasOf(cfg, sp.Slug, snap.Waits)
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

// umbrellasOf reads program slug's open umbrella tasks, and the local
// members' tasks their parts and waits name, each board once.
func umbrellasOf(cfg *config.Config, slug string, waits []program.Wait) ([]*tasks.Task, map[string]*tasks.Task) {
	g, err := program.Open(slug, cfg)
	if err != nil {
		return nil, nil
	}
	b, err := g.Tasks().Load()
	if err != nil {
		return nil, nil
	}
	var out []*tasks.Task
	var refs []string
	for _, t := range b.Tasks {
		if !t.Status.Closed() {
			out = append(out, t)
			refs = append(refs, t.Parts...)
		}
	}
	for _, w := range waits {
		refs = append(refs, w.Task)
	}
	boards := map[string]*tasks.Board{}
	found := map[string]*tasks.Task{}
	for _, ref := range refs {
		alias, member, n, ok := tasks.CrossRef(ref)
		if !ok || alias != "" || found[ref] != nil {
			continue
		}
		mb, seen := boards[member]
		if !seen {
			if p, err := project.OpenWith(member, cfg); err == nil {
				mb, _ = p.Tasks().Load()
			}
			boards[member] = mb
		}
		if mb != nil {
			if t := mb.Find(n); t != nil {
				found[ref] = t
			}
		}
	}
	return out, found
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
	pr.busy = stateBusy(dir.state)
	bySlug := map[string]ProjectData{}
	for _, p := range ps {
		bySlug[p.Slug] = p
	}
	var kids []treeRow
	for _, m := range g.Projects {
		if alias, slug, remote := config.Remote(m); remote {
			pr.slugs = append(pr.slugs, programKey(g.Slug)+"/"+peerFold(alias, slug))
			rows := pairedMemberRows(g, alias, slug, in)
			if rows[0].kind == treePeerProject && !rows[0].stale {
				pr.threads += rows[0].threads
			}
			pr.hint = pr.hint || rows[0].hint
			pr.busy = pr.busy || rows[0].busy
			kids = append(kids, rows...)
			continue
		}
		p, ok := bySlug[m]
		if !ok {
			continue
		}
		mi := in
		mi.prog = g.Slug
		rows := projectRows(p, sessions, byID, mi)
		for i := range rows {
			rows[i].prog = g.Slug
		}
		if !p.inactive() {
			pr.threads += len(p.Threads)
			pr.slugs = append(pr.slugs, p.Slug)
		}
		pr.hint = pr.hint || rows[0].hint
		pr.busy = pr.busy || rows[0].busy
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

// pairedMembers maps the paired members of progs ("mini/acme-ios") to
// their program: the tag of their rows under their server's group.
func pairedMembers(progs []ProgramData) map[string]string {
	out := map[string]string{}
	for _, g := range progs {
		for _, m := range g.Projects {
			if _, _, remote := config.Remote(m); remote && out[m] == "" {
				out[m] = g.Slug
			}
		}
	}
	return out
}

// pairedMemberRows are paired member alias/slug's rows under program g:
// the mirror's entry for it as its server's group has it (peerProjectRows),
// named alias/slug, with keys and a fold of their own; else, while the
// mirror has none (its invite waits, or it isn't shared with this
// server), its row alone, dim, saying why.
func pairedMemberRows(g ProgramData, alias, slug string, in treeIn) []treeRow {
	name := alias + "/" + slug
	i := slices.IndexFunc(in.peers, func(pm proto.PeerMirror) bool { return pm.Alias == alias })
	pl := &peerLink{state: proto.LinkOffline}
	if i >= 0 {
		pm := in.peers[i]
		l := mirrorLink(pm)
		pl = &l
		name = peerShown(in.peers, alias) + "/" + slug
		if j := slices.IndexFunc(pm.Projects, func(p proto.PeerMirrorProject) bool { return p.Project == slug }); j >= 0 {
			l.synced = pm.Projects[j].Synced
			rows := peerProjectRows(alias, pm.Projects[j], pl, in.tree, g.Slug)
			rows[0].label = name
			rows[0].current = peerFold(alias, slug) == in.current
			return rows
		}
	}
	// A short word: the dashboard's CHILDREN says the invite in full.
	tag := "not shared"
	if r, ok := g.Invites[alias+"/"+slug]; ok {
		switch {
		case r.Pending():
			tag = "invited"
		case r.State == link.ReqDeclined:
			tag = "declined"
		}
	}
	return []treeRow{{kind: treePeerProject, peer: alias, slug: slug, prog: g.Slug, pct: -1, link: pl, stale: true, fold: true,
		label: name, tag: tag}}
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
