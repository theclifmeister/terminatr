package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The Access tab's sandbox rows (docs/SPEC.md §5.2, T574 §4.1, §4.2):
// what a project's threads may do beyond their sandbox, in a project's
// popup and, for all projects, in the , popup. Inside the sandbox: the
// hosts its commands may reach, local ports, and the guidance its brief
// gets; then the commands a thread runs outside its sandbox (Outside the
// sandbox), and the permission rules it gets on top of tm's. It shows
// only what applies on
// the platform the server is on, where threads run
// (config.SandboxSettings); config.toml keeps the rest, and a launch
// skips it. A guide is a title and a text (texteditor.go edits it).
// Every list adds down the layers (config.MergeSandbox): a
// project's view lists all projects' entries too, X drops one for the
// project and takes the drop back. enter or e changes an entry of the
// view's own table in place (T619): its value, an excluded command's
// ask, a guide's title and text. Writes are the human's only.

// sandboxOutside is what an excluded command opens, said wherever one is
// added or listed.
const sandboxOutside = "Excluded commands run outside the sandbox, with your full file and network access (tm's socket included): only the guard and the permission check still judge them."

// sandboxBoth is what an excluded command with an allow rule covering it
// means, said plainly wherever either is added or listed.
const sandboxBoth = "An excluded command that runs without asking runs outside the sandbox with no prompt: if it can run code (a test runner runs test files the thread can write), the thread may run any code as you without asking."

// sandboxAsk is what each excluded command's choice means.
const sandboxAsk = "Each one asks first (the default: the guard puts it to you, whatever auto mode or an allow rule would let through) or runs without asking (W changes it)."

// sandboxLocal is what local ports open, put to the human before they
// are opened.
const sandboxLocal = "A thread can then connect to every service listening on a local port (databases, Docker over TCP, Chrome's debug port, IDE extensions) and can take a port another program expects. It may also listen on ports, which macOS's sandbox doesn't limit to localhost. A service that runs commands lets the thread out of the sandbox."

// sandboxHost is what a host opens, put to the human before it is
// added.
const sandboxHost = "Every command a thread runs inside its sandbox may then reach it, not only the tool you add it for."

// sandboxGuide heads the Access tab.
const sandboxGuide = "Prefer rules inside the sandbox: the thread's commands stay in it, opened only as far as the tool needs. Excluded commands (Outside the sandbox) run outside it: a last resort. " + sandboxBoth

// sandboxAdds says how a project's lists combine with all projects'.
const sandboxAdds = "A project's entries add to all projects'. In the list, enter or e changes an entry in place and X removes it; on a project, all projects' entries change in the , popup, and X drops one or takes the drop back."

