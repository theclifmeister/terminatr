package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The Sandbox tab (docs/SPEC.md §5.2): what a project's threads may do
// beyond their sandbox, in a project's popup and, for all projects, in
// the , popup. Presets first, named switches that open what one kind of
// tool needs while its commands stay in the sandbox; then, under
// Advanced, the sandbox lists: the commands a thread runs outside its
// sandbox, and the permission rules it gets on top of tm's. It shows
// only what applies on the platform the server is on, where threads
// run (config.SandboxSettings); config.toml keeps the rest, and a launch
// skips it. A project's switch or list of a key replaces all projects'
// (config.ThreadSandbox), so on a project a list's view edits the
// project's own list, starting from all projects' when it follows
// them, and f follows them again. Writes are the human's only.

// sandboxOutside is what an excluded command opens, said wherever one is
// added or listed.
const sandboxOutside = "Excluded commands run outside the sandbox, with your full file and network access (tm's socket included): only the guard and the permission check still judge them."

// sandboxBoth is what an excluded command with an allow rule covering it
// means, said plainly wherever either is added or listed.
const sandboxBoth = "An excluded command that an allow rule also covers runs outside the sandbox without asking: if it can run code (a test runner runs test files the thread can write), the thread may run any code as you without asking."

// sandboxLocal is what local binding opens, put to the human before a
// preset with it is turned on.
const sandboxLocal = "A thread can then connect to every service listening on a local port (databases, Docker over TCP, Chrome's debug port, IDE extensions) and can take a port another program expects. It may also listen on ports, which macOS's sandbox doesn't limit to localhost. A service that runs commands lets the thread out of the sandbox."

// sandboxDownloads is what browser testing's download hosts open, put to
// the human before it is turned on.
var sandboxDownloads = "It opens " + strings.Join(config.BrowserDomains, ", ") + " to every command a thread runs, not only npm's and Playwright's. storage.googleapis.com is every Google Cloud Storage bucket's host: a thread's commands can download from, or send your files to, any bucket there."

// sandboxConfirm is what turning on a preset puts to the human first.
var sandboxConfirm = map[string]string{config.SandboxBrowser: sandboxDownloads, config.SandboxDevServers: sandboxLocal}

// sandboxGuide heads the Sandbox tab.
const sandboxGuide = "Prefer a preset: the thread's commands stay inside the sandbox, opened only as far as the tool needs. Excluded commands (Advanced) run outside it: a last resort. " + sandboxBoth

// sandboxLists are each list's row label, help and view text.
var sandboxLists = map[string]struct{ label, help, about, example string }{
	config.SandboxExcluded: {"Excluded commands",
		"A last resort: commands a thread's Bash runs outside its sandbox, for a tool no preset gets working (full Chrome, headed Playwright). " + sandboxOutside + " Claude's Bash rule syntax: a trailing * takes any arguments. A project's list replaces all projects'.",
		sandboxOutside + " Claude still asks, or auto mode decides, unless an allow rule covers the command. " + sandboxBoth,
		"npx playwright *"},
	config.SandboxAllow: {"Allow rules",
		"Permission rules a thread's agent gets on top of tm's, so auto mode or a prompt doesn't stop a command you excluded (Bash(npx playwright *)). " + sandboxBoth + " tm's deny rules and the guard still win. A project's list replaces all projects'.",
		"Calls these rules match run without a prompt or auto mode's review; tm's deny rules and the guard still win. " + sandboxBoth,
		"Bash(npx playwright *)"},
}

