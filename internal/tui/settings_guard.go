package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The guard's settings (docs/SPEC.md §8.6, Guard): on or off, its mode,
// each rule, the allowances, and the refusals to allow or keep refusing.
// A project's and all projects' Access tabs have the same rows; a project's
// allowances add to all projects'.

// guardRuleWords name the guard's rules for the user, in config.GuardRules
// order.
var guardRuleWords = map[string]struct{ label, help string }{
	"force-push":    {"Force-pushing", "No force-push, to any branch. Off, a force-push to a protected branch is still refused."},
	"push-default":  {"Pushing to the default branch", "Nothing is pushed straight to main, master or a repository's default branch: work lands through a pull request."},
	"worktree-only": {"Threads write only in their worktree", "Off in config.toml. A thread's file tools write only inside its worktree, the temporary folders and its write folders (Access, Write folders), which open a folder besides it: on again, it leaves this list."},
	"delete-branch": {"Deleting branches and worktrees", "No agent deletes a branch, a worktree or a repository; tm cleans them up once the work has merged."},
	"merge":         {"Threads merging", "A thread never merges a pull request while the coordinator merges them; off, still refused (set who merges instead)."},
	"credentials":   {"Reading credentials", "No agent reads credential files, the keychain or tokens, or prints the environment."},
}

// guardRuleWord is rule in the user's words: a mod's rule as it is
// named (mod:<mod>.<rule>).
func guardRuleWord(rule string) string {
	if w, ok := guardRuleWords[rule]; ok {
		return strings.ToLower(w.label)
	}
	return rule
}

// guardModeWords name guard_mode's values.
var guardModeWords = map[string]string{config.GuardEnforce: "refuse", config.GuardLearn: "refuse, then ask you"}

// guardRows are the guard's rows, on the Access tab under Guard.
func (sr safetyRows) guardRows() []setting {
	return []setting{sr.guardOnRow(), sr.guardModeRow(), sr.guardRulesRow(), sr.guardAllowRow(), sr.guardRefusalsRow()}
}

func (sr safetyRows) guardOnRow() setting {
	return setting{label: "Guard", help: "Refuse agents' tool calls that break the standing rules (Guard rules), before any permission prompt. Off, the agent's own permission rules and the sandbox decide alone. Turning it off asks first.",
		keys: []string{"guard"}, word: onOffWord("guard"),
		value: func(m *dash) string { return onOff(sr.on(m, "guard")) },
		change: func(m *dash) tea.Cmd {
			if !sr.on(m, "guard") {
				return sr.set(m, "guard", true, "the guard is on "+sr.forWho)
			}
			m.confirm("Turn the guard off", "Turn the guard off "+sr.forWho+"? Agents' tool calls are then held only by the agent's own permission rules and the sandbox, and threads can print their secret variables.", func() tea.Cmd {
				return sr.set(m, "guard", false, "the guard is off "+sr.forWho)
			})
			return nil
		}}
}

