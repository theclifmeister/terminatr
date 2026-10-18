package mod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/update"
)

// Where a mod comes from (mods design §8, Install): a folder, or a git
// URL at a tag, the mod at the repository's root or in a subfolder of
// it. There is no catalogue in v1, and tm never installs a mod from a
// project's repo on its own.

// SourceFile records, in an installed mod's folder, where it came from.
// The mod can't write its own folder (the broker gives it only its data
// dir), so it can't point its updates elsewhere.
const SourceFile = ".tm-source.json"

// Source is where a mod was installed from: Path, a folder, or Git, a
// URL at Ref (a tag or branch; "" the default branch), with the mod in
// its folder Subdir ("" the root).
type Source struct {
	Path string `json:"path,omitempty"`
	Git  string `json:"git,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Subdir is the mod's folder in the repository, a slash path that
	// CheckSubdir accepts.
	Subdir string `json:"subdir,omitempty"`
	// Commit is the git commit installed.
	Commit string `json:"commit,omitempty"`
	// Tree is the git tree of Subdir at Commit, so an update check can
	// tell whether the mod's folder changed, not just the repository.
	Tree string `json:"tree,omitempty"`
	// Hash is the sha256 of the installed files (TreeHash).
	Hash string `json:"hash,omitempty"`
	// Installed is when the mod was first installed; Updated when
	// tm mod update last replaced its files (zero if never).
	Installed time.Time `json:"installed,omitzero"`
	Updated   time.Time `json:"updated,omitzero"`
}

// Words says where an installed mod came from, for the Mods tab and tm
// mod show: its source, the commit, and when it was installed and last
// updated.
func (s Source) Words() string {
	out := "Installed from " + s.String()
	if len(s.Commit) >= 7 {
		out += " at commit " + s.Commit[:7]
	}
	out += "."
	if !s.Installed.IsZero() {
		out += " Installed " + s.Installed.Local().Format("2006-01-02 15:04") + "."
	}
	if !s.Updated.IsZero() {
		out += " Updated " + s.Updated.Local().Format("2006-01-02 15:04") + "."
	}
	return out
}

// String is the source as tm mod install takes it: a GitHub subfolder
// at a ref as its tree URL, any other as <git-url>[//<subdir>][@<ref>].
func (s Source) String() string {
	git := RedactURL(s.Git)
	switch {
	case s.Path != "":
		return s.Path
	case s.Subdir != "" && s.Ref != "" && githubRepoRE.MatchString(s.Git) && !strings.Contains(s.Ref, "/"):
		return git + "/tree/" + s.Ref + "/" + s.Subdir
	}
	out := git
	if s.Subdir != "" {
		out += "//" + s.Subdir
	}
	if s.Ref != "" {
		out += "@" + s.Ref
	}
	return out
}

var (
	// scpRE is git's user@host:path form.
	scpRE = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:`)
	refRE = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/+-]*$`)
	// githubRepoRE is a GitHub repository's https URL; githubTreeRE one
	// of its folders at a ref, as GitHub links it.
	githubRepoRE = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	githubTreeRE = regexp.MustCompile(`^(https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+)/(tree|blob)/([^/]+)(?:/(.*))?$`)
	// segRE is one name in a subfolder's path.
	segRE = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

// ErrNotPublic refuses a git URL that is not a public https one: SSH
// (ssh:// or user@host:path), or a user, password or token in the URL.
// Mods install over https from public URLs only; a record of a URL with
// a secret would keep it where the Mods tab and agents can read it.
var ErrNotPublic = errors.New("mods install from public URLs only: use an https:// URL with no user, password or token in it (SSH and private repositories can't be installed)")

// urlUserinfo splits the userinfo out of a URL with a scheme://
// authority (https://, ssh://), and says whether it had any. Extra
// slashes or backslashes after :// are skipped as curl skips them, so
// https:///user:TOKEN@host/x has the userinfo user:TOKEN. The scp form
// user@host:path has none in this sense: it can't carry a password.
func urlUserinfo(u string) (before, userinfo, after string, ok bool) {
	i := strings.Index(u, "://")
	if i < 0 || strings.HasPrefix(u, "file://") {
		return "", "", "", false
	}
	rest := strings.TrimLeft(u[i+3:], `/\`)
	end := strings.IndexAny(rest, `/\?#`)
	if end < 0 {
		end = len(rest)
	}
	at := strings.LastIndex(rest[:end], "@")
	if at < 0 {
		return "", "", "", false
	}
	return u[:i+3], rest[:at], rest[at+1:], true
}

