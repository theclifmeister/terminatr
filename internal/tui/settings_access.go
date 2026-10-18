package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Access tab (T574 §4.1, §4.5): everything that decides what a
// project's threads may do, in one place, in a project's popup (5
// access) and, for all projects, in the , popup (3 Access). Prompts and
// escapes first, then the sandbox's presets and lists
// (settings_sandbox.go), the guard (settings_guard.go), the secret
// environment (settings_secretenv.go) and tm's own rules, read-only.
// The Settings and All projects tabs keep the workflow rows. e on a
// project's tab opens its effective access (access.go).

// accessSettings are the Access tab's rows for slug ("" for all
// projects) on platform goos.
func accessSettings(slug, goos string) []setting {
	sr := newSafetyRows(slug)
	prompts := sr.scope([]setting{sr.escapesRow(), sr.yoloRow(), sr.toggle("coordinator_approves")})
	prompts[0].section = "Prompts and escapes"
	guard := sr.scope(sr.guardRows())
	guard[0].section = "Guard"
	env := sr.secretEnvRow()
	env.section = "Secrets"
	rows := append(prompts, sandboxSettings(slug, goos)...)
	rows = append(rows, guard...)
	return append(rows, env, sr.builtInRow())
}

// escapesWords name sandbox_escapes' values.
func escapesWords(s config.Safety) string {
	if s.SandboxEscapes {
		return "ask you or auto mode"
	}
	return "never"
}

func (sr safetyRows) escapesRow() setting {
	return setting{label: "Escapes", help: "Whether a thread's agent may ask to run a command outside its sandbox when the sandbox stops it: on, you (or auto mode) approve each one; never, what can't run in the sandbox doesn't run. Claude threads; Codex threads ask as before. Threads get a change from their next start or tm thread restart.",
		keys: []string{"sandbox_escapes"}, word: escapesWords,
		value: func(m *dash) string { return escapesWords(sr.safety(m)) },
		change: func(m *dash) tea.Cmd {
			s := sr.safety(m)
			s.SandboxEscapes = !s.SandboxEscapes
			return sr.set(m, "sandbox_escapes", s.SandboxEscapes, "escapes "+sr.forWho+": "+escapesWords(s)+", for threads started from now on")
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