func (sr safetyRows) guardModeRow() setting {
	word := func(s config.Safety) string { return cmp.Or(guardModeWords[s.GuardMode], s.GuardMode) }
	return setting{label: "Guard mode", help: "Refuse: a refused call is listed under Guard refusals, to allow or keep refusing when you like. Refuse, then ask you (learning): each new refusal is also put to you in the inbox, and the agent is told you review it; nothing passes until you allow it.",
		keys: []string{"guard_mode"}, word: word,
		value: func(m *dash) string { return word(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			ms := config.GuardModes
			next := ms[(slices.Index(ms, sr.safety(m).GuardMode)+1)%len(ms)]
			return sr.set(m, "guard_mode", next, "the guard "+sr.ofWho+" will "+guardModeWords[next])
		}}
}

// guardOn counts the guard's rules s leaves on.
func guardOn(s config.Safety) int {
	n := 0
	for _, id := range config.GuardRules {
		if !slices.Contains(s.GuardOff, id) {
			n++
		}
	}
	return n
}

func (sr safetyRows) guardRulesRow() setting {
	word := func(s config.Safety) string {
		if n := guardOn(s); n < len(config.GuardRules) {
			return fmt.Sprintf("%d of %d on", n, len(config.GuardRules))
		}
		return "all on"
	}
	return setting{label: "Guard rules", help: "What the guard refuses. Enter lists them to turn each on or off. Whatever is off, while the guard is on it never lets an agent read tm's own files, print a secret variable tm gives threads, force-push to a protected branch, or a thread merge while the coordinator merges.",
		keys: []string{"guard_off"}, word: word,
		value: func(m *dash) string { return word(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			m.push(&guardRulesView{sr: sr})
			return nil
		}}
}

func (sr safetyRows) guardAllowRow() setting {
	return setting{label: "Guard allowances", help: "Calls the guard lets through though a rule would refuse them: one command or file, or a pattern where * matches anything. A project's add to all projects'. Allow a refusal from Guard refusals; enter lists them to change or remove.",
		value: func(m *dash) string {
			list := sr.safety(m).GuardAllow
			if len(list) == 0 {
				return "none"
			}
			own := len(config.OwnAllows(list))
			if sr.all || own == len(list) {
				return fmt.Sprint(len(list))
			}
			return fmt.Sprintf("%d (%d of all projects)", len(list), len(list)-own)
		},
		change: func(m *dash) tea.Cmd {
			m.push(&guardAllowView{sr: sr})
			return nil
		}}
}

func (sr safetyRows) guardRefusalsRow() setting {
	return setting{label: "Guard refusals", help: "The calls the guard refused lately, with the whole command or file: allow one for this project or all projects (the agent is told it may retry), or keep refusing it, which stops listing it.",
		value: func(m *dash) string { return "enter lists them" },
		change: func(m *dash) tea.Cmd {
			list, err := m.src.GuardRefusals(sr.slug)
			if err != nil {
				m.fail(err)
				return nil
			}
			m.push(&guardRefusalsView{sr: sr, list: list})
			return nil
		}}
}

// guardRulesView lists the guard's rules, one to turn on or off with
// enter; each change is saved at once. worktree-only is listed only
// while it is off (T574 case 8): write folders open a folder besides the
// worktree, for the guard and the sandbox alike.
type guardRulesView struct {
	sr  safetyRows
	sel int
}

// ids are the rules the view lists.
func (v *guardRulesView) ids(m *dash) []string {
	off := v.sr.safety(m).GuardOff
	return slices.DeleteFunc(slices.Clone(config.GuardRules), func(id string) bool {
		return id == "worktree-only" && !slices.Contains(off, id)
	})
}

func (v *guardRulesView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	ids := v.ids(m)
	switch k.String() {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(ids))
	case "enter", "space", " ":
		if v.sel >= len(ids) {
			return nil
		}
		id := ids[v.sel]
		off := slices.Clone(v.sr.safety(m).GuardOff)
		msg := guardRuleWord(id) + ": refused " + v.sr.forWho
		if i := slices.Index(off, id); i >= 0 {
			off = slices.Delete(off, i, i+1)
		} else {
			off = append(off, id)
			msg = guardRuleWord(id) + ": no longer refused " + v.sr.forWho
		}
		if off == nil {
			off = []string{}
		}
		return v.sr.set(m, "guard_off", off, msg)
	}
	return nil
}

func (v *guardRulesView) render(m *dash) string {
	w := m.inner(viewWidth)
	off := v.sr.safety(m).GuardOff
	var lines []string
	sel := 0
	for i, id := range v.ids(m) {
		mark, word := ic().todoDone, "refused"
		if slices.Contains(off, id) {
			mark, word = ic().todoOpen, "off"
		}
		l := mark + " " + fit(guardRuleWords[id].label, 36) + "  " + th().Faint.Render(word)
		if i == v.sel {
			l = th().Sel.Render(fit(ansi.Strip(l), w))
		}
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, l)
		lines = append(lines, faintLines(guardRuleWords[id].help, w)...)
		if i == v.sel {
			sel = len(lines) - 1
		}
	}
	return m.popup(box{title: "Guard rules " + v.sr.forWho, body: lines, sel: sel, keys: "enter refuse or not · ↑ ↓ move · esc back"})
}

// guardAllowView lists the allowances: enter changes one's command or
// file, x removes it. On a project, all projects' are listed too, marked,
// and changed in all projects'.
type guardAllowView struct {
	sr  safetyRows
	sel int
}

