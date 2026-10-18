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

// The Access tab's sandbox rows (docs/SPEC.md §5.2, T574 §4.1, §4.2):
// what a project's threads may do beyond their sandbox, in a project's
// popup and, for all projects, in the , popup. Inside the sandbox: the
// hosts its commands may reach, local ports, and the guidance its brief
// gets; then the commands a thread runs outside its sandbox (Outside the
// sandbox), and the permission rules it gets on top of tm's. p adds a
// preset's rules (settings_presets.go). It shows only what applies on
// the platform the server is on, where threads run
// (config.SandboxSettings); config.toml keeps the rest, and a launch
// skips it. Every list adds down the layers (config.MergeSandbox): a
// project's view lists all projects' entries too, d drops one for the
// project and u takes it back. Writes are the human's only.

// sandboxOutside is what an excluded command opens, said wherever one is
// added or listed.
const sandboxOutside = "Excluded commands run outside the sandbox, with your full file and network access (tm's socket included): only the guard and the permission check still judge them."

// sandboxBoth is what an excluded command with an allow rule covering it
// means, said plainly wherever either is added or listed.
const sandboxBoth = "An excluded command that an allow rule also covers runs outside the sandbox without asking: if it can run code (a test runner runs test files the thread can write), the thread may run any code as you without asking."

// sandboxLocal is what local ports open, put to the human before they
// are opened.
const sandboxLocal = "A thread can then connect to every service listening on a local port (databases, Docker over TCP, Chrome's debug port, IDE extensions) and can take a port another program expects. It may also listen on ports, which macOS's sandbox doesn't limit to localhost. A service that runs commands lets the thread out of the sandbox."

// sandboxHost is what a host opens, put to the human before it is
// added.
const sandboxHost = "Every command a thread runs inside its sandbox may then reach it, not only the tool you add it for."

// sandboxGuide heads the Access tab.
const sandboxGuide = "Prefer rules inside the sandbox (p adds a preset's): the thread's commands stay in it, opened only as far as the tool needs. Excluded commands (Outside the sandbox) run outside it: a last resort. " + sandboxBoth

// sandboxAdds says how a project's lists combine with all projects'.
const sandboxAdds = "A project's entries add to all projects'; on a project, d drops one of all projects' and u takes it back."

// sandboxReplaces says how a project's excluded commands and allow
// rules combine with all projects', until the migration
// (config.ReplaceKeys).
const sandboxReplaces = "A project's own list replaces all projects' (adding its first entry starts it from theirs; x on the row follows them again); a project that follows them may drop one (d)."

