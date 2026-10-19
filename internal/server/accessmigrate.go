package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/version"
)

// The access migration (T574 §6): at its start, before any agent runs,
// the server moves a config.toml of tm 0.37 or before to the access
// rules (config.MigrateFile), keeping a backup beside it, and tells
// each project: a journal line and an inbox note listing each change,
// what it left for the human, and the two meanings that change. When
// the before/after check refuses, the file stays as it is, read as it
// was, and each project's inbox says why, once for each reason. Each
// write folder a file allow rule or a worktree-only allowance became
// reaches further than it did, which the check can't see: the note
// names it and needs the user. Whatever the migration does, every
// project with yolo on is told once that yolo now turns Claude's
// sandbox off too (tellYolo).

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
		same := "threads get the same rules."
		for _, w := range []struct{ table, who, undo string }{{"", config.AllProjectsName, "--all"}, {slug, slug, slug}} {
			for _, f := range m.Widened[w.table] {
				parts = append(parts, "Check write folder "+f+" ("+w.who+"): it was a file allow rule or a guard allowance, which let only Claude's file tools write there; now the thread's sandboxed commands (Bash) and Codex threads can write there too. tm access rm write "+f+" "+w.undo+" undoes it.")
				ask, same = true, "threads get the same rules, but for the write folders named below."
			}
		}
		if slices.Contains(m.Yolo, slug) {
			parts = append(parts, slug+" has yolo on: its threads run with no sandbox, so its rules weren't compared.")
		}
		summary := "tm moved config.toml to the access rules (the old file: " + backup + "); " + same + " " + strings.Join(parts, " ") + " " + accessMeanings
		return summary, ask
	}, false)
}

// yoloNote is what tellYolo tells a project with yolo on.
const yoloNote = "Yolo now turns the sandbox off for Claude threads too, as it already did for Codex threads: threads of this project run with no sandbox and no permission prompts, and can read and write anything you can and reach tm's socket; the guard keeps only its floor. tm access set yolo off <project> turns it off."

// tellYolo tells each project with yolo on, once for this tm home (a
// mark in state/), that yolo now turns Claude's sandbox off too.
func tellYolo(logf func(string, ...any)) {
	d, err := home.Dir()
	if err != nil {
		return
	}
	mark := filepath.Join(d, "state", "yolo-meaning-told")
	if _, err := os.Stat(mark); err == nil {
		return
	}
	c, err := config.Load()
	if c == nil {
		logf("yolo note: %v; told at a later start", err)
		return
	}
	list, _ := project.List()
	for _, sum := range list {
		if s, err := c.Safety(sum.Slug); err != nil || !s.Yolo {
			continue
		}
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		items, _ := p.Inbox()
		if slices.ContainsFunc(items, func(it project.Item) bool { return it.Kind == KindAccessMigration && it.Summary == yoloNote }) {
			continue
		}
		p.Journal(caller.Caller{Kind: caller.Ticker}, "settings.migrate", sum.Slug, "told: yolo turns the sandbox off for Claude threads too")
		p.AddItem(KindAccessMigration, "yolo", yoloNote, true)
	}
	os.MkdirAll(filepath.Dir(mark), 0o700)
	if err := os.WriteFile(mark, []byte("yolo's meaning told\n"), 0o600); err != nil {
		logf("yolo note: %v", err)
	}
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
		// Plain text: it quotes config.toml's entries, refused ones too.
		summary = textsafe.Line(summary, 0)
		if once {
			items, _ := p.Inbox()
			if slices.ContainsFunc(items, func(it project.Item) bool { return it.Kind == KindAccessMigration && it.Summary == summary }) {
				continue
			}
		}
		p.Journal(caller.Caller{Kind: caller.Ticker}, "settings.migrate", sum.Slug, summary)
		p.AddItem(KindAccessMigration, "config.toml", summary, ask)
	}
}
