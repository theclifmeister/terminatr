package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A program's settings popup (the programs design §3.3, §11.1): its
// members, its authority, its thread cap and the rest of its table in
// the settings file, the user's alone. It opens from a program's or its
// director's sidebar row (its menu, or prefix+a with the sidebar's
// cursor on the row). Delegate gains reach, so it asks first; back to
// propose is one key. Members on paired servers are listed and kept as
// they are: inviting one comes with B14.

// programView is program slug's settings popup.
type programView struct {
	slug string
	list settingsList
}

// openProgramSettings opens program slug's settings popup.
func (m *dash) openProgramSettings(slug string) {
	if m.programData(slug) == nil {
		m.msg = "no program " + slug + " (programs may be off)"
		return
	}
	m.push(&programView{slug: slug, list: settingsList{rows: programSettings(slug)}})
}

// programData is program slug's, nil for none.
func (m *dash) programData(slug string) *ProgramData {
	for i := range m.data.Programs {
		if m.data.Programs[i].Slug == slug {
			return &m.data.Programs[i]
		}
	}
	return nil
}

func (v *programView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if s := k.String(); s == "esc" || s == "q" {
		m.pop()
		return nil
	}
	cmd, _ := v.list.key(m, k)
	return cmd
}

func (v *programView) render(m *dash) string {
	w := m.inner(viewWidth)
	lines, sel, hits := v.list.lines(m, w)
	head := []string{th().Faint.Render(fit("The program's settings, yours alone: its director never changes them.", w)), ""}
	if g := m.programData(v.slug); g != nil && g.Error != "" {
		head = append(head, th().Bad.Render(fit("its folder doesn't read: "+oneLine(g.Error), w)), "")
	}
	hits = append(make([]int, len(head)), hits...)
	for i := range head {
		hits[i] = noHit
	}
	if sel >= 0 {
		sel += len(head)
	}
	keys := "enter change · + - number · X default · ↑ ↓ move · esc close"
	return m.popup(box{title: v.slug + " · program settings", body: append(head, lines...), sel: sel, hits: hits, keys: keys})
}

func (v *programView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if item == noHit {
		return nil
	}
	return v.list.click(m, item, col)
}

func (v *programView) wheel(m *dash, d int) { v.list.key(m, arrow(d)) }

// programSettings are program slug's rows.
func programSettings(slug string) []setting {
	table := config.ProgramTable(slug)
	get := func(m *dash) config.Program {
		if g := m.programData(slug); g != nil {
			return g.Settings
		}
		return config.Program{}
	}
	set := func(key string, value any, msg string) func(m *dash) tea.Cmd {
		return func(m *dash) tea.Cmd { return m.setSetting(table, key, value, msg) }
	}
	unset := func(key string) func(m *dash) tea.Cmd {
		return func(m *dash) tea.Cmd { return m.setSetting(table, key, nil, key+": default") }
	}
	toggle := func(label, key, help string, val func(config.Program) bool) setting {
		return setting{label: label, help: help,
			value: func(m *dash) string { return onOff(val(get(m))) },
			change: func(m *dash) tea.Cmd {
				on := !val(get(m))
				return m.setSetting(table, key, on, label+" "+onOff(on))
			},
			unset: unset(key)}
	}
	remoteRow := toggle("Remote control", "remote_control", "The director keeps remote control on, so you can continue it from another device: a new one starts with it, and tm turns it back on when it drops. prefix+r changes the running one; your off holds until it is started anew.", func(p config.Program) bool { return p.RemoteControl })
	remoteRow.note = func(m *dash) []string {
		return remoteNoteOf(proto.RoleDirector, get(m).RemoteControl, m.data.Sessions, slug)
	}
	return []setting{
		{label: "Members", help: "The projects in the program, in the order its dashboard lists them. A project is in one program at most. Enter picks them.",
			value:  func(m *dash) string { return membersWords(get(m).Projects) },
			change: func(m *dash) tea.Cmd { m.push(newMembersView(m, slug)); return nil }},
		{label: "Authority", help: "Propose: the director proposes routes and starts, and you start threads. Delegate: it may also start threads in its projects on its own, within the caps. Accepting, dropping and answering stay yours either way.",
			value: func(m *dash) string { return get(m).Authority },
			change: func(m *dash) tea.Cmd {
				if get(m).Authority == config.AuthorityDelegate {
					return set("authority", config.AuthorityPropose, "authority: propose")(m)
				}
				m.confirm("Delegate to the director?", fmt.Sprintf(
					"%s's director may then start threads in its projects on its own, within each project's, the program's and all projects' thread caps. It never accepts, drops or answers for you, and each project's own settings still hold. Back to propose is one key. Delegate? (Y/N)", slug),
					func() tea.Cmd { return set("authority", config.AuthorityDelegate, "authority: delegate")(m) })
				return nil
			},
			unset: unset("authority")},
		{label: "Thread cap", help: "At most this many threads at once across the program's projects, besides each project's own cap and the cap of all projects. 0 is no program cap.",
			value: func(m *dash) string { return programCapWords(get(m).ParallelThreads) },
			adjust: func(m *dash, d int) tea.Cmd {
				n := max(0, min(get(m).ParallelThreads+d, config.MaxParallelThreads))
				return m.setSetting(table, "parallel_threads", n, "thread cap: "+programCapWords(n))
			},
			unset: unset("parallel_threads")},
		{label: "Dependency met", help: "When a task another one waits on counts as done: merged (its PR merged, or it is done) or done (you accepted it).",
			value: func(m *dash) string { return get(m).DependencyMet },
			change: func(m *dash) tea.Cmd {
				next := config.DependencyDone
				if get(m).DependencyMet == config.DependencyDone {
					next = config.DependencyMerged
				}
				return set("dependency_met", next, "dependency met: "+next)(m)
			},
			unset: unset("dependency_met")},
		{label: "Director agent", help: "The agent the director runs as; the coordinator's agent unless you pick one. It applies from the director's next start.",
			value: func(m *dash) string {
				if a := get(m).DirectorAgent; a != "" {
					return a
				}
				return "the coordinator's"
			},
			change: func(m *dash) tea.Cmd {
				choices := append([]string{""}, m.src.Agents()...)
				i := slices.Index(choices, get(m).DirectorAgent)
				next := choices[(i+1)%len(choices)]
				if next == "" {
					return unset("director_agent")(m)
				}
				return set("director_agent", next, "director agent: "+next)(m)
			},
			unset: unset("director_agent")},
		directorModelRow(slug, get, set, unset),
		remoteRow,
		toggle("Auto clear", "auto_clear", "The director saves what it learned and starts a fresh conversation when its context fills, as a coordinator does.", func(p config.Program) bool { return p.AutoClear }),
		toggle("Answer facts", "answer_facts", "The director passes settled facts (merged, released, decided by you) from one project to another without asking, citing where they come from. Decisions always come to you.", func(p config.Program) bool { return p.AnswerFacts }),
		toggle("Search projects", "search_children", "The director may search its projects' histories, as a thread sees them: never their inbox or coordinator notes.", func(p config.Program) bool { return p.SearchChildren }),
		toggle("Paused", "paused", "The director keeps running, but routing tasks and starting threads are refused until you resume.", func(p config.Program) bool { return p.Paused }),
	}
}