// HasCredentials says whether git URL u carries userinfo.
func HasCredentials(u string) bool {
	_, _, _, ok := urlUserinfo(u)
	return ok
}

// IsSSH says whether git URL u goes over SSH: ssh:// or the scp form.
func IsSSH(u string) bool { return strings.HasPrefix(u, "ssh://") || scpRE.MatchString(u) }

// NotPublic says whether git URL u is anything but a plain public
// https:// URL (or file://): SSH, userinfo, and the oddities that make
// git or curl read a URL differently than it looks: an empty or
// slash-led host, a backslash, whitespace or a control character.
func NotPublic(u string) bool {
	switch {
	case HasCredentials(u) || IsSSH(u):
		return true
	case strings.HasPrefix(u, "file://"):
		return false
	case !strings.HasPrefix(u, "https://"):
		return true
	}
	host := u[len("https://"):]
	if host == "" || host[0] == '/' || host[0] == '?' || host[0] == '#' {
		return true
	}
	if strings.ContainsAny(u, "?#") {
		// A query or fragment is where tokens go (?token=x).
		return true
	}
	for _, r := range u {
		if r == '\\' || r <= ' ' || r == 0x7f || unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// RedactURL is u with any userinfo replaced by ***, for showing.
func RedactURL(u string) string {
	if before, _, after, ok := urlUserinfo(u); ok {
		return before + "***@" + after
	}
	return u
}

// CheckSubdir checks p, a mod's folder in a git repository, as a slash
// path that stays inside the repository and names no git internals: no
// leading /, no empty, . or .. names, no .git, no name starting with -,
// and only letters, digits and . _ + - in names (no \, : or %).
func CheckSubdir(p string) error {
	switch {
	case p == "":
		return errors.New("no folder named after //")
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "":
		return fmt.Errorf("%s: the folder must be relative to the repository's root, not absolute", p)
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "" || seg == ".":
			return fmt.Errorf("%s: empty or . names aren't allowed in the folder's path", p)
		case seg == "..":
			return fmt.Errorf("%s: .. isn't allowed: the folder must be inside the repository", p)
		case strings.EqualFold(seg, ".git"):
			return fmt.Errorf("%s: a mod can't come from a .git folder", p)
		case strings.HasPrefix(seg, "-") || !segRE.MatchString(seg):
			return fmt.Errorf("%s: a folder name may hold only letters, digits and . _ + -, and not start with -", p)
		}
	}
	return nil
}

// parseGit reads a git source: a GitHub tree URL
// (https://github.com/<owner>/<repo>/tree/<ref>/<path>), or a git URL
// with an optional //<path> (the mod's folder in the repository) and
// an optional @<ref> after it.
func parseGit(arg string) (Source, error) {
	if NotPublic(arg) && !strings.HasPrefix(arg, "file://") {
		return Source{}, ErrNotPublic
	}
	if m := githubTreeRE.FindStringSubmatch(arg); m != nil {
		if m[2] == "blob" {
			return Source{}, fmt.Errorf("%s is a file: give its folder's /tree/ URL", arg)
		}
		ref, sub := m[3], strings.TrimRight(m[4], "/")
		if !refRE.MatchString(ref) {
			return Source{}, fmt.Errorf("%q isn't a tag or branch name", ref)
		}
		if strings.Contains(sub, "@") {
			return Source{}, fmt.Errorf("%s: a tree URL names its ref after /tree/; drop the @", arg)
		}
		if sub != "" {
			if err := CheckSubdir(sub); err != nil {
				return Source{}, err
			}
		}
		return Source{Git: m[1], Ref: ref, Subdir: sub}, nil
	}
	url, ref := arg, ""
	if i := strings.LastIndex(arg, "@"); i > strings.LastIndexAny(arg, "/:") {
		url, ref = arg[:i], arg[i+1:]
		if !refRE.MatchString(ref) {
			return Source{}, fmt.Errorf("%q isn't a tag or branch name", ref)
		}
	}
	// The repository's path starts after the scheme's :// (or scp's :);
	// a // after that starts the subfolder.
	start := strings.Index(url, "://") + 3
	if start < 3 {
		start = strings.Index(url, ":") + 1
	}
	i := strings.Index(url[start:], "//")
	if i < 0 {
		return Source{Git: url, Ref: ref}, nil
	}
	if strings.HasPrefix(url, "file://") && i == 0 {
		// file:///abs: the third slash is the path's.
		if j := strings.Index(url[start+1:], "//"); j >= 0 {
			i = j + 1
		} else {
			return Source{Git: url, Ref: ref}, nil
		}
	}
	repo, sub := url[:start+i], strings.TrimRight(url[start+i+2:], "/")
	if strings.TrimLeft(repo[start:], "/") == "" {
		return Source{}, fmt.Errorf("%s: no repository before //", arg)
	}
	if err := CheckSubdir(sub); err != nil {
		return Source{}, err
	}
	return Source{Git: repo, Ref: ref, Subdir: sub}, nil
}

// ParseSource reads tm mod install's argument: a git URL (https://,
// ssh://, file:// or user@host:path) with an optional //<subfolder> and
// @tag, or a GitHub tree URL (parseGit), or else a folder, relative to
// cwd. Plain http:// and git:// are refused: nothing would vouch for
// what they fetch.
func ParseSource(arg, cwd string) (Source, error) {
	switch {
	case arg == "" || strings.HasPrefix(arg, "-"):
		return Source{}, fmt.Errorf("%q is neither a folder nor a git URL", arg)
	case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "git://"):
		return Source{}, fmt.Errorf("%s: use https://, which checks whom it talks to", arg)
	case strings.HasPrefix(arg, "https://"), strings.HasPrefix(arg, "ssh://"), strings.HasPrefix(arg, "file://"), scpRE.MatchString(arg):
		return parseGit(arg)
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
		return Source{}, fmt.Errorf("%s: no such folder, and not a git URL (https:// or file://)", arg)
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
	if red := RedactURL(s.Git); red != s.Git {
		// A secret from before tm refused them: don't leave it on disk.
		s.Git = red
		_ = writeSourceAtomic(dir, s)
	}
	return s, true
}

// writeSourceAtomic replaces the record through a temp file and a
// rename, so a secret isn't overwritten in place (and a crash leaves
// the old or the new record whole).
func writeSourceAtomic(dir string, s Source) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tm-source-")
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, SourceFile))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
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
			if !linkInside(root, rel, target) {
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

// maxHops bounds how many links linkInside follows for one link.
const maxHops = 40

// linkInside reports whether the link at rel in root, to target, stays
// inside root as the OS resolves it: name by name, following each link
// on the way, so that d -> .. and e -> d/../x (inside, read as text)
// count as leading out. An absolute target, a loop and a name missing
// before a .. are refused; a name missing at the end is not followed,
// so it can't lead out.
func linkInside(root, rel, target string) bool {
	var dir []string
	if d := path.Dir(rel); d != "." {
		dir = strings.Split(d, "/")
	}
	split := func(t string) ([]string, bool) {
		if filepath.IsAbs(t) || filepath.VolumeName(t) != "" || strings.HasPrefix(filepath.ToSlash(t), "/") {
			return nil, false
		}
		return strings.Split(filepath.ToSlash(t), "/"), true
	}
	todo, ok := split(target)
	for hops := 0; ok && len(todo) > 0; {
		c := todo[0]
		todo = todo[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			if len(dir) == 0 {
				return false
			}
			dir = dir[:len(dir)-1]
			continue
		}
		fi, err := os.Lstat(filepath.Join(root, filepath.Join(dir...), c))
		switch {
		case err != nil:
			if slices.Contains(todo, "..") {
				return false
			}
		case fi.Mode()&fs.ModeSymlink != 0:
			if hops++; hops > maxHops {
				return false
			}
			t, err := os.Readlink(filepath.Join(root, filepath.Join(dir...), c))
			if err != nil {
				return false
			}
			var more []string
			if more, ok = split(t); ok {
				todo = append(more, todo...)
			}
			continue
		}
		dir = append(dir, c)
	}
	return ok
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
// transports that run commands (gitx), and returns its output.
func git(dir string, args ...string) (string, error) {
	// No credential helper (an empty value resets the list, the
	// system's and the user's included), no prompt: a private repository
	// fails instead of using the user's stored logins.
	//
	// Nor does it read the user's or the system's git config (an
	// insteadOf could turn a public URL into a credentialed or SSH one,
	// http.extraHeader and the cookie and client certificate settings
	// would send secrets), nor ~/.netrc, which curl reads from HOME:
	// HOME is an empty folder for the run.
	home, err := os.MkdirTemp("", "tm-mod-git-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(home)
	return gitx.Run(context.Background(), dir, gitx.Opts{Timeout: gitTimeout, Env: gitEnv(home), Unset: gitUnset}, args...)
}

// gitEnv is the environment that keeps git from using the user's
// credentials and settings, with home an empty folder.
func gitEnv(home string) []string {
	env := []string{
		"HOME=" + home, "USERPROFILE=" + home, "XDG_CONFIG_HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
		"NETRC=" + os.DevNull,
	}
	// Only the helper is reset: the config files are shut out above,
	// and an empty http.sslCert or http.proxyAuthMethod breaks https.
	env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_ALLOW_PROTOCOL=https:file", "GIT_ASKPASS=", "SSH_ASKPASS=")
	return env
}

// gitUnset are the variables of tm's own environment that would change
// what git sends or trusts: settings given on a wrapper's command line,
// SSH, TLS overrides.
var gitUnset = []string{"GIT_CONFIG_PARAMETERS", "GIT_SSH_COMMAND", "GIT_SSH", "SSH_AUTH_SOCK",
	"GIT_SSL_NO_VERIFY", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH", "GIT_SSL_CERT", "GIT_SSL_KEY"}

// clone fetches url at ref ("" the default branch) into dir, shallow,
// and returns the commit. With sub, a folder CheckSubdir accepted, the
// clone is sparse: only sub's files (and the root's) are fetched and
// checked out, and tree is sub's git tree.
func clone(url, ref, sub, dir string) (commit, tree string, err error) {
	args := []string{"clone", "--quiet", "--depth", "1", "--no-tags"}
	if sub != "" {
		args = append(args, "--filter=blob:none", "--sparse")
	}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	if _, err := git("", append(args, "--", url, dir)...); err != nil {
		return "", "", err
	}
	if sub != "" {
		if tree, err = subdirTree(dir, "HEAD", sub); err != nil {
			return "", "", err
		}
		if _, err := git(dir, "sparse-checkout", "set", "--cone", "--", sub); err != nil {
			return "", "", err
		}
	}
	commit, err = git(dir, "rev-parse", "HEAD")
	return commit, tree, err
}

// subdirIn is folder sub (CheckSubdir) of the checkout root, refused
// unless each name on the way is a real folder: a link there could lead
// the copy out of the checkout.
func subdirIn(root, sub string) (string, error) {
	p := root
	for _, seg := range strings.Split(sub, "/") {
		p = filepath.Join(p, seg)
		fi, err := os.Lstat(p)
		switch {
		case err != nil:
			return "", fmt.Errorf("it has no folder %s", sub)
		case fi.Mode()&fs.ModeSymlink != 0:
			return "", fmt.Errorf("%s is a link in the repository, not a folder", sub)
		case !fi.IsDir():
			return "", fmt.Errorf("%s isn't a folder", sub)
		}
	}
	return p, nil
}

// remoteTree is the git tree of folder sub at ref of url, fetched
// shallow and without file contents into a scratch repository.
func remoteTree(url, ref, sub string) (string, error) {
	tmp, err := os.MkdirTemp("", "tm-mod-tree-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if ref == "" {
		ref = "HEAD"
	}
	if _, err := git(tmp, "init", "--quiet"); err != nil {
		return "", err
	}
	if _, err := git(tmp, "fetch", "--quiet", "--depth", "1", "--no-tags", "--filter=blob:none", "--", url, ref); err != nil {
		return "", err
	}
	return subdirTree(tmp, "FETCH_HEAD", sub)
}

// subdirTree is the git tree of folder sub at commit in the repository
// dir, refused when sub is missing or a file or link there.
func subdirTree(dir, commit, sub string) (string, error) {
	out, err := git(dir, "ls-tree", "--full-tree", commit, "--", sub)
	if err != nil {
		return "", err
	}
	// <mode> <type> <id>\t<path>
	meta, path, _ := strings.Cut(out, "\t")
	f := strings.Fields(meta)
	switch {
	case out == "" || path != sub || len(f) != 3:
		return "", fmt.Errorf("it has no folder %s", sub)
	case f[0] == "120000":
		return "", fmt.Errorf("%s is a link in the repository, not a folder", sub)
	case f[1] != "tree":
		return "", fmt.Errorf("%s isn't a folder", sub)
	}
	return f[2], nil
}

// remoteHead is the commit ref ("" the default branch) names at url.
func remoteHead(url, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	out, err := git("", "ls-remote", "--", url, ref, ref+"^{}")
	if err != nil {
		return "", err
	}
	found := ""
	for _, l := range strings.Split(out, "\n") {
		commit, name, ok := strings.Cut(l, "\t")
		if !ok {
			continue
		}
		switch name {
		case "refs/tags/" + ref + "^{}", ref + "^{}":
			// An annotated tag's peeled line is the commit; the line
			// before it is the tag object.
			return commit, nil
		case ref, "refs/heads/" + ref, "refs/tags/" + ref:
			if found == "" {
				found = commit
			}
		}
	}
	if found != "" {
		return found, nil
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
// the one pinned, or a branch at another commit. For a mod in a
// subfolder, a newer tag or commit counts only when the subfolder's
// tree differs from the one installed. next names what an update would
// install: the folder's manifest version, the tag, or the commit.
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
	case s.Git != "" && NotPublic(s.Git):
		return "", false, recordNotPublic(s)
	case isVersionTag(s.Ref):
		tag, err := newestTag(s.Git, s.Ref)
		if err != nil || tag == "" {
			return "", false, err
		}
		if changed, err := subdirChanged(s, tag); err != nil || !changed {
			return "", false, err
		}
		return tag, true, nil
	}
	commit, err := remoteHead(s.Git, s.Ref)
	if err != nil || commit == s.Commit {
		return "", false, err
	}
	if changed, err := subdirChanged(s, commit); err != nil || !changed {
		return "", false, err
	}
	return shortCommit(commit), true, nil
}

// subdirChanged says whether the subfolder of s differs at ref (a tag
// or commit) from the tree installed; a mod at the root always has.
func subdirChanged(s Source, ref string) (bool, error) {
	if s.Subdir == "" || s.Tree == "" {
		return true, nil
	}
	if ref == s.Commit {
		return false, nil
	}
	if isVersionTag(ref) {
		ref = "refs/tags/" + ref
	} else if s.Ref != "" {
		// A commit can't be fetched by its id from every host: fetch the
		// branch it is the head of.
		ref = s.Ref
	} else {
		ref = ""
	}
	tree, err := remoteTree(s.Git, ref, s.Subdir)
	if err != nil {
		return false, err
	}
	return tree != s.Tree, nil
}

// Next is the source an update of a mod installed from s fetches: a
// version tag moves on to the newest one; a folder, branch or other tag
// is fetched again as it is.
func Next(s Source) (Source, error) {
	if s.Git != "" && NotPublic(s.Git) {
		return Source{}, recordNotPublic(s)
	}
	next := Source{Path: s.Path, Git: s.Git, Ref: s.Ref, Subdir: s.Subdir}
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

// recordNotPublic is the refusal of a recorded source tm no longer
// fetches (installed over SSH, or with credentials in the URL).
func recordNotPublic(s Source) error {
	return fmt.Errorf("%s: this source isn't a public https URL, which is all tm fetches from now: remove the mod and install it again from an https:// URL", s)
}