// sandboxPresets are each preset's row label and help.
var sandboxPresets = map[string]struct{ label, help string }{
	config.SandboxBrowser: {"Browser testing (Playwright, Chromium, downloads)",
		"Playwright and other Chromium tools inside the sandbox: threads are told to launch the headless shell in one process (--single-process), the one Chromium that starts in macOS's sandbox, and get a ready playwright.sandbox.config.ts. npm installs and Playwright's browser downloads work: " + sandboxDownloads + " No local ports are opened: to test against a local dev server, turn on Local dev servers too. Full Chrome and headed runs still need an excluded command (Advanced). Turning it on says so first."},
	config.SandboxDevServers: {"Local dev servers",
		"Threads' commands may listen on and connect to local ports: a dev server, a test server, a local database. A thread can connect to every service listening on a local port (databases, Docker over TCP, Chrome's debug port, IDE extensions) and can take a port another program expects; a server bound to all interfaces is reachable from your network. Turning it on says so first."},
}

// agentsWord says which agents' threads get key.
func agentsWord(key string) string {
	if config.SandboxSettings[key].Codex {
		return "Claude and Codex threads."
	}
	return "Claude threads only; Codex has no such setting."
}

// serverOS is the platform the server runs threads on: the one it says,
// else this console's (a server of an older build says none).
func (m *dash) serverOS() string {
	if os := m.data.Server.OS; os != "" {
		return os
	}
	return caps.OS
}

// sandboxSettings are the Sandbox tab's rows for slug ("" for all
// projects) on platform goos: the presets that apply there under
// Presets, then the lists under Advanced.
func sandboxSettings(slug, goos string) []setting {
	sr := newSafetyRows(slug)
	var rows []setting
	for _, k := range config.SandboxPresets {
		if config.SandboxApplies(k, goos) {
			rows = append(rows, sr.presetRow(k))
		}
	}
	if len(rows) > 0 {
		rows[0].section = "Presets"
	}
	adv := len(rows)
	for _, k := range config.SandboxKeys {
		if config.SandboxApplies(k, goos) {
			rows = append(rows, sr.sandboxRow(k))
		}
	}
	if adv < len(rows) {
		rows[adv].section = "Advanced"
	}
	return rows
}

// sandboxHead are the Sandbox tab's lines above its rows: the guidance,
// and the platform it shows (with why no preset shows, when none does).
func sandboxHead(m *dash, w int) []string {
	goos := m.serverOS()
	where := "Shown: what applies on " + config.OSName(goos) + ", where this server's threads run."
	if !slices.ContainsFunc(config.SandboxPresets, func(k string) bool { return config.SandboxApplies(k, goos) }) {
		where += " No preset applies there"
		if goos == config.OSLinux {
			where += ": Playwright's Chromium should need none in Linux's sandbox"
		}
		where += "."
	}
	return append(faintLines(sandboxGuide, w), append([]string{""}, faintLines(where, w)...)...)
}

// presetOf is whether preset key is on for the rows' threads, and
// whether the table sets it itself (all projects' always does).
func (sr safetyRows) presetOf(m *dash, key string) (bool, bool) {
	if on, ok := m.data.SandboxPresets[sr.slug][key]; ok || sr.all {
		return on, true
	}
	return m.data.SandboxPresets[""][key], false
}

func (sr safetyRows) presetRow(key string) setting {
	p := sandboxPresets[key]
	row := setting{label: p.label, help: p.help + " " + agentsWord(key) + " Threads get a change from their next start.",
		value: func(m *dash) string {
			on, _ := sr.presetOf(m, key)
			return onOff(on)
		},
		change: func(m *dash) tea.Cmd {
			on, _ := sr.presetOf(m, key)
			next := !on
			msg := p.label + " " + onOff(next) + " " + sr.forWho + ": threads get it from their next start"
			if sr.all && !next {
				return sr.savePreset(m, key, nil, msg) // off is all projects' default
			}
			if !next {
				return sr.savePreset(m, key, &next, msg)
			}
			warn, ok := sandboxConfirm[key]
			if !ok {
				return sr.savePreset(m, key, &next, msg)
			}
			m.confirm(p.label+" "+sr.forWho, "Turn on "+p.label+" "+sr.forWho+"? "+warn, func() tea.Cmd {
				return sr.savePreset(m, key, &next, msg)
			})
			return nil
		}}
	if !sr.all {
		row.from = func(m *dash) string {
			if _, own := sr.presetOf(m, key); !own {
				return config.AllProjectsName
			}
			return ""
		}
		row.unset = func(m *dash) tea.Cmd {
			if _, own := sr.presetOf(m, key); !own {
				m.msg = p.label + " of " + sr.slug + " already follows all projects"
				return nil
			}
			return sr.savePreset(m, key, nil, sr.slug+" follows all projects' "+p.label+": threads get it from their next start")
		}
	}
	return row
}