// sandboxLists are each list's row label, help, view text, example and
// what messages call its entries.
var sandboxLists = map[string]struct{ label, help, about, example, word string }{
	config.SandboxHosts: {"Network hosts",
		"Hosts a thread's sandboxed commands may reach: a host name (registry.npmjs.org), or *.example.com for every host under one. " + sandboxHost + " " + sandboxAdds,
		"Sandboxed commands may reach these hosts. " + sandboxHost,
		"registry.npmjs.org", "hosts"},
	config.SandboxGuides: {"Guidance for threads",
		"Texts tm adds after a thread's brief, each a title and a text you write. Plain text: they open nothing, and apply on every platform. " + sandboxAdds,
		"Texts tm adds after a thread's brief, each under its title. They open nothing.",
		"Test data", "guides"},
	config.SandboxExcluded: {"Excluded commands",
		"A last resort: commands a thread's Bash runs outside its sandbox, for a tool nothing inside the sandbox gets working (full Chrome, headed Playwright). " + sandboxOutside + " " + sandboxAsk + " Claude's Bash rule syntax: a trailing * takes any arguments. " + sandboxAdds,
		sandboxOutside + " " + sandboxAsk + " " + sandboxBoth,
		"npx playwright *", "excluded commands"},
	config.SandboxAllow: {"Allow rules",
		"Permission rules a thread's agent gets on top of tm's, for tools that neither run commands nor write files (WebFetch(domain:playwright.dev), an MCP tool): an excluded command or a write folder does those. tm's deny rules and the guard still win. " + sandboxAdds,
		"Calls these rules match run without a prompt or auto mode's review; tm's deny rules and the guard still win. Commands and file writes aren't allow rules: an excluded command or a write folder does those.",
		"WebFetch(domain:playwright.dev)", "allow rules"},
	config.SandboxSockets: {"Sockets",
		"Unix sockets besides tm's a thread's sandboxed commands may connect to: a local database's, a dev server's, Docker's. Whatever the program listening there does for them, it does as you, outside the sandbox (Docker's socket runs any container with any of your folders mounted). One socket, its path written whole; never in tm's folder or its run folder (the human socket). " + sandboxAdds,
		"Sandboxed commands may connect to these sockets: what the program listening there does for them, it does as you, outside the sandbox.",
		"~/.colima/default/docker.sock", "sockets"},
	config.SandboxApproved: {"Approved commands",
		"Commands a thread may ask tm to run outside its sandbox, in its worktree (tm run -- make test): its words, matched word for word, a last * taking any further arguments; no shell. tm runs them as you, with its own environment, and journals each run. The thread writes the files most commands read (a Makefile, the tests), so it can make one run any code as you: approve only what you'd let it run anyway. Codex threads also run the excluded commands this way. " + sandboxAdds,
		"tm runs these outside the sandbox, as you, in the thread's worktree, when it asks (tm run). The thread writes the files they read, so it can make them run any code as you.",
		"make test", "approved commands"},
	config.SandboxWrite: {"Write folders",
		"Folders besides its worktree a thread writes in: its file tools without asking, and its sandboxed commands too. One rule for all of it: the guard, Claude's file permission and both agents' sandboxes. A whole folder, everything below it included; never your home, a folder high up, or one holding settings or git hooks. " + sandboxAdds,
		"A thread's file tools and its sandboxed commands write in these folders without asking; tm's own folder, config.toml and the project folder stay closed.",
		"~/src/fixtures", "write folders"},
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
// all projects) on platform goos: hosts, local ports, guidance and write
// folders under Inside the sandbox, where they apply, then each other
// list under its own section.
func sandboxSettings(slug, goos string) []setting {
	sr := newSafetyRows(slug)
	var rows []setting
	for _, k := range []string{config.SandboxHosts, config.SandboxLocalPorts, config.SandboxGuides, config.SandboxWrite, config.SandboxSockets} {
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
	for _, k := range []string{config.SandboxExcluded, config.SandboxApproved, config.SandboxAllow} {
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
func accessHead(m *dash, slug string, w int) []string {
	if !sandboxShown(m, slug) {
		return faintLines(protectionGuide(newSafetyRows(slug).safety(m).Protection), w)
	}
	goos := m.serverOS()
	where := "Shown: what applies on " + config.OSName(goos) + ", where this server's threads run."
	if !config.SandboxApplies(config.SandboxHosts, goos) {
		where += " Hosts and local ports don't apply there"
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
				return len(t.Lists[key]) > 0 || slices.ContainsFunc(t.Lists[config.SandboxDrop], func(d string) bool { return m.data.Sandbox[""].Has(key, d) })
			})
		}
	}
	return row
}

// sandboxCount is a list row's value: how many entries threads get,
// how many of them a project's own, dropped and refused.
func sandboxCount(sr safetyRows, t config.ThreadSandbox, key string) string {
	var given, own, dropped, refused int
	for _, r := range t.Rules {
		switch {
		case r.Key != key:
		case r.Refused != "":
			refused++
		case r.Dropped:
			dropped++
		default:
			given++
			if r.Own {
				own++
			}
		}
	}
	if given+dropped+refused == 0 {
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
// projects' entries on a project's too (T574 §4.2): A adds an entry,
// enter or e changes one the table sets, X removes it, on a project
// drops one of all projects' or takes the drop back.
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
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[k.String()], len(es))
		return nil
	case "A", "+":
		if v.list == config.SandboxGuides {
			v.addGuide(m)
			return nil
		}
		v.add(m)
		return nil
	case "enter", "e":
		switch {
		case v.sel >= len(es):
		case v.list == config.SandboxGuides && k.String() == "enter" && !e.stray:
			v.openGuide(m, e)
		case v.list == config.SandboxGuides:
			v.editGuide(m, e)
		default:
			v.edit(m, e)
		}
		return nil
	case "X", "delete", "backspace":
		switch {
		case v.sel >= len(es):
		case e.Dropped:
			msg := v.sr.slug + " takes " + e.Value + " back from all projects: threads get it from their next start"
			if e.stray {
				msg = "the drop of " + e.Value + " removed " + v.sr.forWho
			}
			return v.sr.editSandbox(m, "drop - "+e.Value, msg, func(t *config.SandboxTable) { t.Remove(config.SandboxDrop, e.Value) })
		case !e.Own:
			return v.sr.editSandbox(m, "drop + "+e.Value, e.Value+" dropped "+v.sr.forWho+": its threads don't get it from their next start",
				func(t *config.SandboxTable) { t.Add(config.SandboxDrop, e.Value) })
		default:
			v.sel = max(min(v.sel, len(es)-2), 0)
			return v.sr.editSandbox(m, v.list+" - "+e.Value, e.Value+" removed from the "+word+" "+v.sr.forWho+": threads started from now on don't get it",
				func(t *config.SandboxTable) { t.Remove(v.list, e.Value) })
		}
		return nil
	case "W":
		return v.toggleAsk(m, e, v.sel < len(es))
	}
	return nil
}

