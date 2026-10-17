package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The sandbox lists of threads (docs/SPEC.md §5.2, Sandbox lists), after
// the secret environment: the commands a thread runs outside its
// sandbox, and the permission rules it gets on top of tm's. A project's
// list of a key replaces all projects' (config.ThreadSandbox), so on a
// project the view edits the project's own list, starting from all
// projects' when it follows them, and f follows them again.

// sandboxOutside is what an excluded command opens, said wherever one is
// added or listed.
const sandboxOutside = "Excluded commands run outside the sandbox, with your full file and network access (tm's socket included): only the guard and the permission check still judge them."

// sandboxLists are each list's row label, help and view text.
var sandboxLists = map[string]struct{ label, help, about, example string }{
	config.SandboxExcluded: {"Sandbox: excluded commands",
		"Commands a thread's Bash runs outside its sandbox, for a tool the sandbox can't hold (Playwright's Chromium on macOS). " + sandboxOutside + " Claude's Bash rule syntax: a trailing * takes any arguments. A project's list replaces all projects'. Claude threads only; Codex has no such setting.",
		sandboxOutside + " Claude still asks, or auto mode decides, unless an allow rule covers the command.",
		"npx playwright *"},
	config.SandboxAllow: {"Sandbox: allow rules",
		"Permission rules a thread's agent gets on top of tm's, so auto mode or a prompt doesn't stop a command you excluded (Bash(npx playwright *)). tm's deny rules and the guard still win. A project's list replaces all projects'. Claude threads only.",
		"Calls these rules match run without a prompt or auto mode's review; tm's deny rules and the guard still win.",
		"Bash(npx playwright *)"},
}

// sandboxRows are the two lists' rows.
func (sr safetyRows) sandboxRows() []setting {
	return []setting{sr.sandboxRow(config.SandboxExcluded), sr.sandboxRow(config.SandboxAllow)}
}

func (sr safetyRows) sandboxRow(key string) setting {
	l := sandboxLists[key]
	row := setting{label: l.label, help: l.help,
		value: func(m *dash) string {
			list, _ := sr.sandboxOf(m, key)
			if len(list) == 0 {
				return "none"
			}
			return fmt.Sprint(len(list))
		},
		change: func(m *dash) tea.Cmd {
			m.push(&sandboxView{sr: sr, list: key})
			return nil
		}}
	if !sr.all {
		row.from = func(m *dash) string {
			if _, own := sr.sandboxOf(m, key); !own {
				return config.AllProjectsName
			}
			return ""
		}
		row.unset = func(m *dash) tea.Cmd { return (&sandboxView{sr: sr, list: key}).follow(m) }
	}
	return row
}

// sandboxOf is the list of key threads of the rows' table get, and
// whether the table sets it itself (all projects' list always does).
func (sr safetyRows) sandboxOf(m *dash, key string) ([]string, bool) {
	if l, ok := m.data.Sandbox[sr.slug][key]; ok || sr.all {
		return l, true
	}
	return m.data.Sandbox[""][key], false
}

// warnLines is text wrapped to w cells, in the warning colour.
func warnLines(text string, w int) []string {
	lines := wrapLines(text, w)
	for i, l := range lines {
		lines[i] = th().Warn.Render(l)
	}
	return lines
}

// sandboxView lists one sandbox list: a adds an entry, x removes the
// selected one, f (on a project) follows all projects' list again.
type sandboxView struct {
	sr   safetyRows
	list string // config.SandboxExcluded or config.SandboxAllow
	sel  int
}

