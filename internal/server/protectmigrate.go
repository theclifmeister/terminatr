package server

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
)

// The protection pin: tm starts a new install at protection
// "open" (config.ProtectOpen), where it used to run every
// thread in its sandbox with the guard's rules. At the first start of
// this release in a tm home (a mark in state/), an install that had
// projects or settings is pinned to what it ran at
// (config.MigrateProtection), and each project's journal says so and
// how to open it up (nothing changes for it, so its inbox gets nothing);
// a new one stays open.

// protectNote is what pinProtection tells each project.
func protectNote(level string) string {
	return "tm now starts new installs with protection \"open\" (agents with their own permission prompts, no thread sandbox, the guard's floor only). This install keeps what it ran with: protection \"" + level + "\". Settings (,) then 3 shows the levels; tm access set protection open --all opens them up."
}

// pinProtection pins an existing install's protection, once per tm
// home, before any agent runs.
func pinProtection(logf func(string, ...any)) {
	d, err := home.Dir()
	if err != nil {
		return
	}
	mark := filepath.Join(d, "state", "protection-pinned")
	if _, err := os.Stat(mark); err == nil {
		return
	}
	existing := false
	if path, err := config.Path(); err == nil {
		if _, err := os.Stat(path); err == nil {
			existing = true
		}
	}
	if list, err := project.List(); err == nil && len(list) > 0 {
		existing = true
	}
	pinned, err := config.MigrateProtection(existing)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Left for a later start: the file is read as it is, and a level
		// it doesn't set is the open default.
		logf("protection: %v; config.toml is pinned at a later start", err)
		noteAccess(func(string) (string, bool) {
			return "tm couldn't pin this install's protection in config.toml (" + err.Error() + "): until it does, threads start at the open default, without their sandbox. Fix the file, then restart the server (tm server restart).", true
		}, true)
		return
	}
	if pinned != "" {
		logf("protection pinned at %s", pinned)
		if list, err := project.List(); err == nil {
			for _, sum := range list {
				if p, err := project.Open(sum.Slug); err == nil {
					p.Journal(caller.Caller{Kind: caller.Ticker}, "settings.migrate", sum.Slug, protectNote(pinned))
				}
			}
		}
	}
	os.MkdirAll(filepath.Dir(mark), 0o700)
	if err := os.WriteFile(mark, []byte("protection pinned\n"), 0o600); err != nil {
		logf("protection: %v", err)
	}
}

// protectFlags are what a thread of agent starts with at protection
// level: without its sandbox below sandboxed, without escapes at strict.
// Codex threads keep their sandbox at every level: tm's profile is how
// Codex reaches tm's socket and keeps out of the project folder, and it
// is how Codex runs on its own too.
func protectFlags(level, agentName string) (noSandbox, noEscapes bool) {
	s := config.Safety{Protection: level}
	return !s.ThreadSandbox() && agentName != "codex", !s.SandboxEscapes()
}

// knownProtection reports whether level is one of protection's; an
// unknown one is refused rather than read as below every level.
func knownProtection(level string) bool { return slices.Contains(config.ProtectLevels, level) }