// save writes list as the allowances of the project's table, or all
// projects' (all), and shows them at once.
func (v *guardAllowView) save(m *dash, all bool, list []config.GuardAllow, msg string) tea.Cmd {
	sr := v.sr
	table := sr.table
	if all {
		table = config.DefaultsTable
	}
	if all && !sr.all && m.data.Defaults != nil {
		d := *m.data.Defaults
		d.GuardAllow = list
		m.data.Defaults = &d
	}
	shown := list
	if !sr.all {
		var keep []config.GuardAllow
		for _, a := range sr.safety(m).GuardAllow {
			if a.All != all {
				keep = append(keep, a)
			}
		}
		for _, a := range list {
			a.All = all
			keep = append(keep, a)
		}
		shown = keep
	}
	s := sr.safety(m)
	s.GuardAllow = shown
	if sr.all {
		m.data.Defaults = &s
	} else if p := m.projectData(sr.slug); p != nil {
		p.Safety = &s
	}
	var value any = list
	if len(list) == 0 {
		value = nil
	}
	return m.setSetting(table, "guard_allow", value, msg)
}

// tableOf is the allowances of the table a's from, a's place in them.
func (v *guardAllowView) tableOf(m *dash, a config.GuardAllow) ([]config.GuardAllow, int) {
	var out []config.GuardAllow
	for _, x := range v.sr.safety(m).GuardAllow {
		if x.All == a.All {
			x.All = false
			out = append(out, x)
		}
	}
	return out, slices.IndexFunc(out, a.Same)
}

func (v *guardAllowView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	list := v.sr.safety(m).GuardAllow
	switch k.String() {
	case "esc", "q":
		m.pop()
		return nil
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(list))
		return nil
	}
	if v.sel >= len(list) {
		return nil
	}
	a := list[v.sel]
	all := a.All || v.sr.all
	where := v.sr.forWho
	if a.All {
		where = "for all projects"
	}
	switch k.String() {
	case "x", "delete", "backspace":
		table, i := v.tableOf(m, a)
		if i < 0 {
			return nil
		}
		v.sel = max(min(v.sel, len(list)-2), 0)
		return v.save(m, all, slices.Delete(table, i, i+1), "allowance removed "+where+": the guard refuses "+guardRuleWord(a.Rule)+" there again")
	case "enter", "space", " ":
		label := "Command (* matches anything):"
		if a.Path != "" {
			label = "File (* matches anything; ~ is your home):"
		}
		switch {
		case a.Rule == "credentials":
			label = strings.TrimSuffix(strings.Split(label, " (")[0], ":") + " (exact, no *):"
		case a.Exact:
			label = strings.TrimSuffix(strings.Split(label, " (")[0], ":") + " (exact: * is a plain *):"
		}
		m.push(&inputView{title: "Allowance of " + guardRuleWord(a.Rule) + " " + where, keep: true, text: a.Text(), label: label,
			submit: func(t string) tea.Cmd {
				t = strings.TrimSpace(t)
				table, i := v.tableOf(m, a)
				if i < 0 {
					return nil
				}
				if t == "" {
					return v.save(m, all, slices.Delete(table, i, i+1), "allowance removed "+where)
				}
				b := config.GuardAllow{Rule: a.Rule, Exact: a.Exact}
				if a.Path != "" {
					b.Path = t
				} else {
					b.Command = t
				}
				if err := config.CheckGuardAllow(b); err != nil {
					m.msg = "the allowance " + err.Error() + ": it stays as it is"
					return nil
				}
				table[i] = b
				return v.save(m, all, table, "allowance changed "+where)
			}})
	}
	return nil
}

func (v *guardAllowView) render(m *dash) string {
	w := m.inner(viewWidth)
	list := v.sr.safety(m).GuardAllow
	lines := []string{th().Faint.Render("Calls the guard lets through " + v.sr.forWho + ". Allow more from Guard refusals.")}
	sel := -1
	for i, a := range list {
		lines = append(lines, "")
		head := guardRuleWords[a.Rule].label
		if head == "" {
			head = a.Rule
		}
		if a.Exact {
			head += " · exact"
		}
		if a.All && !v.sr.all {
			head += " · all projects"
		}
		text := textsafe.Line(a.Text(), max(w-2, 8))
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(head, w)), th().Sel.Render(fit("  "+text, w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(head), "  "+th().Accent.Render(text))
		}
	}
	if len(list) == 0 {
		lines = append(lines, "", th().Faint.Render("none yet"))
	}
	return m.popup(box{title: "Guard allowances " + v.sr.forWho, body: lines, sel: sel, keys: "enter change · x remove · ↑ ↓ move · esc back"})
}

