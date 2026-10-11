package mod

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/update"
)

// Where a mod comes from (mods design §8, Install): a folder, or a git
// URL at a tag. There is no catalogue in v1, and tm never installs a mod
// from a project's repo on its own.

// SourceFile records, in an installed mod's folder, where it came from.
// The mod can't write its own folder (the broker gives it only its data
// dir), so it can't point its updates elsewhere.
const SourceFile = ".tm-source.json"

// Source is where a mod was installed from: Path, a folder, or Git, a
// URL at Ref (a tag or branch; "" the default branch).
type Source struct {
	Path string `json:"path,omitempty"`
	Git  string `json:"git,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Commit is the git commit installed.
	Commit string `json:"commit,omitempty"`
	// Hash is the sha256 of the installed files (TreeHash).
	Hash      string    `json:"hash,omitempty"`
	Installed time.Time `json:"installed,omitzero"`
}

// String is the source as tm mod install takes it.
func (s Source) String() string {
	switch {
	case s.Path != "":
		return s.Path
	case s.Ref != "":
		return s.Git + "@" + s.Ref
	}
	return s.Git
}

var (
	// scpRE is git's user@host:path form.
	scpRE = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:`)
	refRE = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/+-]*$`)
)

// ParseSource reads tm mod install's argument: a git URL (https://,
// ssh://, file:// or user@host:path) with an optional @tag, or else a
// folder, relative to cwd. Plain http:// and git:// are refused: nothing
// would vouch for what they fetch.
func ParseSource(arg, cwd string) (Source, error) {
	switch {
	case arg == "" || strings.HasPrefix(arg, "-"):
		return Source{}, fmt.Errorf("%q is neither a folder nor a git URL", arg)
	case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "git://"):
		return Source{}, fmt.Errorf("%s: use https:// or ssh://, which check whom they talk to", arg)
	case strings.HasPrefix(arg, "https://"), strings.HasPrefix(arg, "ssh://"), strings.HasPrefix(arg, "file://"), scpRE.MatchString(arg):
		url, ref := arg, ""
		if i := strings.LastIndex(arg, "@"); i > strings.LastIndexAny(arg, "/:") {
			url, ref = arg[:i], arg[i+1:]
			if !refRE.MatchString(ref) {
				return Source{}, fmt.Errorf("%q isn't a tag or branch name", ref)
			}
		}
		return Source{Git: url, Ref: ref}, nil
	}
	p := arg
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return Source{}, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return Source{}, fmt.Errorf("%s: no such folder, and not a git URL (https://, ssh://, file:// or user@host:path)", arg)
	}
	if !fi.IsDir() {
		return Source{}, fmt.Errorf("%s isn't a folder", arg)
	}
	return Source{Path: p}, nil
}

// ReadSource reads where the mod in dir came from; a mod put there by
// hand, or by tm mod dev, has no record.
func ReadSource(dir string) (Source, bool) {
	data, err := os.ReadFile(filepath.Join(dir, SourceFile))
	if err != nil {
		return Source{}, false
	}
	var s Source
	if json.Unmarshal(data, &s) != nil || s.Path == "" && s.Git == "" {
		return Source{}, false
	}
	return s, true
}

func writeSource(dir string, s Source) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SourceFile), append(data, '\n'), 0o644)
}

// Limits on what tm copies in as a mod.
const (
	maxFiles = 2000
	maxBytes = 64 << 20
)

// walkMod calls fn for each file, folder and link in the mod folder root,
// by its slash path relative to root, sorted, skipping .git folders and
// the source record. It refuses what tm wouldn't copy in: a link that
// is absolute or leads out of root, anything but files, folders and
// links, and more than maxFiles files or maxBytes bytes.
func walkMod(root string, fn func(rel string, d fs.DirEntry, abs string) error) error {
	files, bytes := 0, int64(0)
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && d.Name() == ".git":
			return filepath.SkipDir
		case rel == SourceFile:
			return nil
		}
		t := d.Type()
		switch {
		case t&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				return fmt.Errorf("%s links to %s: a mod's links must stay inside it", rel, target)
			}
			if r := filepath.Clean(filepath.Join(filepath.Dir(rel), target)); r == ".." || strings.HasPrefix(filepath.ToSlash(r), "../") {
				return fmt.Errorf("%s links to %s: a mod's links must stay inside it", rel, target)
			}
		case d.IsDir():
		case t.IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			files++
			bytes += fi.Size()
			if files > maxFiles || bytes > maxBytes {
				return fmt.Errorf("is more than %d files or %d MB: too big for a mod", maxFiles, maxBytes>>20)
			}
		default:
			return fmt.Errorf("%s isn't a file, a folder or a link", rel)
		}
		return fn(rel, d, p)
	})
}

// copyMod copies the mod folder src to dst, which doesn't exist yet:
// files (executable or not), folders and links, as walkMod lists them.
func copyMod(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return walkMod(src, func(rel string, d fs.DirEntry, abs string) error {
		to := filepath.Join(dst, filepath.FromSlash(rel))
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(abs)
			if err != nil {
				return err
			}
			return os.Symlink(target, to)
		case d.IsDir():
			return os.Mkdir(to, 0o755)
		}
		return copyFile(abs, to)
	})
}

func copyFile(from, to string) error {
	fi, err := os.Stat(from)
	if err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if fi.Mode()&0o111 != 0 {
		perm = 0o755
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// TreeHash is the sha256 of the mod folder dir as tm would copy it in:
// each path, its kind, and a file's executable bit and content or a
// link's target. A source folder and its installed copy hash the same.
func TreeHash(dir string) (string, error) {
	h := sha256.New()
	err := walkMod(dir, func(rel string, d fs.DirEntry, abs string) error {
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(abs)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "l %s %s\n", rel, filepath.ToSlash(target))
		case d.IsDir():
			fmt.Fprintf(h, "d %s\n", rel)
		default:
			fi, err := os.Stat(abs)
			if err != nil {
				return err
			}
			kind := "f"
			if fi.Mode()&0o111 != 0 {
				kind = "x"
			}
			f, err := os.Open(abs)
			if err != nil {
				return err
			}
			fh := sha256.New()
			_, err = io.Copy(fh, f)
			f.Close()
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s %s %x\n", kind, rel, fh.Sum(nil))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// gitTimeout bounds each git call.
const gitTimeout = 2 * time.Minute

// git runs git with args, never asking for a password and refusing
// transports that run commands, and returns its output.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "protocol.ext.allow=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// clone fetches url at ref ("" the default branch) into dir, shallow,
// and returns the commit.
func clone(url, ref, dir string) (string, error) {
	args := []string{"clone", "--quiet", "--depth", "1", "--no-tags"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	if _, err := git("", append(args, "--", url, dir)...); err != nil {
		return "", err
	}
	return git(dir, "rev-parse", "HEAD")
}

// remoteHead is the commit ref ("" the default branch) names at url.
func remoteHead(url, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	out, err := git("", "ls-remote", "--", url, ref)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(out, "\n") {
		commit, name, ok := strings.Cut(l, "\t")
		if ok && (name == ref || name == "refs/heads/"+ref || name == "refs/tags/"+ref) {
			return commit, nil
		}
	}
	return "", fmt.Errorf("%s has no %s", url, ref)
}

// isVersionTag reports whether ref reads as a release tag (v1.2.3 or
// 1.2.3), which an update moves on from to the newest.
func isVersionTag(ref string) bool {
	_, ok := update.Newer(ref, ref)
	return ok
}

// newestTag is the newest version tag at url newer than ref, or "".
func newestTag(url, ref string) (string, error) {
	out, err := git("", "ls-remote", "--tags", "--refs", "--", url)
	if err != nil {
		return "", err
	}
	best := ref
	for _, l := range strings.Split(out, "\n") {
		_, name, ok := strings.Cut(l, "\t")
		tag, isTag := strings.CutPrefix(name, "refs/tags/")
		if !ok || !isTag || !refRE.MatchString(tag) {
			continue
		}
		if newer, ok := update.Newer(tag, best); ok && newer {
			best = tag
		}
	}
	if best == ref {
		return "", nil
	}
	return best, nil
}

// Latest says whether the source s of an installed mod has moved on:
// a folder whose files differ from the copy, a newer version tag than
// the one pinned, or a branch at another commit. next names what an
// update would install: the folder's manifest version, the tag, or the
// commit.
func Latest(s Source) (next string, behind bool, err error) {
	switch {
	case s.Path != "":
		h, err := TreeHash(s.Path)
		if err != nil {
			return "", false, err
		}
		if h == s.Hash {
			return "", false, nil
		}
		if m, err := ReadManifest(s.Path); m != nil && err == nil {
			return m.Version, true, nil
		}
		return "changed", true, nil
	case s.Git == "":
		return "", false, errors.New("no source recorded")
	case isVersionTag(s.Ref):
		tag, err := newestTag(s.Git, s.Ref)
		return tag, tag != "", err
	}
	commit, err := remoteHead(s.Git, s.Ref)
	if err != nil || commit == s.Commit {
		return "", false, err
	}
	return shortCommit(commit), true, nil
}

// Next is the source an update of a mod installed from s fetches: a
// version tag moves on to the newest one; a folder, branch or other tag
// is fetched again as it is.
func Next(s Source) (Source, error) {
	next := Source{Path: s.Path, Git: s.Git, Ref: s.Ref}
	if s.Git != "" && isVersionTag(s.Ref) {
		tag, err := newestTag(s.Git, s.Ref)
		if err != nil {
			return Source{}, err
		}
		if tag != "" {
			next.Ref = tag
		}
	}
	return next, nil
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