// directorModels are the models program slug's director may run: its
// agent's (director_agent, else all projects' coordinator agent), in
// all projects' Thread models, as config.DirectorSafety reads them.
func directorModels(m *dash, p config.Program) (string, []string, string) {
	all := config.Defaults
	if m.data.Defaults != nil {
		all = *m.data.Defaults
	}
	name, why := resolvedAgent(cmp.Or(p.DirectorAgent, all.CoordinatorAgent), m.agentList())
	if name == "" {
		return "", nil, why
	}
	for _, c := range m.catalogList() {
		if c.Agent != name {
			continue
		}
		if !c.Known {
			return name, nil, name + "'s models are unknown (" + c.Reason + ")"
		}
		var out []string
		for _, x := range c.InScope(all.Models) {
			out = append(out, x.Name)
		}
		return name, out, ""
	}
	return name, nil, name + " isn't installed"
}

// directorModelWords is a program's director_model as its row shows
// it: unset, the coordinators' model while the director's agent is
// theirs too, else its agent's default (config.DirectorSafety).
func directorModelWords(p config.Program) string {
	switch {
	case p.DirectorModel != "":
		return p.DirectorModel
	case p.DirectorAgent == "":
		return "the coordinator's"
	}
	return "agent's default"
}

// directorModelRow is the program's Director model row: enter steps
// through its agent's models, X goes back to the default, and a running
// director switches at once where its agent can (session.model); the
// ticker catches what this misses.
func directorModelRow(slug string, get func(*dash) config.Program, set func(string, any, string) func(*dash) tea.Cmd, unset func(string) func(*dash) tea.Cmd) setting {
	running := func(m *dash) *proto.SessionInfo {
		for i, s := range m.data.Sessions {
			if s.Role == proto.RoleDirector && s.Project == slug && s.Agent != "" {
				return &m.data.Sessions[i]
			}
		}
		return nil
	}
	sync := func(m *dash, save tea.Cmd) tea.Cmd {
		s := running(m)
		if s == nil {
			return save
		}
		id, src := s.ID, m.src
		return tea.Sequence(save, func() tea.Msg { src.SyncModel(id); return nil })
	}
	return setting{label: "Director model", help: "The model the director runs, from its agent's models in all projects' Thread models; the coordinator's model while it runs the coordinator's agent. A running director switches at once where its agent can, else on its next start. Enter picks the next one; X goes back to the default.",
		value: func(m *dash) string { return directorModelWords(get(m)) },
		change: func(m *dash) tea.Cmd {
			name, list, why := directorModels(m, get(m))
			if len(list) == 0 {
				m.msg = cmp.Or(why, "the director's agent offers no models here") + ": the director runs its agent's default"
				return nil
			}
			choices := append([]string{""}, list...)
			next := choices[(slices.Index(choices, get(m).DirectorModel)+1)%len(choices)]
			if next == "" {
				return sync(m, unset("director_model")(m))
			}
			return sync(m, set("director_model", next, "the director runs "+next+" ("+name+")")(m))
		},
		unset: func(m *dash) tea.Cmd { return sync(m, unset("director_model")(m)) },
		note: func(m *dash) []string {
			if s := running(m); s != nil && s.ModelPending {
				return []string{th().Warn.Render("the running director runs " + modelWord(s.Model) + "; " + modelWord(s.ModelNext) + " on its next start (" + s.Agent + " can't switch models live)")}
			}
			cur := get(m).DirectorModel
			if cur == "" {
				return nil
			}
			name, list, why := directorModels(m, get(m))
			switch {
			case why != "":
				return []string{th().Warn.Render(why + ": the director runs its agent's default")}
			case !slices.Contains(list, cur):
				return []string{th().Warn.Render(name + " doesn't offer " + cur + " in all projects' models now: the director runs " + name + "'s default")}
			}
			return nil
		}}
}

