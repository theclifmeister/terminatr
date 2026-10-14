package modbroker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// MaxFile is the most fs.read returns and fs.write takes.
const MaxFile = 8 << 20

// Keychains are the credential stores agent.Secrets doesn't list: the
// OS keychains, and the token files of common command-line tools. No mod
// reaches them, whatever its files grant says.
func Keychains(home string, getenv func(string) string) []string {
	var out []string
	if home != "" {
		for _, p := range []string{
			"Library/Keychains", ".local/share/keyrings", ".password-store",
			".config/glab-cli", ".config/hub", ".config/op", ".op", ".vault-token", ".pgpass", ".my.cnf",
			".cargo/credentials", ".cargo/credentials.toml", ".gem/credentials", ".terraform.d/credentials.tfrc.json",
		} {
			out = append(out, filepath.Join(home, filepath.FromSlash(p)))
		}
	}
	out = append(out, "/Library/Keychains", "/System/Library/Keychains")
	for _, v := range []string{"APPDATA", "LOCALAPPDATA"} {
		if d := getenv(v); d != "" && filepath.IsAbs(d) {
			out = append(out, filepath.Join(d, "Microsoft", "Credentials"), filepath.Join(d, "Microsoft", "Protect"))
		}
	}
	return out
}

// Real is paths cleaned, each followed by its symlinks resolved where
// that differs, without repeats or empties.
func Real(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		for _, q := range []string{p, fsx.Canonical(p)} {
			if !seen[q] {
				seen[q] = true
				out = append(out, q)
			}
		}
	}
	return out
}

// Paths are the files one mod may reach: its own folder always, and the
// roots of its files grant (Read; Write, which may also be read). Never,
// whatever the roots: a credential store (Secrets), a .env file, a .git
// folder, or ~/.terminatr outside the mod's own folder.
type Paths struct {
	Home, TMHome, Own string
	Read, Write       []string
	Secrets           []string
	// resolved: the roots, ~ expanded, each as written and with its
	// symlinks resolved.
	own, tm     []string
	read, write []string
}

// resolve expands and resolves the roots.
func (p Paths) resolve() Paths {
	exp := func(roots []string) []string {
		var out []string
		for _, r := range roots {
			if r == "~" || strings.HasPrefix(r, "~/") {
				if p.Home == "" {
					continue
				}
				r = filepath.Join(p.Home, filepath.FromSlash(strings.TrimPrefix(r, "~")))
			}
			if !filepath.IsAbs(r) {
				continue
			}
			out = append(out, r)
		}
		return Real(out)
	}
	p.own = Real([]string{p.Own})
	p.tm = Real([]string{p.TMHome})
	p.write = exp(p.Write)
	p.read = append(exp(p.Read), p.write...)
	return p
}

