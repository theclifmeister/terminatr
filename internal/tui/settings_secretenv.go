package tui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The secret environment of threads (docs/SPEC.md §8.6, Secret
// environment), the Access tab's Secrets row: the variables a thread gets
// when it starts, by name and source. The console and the server only
// ever handle names and sources: a value is got by the server, at a
// thread's start or a test, and never comes back.

// secretEnvRow lists the table's variables, to add, remove or test.
func (sr safetyRows) secretEnvRow() setting {
	return setting{label: "Secret environment", help: "Variables a thread gets in its environment when it starts, each from gh auth token, a login keychain item (macOS) or the server's own environment: its tools (npm, a registry client) use the token and the guard keeps the agent from printing it. A project's add to all projects', replacing one of the same name. Only the name and where the value comes from are kept; enter lists them to add, remove or test.",
		value: func(m *dash) string {
			own, all := sr.envOf(m)
			if len(own)+len(all) == 0 {
				return "none"
			}
			n := len(own)
			from := 0
			for name := range all {
				if _, ok := own[name]; !ok {
					from++
				}
			}
			if from == 0 {
				return fmt.Sprint(n)
			}
			return fmt.Sprintf("%d (%d of all projects)", n+from, from)
		},
		change: func(m *dash) tea.Cmd {
			m.push(&secretEnvView{sr: sr})
			return nil
		}}
}

// envOf is the variables of the rows' table, and on a project all
// projects' too (all, nil on all projects).
func (sr safetyRows) envOf(m *dash) (own, all map[string]config.EnvSource) {
	if sr.all {
		return m.data.Env[""], nil
	}
	return m.data.Env[sr.slug], m.data.Env[""]
}

// envSourceWord is a source in the user's words.
func envSourceWord(e config.EnvSource) string {
	switch kind, ref := e.Kind(); kind {
	case config.EnvFromGH:
		return "gh auth token"
	case "keychain":
		return fmt.Sprintf("the keychain item %q", ref)
	case "env":
		return "the server's " + ref
	}
	return e.From
}

// envEntry is one listed variable: of all projects (on a project's
// list), and then replaced by the project's own of the same name.
type envEntry struct {
	name     string
	src      config.EnvSource
	all      bool
	replaced bool
}

// secretEnvView lists the variables: A adds one (its name, then its
// source), X removes the selected one, T tests it. On a project, all
// projects' are listed too, marked, and removed or tested there.
type secretEnvView struct {
	sr  safetyRows
	sel int
}

// entries are the listed variables: the table's own, then on a project
// all projects', each by name.
func (v *secretEnvView) entries(m *dash) []envEntry {
	own, all := v.sr.envOf(m)
	var out []envEntry
	for _, n := range slices.Sorted(maps.Keys(own)) {
		out = append(out, envEntry{name: n, src: own[n], all: v.sr.all})
	}
	if !v.sr.all {
		for _, n := range slices.Sorted(maps.Keys(all)) {
			_, mine := own[n]
			out = append(out, envEntry{name: n, src: all[n], all: true, replaced: mine})
		}
	}
	return out
}

// save names the variable in its table (all projects' when all), or
// removes it (src nil), and shows it at once.
func (v *secretEnvView) save(m *dash, all bool, name string, src *config.EnvSource, msg string) tea.Cmd {
	key, table := v.sr.slug, v.sr.table
	if all {
		key, table = "", config.DefaultsTable
	}
	env := maps.Clone(m.data.Env[key])
	if env == nil {
		env = map[string]config.EnvSource{}
	}
	if src != nil {
		env[name] = *src
	} else {
		delete(env, name)
	}
	if m.data.Env == nil {
		m.data.Env = map[string]map[string]config.EnvSource{}
	}
	m.data.Env[key] = env
	s := m.src
	return m.act(func() actionMsg {
		if err := s.SetSecretEnv(table, name, src); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}

func (v *secretEnvView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	list := v.entries(m)
	switch k.String() {
	case "esc", "q":
		m.pop()
		return nil
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(list))
		return nil
	case "A", "+":
		v.add(m)
		return nil
	}
	if v.sel >= len(list) {
		return nil
	}
	e := list[v.sel]
	where := v.sr.forWho
	if e.all {
		where = "for all projects"
	}
	switch k.String() {
	case "X", "delete", "backspace":
		v.sel = max(min(v.sel, len(list)-2), 0)
		return v.save(m, e.all, e.name, nil, e.name+" removed "+where+": threads started from now on don't get it")
	case "T", "enter", "space", " ":
		slug := v.sr.slug
		if e.all {
			slug = ""
		}
		s := m.src
		return m.act(func() actionMsg {
			msg, err := s.TestSecretEnv(slug, e.name)
			return actionMsg{msg: msg, err: err}
		})
	}
	return nil
}

// add asks for a new variable's name, then its source, then what the
// source names, and saves it in the view's table.
func (v *secretEnvView) add(m *dash) {
	title := "Secret variable " + v.sr.forWho
	m.promptNo(title, "Name (letters, digits and _, e.g. NODE_AUTH_TOKEN):", "", 128, func(name string) tea.Cmd {
		// Checked with a source that passes, so the name's error shows
		// before the source is asked.
		if err := config.CheckEnv(name, config.EnvSource{From: config.EnvFromGH}); err != nil {
			m.msg = "not added: " + err.Error()
			return nil
		}
		own, _ := v.sr.envOf(m)
		verb := "added"
		if _, ok := own[name]; ok {
			verb = "replaced"
		}
		done := func(from string) tea.Cmd {
			src := config.EnvSource{From: from}
			if err := config.CheckEnv(name, src); err != nil {
				m.msg = "not added: " + err.Error()
				return nil
			}
			v.sel = 0
			return v.save(m, v.sr.all, name, &src, fmt.Sprintf("%s %s %s, from %s: threads get it from their next start (T tests it)", name, verb, v.sr.forWho, envSourceWord(src)))
		}
		ask := func(label, prefix string) func(*dash) tea.Cmd {
			return func(m *dash) tea.Cmd {
				m.promptNo(title, label, "", 200, func(t string) tea.Cmd { return done(prefix + t) })
				return nil
			}
		}
		m.push(&menuView{title: "Where " + name + " comes from", centred: true, items: []menuItem{
			{label: "gh auth token (GitHub's token of the logged-in gh)", run: func(*dash) tea.Cmd { return done(config.EnvFromGH) }},
			{label: "A login keychain item (macOS)", run: ask("The keychain item's service name:", "keychain:")},
			{label: "A variable of the server's environment", run: ask("The variable's name in the server's environment:", "env:")},
		}})
		return nil
	})
}

func (v *secretEnvView) render(m *dash) string {
	w := m.inner(viewWidth)
	list := v.entries(m)
	lines := faintLines("Variables threads get when they start "+v.sr.forWho+", by name and where the value comes from; tm never shows a value.", w)
	sel := -1
	for i, e := range list {
		lines = append(lines, "")
		head := e.name
		switch {
		case e.replaced:
			head += " · all projects, replaced here"
		case e.all && !v.sr.all:
			head += " · all projects"
		}
		from := "  from " + textsafe.Line(envSourceWord(e.src), max(w-7, 8))
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(head, w)), th().Sel.Render(fit(ansi.Strip(from), w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(head), th().Accent.Render(from))
		}
	}
	if len(list) == 0 {
		lines = append(lines, "", th().Faint.Render("none yet: A adds one"))
	}
	return m.popup(box{title: "Secret environment " + v.sr.forWho, body: lines, sel: sel, keys: "A add · X remove · T test · ↑ ↓ move · esc back"})
}

// secretTestWord says what a test of name found: ok, or why not.
func secretTestWord(name string, ok bool, why string) string {
	if ok {
		return name + ": ok, its source gives a value (not shown)"
	}
	return name + ": its source fails: " + strings.TrimSpace(why)
}
