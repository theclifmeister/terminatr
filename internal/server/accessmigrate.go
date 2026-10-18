package server

import (
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/version"
)

// The access migration (T574 §6): at its start, before any agent runs,
// the server moves a config.toml of tm 0.37 or before to the access
// rules (config.MigrateFile), keeping a backup beside it, and tells
// each project: a journal line and an inbox note listing each change,
// what it left for the human, and the two meanings that change. When
// the before/after check refuses, the file stays as it is, read as it
// was, and each project's inbox says why, once for each reason.

// KindAccessMigration is the inbox item of the access migration.
const KindAccessMigration = "access-migration"

// accessMeanings are the two meanings that change, which every note
// says.
const accessMeanings = "Two meanings change: yolo now turns the sandbox off for Claude threads too (Codex's already was), and escape prompts go only to you, never to the coordinator."

// release is the release the backup is named after: 0.38 of v0.38.1.
func release() string {
	v := strings.TrimPrefix(version.Version, "v")
	if parts := strings.SplitN(v, ".", 3); len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// migrateAccess runs the migration and tells the projects.
func migrateAccess(logf func(string, ...any)) {
	m, backup, err := config.MigrateFile(release())
	switch {
	case err != nil:
		logf("access migration: %v; config.toml is read as it was", err)
		noteAccess(func(string) (string, bool) {
			return "tm couldn't move config.toml to the access rules (" + err.Error() + "): it is read as it was, and sandbox changes wait until it moves. Fix it, then restart the server (tm server restart).", true
		}, true)
		return
	case m == nil:
		return
	case m.Differs != "":
		logf("access migration refused: %s; config.toml is read as it was", m.Differs)
		noteAccess(func(string) (string, bool) {
			return "tm didn't move config.toml to the access rules: " + m.Differs + ". It is read as it was, and sandbox changes wait until it moves; change the file so it moves the same (tm doctor lists what is old), then restart the server (tm server restart).", true
		}, true)
		return
	}
	logf("access migration: config.toml moved to the access rules, the old file kept as %s", backup)
	noteAccess(func(slug string) (string, bool) {
		var parts []string
		ask := false
		add := func(who string, changes, kept []string) {
			if len(changes) > 0 {
				parts = append(parts, who+": "+strings.Join(changes, "; ")+".")
			}
			if len(kept) > 0 {
				parts = append(parts, who+", kept for you to decide: "+strings.Join(kept, "; ")+".")
				ask = true
			}
		}
		add(config.AllProjectsName, m.Changes[""], m.Kept[""])
		add(slug, m.Changes[slug], m.Kept[slug])
		if slices.Contains(m.Yolo, slug) {
			parts = append(parts, slug+" has yolo on: its threads run with no sandbox, so its rules weren't compared.")
		}
		summary := "tm moved config.toml to the access rules (the old file: " + backup + "); threads get the same rules. " + strings.Join(parts, " ") + " " + accessMeanings
		return summary, ask
	}, false)
}

// noteAccess journals the migration in each project and leaves it an
// inbox note, what note says for it (and whether it needs the user);
// once only, an open note saying the same, with once.
func noteAccess(note func(slug string) (string, bool), once bool) {
	list, err := project.List()
	if err != nil {
		return
	}
	for _, sum := range list {
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		summary, ask := note(sum.Slug)
		if once {
			items, _ := p.Inbox()
			if slices.ContainsFunc(items, func(it project.Item) bool { return it.Kind == KindAccessMigration && it.Summary == strings.Join(strings.Fields(summary), " ") }) {
				continue
			}
		}
		p.Journal(caller.Caller{Kind: caller.Ticker}, "settings.migrate", sum.Slug, summary)
		p.AddItem(KindAccessMigration, "config.toml", summary, ask)
	}
}