// Check returns the file path names, symlinks resolved, when the mod may
// read it (write false) or write it, or the refusal. A relative path is
// in the mod's own folder; ~/ is the home folder.
//
// The check and the open that follows are two steps: a link swapped in
// between could move the file. A mod can't make links (no API does), so
// only another process could, and that process could read the file
// itself.
func (p Paths) Check(path string, write bool) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) || !utf8.ValidString(path) {
		return "", BadParams("%q isn't a path", path)
	}
	abs := path
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		if p.Home == "" {
			return "", Denied("%s: no home folder", path)
		}
		abs = filepath.Join(p.Home, filepath.FromSlash(strings.TrimPrefix(path, "~")))
	case caps.Windows && rootless(path):
		// \Windows\win.ini is on the own folder's drive, as /etc/passwd
		// is the root's on Unix: outside, not a file of the own folder
		// (T301 B10).
		abs = filepath.Join(filepath.VolumeName(p.Own)+`\`, path)
	case !filepath.IsAbs(path):
		abs = filepath.Join(p.Own, path)
	}
	abs = filepath.Clean(abs)
	if caps.Windows {
		if why := windowsName(abs); why != "" {
			return "", Denied("%s: %s", path, why)
		}
	}
	roots := p.read
	kind := "read"
	if write {
		roots, kind = p.write, "write"
	}
	roots = append(append([]string{}, p.own...), roots...)
	outside := Denied("%s is outside the mod's own folder and the roots its files grant may %s", path, kind)
	// By name first: nothing outside the roots is looked at on disk (an
	// automount such as /net would block the look).
	if why := p.denied(abs); why != "" {
		return "", Denied("%s %s", path, why)
	}
	if !p.under(abs, roots) {
		return "", outside
	}
	real, err := realPath(abs)
	if err != nil {
		return "", Denied("%s: %v", path, err)
	}
	if why := p.denied(real); why != "" {
		return "", Denied("%s %s", path, why)
	}
	if !p.under(real, roots) {
		return "", outside
	}
	return real, nil
}

// CheckIn checks rel, a path relative to root (a thread's worktree,
// which tm reads for the mod: thread.changes), with slashes: it must
// stay inside root, symlinks resolved, and name no .git folder, .env
// file or credential store, nor tm's own folder unless root is in it.
// It returns rel cleaned.
func (p Paths) CheckIn(root, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) || !utf8.ValidString(rel) || strings.Contains(rel, `\`) {
		return "", BadParams("%q isn't a path", rel)
	}
	clean := path.Clean(rel)
	if path.IsAbs(clean) || filepath.IsAbs(filepath.FromSlash(clean)) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.VolumeName(filepath.FromSlash(clean)) != "" {
		return "", Denied("%s isn't inside the worktree", rel)
	}
	for _, c := range strings.Split(clean, "/") {
		switch {
		case fold(c) == ".git":
			return "", Denied("%s is in a .git folder", rel)
		case isEnvFile(c):
			return "", Denied("%s is a .env file, which holds secrets", rel)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", NotFound("the worktree: %v", err)
	}
	real, err := realPath(filepath.Join(realRoot, filepath.FromSlash(clean)))
	if err != nil {
		return "", Denied("%s: %v", rel, err)
	}
	if !fsx.Under(real, realRoot) {
		return "", Denied("%s isn't inside the worktree", rel)
	}
	if why := p.denied(real); why != "" && why != "is tm's own folder" {
		return "", Denied("%s %s", rel, why)
	}
	for _, t := range p.tm {
		if fsx.Under(real, t) && !fsx.Under(realRoot, t) {
			return "", Denied("%s is tm's own folder", rel)
		}
	}
	return clean, nil
}

// rootless says whether a Windows path is rooted but names no drive
// (\x or /x), which filepath.IsAbs calls relative.
func rootless(path string) bool {
	return (strings.HasPrefix(path, `\`) || strings.HasPrefix(path, "/")) &&
		!strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, "//")
}

func (p Paths) under(x string, roots []string) bool {
	for _, r := range roots {
		if fsx.Under(x, r) {
			return true
		}
	}
	return false
}

// denied says why no mod may reach x, or "".
func (p Paths) denied(x string) string {
	for _, c := range strings.Split(x, string(filepath.Separator)) {
		switch {
		case fold(c) == ".git":
			return "is in a .git folder"
		case isEnvFile(c):
			return "is a .env file, which holds secrets"
		}
	}
	for _, s := range p.Secrets {
		if fsx.Under(x, s) {
			return "is a credential store"
		}
	}
	for _, t := range p.tm {
		if !fsx.Under(x, t) {
			continue
		}
		for _, o := range p.own {
			if fsx.Under(x, o) {
				return ""
			}
		}
		return "is tm's own folder"
	}
	return ""
}

// isEnvFile: .env, .env.local, .envrc and the like.
func isEnvFile(name string) bool {
	n := fold(name)
	return n == ".env" || n == ".envrc" || strings.HasPrefix(n, ".env.")
}

func fold(s string) string {
	if caps.CaseFold {
		return strings.ToLower(s)
	}
	return s
}

var shortName = regexp.MustCompile(`~[0-9]`)

// windowsName says why a Windows path is refused: a network share, an
// alternate data stream, a name Windows trims (a trailing dot or space)
// or an 8.3 short name, each of which could name a file other than the
// one the checks see.
func windowsName(abs string) string {
	vol := filepath.VolumeName(abs)
	if strings.HasPrefix(vol, `\\`) {
		return "network and device paths aren't reached"
	}
	for _, c := range strings.Split(abs[len(vol):], `\`) {
		switch {
		case strings.Contains(c, ":"):
			return "names a data stream"
		case c != "" && c != "." && c != ".." && (strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ")):
			return "has a name ending in a dot or space"
		case shortName.MatchString(c):
			return "has a short (8.3) name; use the long one"
		}
	}
	return ""
}

// realPath is abs with its symlinks resolved; for a path that doesn't
// exist yet, its nearest existing folder's, the rest as written. A link
// whose target is missing is refused: writing through it would create
// the target, wherever it points.
func realPath(abs string) (string, error) {
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r, nil
	}
	dir, rest := abs, []string{}
	for {
		if _, err := os.Lstat(dir); err == nil {
			r, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return "", errors.New("goes through a link whose target is missing")
			}
			return filepath.Join(append([]string{r}, rest...)...), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
	}
}

// fs.read {path} → the file's text.
func fsRead(_ context.Context, c *Call) (any, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	real, err := c.Paths.Check(a.Path, false)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(real)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, NotFound("%s: no such file", a.Path)
	case err != nil:
		return nil, err
	case !fi.Mode().IsRegular():
		return nil, BadParams("%s isn't a file", a.Path)
	case fi.Size() > MaxFile:
		return nil, Denied("%s is over %d bytes", a.Path, MaxFile)
	}
	b, err := os.ReadFile(real)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// fs.write {path, text, append} writes (or appends to) a file. Folders
// are made only inside the mod's own folder.
func fsWrite(_ context.Context, c *Call) (any, error) {
	var a struct {
		Path   string `json:"path"`
		Text   string `json:"text"`
		Append bool   `json:"append"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if len(a.Text) > MaxFile {
		return nil, Denied("%s: over %d bytes", a.Path, MaxFile)
	}
	real, err := c.Paths.Check(a.Path, true)
	if err != nil {
		return nil, err
	}
	own := false
	for _, o := range c.Paths.own {
		own = own || fsx.Under(real, o)
	}
	if own {
		if err := os.MkdirAll(filepath.Dir(real), 0o700); err != nil {
			return nil, err
		}
	}
	if fi, err := os.Stat(real); err == nil && !fi.Mode().IsRegular() {
		return nil, BadParams("%s isn't a file", a.Path)
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if a.Append {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(real, flag, 0o600)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, NotFound("%s: its folder doesn't exist", a.Path)
	}
	if err != nil {
		return nil, err
	}
	_, werr := f.WriteString(a.Text)
	if err := f.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		return nil, werr
	}
	if !own {
		c.Audit("fs.write %s (%d bytes)", real, len(a.Text))
	}
	return nil, nil
}