// save writes list as the view's table's own (nil: none of its own, so
// a project follows all projects') and shows it at once.
func (v *sandboxView) save(m *dash, list []string, msg string) tea.Cmd {
	slug := v.sr.slug
	if m.data.Sandbox == nil {
		m.data.Sandbox = map[string]map[string][]string{}
	}
	lists := map[string][]string{}
	for k, l := range m.data.Sandbox[slug] {
		lists[k] = l
	}
	if list == nil {
		delete(lists, v.list)
	} else {
		lists[v.list] = list
	}
	m.data.Sandbox[slug] = lists
	s, table, key := m.src, v.sr.table, v.list
	return m.act(func() actionMsg {
		if err := s.SetSandbox(table, key, list); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}

// follow drops a project's own list, so its threads get all projects'.
func (v *sandboxView) follow(m *dash) tea.Cmd {
	if _, own := v.sr.sandboxOf(m, v.list); !own || v.sr.all {
		m.msg = "this list of " + v.sr.slug + " already follows all projects"
		return nil
	}
	v.sel = 0
	return v.save(m, nil, v.sr.slug+" follows all projects' "+sandboxWord(v.list)+": threads get them from their next start")
}

// sandboxWord names key's entries in messages.
func sandboxWord(key string) string {
	if key == config.SandboxExcluded {
		return "excluded commands"
	}
	return "allow rules"
}

func (v *sandboxView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	list, _ := v.sr.sandboxOf(m, v.list)
	switch k.String() {
	case "esc", "q":
		m.pop()
		return nil
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(list))
		return nil
	case "a", "+":
		v.add(m)
		return nil
	case "f":
		if v.sr.all {
			return nil
		}
		return v.follow(m)
	case "x", "delete", "backspace":
		if v.sel >= len(list) {
			return nil
		}
		e := list[v.sel]
		rest := slices.Delete(slices.Clone(list), v.sel, v.sel+1)
		if v.sr.all && len(rest) == 0 {
			rest = nil // all projects' empty list is none at all
		}
		v.sel = max(min(v.sel, len(rest)-1), 0)
		return v.save(m, rest, e+" removed from the "+sandboxWord(v.list)+" "+v.sr.forWho+": threads started from now on don't get it")
	}
	return nil
}

// add asks for an entry, checks it, puts its warning (if any) to the
// human, and saves the list with it last.
func (v *sandboxView) add(m *dash) {
	l := sandboxLists[v.list]
	title := l.label + " " + v.sr.forWho
	m.promptNo(title, "e.g. "+l.example+":", "", 300, func(e string) tea.Cmd {
		list, _ := v.sr.sandboxOf(m, v.list)
		if slices.Contains(list, e) {
			m.msg = "not added: " + e + " is listed already"
			return nil
		}
		next := append(slices.Clone(list), e)
		if err := config.CheckSandboxList(v.list, next); err != nil {
			m.msg = "not added: " + err.Error()
			return nil
		}
		msg := e + " added to the " + sandboxWord(v.list) + " " + v.sr.forWho + ": threads get it from their next start"
		save := func() tea.Cmd {
			v.sel = len(next) - 1
			return v.save(m, next, msg)
		}
		q := ""
		if v.list == config.SandboxExcluded {
			q = sandboxOutside
		}
		if w := config.SandboxWarning(v.list, e); w != "" {
			q += " " + e + ": " + w + "."
		}
		if q == "" {
			return save()
		}
		m.confirm(title, "Add "+e+" "+v.sr.forWho+"? "+q, save)
		return nil
	})
}

func (v *sandboxView) render(m *dash) string {
	w := m.inner(viewWidth)
	l := sandboxLists[v.list]
	list, own := v.sr.sandboxOf(m, v.list)
	lines := faintLines(l.about, w)
	if v.list == config.SandboxExcluded {
		lines = warnLines(l.about, w)
	}
	if !v.sr.all {
		where := "This project's own list: it replaces all projects'; f follows theirs again."
		if !own {
			where = "All projects' list, which " + v.sr.slug + " follows: a change here makes it " + v.sr.slug + "'s own."
		}
		lines = append(lines, "")
		lines = append(lines, faintLines(where, w)...)
	}
	sel := -1
	for i, e := range list {
		lines = append(lines, "")
		text := textsafe.Line(e, w)
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(text))
		}
		if warn := config.SandboxWarning(v.list, e); warn != "" {
			for _, l := range warnLines(warn, w-2) {
				lines = append(lines, "  "+l)
			}
		}
	}
	if len(list) == 0 {
		lines = append(lines, "", th().Faint.Render("none: a adds one"))
	}
	keys := "a add · x remove · ↑ ↓ move · esc back"
	if !v.sr.all {
		keys = "a add · x remove · f follow all projects · esc back"
	}
	return m.popup(box{title: l.label + " " + v.sr.forWho, body: lines, sel: sel, keys: keys})
}
