package config

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Write folders (T574 §4.6, case 2): one rule for a folder besides its
// worktree that a thread writes in. tm gives it to every layer at once:
// the guard's writable list (file tools, under worktree-only), Claude's
// Edit(...) allow rule (its file tools, and its Bash sandbox writes
// there too) and Codex's filesystem write. A write folder is a folder,
// everything below it included: no pattern, never the whole home or
// disk, nor a folder of files that start programs (shell profiles, git
// hooks and config, agents' and tm's settings). The launch also leaves
// out one that holds or is in tm's own folder, config.toml or the
// project folder.

// CheckWriteFolder checks one write folder: absolute or from ~/, clean,
// without wildcards, at least two folders below the home folder (or
// /Users/<name>, /home/<name>) or two below the disk's root, and no part
// of it one of startFiles.
func CheckWriteFolder(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	p := strings.ReplaceAll(e, `\`, "/")
	var parts []string
	switch {
	case p == "~" || strings.HasPrefix(p, "~/"):
		parts = []string{"~"}
		parts = append(parts, strings.Split(strings.TrimPrefix(p, "~/"), "/")...)
		if p == "~" {
			parts = parts[:1]
		}
	case strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//"):
		parts = strings.Split(strings.TrimPrefix(p, "/"), "/")
	case windowsAbs.MatchString(e):
		parts = strings.Split(p[3:], "/")
	default:
		return fmt.Errorf("%q isn't a folder from the root or from ~/: write it whole, e.g. ~/src/fixtures", e)
	}
	switch {
	case strings.ContainsAny(p, "*?[]{}"):
		return fmt.Errorf("%q has a pattern: a write folder is one folder, everything below it included", e)
	case p != "~" && path.Clean(p) != p, slices.Contains(parts, ".."), slices.Contains(parts, "."), slices.Contains(parts, ""):
		return fmt.Errorf("%q isn't written plainly: no .., . or // in it, nor a / at its end", e)
	case strings.Contains(p, "$"):
		return fmt.Errorf("%q has a $: a write folder is a plain path, without variables", e)
	}
	for _, x := range parts {
		if slices.Contains(startFiles, x) {
			return fmt.Errorf("%q is in %s, which holds files that start programs as you outside the sandbox: never a write folder", e, x)
		}
	}
	if high(parts) {
		return fmt.Errorf("%q is a whole tree high up, which holds other repositories and your settings: name the folder itself, e.g. ~/src/fixtures", e)
	}
	return nil
}

// high reports whether a folder's parts (from ~, or from the root) are
// the home folder, one folder below it, or the root and the two levels
// below it (/Users/<name> is a home folder).
func high(parts []string) bool {
	if parts[0] == "~" {
		return len(parts) < 3
	}
	home := slices.Contains([]string{"Users", "home", "users"}, parts[0])
	if home {
		return len(parts) < 4
	}
	return len(parts) < 2
}

// writeWarning is what a write folder opens, said where it is added:
// the thread's file tools and its sandboxed commands write there
// without asking, on Claude and Codex alike.
func writeWarning(string) string {
	return "the thread's file tools and its sandboxed commands write there without asking, on Claude and Codex threads alike"
}