// guardRefusalsView lists the guard's open refusals, the latest first:
// a allows the selected one for its project, A for all projects, k keeps
// refusing it; enter shows its whole command.
type guardRefusalsView struct {
	sr   safetyRows
	list []guardlog.Entry
	sel  int
	open bool // the selected refusal shown whole
}

// sessionOf is the session to tell about an allowance of e: its thread's
// running session, else the one refused, while it runs.
func sessionOf(m *dash, e guardlog.Entry) string {
	for _, s := range m.data.Sessions {
		if e.Thread != "" && s.Project == e.Project && s.Thread == e.Thread && s.Role == proto.RoleThread {
			return s.ID
		}
	}
	for _, s := range m.data.Sessions {
		if s.ID == e.Session {
			return s.ID
		}
	}
	return ""
}

func (v *guardRefusalsView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		if v.open {
			v.open = false
			return nil
		}
		m.pop()
		return nil
	case "up", "k", "down", "j":
		v.sel, v.open = moveSel(v.sel, scrollKeys[k.String()], len(v.list)), false
		return nil
	}
	if v.sel >= len(v.list) {
		return nil
	}
	e := v.list[v.sel]
	how := ""
	switch k.String() {
	case "enter", "space", " ":
		v.open = !v.open
		return nil
	case "a":
		how = guardAllowHere
	case "A":
		how = guardAllowAll
	case "x", "K":
		how = guardKeep
	default:
		return nil
	}
	if how != guardKeep && e.Floor {
		m.msg = "the guard never allows this one: " + floorWhy(e) + "; x keeps refusing it"
		return nil
	}
	v.list = slices.Delete(slices.Clone(v.list), v.sel, v.sel+1)
	v.sel, v.open = max(min(v.sel, len(v.list)-1), 0), false
	src, session := m.src, sessionOf(m, e)
	return m.act(func() actionMsg {
		msg, err := src.GuardDecide(e, how, session)
		return actionMsg{msg: msg, err: settingsErr(err)}
	})
}

// floorWhy says why a refusal is never allowed.
func floorWhy(e guardlog.Entry) string {
	switch e.Rule {
	case "merge":
		return "a thread doesn't merge while the coordinator merges"
	case "force-push":
		return "it force-pushes to a protected branch"
	}
	return "it reads tm's own files or prints a secret variable"
}

func (v *guardRefusalsView) render(m *dash) string {
	w := m.inner(viewWidth)
	what := "Calls the guard refused " + v.sr.forWho + ", the latest first."
	var lines []string
	sel := -1
	for i, e := range v.list {
		if i > 0 {
			lines = append(lines, "")
		}
		who := e.Label()
		if v.sr.all {
			who = e.Project + " · " + who
		}
		head := fmt.Sprintf("%s · %s · %s · %s", who, guardRuleWord(e.Rule), textsafe.Line(e.Tool, 30), age(infoClock().Sub(e.Last))+" ago")
		if e.Count > 1 {
			head += fmt.Sprintf(" · %d times", e.Count)
		}
		body := []string{"  " + textsafe.Line(e.Subject, max(w-2, 8))}
		if i == v.sel && v.open {
			body = nil
			for _, l := range strings.Split(textsafe.Clean(e.Subject, true), "\n") {
				body = append(body, "  "+textsafe.Cut(l, max(w-2, 8)))
			}
		}
		if e.Floor {
			body = append(body, "  "+th().Warn.Render("never allowed: "+floorWhy(e)))
		}
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(head, w)))
			for _, l := range body {
				lines = append(lines, th().Sel.Render(fit(ansi.Strip(l), w)))
			}
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(head))
			for _, l := range body {
				lines = append(lines, th().Accent.Render(l))
			}
		}
	}
	if len(v.list) == 0 {
		lines = append(lines, th().Faint.Render("none to review"))
	}
	lines = append([]string{th().Faint.Render(what), ""}, lines...)
	if sel >= 0 {
		sel += 2
	}
	return m.popup(box{title: "Guard refusals " + v.sr.forWho, body: lines, sel: sel, keys: "a allow here · A allow for all projects · x keep refusing · enter whole command · esc back"})
}
