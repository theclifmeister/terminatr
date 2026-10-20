package config

import (
	"bytes"
	"maps"
	"slices"

	"github.com/BurntSushi/toml"
)

// Protection is how much of tm's security a project's agents run under
// (docs/SPEC.md §5.2, §11.2), one ladder whose every step adds to the
// one before. Below them all, whatever the level, hold tm's own deny
// rules on the project folder and the guard's floor (§8.6): they never
// prompt in normal work.
const (
	// ProtectOpen: the agent as it runs on its own, with its own
	// permission prompts; a Claude thread's Bash sandbox is off and the
	// guard keeps its floor only. The default of a new install.
	ProtectOpen = "open"
	// ProtectGuarded: open, plus the guard's rules (worktree-only,
	// force-push, push-default, delete-branch, merge, credentials, the
	// mods' rules) and allowances.
	ProtectGuarded = "guarded"
	// ProtectSandboxed: guarded, plus a thread's sandbox and its lists;
	// a thread may ask to run a command outside it (an escape).
	ProtectSandboxed = "sandboxed"
	// ProtectStrict: sandboxed, without escapes: what can't run in the
	// sandbox doesn't run (sandbox_escapes = false before).
	ProtectStrict = "strict"
)

// ProtectLevels are protection's values, lowest first: the settings'
// cycle order.
var ProtectLevels = []string{ProtectOpen, ProtectGuarded, ProtectSandboxed, ProtectStrict}

// ProtectPinned is the level an install of tm 0.42 or older ran at,
// which the migration writes into its file (MigrateProtection):
// strict where its sandbox_escapes was off.
func ProtectPinned(escapes bool) string {
	if escapes {
		return ProtectSandboxed
	}
	return ProtectStrict
}

// AtLeast reports whether s's level is level or above it.
func (s Safety) AtLeast(level string) bool {
	return slices.Index(ProtectLevels, s.Protection) >= slices.Index(ProtectLevels, level)
}

// GuardRulesOn reports whether the guard's rules apply (guarded and up,
// with the guard on); below, it keeps its floor only.
func (s Safety) GuardRulesOn() bool { return s.Guard && s.AtLeast(ProtectGuarded) }

// ThreadSandbox reports whether threads run in their sandbox
// (sandboxed and up).
func (s Safety) ThreadSandbox() bool { return s.AtLeast(ProtectSandboxed) }

// SandboxEscapes reports whether a sandboxed thread may ask to run a
// command outside its sandbox (sandboxed, not strict).
func (s Safety) SandboxEscapes() bool { return s.Protection != ProtectStrict }

// MigrateProtection pins an install from before protection (tm 0.42 and
// older) to the level its threads ran at, once, at the server's start:
// a file no table of which sets protection gets [defaults] protection =
// ProtectPinned of its all projects' sandbox_escapes, each project that
// set sandbox_escapes its own, and the older key goes. existing says
// the install had projects or settings before; a new one (no file, no
// projects) stays at the open default and gets nothing written. pinned
// is the level written, "" when nothing was.
func MigrateProtection(existing bool) (pinned string, err error) {
	err = edit(func(data []byte) ([]byte, error) {
		out, level, err := migrateProtection(data, existing)
		pinned = level
		return out, err
	})
	if err != nil {
		pinned = ""
	}
	return pinned, err
}

// migrateProtection is MigrateProtection of the file's data.
func migrateProtection(data []byte, existing bool) ([]byte, string, error) {
	var raw struct {
		Projects map[string]rawSafety `toml:"projects"`
		Defaults rawSafety            `toml:"defaults"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return data, "", err
	}
	if raw.Defaults.Protection != nil || slices.ContainsFunc(slices.Collect(maps.Values(raw.Projects)), func(r rawSafety) bool { return r.Protection != nil }) {
		return data, "", nil
	}
	if !existing && len(bytes.TrimSpace(data)) == 0 {
		return data, "", nil
	}
	escapes := raw.Defaults.SandboxEsc == nil || *raw.Defaults.SandboxEsc
	pinned := ProtectPinned(escapes)
	out, err := Edit(data, "defaults", "protection", pinned)
	if err != nil {
		return data, "", err
	}
	out = Remove(out, "defaults", "sandbox_escapes")
	for _, slug := range slices.Sorted(maps.Keys(raw.Projects)) {
		esc := raw.Projects[slug].SandboxEsc
		if esc == nil {
			continue
		}
		table := "projects." + slug
		if out, err = Edit(out, table, "protection", ProtectPinned(*esc)); err != nil {
			return data, "", err
		}
		out = Remove(out, table, "sandbox_escapes")
	}
	return out, pinned, nil
}

// AccessResetKeys are the Access tab's switches a reset puts back to
// what the table follows (ResetAccess): a project's to all projects',
// all projects' to tm's defaults (protection open).
var AccessResetKeys = []string{"protection", "yolo", "coordinator_approves", "coordinator_access", "guard", "guard_mode", "guard_off"}

// ResetAccess removes table's ("defaults" or "projects.<slug>") own
// values of AccessResetKeys, and the older sandbox_escapes, keeping its
// sandbox lists, guard allowances and secret environment: those took the
// human's work to build, and the keys keep names (a coordinator's reset
// keeps coordinator_access). reset are the keys it removed, in
// AccessResetKeys order; ErrForm when the file sets one in a form the
// line editor doesn't change, and then nothing is removed.
func ResetAccess(table string, keep ...string) (reset []string, err error) {
	err = edit(func(data []byte) ([]byte, error) {
		reset = nil
		for _, k := range append(slices.Clone(AccessResetKeys), "sandbox_escapes") {
			if slices.Contains(keep, k) || !sets(data, table, k) {
				continue
			}
			out := Remove(data, table, k)
			if bytes.Equal(out, data) {
				return nil, ErrForm
			}
			data = out
			if k == "sandbox_escapes" {
				k = "protection"
			}
			if !slices.Contains(reset, k) {
				reset = append(reset, k)
			}
		}
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(reset, func(a, b string) int {
		return slices.Index(AccessResetKeys, a) - slices.Index(AccessResetKeys, b)
	})
	return reset, nil
}