// savePreset writes preset key's switch in the rows' table (nil: none of
// its own) and shows it at once.
func (sr safetyRows) savePreset(m *dash, key string, on *bool, msg string) tea.Cmd {
	if m.data.SandboxPresets == nil {
		m.data.SandboxPresets = map[string]map[string]bool{}
	}
	sw := map[string]bool{}
	for k, v := range m.data.SandboxPresets[sr.slug] {
		sw[k] = v
	}
	if on == nil {
		delete(sw, key)
	} else {
		sw[key] = *on
	}
	m.data.SandboxPresets[sr.slug] = sw
	s, table := m.src, sr.table
	return m.act(func() actionMsg {
		if err := s.SetSandboxPreset(table, key, on); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}

func (sr safetyRows) sandboxRow(key string) setting {
	l := sandboxLists[key]
	row := setting{label: l.label, help: l.help + " " + agentsWord(key),
		value: func(m *dash) string {
			list, _ := sr.sandboxOf(m, key)
			if len(list) == 0 {
				return "none"
			}
			if n := len(slices.DeleteFunc(config.SandboxProblems(key, list), func(p string) bool { return p == "" })); n > 0 {
				return fmt.Sprintf("%d, %d invalid", len(list), n)
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
		if c := v.covered(m, e); c != "" {
			q += " " + c + "."
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
	problems := config.SandboxProblems(v.list, list)
	if slices.ContainsFunc(problems, func(p string) bool { return p != "" }) {
		lines = append(lines, "")
		lines = append(lines, warnLines("An entry marked ✗ fails the checks: until it is fixed or removed (x), config.toml's sandbox lists and presets for "+v.sr.forWho+" are all dropped at launch: threads start without them.", w)...)
	}
	for i, e := range list {
		lines = append(lines, "")
		text := textsafe.Line(e, w)
		if problems[i] != "" {
			text = textsafe.Line("✗ "+e, w)
		}
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		} else {
			lines = append(lines, th().Head.Render(text))
		}
		if problems[i] != "" {
			for _, l := range warnLines(problems[i], w-2) {
				lines = append(lines, "  "+l)
			}
			continue
		}
		for _, warn := range []string{config.SandboxWarning(v.list, e), v.covered(m, e)} {
			if warn == "" {
				continue
			}
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

// covered says which entries of the other list pair with entry e of the
// view's list: the allow rule that lets excluded command e run without
// asking, or the excluded commands allow rule e lets run so; "" for
// none.
func (v *sandboxView) covered(m *dash, e string) string {
	if v.list == config.SandboxExcluded {
		allow, _ := v.sr.sandboxOf(m, config.SandboxAllow)
		if a := config.SandboxCovered(e, allow); a != "" {
			return "the allow rule " + a + " covers it: it runs outside the sandbox without asking"
		}
		return ""
	}
	excluded, _ := v.sr.sandboxOf(m, config.SandboxExcluded)
	var cmds []string
	for _, x := range excluded {
		if config.SandboxCovered(x, []string{e}) != "" {
			cmds = append(cmds, x)
		}
	}
	if len(cmds) == 0 {
		return ""
	}
	return "covers the excluded " + strings.Join(cmds, ", ") + ": they run outside the sandbox without asking"
}
