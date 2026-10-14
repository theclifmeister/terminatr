// Package changes reads what a thread's worktree changed, by git alone
// and read-only: the files changed since its base or since its last
// commit, with their +/- counts and whether each change is uncommitted,
// committed but unpushed, or pushed (docs/SPEC.md §5.3, thread.changes;
// mod API 1.8). tm runs git itself, with fixed arguments, no shell and
// no optional locks, so a read never contends with the agent's own git.
//
// Everything it returns is data from the worktree: paths and patches are
// cleaned of control and format characters (widget.Clean), and paths are
// relative to the worktree.
package changes

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Bounds of one read: the whole read's time, each git output, the files
// listed, a patch, and an untracked file whose lines are counted.
const (
	Timeout      = 30 * time.Second
	MaxOutput    = 1 << 20
	MaxFiles     = 2000
	MaxPatch     = 256 << 10
	maxUntracked = 8 << 20
)

// States of a worktree, modes of a read and buckets of a file.
const (
	StateNone  = "none" // no repo, no worktree, or not a checkout
	StateClean = "clean"
	StateDirty = "dirty"

	ModeBase        = "base"        // against the merge base with the thread's base
	ModeUncommitted = "uncommitted" // against HEAD

	Uncommitted = "uncommitted"
	Unpushed    = "unpushed"
	Pushed      = "pushed"
)

// File is one changed file.
type File struct {
	Path string `json:"path"`
	// Old is a renamed file's old path.
	Old string `json:"old,omitempty"`
	// Status is A (added), M (modified), D (deleted), R (renamed), T
	// (type changed) or ? (untracked).
	Status string `json:"status"`
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Binary bool   `json:"binary,omitempty"`
	// Bucket is the newest layer the file changed in: uncommitted, then
	// unpushed, then pushed.
	Bucket string `json:"bucket"`
}

// Count is a number of files and their added and deleted lines.
type Count struct {
	Files int `json:"files"`
	Add   int `json:"add"`
	Del   int `json:"del"`
}

// Buckets split the totals by each file's bucket.
type Buckets struct {
	Uncommitted Count `json:"uncommitted"`
	Unpushed    Count `json:"unpushed"`
	Pushed      Count `json:"pushed"`
}

// Changeset is what a read answers.
type Changeset struct {
	State string `json:"state"`
	Mode  string `json:"mode"`
	// Branch is the branch checked out ("" when HEAD is detached), Base
	// the ref the changes are counted from ("" when there is none), and
	// Head HEAD's commit ("" before the first commit).
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
	Head   string `json:"head,omitempty"`
	// Ahead is how many commits HEAD has since the base, Unpushed how
	// many of them origin/<branch> lacks, Pushed whether origin/<branch>
	// exists.
	Ahead    int     `json:"ahead"`
	Unpushed int     `json:"unpushed"`
	Pushed   bool    `json:"pushed"`
	Files    []File  `json:"files"`
	Totals   Count   `json:"totals"`
	Buckets  Buckets `json:"buckets"`
	// Truncated: a git output went over MaxOutput or there were more
	// than MaxFiles files; the totals count what was read.
	Truncated bool `json:"truncated,omitempty"`
	// Patch is one file's unified diff, when asked for, up to MaxPatch.
	Patch          string `json:"patch,omitempty"`
	PatchTruncated bool   `json:"patch_truncated,omitempty"`
}

// Options say what to read.
type Options struct {
	// Repo is the thread's repository (its main checkout); "" for a
	// thread without one, which has no changes.
	Repo string
	// Base is the thread's recorded base ("origin/main"); "" or "HEAD"
	// for none, when origin's default branch is used, else the repo's
	// checked-out branch.
	Base string
	// Mode is ModeBase ("" too) or ModeUncommitted.
	Mode string
	// Path, when set, asks for that file's patch: relative to the
	// worktree, with slashes, checked by the caller.
	Path string
}

// ErrMode is a mode that is neither base nor uncommitted.
var ErrMode = errors.New(`mode is "base" or "uncommitted"`)