// sandboxLists are each list's row label, help, view text, example and
// what messages call its entries.
var sandboxLists = map[string]struct{ label, help, about, example, word string }{
	config.SandboxHosts: {"Network hosts",
		"Hosts a thread's sandboxed commands may reach: a host name (registry.npmjs.org), or *.example.com for every host under one. " + sandboxHost + " " + sandboxAdds,
		"Sandboxed commands may reach these hosts. " + sandboxHost,
		"registry.npmjs.org", "hosts"},
	config.SandboxGuides: {"Guidance for threads",
		"Texts tm adds after a thread's brief, worded from its other rules (which download hosts and local ports are open). They open nothing. " + sandboxAdds,
		"Texts tm adds after a thread's brief. They open nothing.",
		config.GuideBrowserTesting, "guidance texts"},
	config.SandboxExcluded: {"Excluded commands",
		"A last resort: commands a thread's Bash runs outside its sandbox, for a tool nothing inside the sandbox gets working (full Chrome, headed Playwright). " + sandboxOutside + " Claude's Bash rule syntax: a trailing * takes any arguments. " + sandboxReplaces,
		sandboxOutside + " Claude still asks, or auto mode decides, unless an allow rule covers the command. " + sandboxBoth,
		"npx playwright *", "excluded commands"},
	config.SandboxAllow: {"Allow rules",
		"Permission rules a thread's agent gets on top of tm's, so auto mode or a prompt doesn't stop a command you excluded (Bash(npx playwright *)). " + sandboxBoth + " tm's deny rules and the guard still win. " + sandboxReplaces,
		"Calls these rules match run without a prompt or auto mode's review; tm's deny rules and the guard still win. " + sandboxBoth,
		"Bash(npx playwright *)", "allow rules"},
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

// sandboxSections head the sandbox lists' rows on the Access tab.
var sandboxSections = map[string]string{config.SandboxExcluded: "Outside the sandbox", config.SandboxAllow: "Permission rules"}

// sandboxSettings are the Access tab's sandbox rows for slug ("" for
// all projects) on platform goos: hosts, local ports and guidance under
// Inside the sandbox, where they apply, then each other list under its
// own section.
func sandboxSettings(slug, goos string) []setting {
	sr := newSafetyRows(slug)
	var rows []setting
	for _, k := range []string{config.SandboxHosts, config.SandboxLocalPorts, config.SandboxGuides} {
		if !config.SandboxApplies(k, goos) {
			continue
		}
		if k == config.SandboxLocalPorts {
			rows = append(rows, sr.localPortsRow())
		} else {
			rows = append(rows, sr.sandboxRow(k))
		}
	}
	if len(rows) > 0 {
		rows[0].section = "Inside the sandbox"
	}
	for _, k := range []string{config.SandboxExcluded, config.SandboxAllow} {
		if config.SandboxApplies(k, goos) {
			row := sr.sandboxRow(k)
			row.section = sandboxSections[k]
			rows = append(rows, row)
		}
	}
	return rows
}

// accessHead are the Access tab's lines above its rows: the guidance,
// and the platform it shows (with why nothing inside the sandbox shows,
// when nothing does).
func accessHead(m *dash, w int) []string {
	goos := m.serverOS()
	where := "Shown: what applies on " + config.OSName(goos) + ", where this server's threads run."
	if !slices.ContainsFunc(config.Presets, func(p config.Preset) bool { return slices.Contains(p.OS, goos) }) {
		where += " Hosts, local ports, guidance and the presets don't apply there"
		if goos == config.OSLinux {
			where += ": Playwright's Chromium should need none in Linux's sandbox"
		}
		where += "."
	}
	return append(faintLines(sandboxGuide, w), append([]string{""}, faintLines(where, w)...)...)
}

// own is the rows' own sandbox table as the console has it.
func (sr safetyRows) own(m *dash) config.SandboxTable {
	return m.data.Sandbox[sr.slug]
}

// merged is what the rows' threads get: all projects' table, and the
// project's own on top (config.MergeSandbox).
func (sr safetyRows) merged(m *dash) config.ThreadSandbox {
	if sr.all {
		return config.MergeSandbox(m.data.Sandbox[""], config.SandboxTable{})
	}
	return config.MergeSandbox(m.data.Sandbox[""], m.data.Sandbox[sr.slug])
}

// editSandbox changes the rows' sandbox table with fn, shows it at once,
// and writes it, journaling what.
func (sr safetyRows) editSandbox(m *dash, what, msg string, fn func(*config.SandboxTable)) tea.Cmd {
	if m.data.Sandbox == nil {
		m.data.Sandbox = map[string]config.SandboxTable{}
	}
	before := m.data.Sandbox[sr.slug]
	t := before.Clone()
	fn(&t)
	if !sr.all {
		config.StartOwnLists(m.data.Sandbox[""], before, &t)
	}
	m.data.Sandbox[sr.slug] = t
	s, table := m.src, sr.table
	return m.act(func() actionMsg {
		if err := s.EditSandbox(table, what, func(t *config.SandboxTable) error { fn(t); return nil }); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}

func (sr safetyRows) localPortsRow() setting {
	k := config.SandboxLocalPorts
	row := setting{label: "Local ports", help: "Threads' commands may listen on and connect to local ports: a dev server, a test server, a local database. " + sandboxLocal + " A server bound to all interfaces is reachable from your network. Opening them says so first. " + agentsWord(k) + " Threads get a change from their next start.",
		value: func(m *dash) string { return onOff(sr.merged(m).LocalPorts) },
		change: func(m *dash) tea.Cmd {
			next := !sr.merged(m).LocalPorts
			msg := "local ports " + onOff(next) + " " + sr.forWho + ": threads get it from their next start"
			set := func() tea.Cmd {
				return sr.editSandbox(m, fmt.Sprintf("%s = %v", k, next), msg, func(t *config.SandboxTable) {
					if sr.all && !next {
						t.LocalPorts = nil // off is all projects' default
						return
					}
					t.LocalPorts = &next
				})
			}
			if !next {
				return set()
			}
			m.confirm("Local ports "+sr.forWho, "Open local ports "+sr.forWho+"? "+sandboxLocal, set)
			return nil
		}}
	if sr.all {
		row.note = func(m *dash) []string {
			return ownersNote(m, func(p ProjectData) bool { return m.data.Sandbox[p.Slug].LocalPorts != nil })
		}
		return row
	}
	row.from = func(m *dash) string {
		if !sr.merged(m).LocalPortsOwn {
			return config.AllProjectsName
		}
		return ""
	}
	row.unset = func(m *dash) tea.Cmd {
		if sr.own(m).LocalPorts == nil {
			m.msg = "local ports of " + sr.slug + " already follow all projects"
			return nil
		}
		msg := sr.slug + " follows all projects' local ports: threads get them from their next start"
		follow := func() tea.Cmd {
			return sr.editSandbox(m, k+" follows all projects", msg, func(t *config.SandboxTable) { t.LocalPorts = nil })
		}
		if on := m.data.Sandbox[""].LocalPorts; on == nil || !*on || sr.merged(m).LocalPorts {
			return follow()
		}
		// Following all projects opens them: ask as opening them does.
		m.confirm("Local ports "+sr.forWho, "Follow all projects' local ports, which are open? "+sandboxLocal, follow)
		return nil
	}
	return row
}

func (sr safetyRows) sandboxRow(key string) setting {
	l := sandboxLists[key]
	row := setting{label: l.label, help: l.help + " " + agentsWord(key),
		value: func(m *dash) string { return sandboxCount(sr, sr.merged(m), key) },
		change: func(m *dash) tea.Cmd {
			m.push(&sandboxView{sr: sr, list: key})
			return nil
		}}
	if sr.all {
		row.note = func(m *dash) []string {
			return ownersNote(m, func(p ProjectData) bool {
				t := m.data.Sandbox[p.Slug]
				return len(t.Lists[key]) > 0 || t.Replacing(key) || slices.ContainsFunc(t.Lists[config.SandboxDrop], func(d string) bool { return m.data.Sandbox[""].Has(key, d) })
			})
		}
	} else if slices.Contains(config.ReplaceKeys, key) {
		// Until the migration, the project's own list replaces all
		// projects': the row follows them as a setting does.
		row.from = func(m *dash) string {
			if !sr.own(m).Replacing(key) {
				return config.AllProjectsName
			}
			return ""
		}
		row.unset = func(m *dash) tea.Cmd { return (&sandboxView{sr: sr, list: key}).follow(m) }
	}
	return row
}

// sandboxCount is a list row's value: how many entries threads get,
// how many of them a project's own, dropped and refused.
func sandboxCount(sr safetyRows, t config.ThreadSandbox, key string) string {
	var given, own, dropped, replaced, refused int
	for _, r := range t.Rules {
		switch {
		case r.Key != key:
		case r.Refused != "":
			refused++
		case r.Replaced:
			replaced++
		case r.Dropped:
			dropped++
		default:
			given++
			if r.Own {
				own++
			}
		}
	}
	if given+dropped+replaced+refused == 0 {
		return "none"
	}
	v := fmt.Sprint(given)
	var parts []string
	if !sr.all && own > 0 && own < given {
		parts = append(parts, fmt.Sprintf("%d %s", own, sr.slug))
	}
	if dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d of all projects dropped", dropped))
	}
	if replaced > 0 {
		parts = append(parts, fmt.Sprintf("%d of all projects replaced", replaced))
	}
	if refused > 0 {
		parts = append(parts, fmt.Sprintf("%d refused", refused))
	}
	if len(parts) > 0 {
		v += " (" + strings.Join(parts, " · ") + ")"
	}
	return v
}

// warnLines is text wrapped to w cells, in the warning colour.
func warnLines(text string, w int) []string {
	lines := wrapLines(text, w)
	for i, l := range lines {
		lines[i] = th().Warn.Render(l)
	}
	return lines
}

// sandboxView lists one sandbox list as the rows' threads get it, all
// projects' entries on a project's too (T574 §4.2): a adds an entry, x
// removes one the table sets; on a project, d drops one of all
// projects' and u takes it back.
type sandboxView struct {
	sr   safetyRows
	list string // a config.SandboxKeys key
	sel  int
}

// sandboxEntry is one line of a sandbox list view: a rule of the list,
// or a project's drop that matches none of all projects' rules (stray).
type sandboxEntry struct {
	config.SandboxRule
	stray bool
}

// entries are the view's lines: the list's rules, then the stray drops.
func (v *sandboxView) entries(m *dash) []sandboxEntry {
	t := v.sr.merged(m)
	var out []sandboxEntry
	for _, r := range t.Rules {
		if r.Key == v.list {
			if v.sr.all {
				r.Own = true // all projects' table is the view's own
			}
			out = append(out, sandboxEntry{SandboxRule: r})
		}
	}
	for _, d := range t.StrayDrops() {
		out = append(out, sandboxEntry{SandboxRule: config.SandboxRule{Key: v.list, Value: d, Dropped: true}, stray: true})
	}
	return out
}

func (v *sandboxView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	es := v.entries(m)
	word := sandboxLists[v.list].word
	var e sandboxEntry
	if v.sel < len(es) {
		e = es[v.sel]
	}
	switch k.String() {
	case "esc", "q":
		m.pop()
		return nil
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(es))
		return nil
	case "a", "+":
		v.add(m)
		return nil
	case "x", "delete", "backspace":
		switch {
		case v.sel >= len(es):
		case e.stray:
			m.msg = e.Value + " is a drop: u takes it back"
		case e.Replaced:
			m.msg = v.sr.slug + "'s own list replaces all projects': f follows theirs again"
		case !e.Own:
			m.msg = e.Value + " is all projects': d drops it for " + v.sr.slug
		default:
			v.sel = max(min(v.sel, len(es)-2), 0)
			return v.sr.editSandbox(m, v.list+" - "+e.Value, e.Value+" removed from the "+word+" "+v.sr.forWho+": threads started from now on don't get it",
				func(t *config.SandboxTable) { t.Remove(v.list, e.Value) })
		}
		return nil
	case "f":
		return v.follow(m)
	case "d":
		switch {
		case v.sr.all || v.sel >= len(es):
		case e.Own:
			m.msg = e.Value + " is " + v.sr.slug + "'s own: x removes it"
		case e.Replaced:
			m.msg = v.sr.slug + "'s own list replaces all projects': f follows theirs again"
		case e.Dropped:
			m.msg = e.Value + " is dropped already: u takes it back"
		default:
			return v.sr.editSandbox(m, "drop + "+e.Value, e.Value+" dropped "+v.sr.forWho+": its threads don't get it from their next start",
				func(t *config.SandboxTable) { t.Add(config.SandboxDrop, e.Value) })
		}
		return nil
	case "u":
		if v.sr.all || v.sel >= len(es) || !e.Dropped || e.Replaced && !slices.Contains(v.sr.merged(m).Drops, e.Value) {
			return nil
		}
		msg := v.sr.slug + " takes " + e.Value + " back from all projects: threads get it from their next start"
		if e.stray {
			msg = "the drop of " + e.Value + " removed " + v.sr.forWho
		}
		return v.sr.editSandbox(m, "drop - "+e.Value, msg, func(t *config.SandboxTable) { t.Remove(config.SandboxDrop, e.Value) })
	}
	return nil
}

// add asks for an entry, checks it, puts its warning (if any) to the
// human, and adds it to the view's table.
func (v *sandboxView) add(m *dash) {
	l := sandboxLists[v.list]
	title := l.label + " " + v.sr.forWho
	label := "e.g. " + l.example + ":"
	if v.list == config.SandboxGuides {
		var names []string
		for _, g := range config.Guides {
			names = append(names, g.Name)
		}
		label = "one of " + strings.Join(names, ", ") + ":"
	}
	m.promptNo(title, label, "", 300, func(e string) tea.Cmd {
		for _, r := range v.entries(m) {
			switch {
			case r.Value != e || r.stray:
			case r.Own:
				m.msg = "not added: " + e + " is listed already"
				return nil
			case r.Replaced:
			case r.Dropped:
				m.msg = "not added: " + e + " is all projects', dropped " + v.sr.forWho + ": u takes it back"
				return nil
			default:
				m.msg = "not added: all projects give " + e + " already"
				return nil
			}
		}
		if err := config.CheckSandboxList(v.list, []string{e}); err != nil {
			m.msg = "not added: " + err.Error()
			return nil
		}
		msg := e + " added to the " + l.word + " " + v.sr.forWho + ": threads get it from their next start"
		save := func() tea.Cmd {
			v.sel = len(v.entries(m))
			return v.sr.editSandbox(m, v.list+" + "+e, msg, func(t *config.SandboxTable) { t.Add(v.list, e) })
		}
		q := ""
		switch v.list {
		case config.SandboxExcluded:
			q = sandboxOutside
		case config.SandboxHosts:
			q = sandboxHost
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
	es := v.entries(m)
	v.sel = min(v.sel, max(len(es)-1, 0))
	lines := faintLines(l.about, w)
	if v.list == config.SandboxExcluded {
		lines = warnLines(l.about, w)
	}
	if !v.sr.all {
		where := "All projects' entries and " + v.sr.slug + "'s own add up; " + v.sr.slug + " doesn't take the ones it drops (─)."
		if slices.Contains(config.ReplaceKeys, v.list) {
			where = "All projects' list, which " + v.sr.slug + " follows: an entry added here starts " + v.sr.slug + "'s own list from it. " + v.sr.slug + " doesn't take the ones it drops (─)."
			if v.sr.own(m).Replacing(v.list) {
				where = v.sr.slug + "'s own list replaces all projects' (─); f follows theirs again."
			}
		}
		lines = append(lines, "")
		lines = append(lines, faintLines(where, w)...)
	}
	if slices.ContainsFunc(es, func(e sandboxEntry) bool { return e.Refused != "" }) {
		lines = append(lines, "")
		lines = append(lines, warnLines("An entry marked ✗ fails its check: threads start without it, and only it, until it is fixed or removed (x).", w)...)
	}
	sel := -1
	for i, e := range es {
		lines = append(lines, "")
		mark, from := "  ", ""
		switch {
		case e.Refused != "":
			mark = "✗ "
		case e.Dropped, e.Replaced:
			mark = "─ "
		}
		if !v.sr.all {
			from = v.sr.slug
			switch {
			case e.stray:
				from = "dropped · matches none of all projects'"
			case e.Replaced:
				from = config.AllProjectsName + " · replaced"
			case e.Dropped:
				from = config.AllProjectsName + " · dropped"
			case !e.Own:
				from = config.AllProjectsName
			}
		}
		text := textsafe.Line(mark+e.Value, w)
		if from != "" {
			fromW := min(max(w/3, 12), 40)
			valW := max(w-fromW-1, 10)
			text = fit(textsafe.Line(mark+e.Value, valW), valW) + " " + textsafe.Line(from, fromW)
		}
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit(text, w)))
			sel = len(lines) - 1
		} else if e.Dropped || e.Replaced {
			lines = append(lines, th().Faint.Render(text))
		} else {
			lines = append(lines, th().Head.Render(text))
		}
		if e.Refused != "" {
			for _, l := range warnLines(e.Refused, w-2) {
				lines = append(lines, "  "+l)
			}
			continue
		}
		if e.Dropped || e.Replaced {
			continue
		}
		for _, warn := range []string{v.about(e.Value), config.SandboxWarning(v.list, e.Value), v.covered(m, e.Value)} {
			if warn == "" {
				continue
			}
			for _, l := range warnLines(warn, w-2) {
				lines = append(lines, "  "+l)
			}
		}
	}
	if len(es) == 0 {
		lines = append(lines, "", th().Faint.Render("none: a adds one"))
	}
	keys := "a add · x remove · ↑ ↓ move · esc back"
	switch {
	case v.sr.all:
	case v.sr.own(m).Replacing(v.list):
		keys = "a add · x remove · f follow all projects · u take it back · esc back"
	default:
		keys = "a add · x remove · d drop for " + v.sr.slug + " · u take it back · esc back"
	}
	return m.popup(box{title: l.label + " " + v.sr.forWho, body: lines, sel: sel, keys: keys})
}

// follow drops a project's own list of a ReplaceKeys key, so its
// threads get all projects' again.
func (v *sandboxView) follow(m *dash) tea.Cmd {
	if v.sr.all || !v.sr.own(m).Replacing(v.list) {
		m.msg = "the " + sandboxLists[v.list].word + " of " + v.sr.slug + " already follow all projects"
		return nil
	}
	v.sel = 0
	return v.sr.editSandbox(m, v.list+" follows all projects", v.sr.slug+" follows all projects' "+sandboxLists[v.list].word+": threads get them from their next start",
		func(t *config.SandboxTable) { t.Follow(v.list) })
}

// about is what a guidance text tells threads, for its line in the
// guidance view; "" for another list's entry.
func (v *sandboxView) about(e string) string {
	if v.list != config.SandboxGuides {
		return ""
	}
	if g, ok := config.GuideNamed(e); ok {
		return g.About
	}
	return ""
}

// covered says which entries of the other list pair with entry e of the
// view's list: the allow rule that lets excluded command e run without
// asking, or the excluded commands allow rule e lets run so; "" for
// none.
func (v *sandboxView) covered(m *dash, e string) string {
	t := v.sr.merged(m)
	switch v.list {
	case config.SandboxExcluded:
		if a := config.SandboxCovered(e, t.Given(config.SandboxAllow)); a != "" {
			return "the allow rule " + a + " covers it: it runs outside the sandbox without asking"
		}
	case config.SandboxAllow:
		var cmds []string
		for _, x := range t.Given(config.SandboxExcluded) {
			if config.SandboxCovered(x, []string{e}) != "" {
				cmds = append(cmds, x)
			}
		}
		if len(cmds) > 0 {
			return "covers the excluded " + strings.Join(cmds, ", ") + ": they run outside the sandbox without asking"
		}
	}
	return ""
}
