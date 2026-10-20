package tui

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Access tab (T574 §4.1, §4.5): everything that decides what a
// project's threads may do, in one place, in a project's popup (5
// access) and, for all projects, in the , popup (3 access). Protection,
// prompts and escapes first, then the sandbox's presets and lists
// (settings_sandbox.go), the guard (settings_guard.go), the secret
// environment (settings_secretenv.go) and tm's own rules, read-only.
// The Settings and All projects tabs keep the workflow rows. e on a
// project's tab opens its effective access (access.go).

// accessSettings are the Access tab's rows for slug ("" for all
// projects) on platform goos.
func accessSettings(slug, goos string) []setting {
	sr := newSafetyRows(slug)
	prompts := sr.scope([]setting{sr.protectionRow(), sr.yoloRow(), sr.toggle("coordinator_approves")})
	prompts[0].section = "Protection and prompts"
	guard := sr.scope(sr.guardRows())
	guard[0].section = "Guard"
	env := sr.secretEnvRow()
	env.section = "Secrets"
	rows := append(prompts, sandboxSettings(slug, goos)...)
	rows = append(rows, guard...)
	return append(rows, env, sr.builtInRow())
}

// protectionWords name protection's levels, each what it adds.
var protectionWords = map[string]string{
	config.ProtectOpen:      "open: the agent's own prompts",
	config.ProtectGuarded:   "guarded: + the guard's rules",
	config.ProtectSandboxed: "sandboxed: + the thread sandbox",
	config.ProtectStrict:    "strict: sandbox, no escapes",
}

// protectionWord is s's protection in words.
func protectionWord(s config.Safety) string {
	return cmp.Or(protectionWords[s.Protection], s.Protection)
}

func (sr safetyRows) protectionRow() setting {
	return setting{label: "Protection", help: "How much of tm's security agents run under, each level adding to the one before. Open: each agent as it runs on its own, with its own permission prompts; no thread sandbox. Guarded: the guard's rules too (Guard rules below). Sandboxed: Claude threads run in their sandbox, with the lists below, and may ask you to run a command outside it. Strict: no such escapes; what can't run in the sandbox doesn't run. At every level threads never write the project folder, no agent changes your settings or reads tm's files, and Codex threads keep Codex's own sandbox. Enter picks the next level; going back to open asks first. Threads get a change from their next start or tm thread restart; the guard at once.",
		keys: []string{"protection"}, word: protectionWord,
		value: func(m *dash) string { return protectionWord(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			ls := config.ProtectLevels
			next := ls[(slices.Index(ls, sr.safety(m).Protection)+1)%len(ls)]
			msg := "protection " + sr.forWho + ": " + next + ", for threads started from now on"
			if next != config.ProtectOpen {
				return sr.set(m, "protection", next, msg)
			}
			m.confirm("Open protection", "Set protection "+sr.forWho+" to open? Threads started from now on run without their sandbox, with the agent's own permission prompts, and the guard keeps only its floor.", func() tea.Cmd {
				return sr.set(m, "protection", next, msg)
			})
			return nil
		}}
}

// builtInHelp says what tm gives every thread, whatever the settings.
const builtInHelp = "What tm gives every thread, whatever the rows above say: its worktree to write, the project folder to read but never write, config.toml never written, the repository's git objects and refs writable (never its hooks or config), tm's socket (never yours), and the guard's floor (tm's files, printing a secret variable, force-pushing to a protected branch, a thread merging)."

// builtInRow lists tm's own rules: on a project, enter shows them as its
// effective access does.
func (sr safetyRows) builtInRow() setting {
	row := setting{section: "Built in", label: "tm's rules", help: builtInHelp,
		value: func(m *dash) string { return "read-only · a project's Access tab lists them" }}
	if sr.all {
		return row
	}
	row.value = func(m *dash) string { return "read-only · enter lists them" }
	row.change = func(m *dash) tea.Cmd { return m.showAccessFrom(sr.slug, proto.AccessFromTM) }
	return row
}

// resetAccess asks, then puts the Access switches of slug ("" all
// projects) back to what they follow: a project's to all projects', all
// projects' to tm's defaults (R on an Access tab). The lists, the
// guard's allowances and the secret environment stay.
func (m *dash) resetAccess(slug string) tea.Cmd {
	sr := newSafetyRows(slug)
	to := "all projects'"
	if sr.all {
		to = "tm's defaults (protection open)"
	}
	src := m.src
	m.confirm("Reset access", "Reset the access switches "+sr.forWho+" to "+to+"? Protection, yolo mode, coordinator approves, the guard, its mode and its rules turned off go back; the sandbox lists, guard allowances and secret environment stay.", func() tea.Cmd {
		return m.act(func() actionMsg {
			reset, err := src.ResetAccess(sr.table)
			switch {
			case err != nil:
				return actionMsg{err: settingsErr(err)}
			case len(reset) == 0:
				return actionMsg{msg: "nothing to reset " + sr.forWho + ": it follows " + to + " already"}
			}
			return actionMsg{msg: "access reset " + sr.forWho + " to " + to + ", for threads started from now on"}
		})
	})
	return nil
}