// emptyTree is git's empty tree (SHA-1), what HEAD is before the first
// commit; a SHA-256 repository's is asked of git.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Read reads worktree dir's changes, within Timeout.
func Read(ctx context.Context, dir string, o Options) (*Changeset, error) {
	switch o.Mode {
	case "":
		o.Mode = ModeBase
	case ModeBase, ModeUncommitted:
	default:
		return nil, ErrMode
	}
	cs := &Changeset{State: StateNone, Mode: o.Mode, Files: []File{}}
	if o.Repo == "" || dir == "" {
		return cs, nil
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return cs, nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	g := &git{ctx: ctx, dir: dir}
	// The worktree must be a checkout's top, not a folder inside some
	// other repository.
	top, ok := g.line("rev-parse", "--show-toplevel")
	if !ok || !sameDir(top, dir) {
		return cs, g.err()
	}
	cs.Head, _ = g.line("rev-parse", "--verify", "-q", "HEAD")
	cs.Branch, _ = g.line("symbolic-ref", "-q", "--short", "HEAD")
	from := cs.Head
	if from == "" {
		from = emptyTree
		if t, ok := g.line("hash-object", "-t", "tree", os.DevNull); ok {
			from = t
		}
	}
	// The base and the merge base the changes since it count from.
	mb := ""
	if cs.Head != "" {
		for _, b := range bases(g, o) {
			if m, ok := g.line("merge-base", b, "HEAD"); ok {
				cs.Base, mb = b, m
				break
			}
		}
	}
	if mb != "" {
		cs.Ahead = g.count(mb + "..HEAD")
	}
	pushedRef := ""
	if cs.Branch != "" {
		if ref, ok := g.line("for-each-ref", "--format=%(objectname)", "refs/remotes/origin/"+cs.Branch); ok && ref != "" {
			pushedRef, cs.Pushed = "refs/remotes/origin/"+cs.Branch, true
		}
	}
	switch {
	case cs.Head == "":
	case pushedRef != "":
		cs.Unpushed = g.count(pushedRef + "..HEAD")
	default:
		cs.Unpushed = cs.Ahead
	}
	if o.Mode == ModeBase && mb != "" {
		from = mb
	}

	status, _ := g.out(MaxOutput, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	dirty, untracked := parseStatus(status)
	if len(dirty) > 0 {
		cs.State = StateDirty
	} else {
		cs.State = StateClean
	}
	if err := g.err(); err != nil {
		return cs, err
	}
	names, _ := g.out(MaxOutput, append(diffArgs("--name-status"), from)...)
	nums, _ := g.out(MaxOutput, append(diffArgs("--numstat"), from)...)
	files := parseDiff(names, nums)
	for _, p := range untracked {
		f := File{Path: p, Status: "?"}
		f.Add, f.Binary = countLines(ctx, filepath.Join(dir, filepath.FromSlash(p)))
		files = append(files, f)
	}
	// Each file's bucket: uncommitted when status lists it, else
	// unpushed when origin/<branch> (none: the base) differs from HEAD
	// in it, else pushed.
	var ahead map[string]bool
	if o.Mode == ModeBase && cs.Head != "" {
		against := cmpOr(pushedRef, mb)
		if against != "" {
			out, _ := g.out(MaxOutput, "diff", "--no-ext-diff", "--no-textconv", "-z", "-M", "--name-only", against, "HEAD")
			ahead = map[string]bool{}
			for _, p := range splitZ(out) {
				ahead[p] = true
			}
		}
	}
	for i := range files {
		f := &files[i]
		switch {
		case o.Mode == ModeUncommitted || dirty[f.Path] || f.Old != "" && dirty[f.Old]:
			f.Bucket = Uncommitted
		case ahead == nil || ahead[f.Path]:
			f.Bucket = Unpushed
		default:
			f.Bucket = Pushed
		}
		cs.Totals.add(f)
		switch f.Bucket {
		case Uncommitted:
			cs.Buckets.Uncommitted.add(f)
		case Unpushed:
			cs.Buckets.Unpushed.add(f)
		default:
			cs.Buckets.Pushed.add(f)
		}
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	if len(files) > MaxFiles {
		files, g.truncated = files[:MaxFiles], true
	}
	for i := range files {
		files[i].Path, files[i].Old = clean(files[i].Path), clean(files[i].Old)
	}
	cs.Files = files

	if o.Path != "" {
		cs.Patch, cs.PatchTruncated = patch(g, from, o.Path, files, untracked)
	}
	cs.Truncated = g.truncated
	return cs, g.err()
}

// Fingerprint is a digest of what worktree dir's changes follow: its
// HEAD, its status and its pushed branch (origin/<branch>), from two git
// runs; "" without a repo or when git can't read it. While it stays the
// same, so does Read's answer (but for the base moving).
func Fingerprint(ctx context.Context, dir, repo string) string {
	if repo == "" || dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	g := &git{ctx: ctx, dir: dir}
	st, _ := g.out(MaxOutput, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if st == "" {
		return ""
	}
	h := sha256.New()
	io.WriteString(h, st)
	// An edit to a file already changed leaves the status as it was:
	// each changed file's size and time tell it.
	recs, stats := splitZ(st), 0
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if b, ok := strings.CutPrefix(rec, "# branch.head "); ok && b != "(detached)" {
			ref, _ := g.line("for-each-ref", "--format=%(objectname)", "refs/remotes/origin/"+b)
			io.WriteString(h, "\x00"+ref)
			continue
		}
		p := statusPath(rec)
		if rec != "" && rec[0] == '2' {
			i++ // a rename's old path follows
		}
		if p == "" || stats >= MaxFiles {
			continue
		}
		stats++
		if fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			fmt.Fprintf(h, "\x00%d %d", fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// statusPath is the path of a status --porcelain=v2 record, after its
// fields: 8 of a changed entry, 9 of a renamed one, 10 of an unmerged
// one, 1 of an untracked one.
func statusPath(rec string) string {
	n := map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1}
	if rec == "" {
		return ""
	}
	k, ok := n[rec[0]]
	if !ok {
		return ""
	}
	f := strings.SplitN(rec, " ", k+1)
	if len(f) != k+1 {
		return ""
	}
	return f[k]
}

// bases are the refs a thread's changes may count from, in order: its
// recorded base, else origin's default branch, else the repository's
// checked-out branch.
func bases(g *git, o Options) []string {
	var out []string
	if o.Base != "" && o.Base != "HEAD" {
		out = append(out, o.Base)
	}
	if ref, ok := g.line("symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); ok && ref != "" {
		out = append(out, ref)
	}
	main := &git{ctx: g.ctx, dir: o.Repo}
	if ref, ok := main.line("symbolic-ref", "-q", "--short", "HEAD"); ok && ref != "" {
		out = append(out, ref)
	}
	return out
}

// patch is path's unified diff from from, or against nothing when it
// is untracked, and whether it was cut at MaxPatch.
func patch(g *git, from, path string, files []File, untracked []string) (string, bool) {
	var out string
	var over bool
	if slices.Contains(untracked, path) {
		// --no-index exits 1 when the files differ, as they do.
		out, over = g.out(MaxPatch, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "-U3", "--", os.DevNull, path)
	} else {
		args := append(diffArgs("-U3", "--no-color"), from, "--")
		i := slices.IndexFunc(files, func(f File) bool { return f.Path == path })
		if i >= 0 && files[i].Old != "" {
			args = append(args, files[i].Old)
		}
		out, over = g.out(MaxPatch, append(args, path)...)
	}
	return strings.ToValidUTF8(widget.Clean(out), "?"), over
}

func diffArgs(more ...string) []string {
	return append([]string{"diff", "--no-ext-diff", "--no-textconv", "-z", "-M"}, more...)
}

func (c *Count) add(f *File) { c.Files, c.Add, c.Del = c.Files+1, c.Add+f.Add, c.Del+f.Del }

// parseStatus reads status --porcelain=v1 -z: the paths it lists (both
// of a rename) and, of those, the untracked ones.
func parseStatus(out string) (map[string]bool, []string) {
	dirty := map[string]bool{}
	var untracked []string
	recs := splitZ(out)
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 4 {
			continue
		}
		xy, p := r[:2], r[3:]
		dirty[p] = true
		switch {
		case xy == "??":
			untracked = append(untracked, p)
		case xy[0] == 'R' || xy[0] == 'C':
			if i+1 < len(recs) {
				i++
				dirty[recs[i]] = true
			}
		}
	}
	return dirty, untracked
}

// parseDiff reads diff -z --name-status and --numstat of the same
// diff into files.
func parseDiff(names, nums string) []File {
	byPath := map[string]*File{}
	var files []*File
	recs := splitZ(names)
	for i := 0; i < len(recs); i++ {
		st := recs[i]
		if st == "" || i+1 >= len(recs) {
			continue
		}
		f := &File{Status: "M"}
		i++
		f.Path = recs[i]
		switch st[0] {
		case 'A', 'D', 'T':
			f.Status = st[:1]
		case 'R', 'C': // the old path, then the new
			if i+1 < len(recs) {
				i++
				f.Old, f.Path = f.Path, recs[i]
			}
			f.Status = "R"
			if st[0] == 'C' {
				f.Old, f.Status = "", "A"
			}
		}
		byPath[f.Path] = f
		files = append(files, f)
	}
	recs = splitZ(nums)
	for i := 0; i < len(recs); i++ {
		add, rest, ok1 := strings.Cut(recs[i], "\t")
		del, p, ok2 := strings.Cut(rest, "\t")
		if !ok1 || !ok2 {
			continue
		}
		if p == "" && i+2 < len(recs) { // a rename: old, then new
			p = recs[i+2]
			i += 2
		}
		f := byPath[p]
		if f == nil {
			continue
		}
		if add == "-" {
			f.Binary = true
			continue
		}
		f.Add, _ = strconv.Atoi(add)
		f.Del, _ = strconv.Atoi(del)
	}
	out := make([]File, len(files))
	for i, f := range files {
		out[i] = *f
	}
	return out
}

func splitZ(s string) []string {
	s = strings.TrimSuffix(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

// countLines counts an untracked file's lines, as git would add them:
// only a regular file, not a link; one with a NUL in its first 8000
// bytes, or over maxUntracked, is binary.
func countLines(ctx context.Context, path string) (int, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || ctx.Err() != nil {
		return 0, false
	}
	if fi.Size() > maxUntracked {
		return 0, true
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, maxUntracked))
	head, _ := r.Peek(8000)
	if bytes.IndexByte(head, 0) >= 0 {
		return 0, true
	}
	n, last := 0, byte('\n')
	buf := make([]byte, 32<<10)
	for {
		k, err := r.Read(buf)
		if k > 0 {
			n += bytes.Count(buf[:k], []byte{'\n'})
			last = buf[k-1]
		}
		if err != nil {
			break
		}
	}
	if last != '\n' {
		n++ // a last line without a newline
	}
	return n, false
}

// clean makes a path from git safe to draw: no control or format
// characters, valid UTF-8.
func clean(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(widget.Clean(s), "\n", " "), "?")
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(filepath.FromSlash(a))
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(filepath.FromSlash(a)) == filepath.Clean(b)
	}
	return ra == rb
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// git runs git in one worktree under the read's context.
type git struct {
	ctx       context.Context
	dir       string
	truncated bool
}

// err is the read's timeout, once it passed.
func (g *git) err() error {
	if errors.Is(g.ctx.Err(), context.DeadlineExceeded) {
		return errors.New("git took over " + Timeout.String())
	}
	return g.ctx.Err()
}

// out runs git args and returns its stdout up to limit bytes, and
// whether it was cut there (an output over MaxOutput also marks the read
// truncated). A failed git answers "".
func (g *git) out(limit int, args ...string) (string, bool) {
	if g.ctx.Err() != nil {
		return "", false
	}
	cmd := exec.CommandContext(g.ctx, "git", append([]string{"-c", "core.quotepath=off", "-c", "color.ui=never"}, args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C")
	stdout := &capped{n: limit}
	cmd.Stdout, cmd.Stderr = stdout, io.Discard
	cmd.WaitDelay = 2 * time.Second
	proc.Group(cmd)
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case g.ctx.Err() != nil:
		return "", false
	case errors.As(err, &exit) && exit.ExitCode() == 1 && slices.Contains(args, "--no-index"):
	case err != nil:
		return "", false
	}
	if stdout.over && limit == MaxOutput {
		g.truncated = true
	}
	s := stdout.buf.String()
	if stdout.over {
		// A cut -z list ends in a partial record: drop it.
		if i := strings.LastIndexByte(s, 0); i >= 0 && slices.Contains(args, "-z") {
			s = s[:i+1]
		}
	}
	return s, stdout.over
}

// line is git's first line of output, and whether git succeeded.
func (g *git) line(args ...string) (string, bool) {
	if g.ctx.Err() != nil {
		return "", false
	}
	cmd := exec.CommandContext(g.ctx, "git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out := &capped{n: 4096}
	cmd.Stdout, cmd.Stderr = out, io.Discard
	cmd.WaitDelay = 2 * time.Second
	proc.Group(cmd)
	if err := cmd.Run(); err != nil {
		return "", false
	}
	s, _, _ := strings.Cut(out.buf.String(), "\n")
	return strings.TrimSpace(s), true
}

// count is rev-list --count of a range; 0 when git can't tell.
func (g *git) count(rng string) int {
	s, _ := g.line("rev-list", "--count", rng)
	n, _ := strconv.Atoi(s)
	return n
}

// capped keeps the first n bytes written to it.
type capped struct {
	buf  bytes.Buffer
	n    int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.n - c.buf.Len(); room < len(p) {
		c.over = true
		c.buf.Write(p[:max(room, 0)])
	} else {
		c.buf.Write(p)
	}
	return len(p), nil
}
