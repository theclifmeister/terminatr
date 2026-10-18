package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// Write folders (T574 §4.6, case 2): one rule for a folder besides its
// worktree that a thread writes in. tm gives it to every layer at once:
// the guard's writable list (file tools, under worktree-only), Claude's
// Edit(...) allow rule (its file tools, and its Bash sandbox writes
// there too) and Codex's filesystem write. A write folder is a folder,
// everything below it included: no pattern, never the whole home or
// disk, nor a folder of files that start programs (shell profiles, git
// hooks and config, agents' and tm's settings), nor one whose files are
// loaded or run as code (a folder on PATH or holding one, ~/Library but
// its caches, package managers' and toolchains' folders). Names compare
// with case folded where the file system ignores case (macOS). The
// launch also leaves out one that holds or is in tm's own folder,
// config.toml or the project folder.

// codeHome are the folders below home whose files are loaded or run as
// code, by their first part: no write folder is in one. ~/Library is
// one but for ~/Library/Caches/<folder> (libraryCaches).
var codeHome = []string{"Library", "bin", "go", ".local", ".cargo", ".rustup", ".docker", ".oh-my-zsh", ".npm-global", ".npm", ".bun", ".deno",
	".nvm", ".volta", ".asdf", ".rbenv", ".pyenv", ".sdkman", ".gem", ".zsh", ".zshrc.d", ".bashrc.d", ".vim", ".emacs.d", ".tmux",
	".vscode", ".cursor", ".gradle", ".ipython", ".jupyter", ".m2", ".config", ".cache", "AppData"}

// codeHomeDeep are folders two parts below home whose files run as code:
// Windows PowerShell's profiles.
var codeHomeDeep = [][2]string{{"Documents", "PowerShell"}, {"Documents", "WindowsPowerShell"}}

// morePath, when set (SetMorePath), adds folders to PathDirs.
var morePath atomic.Pointer[func() []string]

// SetMorePath makes PathDirs add more's folders: the server adds the
// user's login shell's PATH (server/loginpath.go), as its own may be
// launchd's minimal one.
func SetMorePath(more func() []string) { morePath.Store(&more) }

// PathDirs are the folders on PATH a write folder may not hold or be in:
// this process's PATH, and SetMorePath's.
func PathDirs() []string {
	out := filepath.SplitList(os.Getenv("PATH"))
	if more := morePath.Load(); more != nil {
		out = append(out, (*more)()...)
	}
	return out
}

// codeRoot are the folders outside home whose files are loaded or run
// as code: no write folder is in one.
var codeRoot = []string{"/opt/homebrew", "/opt/local", "/usr", "/bin", "/sbin", "/etc", "/private/etc", "/System", "/Library", "/Applications", "/nix"}

// fold is name as the file system compares it: case folded where it
// ignores case.
func fold(name string) string {
	if caps.CaseFold {
		return strings.ToLower(name)
	}
	return name
}

// inFolder reports whether slash path p is root or below it, compared
// as the file system does.
func inFolder(p, root string) bool {
	p, root = fold(p), strings.TrimSuffix(fold(root), "/")
	return p == root || strings.HasPrefix(p, root+"/")
}

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
		if slices.ContainsFunc(startFiles, func(f string) bool { return fold(f) == fold(x) }) {
			return fmt.Errorf("%q is in %s, which holds files that start programs as you outside the sandbox: never a write folder", e, x)
		}
	}
	if high(parts) {
		return fmt.Errorf("%q is a whole tree high up, which holds other repositories and your settings: name the folder itself, e.g. ~/src/fixtures", e)
	}
	return checkCodeFolder(e, p, parts)
}

// checkCodeFolder refuses write folder e (slash path p, its parts from
// ~ or the root) where files are loaded or run as code: in one of
// codeHome or codeRoot, or on PATH, in a folder of it or holding one
// (with ~ and PATH as this process has them).
func checkCodeFolder(e, p string, parts []string) error {
	home, _ := os.UserHomeDir()
	home = filepath.ToSlash(home)
	abs := p
	if parts[0] == "~" {
		abs = home + strings.TrimPrefix(p, "~")
	}
	rel := parts[1:] // below home, when it is
	if parts[0] != "~" {
		rel = nil
		if home != "" && inFolder(abs, home) && fold(abs) != fold(home) {
			rel = strings.Split(strings.TrimPrefix(abs[len(home):], "/"), "/")
		}
	}
	const code = ", whose files are loaded or run as code (outside the sandbox too): never a write folder"
	if len(rel) >= 2 && slices.ContainsFunc(codeHomeDeep, func(c [2]string) bool { return fold(c[0]) == fold(rel[0]) && fold(c[1]) == fold(rel[1]) }) {
		return fmt.Errorf("%q is in ~/%s/%s%s", e, rel[0], rel[1], code)
	}
	if len(rel) > 0 && slices.ContainsFunc(codeHome, func(c string) bool { return fold(c) == fold(rel[0]) }) {
		caches := len(rel) >= 3 && fold(rel[0]) == fold("Library") && fold(rel[1]) == fold("Caches")
		if !caches {
			return fmt.Errorf("%q is in ~/%s%s (~/Library/Caches/<folder> is the one allowed there)", e, rel[0], code)
		}
	}
	if rel == nil {
		for _, c := range codeRoot {
			if inFolder(abs, c) {
				return fmt.Errorf("%q is in %s%s", e, c, code)
			}
		}
	}
	for _, d := range PathDirs() {
		d = filepath.ToSlash(filepath.Clean(d))
		if d == "." || !filepath.IsAbs(filepath.FromSlash(d)) && !strings.HasPrefix(d, "/") {
			continue
		}
		if inFolder(abs, d) || inFolder(d, abs) {
			return fmt.Errorf("%q holds or is in %s, a folder on PATH%s", e, d, code)
		}
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
	home := fold(parts[0]) == fold("Users") || fold(parts[0]) == fold("home") || parts[0] == "users"
	if home {
		return len(parts) < 4
	}
	return len(parts) < 2
}

// writeWarning is what a write folder opens, said where it is added:
// the thread's file tools and its sandboxed commands write there
// without asking, on Claude and Codex alike.
func writeWarning(e string) string {
	w := "the thread's file tools and its sandboxed commands write there without asking, on Claude and Codex threads alike"
	if strings.Contains(fold(filepath.ToSlash(e)), fold("Library/Caches/")) {
		w += "; tools run what is in a cache (Playwright's browsers in ms-playwright, go-build's objects), so a thread can plant code there that runs as you outside the sandbox"
	}
	return w
}