// membersWords is a program's members as its settings row shows them.
func membersWords(ms []string) string {
	if len(ms) == 0 {
		return "none"
	}
	return strings.Join(ms, ", ")
}

// programCapWords is a program's thread cap in words.
func programCapWords(n int) string {
	if n == 0 {
		return "none"
	}
	return fmt.Sprint(n)
}

// membersView picks a program's members among this server's projects:
// space ticks or unticks one, enter saves the list in its order (ticked
// ones keep their place, new ones go last), esc leaves it unchanged. A
// project in another program can't be ticked; members on paired servers
// stay as they are.
type membersView struct {
	slug  string
	order []string        // the members, in order, the paired ones among them
	on    map[string]bool // ticked
	all   []string        // this server's projects
	other map[string]string
	sel   int
}

func newMembersView(m *dash, slug string) *membersView {
	v := &membersView{slug: slug, on: map[string]bool{}, other: map[string]string{}}
	if g := m.programData(slug); g != nil {
		v.order = slices.Clone(g.Settings.Projects)
	}
	for _, s := range v.order {
		v.on[s] = true
	}
	for _, g := range m.data.Programs {
		if g.Slug == slug {
			continue
		}
		for _, s := range g.Settings.Projects {
			v.other[s] = g.Slug
		}
	}
	for _, p := range m.data.Projects {
		v.all = append(v.all, p.Slug)
	}
	return v
}

// members is the list enter saves.
func (v *membersView) members() []string {
	out := []string{}
	for _, s := range v.order {
		if v.on[s] {
			out = append(out, s)
		}
	}
	for _, s := range v.all {
		if v.on[s] && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func (v *membersView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.pop()
		m.msg = "members unchanged"
	case "up", "down", "pgup", "pgdown":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(v.all))
	case "space", " ":
		if v.sel < len(v.all) {
			s := v.all[v.sel]
			if g := v.other[s]; g != "" {
				m.msg = s + " is in program " + g + ": a project is in one program at most"
				return nil
			}
			v.on[s] = !v.on[s]
		}
	case "enter":
		m.pop()
		ms := v.members()
		return m.setSetting(config.ProgramTable(v.slug), "projects", ms, "members: "+membersWords(ms))
	}
	return nil
}

func (v *membersView) lines(w int) []string {
	var out []string
	for i, s := range v.all {
		mark := "[ ] "
		if v.on[s] {
			mark = "[x] "
		}
		line := mark + s
		if g := v.other[s]; g != "" {
			line += " · in " + g
		}
		if i == v.sel {
			out = append(out, th().Sel.Render(fit(line, w)))
		} else if v.other[s] != "" {
			out = append(out, th().Faint.Render(fit(line, w)))
		} else {
			out = append(out, fit(line, w))
		}
	}
	for _, s := range v.order {
		if strings.Contains(s, "/") {
			out = append(out, th().Faint.Render(fit("    "+s+" · on a paired server, kept", w)))
		}
	}
	if len(out) == 0 {
		out = append(out, th().Faint.Render("No projects on this server."))
	}
	return out
}

func (v *membersView) render(m *dash) string {
	w := m.inner(viewWidth)
	sel := -1
	if len(v.all) > 0 {
		sel = v.sel
	}
	return m.popup(box{title: v.slug + " · members", body: v.lines(w), sel: sel, keys: "space tick · enter save · ↑ ↓ move · esc cancel"})
}