// add asks for an entry, checks it, puts its warning (if any) to the
// human, and adds it to the view's table.
func (v *sandboxView) add(m *dash) {
	l := sandboxLists[v.list]
	title := l.label + " " + v.sr.forWho
	label := "e.g. " + l.example + ":"
	m.promptNo(title, label, "", 300, func(e string) tea.Cmd {
		for _, r := range v.entries(m) {
			switch {
			case r.Value != e || r.stray:
			case r.Own:
				m.msg = "not added: " + e + " is listed already"
				return nil
			case r.Dropped:
				m.msg = "not added: " + e + " is all projects', dropped " + v.sr.forWho + ": X takes it back"
				return nil
			default:
				m.msg = "not added: all projects give " + e + " already"
				return nil
			}
		}
		if err := config.CheckNewSandboxEntry(v.list, e); err != nil {
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
			q = sandboxOutside + " It asks first: W on it lets it run without asking."
		case config.SandboxHosts:
			q = sandboxHost
		}
		if w := config.SandboxWarning(v.list, e); w != "" {
			q += " " + e + ": " + w + "."
		}
		if n := proto.ExcludedOnlyItself(e); n != "" && v.list == config.SandboxExcluded {
			q += " It takes " + n + "."
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
		case e.Dropped:
			mark = "─ "
		}
		if !v.sr.all {
			from = v.sr.slug
			switch {
			case e.stray:
				from = "dropped · matches none of all projects'"
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
		} else if e.Dropped {
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
		if e.Dropped {
			continue
		}
		if v.list == config.SandboxExcluded && !e.stray {
			if e.NoAsk {
				lines = append(lines, "  "+th().Warn.Render("runs without asking"))
			} else {
				lines = append(lines, "  "+th().Faint.Render("asks first"))
			}
		}
	}
	if len(es) == 0 {
		lines = append(lines, "", th().Faint.Render("none: A adds one"))
	}
	keys := "A add · ↑ ↓ move · esc back"
	edit := "enter edit · "
	if v.list == config.SandboxGuides {
		edit = "e edit title · "
	}
	if v.sel < len(es) {
		switch e := es[v.sel]; {
		case e.Dropped:
			keys = "A add · X take it back · ↑ ↓ move · esc back"
		case !e.Own:
			keys = "A add · X drop for " + v.sr.slug + " · ↑ ↓ move · esc back"
		default:
			keys = edit + "A add · X remove · ↑ ↓ move · esc back"
		}
	}
	switch v.list {
	case config.SandboxExcluded:
		keys = "W ask first or not · " + keys
	case config.SandboxGuides:
		keys = "enter show, edit · " + keys
	}
	return m.popup(box{title: l.label + " " + v.sr.forWho, body: lines, sel: sel, keys: keys})
}

// toggleAsk makes excluded command e (ok: one is selected) of the view's
// own list ask first, or run without asking, saying what that opens
// first.
func (v *sandboxView) toggleAsk(m *dash, e sandboxEntry, ok bool) tea.Cmd {
	switch {
	case v.list != config.SandboxExcluded || !ok || e.stray || e.Dropped:
		return nil
	case !e.Own:
		m.msg = e.Value + " is all projects': change it there"
		return nil
	}
	if a := config.SandboxCovered(e.Value, v.sr.merged(m).Given(config.SandboxAllow)); a != "" {
		m.msg = "the allow rule " + a + " lets " + e.Value + " run without asking: remove it from the allow rules first"
		return nil
	}
	ask := e.NoAsk
	msg := e.Value + " asks first " + v.sr.forWho + ": threads get it from their next start, the guard asking at once"
	if !ask {
		msg = e.Value + " runs without asking " + v.sr.forWho + ": threads get it from their next start"
	}
	save := func() tea.Cmd {
		return v.sr.editSandbox(m, fmt.Sprintf("%s %q ask = %v", v.list, e.Value, ask), msg, func(t *config.SandboxTable) { t.SetAsk(e.Value, ask) })
	}
	if ask {
		return save()
	}
	m.confirm(sandboxLists[v.list].label+" "+v.sr.forWho, "Let "+e.Value+" run without asking "+v.sr.forWho+"? "+sandboxBoth, save)
	return nil
}

// guideRefused is why a new guide can't take title, "" when it can: the
// list has one of that title, or the title fails its check.
func (v *sandboxView) guideRefused(m *dash, title string) string {
	for _, r := range v.entries(m) {
		switch {
		case r.Value != title || r.stray:
		case r.Own:
			return "not added: a guide " + title + " is listed already: enter on it edits its text"
		case r.Dropped:
			return "not added: " + title + " is all projects', dropped " + v.sr.forWho + ": X takes it back, or name yours otherwise"
		default:
			return "not added: all projects give a guide " + title + " already: name yours otherwise"
		}
	}
	if err := config.CheckGuideTitle(title); err != nil {
		return "not added: " + err.Error()
	}
	return ""
}

// addGuide asks for a guide's title, then its text, and adds it to the
// view's table.
func (v *sandboxView) addGuide(m *dash) {
	title := "Add a guide " + v.sr.forWho
	m.promptNo(title, "Its title, one line (the brief's heading), e.g. "+sandboxLists[v.list].example+":", "", 120, func(t string) tea.Cmd {
		if why := v.guideRefused(m, t); why != "" {
			m.msg = why
			return nil
		}
		m.editText(title+": "+t, "Its text, as the thread's brief gets it under ## "+t+":", "", config.MaxGuideBody, func(body string) tea.Cmd {
			return v.saveGuide(m, config.Guide{Title: t, Body: body}, true)
		})
		return nil
	})
}

// saveGuide writes guide g to the view's table: added, or its text
// changed.
func (v *sandboxView) saveGuide(m *dash, g config.Guide, added bool) tea.Cmd {
	g.Body = config.GuideBody(g.Body)
	if err := config.CheckGuideBody(g.Body); err != nil {
		m.msg = "not saved: " + err.Error()
		return nil
	}
	what, msg := v.list+" ~ "+g.Title, "the guide "+g.Title+" changed "+v.sr.forWho+": threads get it from their next start"
	if added {
		v.sel = len(v.entries(m))
		what, msg = v.list+" + "+g.Title, "the guide "+g.Title+" added "+v.sr.forWho+": threads get it from their next start"
	}
	return v.sr.editSandbox(m, what, msg, func(t *config.SandboxTable) { t.SetGuide(g) })
}

// openGuide shows guide e's text: to edit when the view's table has it,
// else to read (all projects' on a project, a name of tm 0.38, a
// dropped one).
func (v *sandboxView) openGuide(m *dash, e sandboxEntry) {
	switch {
	case e.Body == "":
		m.push(&textView{title: e.Value, note: "tm 0.38's guidance " + e.Value + ", worded at each launch: the server writes it out as its text when it starts"})
	case !e.Own:
		m.push(&textView{title: e.Value + " · " + config.AllProjectsName, body: e.Body, note: "all projects' guide: change it in the , popup's Access tab"})
	default:
		m.editText(e.Value+" "+v.sr.forWho, "Its text, as the thread's brief gets it under ## "+e.Value+":", e.Body, config.MaxGuideBody, func(body string) tea.Cmd {
			if config.GuideBody(body) == e.Body {
				m.msg = "unchanged"
				return nil
			}
			return v.saveGuide(m, config.Guide{Title: e.Value, Body: body}, false)
		})
	}
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

// uneditable is why entry e can't be changed from the view, "" when it
// can: the view's table must set it.
func (v *sandboxView) uneditable(e sandboxEntry) string {
	switch {
	case e.stray:
		return "a drop that matches none of all projects' rules: X removes it"
	case e.Dropped:
		return e.Value + " is all projects', dropped " + v.sr.forWho + ": X takes it back"
	case !e.Own:
		return e.Value + " is all projects': change it in the , popup's Access tab"
	case v.list == config.SandboxGuides && e.Body == "":
		return "tm 0.38's guidance " + e.Value + ": the server writes it out as its text when it starts"
	}
	return ""
}

// editRefused is why entry e can't become val, "" when it can: checked
// as add checks a new entry.
func (v *sandboxView) editRefused(m *dash, e sandboxEntry, val string) string {
	if val == e.Value {
		return ""
	}
	for _, r := range v.entries(m) {
		switch {
		case r.Value != val || r.stray:
		case r.Own:
			return "not changed: " + val + " is listed already"
		case r.Dropped:
			return "not changed: " + val + " is all projects', dropped " + v.sr.forWho + ": X takes it back"
		default:
			return "not changed: all projects give " + val + " already"
		}
	}
	if err := config.CheckNewSandboxEntry(v.list, val); err != nil {
		return "not changed: " + err.Error()
	}
	return ""
}

// edit opens entry e for editing: its value, then an excluded command's
// choice to ask first; saved as one change.
func (v *sandboxView) edit(m *dash, e sandboxEntry) {
	if why := v.uneditable(e); why != "" {
		m.msg = why
		return
	}
	l := sandboxLists[v.list]
	m.push(&inputView{title: "Edit " + e.Value + " " + v.sr.forWho, label: l.label + ", e.g. " + l.example + ":", text: e.Value, cancel: "unchanged", max: 300,
		submit: func(val string) tea.Cmd {
			if why := v.editRefused(m, e, val); why != "" {
				m.msg = why
				return nil
			}
			ed := config.SandboxEdit{Key: v.list, Old: e.Value, New: val}
			if v.list != config.SandboxExcluded {
				return v.saveEdit(m, e, ed)
			}
			pick := func(ask bool) menuItem {
				label := "asks first"
				if !ask {
					label = "runs without asking"
				}
				return menuItem{label: label, run: func(m *dash) tea.Cmd {
					ed.Ask = &ask
					return v.saveEdit(m, e, ed)
				}}
			}
			sel := 0
			if e.NoAsk {
				sel = 1
			}
			m.push(&menuView{title: val + " " + v.sr.forWho, items: []menuItem{pick(true), pick(false)}, sel: sel, centred: true})
			return nil
		}})
}

// editGuide opens guide e for editing: its title, then its text; saved
// as one change.
func (v *sandboxView) editGuide(m *dash, e sandboxEntry) {
	if why := v.uneditable(e); why != "" {
		m.msg = why
		return
	}
	title := "Edit the guide " + e.Value + " " + v.sr.forWho
	m.push(&inputView{title: title, label: "Its title, one line (the brief's heading):", text: e.Value, cancel: "unchanged", max: 120,
		submit: func(t string) tea.Cmd {
			if why := v.editRefused(m, e, t); why != "" {
				m.msg = why
				return nil
			}
			m.editText(title, "Its text, as the thread's brief gets it under ## "+t+":", e.Body, config.MaxGuideBody, func(body string) tea.Cmd {
				body = config.GuideBody(body)
				if err := config.CheckGuideBody(body); err != nil {
					m.msg = "not saved: " + err.Error()
					return nil
				}
				return v.saveEdit(m, e, config.SandboxEdit{Key: v.list, Old: e.Value, New: t, Body: &body})
			})
			return nil
		}})
}

// saveEdit writes edit ed of entry e, putting what a new value opens to
// the human first, as add does.
func (v *sandboxView) saveEdit(m *dash, e sandboxEntry, ed config.SandboxEdit) tea.Cmd {
	if !ed.Changed(e.SandboxRule) {
		m.msg = "unchanged"
		return nil
	}
	l := sandboxLists[v.list]
	what := v.list + " ~ " + ed.Old
	msg := e.Value + " changed in the " + l.word + " " + v.sr.forWho + ": threads get it from their next start"
	if ed.New != ed.Old {
		what += " → " + ed.New
		msg = e.Value + " changed to " + ed.New + " in the " + l.word + " " + v.sr.forWho + ": threads get it from their next start"
	}
	if ed.Ask != nil {
		what += fmt.Sprintf(" (ask = %v)", *ed.Ask)
	}
	q := ""
	if ed.New != ed.Old {
		switch v.list {
		case config.SandboxExcluded:
			q = sandboxOutside
		case config.SandboxHosts:
			q = sandboxHost
		}
		if w := config.SandboxWarning(v.list, ed.New); w != "" {
			q += " " + ed.New + ": " + w + "."
		}
	}
	if ed.Ask != nil && !*ed.Ask && !e.NoAsk {
		q += " " + sandboxBoth
	}
	if ed.New != ed.Old || ed.Ask != nil && *ed.Ask {
		if c := v.covered(m, ed.New); c != "" {
			q += " " + c + "."
		}
	}
	save := func() tea.Cmd { return v.sr.editRule(m, ed, what, msg) }
	if q == "" {
		return save()
	}
	change := "Change " + e.Value + " to " + ed.New
	if ed.New == ed.Old {
		change = "Change " + e.Value
	}
	m.confirm("Edit "+e.Value+" "+v.sr.forWho, change+" "+v.sr.forWho+"? "+strings.TrimSpace(q), save)
	return nil
}

// editRule changes one entry of the rows' table by ed, shows it at once
// (all projects' taking the projects' drops of it along), and writes
// it, journaling what.
func (sr safetyRows) editRule(m *dash, ed config.SandboxEdit, what, msg string) tea.Cmd {
	if m.data.Sandbox == nil {
		m.data.Sandbox = map[string]config.SandboxTable{}
	}
	t := m.data.Sandbox[sr.slug].Clone()
	if err := ed.Apply(&t); err != nil {
		m.msg = "not changed: " + err.Error()
		return nil
	}
	m.data.Sandbox[sr.slug] = t
	moved := false
	for slug, own := range m.data.Sandbox {
		if o := own.Clone(); sr.all && slug != "" && ed.MoveDrop(&o, t) {
			m.data.Sandbox[slug], moved = o, true
		}
	}
	if moved {
		msg = strings.TrimSuffix(msg, ": threads get it from their next start") + "; the projects that dropped " + ed.Old + " drop " + ed.New + ": threads get it from their next start"
	}
	s, table := m.src, sr.table
	return m.act(func() actionMsg {
		if err := s.EditSandboxRule(table, what, ed); err != nil {
			return actionMsg{err: settingsErr(err)}
		}
		return actionMsg{msg: msg}
	})
}
