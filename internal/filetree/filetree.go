// Package filetree reads a folder an agent writes, for the human's eyes
// and read only (docs/SPEC.md §3.3, files.list): the folders and files
// under one root, with git's marks.
//
// The root's contents are data, and nothing in them moves a read out of
// the root. A path is relative, with slashes, and names no parent and no
// .git. A symbolic link opens only when what it leads to is inside the
// root and outside any .git; the files are opened through an os.Root, so
// a link swapped in after the check can't lead out. The open file then
// says where it is (fsx.HandlePath) and that place answers to the rules
// again, .git and git-ignored both, so a link swapped in reads nothing it
// couldn't. Where the system can't say, the path is looked at again.
// Special files don't open, nor block. Git runs pinned to the root
// (gitx.Pin), never looking its repository up from the folder and
// starting no program on its behalf. A git-ignored path answers only
// when asked for, and an answer git couldn't give refuses.
//
// A scope OpenFar opened answers a paired server, and holds more back: text
// only, never what is git-ignored, and no contents under a name that holds
// secrets, in a nested checkout, of a file with other names or of a private
// key (withheld.go), where a link leads as well as by its name. A path answers
// only as the file system spells it, and an ignored one the same whether it is
// there or not.
package filetree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Why a path doesn't answer.
var (
	ErrPath    = errors.New("not a path in the folder")
	ErrLinkOut = errors.New("a link leads out of the folder")
	ErrIgnored = errors.New("git-ignored")
	ErrGone    = errors.New("no longer there")
	ErrNotFile = errors.New("not the kind of file asked for")
	// ErrWithheld: a far scope's listing shows it, and no more.
	ErrWithheld = errors.New("withheld")
	// ErrNoRoot: the root is no folder, or no checkout of its repo.
	ErrNoRoot = errors.New("no folder to read")
)

const (
	// maxScan bounds the entries read of one folder and a plain folder's
	// search.
	maxScan = 20000
	// maxListBytes keeps a listing well within a 1 MiB line.
	maxListBytes = 512 << 10
	// sniff is how much of a file tells it isn't text.
	sniff = 8192
)

// Scope is one root to read.
type Scope struct {
	Root string // its real path
	repo string
	base string
	tree *gitx.Tree
	// far: this scope answers a paired server (OpenFar).
	far bool
	// beforeOpen stands in a test for a link swapped after the gate.
	beforeOpen func()
	// gitRan stands in a test to count git's runs.
	gitRan func(args []string)
}

// handlePath is fsx.HandlePath; a test takes it away, as on a system with
// none.
var handlePath = fsx.HandlePath

// Open checks root: a folder, and with repo a checkout of it by the
// repository's own records; without, a plain folder, which has no marks
// and nothing ignored. base is what the marks count from (changes.Options).
func Open(ctx context.Context, root, repo, base string) (*Scope, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoRoot, err)
	}
	fi, err := os.Lstat(real)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a folder", ErrNoRoot, root)
	}
	s := &Scope{Root: real, repo: repo, base: base}
	if repo != "" {
		if s.tree, err = gitx.Pin(ctx, repo, real); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoRoot, err)
		}
	}
	return s, nil
}

// OpenFar is Open for a paired server: a checkout only, since a plain folder
// has no git to say what is ignored.
func OpenFar(ctx context.Context, root, repo, base string) (*Scope, error) {
	if repo == "" {
		return nil, fmt.Errorf("%w: not a checkout", ErrNoRoot)
	}
	s, err := Open(ctx, root, repo, base)
	if err != nil {
		return nil, err
	}
	s.far = true
	return s, nil
}

// Git says the root is a checkout, with marks and ignored files.
func (s *Scope) Git() bool { return s.tree != nil }

// hiddenName says a path's element is .git, however the file system might
// spell it: any case, Windows' dropped trailing dots and spaces and its
// short name.
func hiddenName(name string) bool {
	return strings.EqualFold(strings.TrimRight(name, ". "), ".git") || strings.EqualFold(name, "git~1")
}

// CleanPath checks a path as the methods take it: "" or "." is the top;
// else relative, with slashes, in its clean form, valid and safe text,
// naming no parent and no .git.
func CleanPath(rel string) (string, error) {
	if rel == "" || rel == "." {
		return "", nil
	}
	// A backslash is Windows' separator, and a name elsewhere too rare to
	// keep: refused on every system, the rules are the same.
	if !utf8.ValidString(rel) || !textsafe.Safe(rel) || path.Clean(rel) != rel || strings.Contains(rel, `\`) ||
		!filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("%w: %q", ErrPath, rel)
	}
	if slices.ContainsFunc(strings.Split(rel, "/"), hiddenName) {
		return "", fmt.Errorf("%w: %q", ErrPath, rel)
	}
	return rel, nil
}

// inside is real path p relative to the root, with slashes ("" for the
// top): ErrLinkOut when it's outside, or in a .git.
func (s *Scope) inside(p string) (string, error) {
	rel, ok := fsx.Rel(s.Root, p)
	if !ok {
		return "", ErrLinkOut
	}
	if rel == "." {
		return "", nil
	}
	rel = filepath.ToSlash(rel)
	if slices.ContainsFunc(strings.Split(rel, "/"), hiddenName) {
		return "", ErrLinkOut
	}
	return rel, nil
}

// resolve is where clean path rel really is, its links followed.
func (s *Scope) resolve(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	p, err := filepath.EvalSymlinks(filepath.Join(s.Root, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrGone, rel)
		}
		return "", fmt.Errorf("%w: %s: %v", ErrPath, rel, err)
	}
	return s.inside(p)
}

// git runs git in the root, pinned.
func (s *Scope) git(ctx context.Context, o gitx.Opts, args ...string) (string, bool, error) {
	o.Tree = s.tree
	o.Config = append(o.Config, "core.quotepath=off")
	if s.gitRan != nil {
		s.gitRan(args)
	}
	return gitx.Output(ctx, "", o, args...)
}

// isIgnored says real path rel is git-ignored; a tracked file isn't.
func (s *Scope) isIgnored(ctx context.Context, rel string) (bool, error) {
	if s.tree == nil || rel == "" {
		return false, nil
	}
	out, _, err := s.git(ctx, gitx.Opts{OK: []int{1}}, "check-ignore", "--", rel)
	return strings.TrimSpace(out) != "", err
}

// gate refuses a git-ignored path unless asked for, and when git can't
// tell. A far scope can't ask, and refuses what is withheld too.
func (s *Scope) gate(ctx context.Context, rel string, ignored bool) error {
	if s.far {
		ignored = false
		if s.withheld(rel) {
			return fmt.Errorf("%w: %s", ErrWithheld, rel)
		}
	}
	if ignored {
		return nil
	}
	ign, err := s.isIgnored(ctx, rel)
	switch {
	case err != nil:
		return fmt.Errorf("%w: git can't say: %v", ErrIgnored, err)
	case ign:
		return fmt.Errorf("%w: %s", ErrIgnored, rel)
	}
	return nil
}

// ignoredUnder are the ignored paths at or under real path rel, a whole
// ignored folder as one, without its slash; cut says git's answer was too
// long to hold them all.
func (s *Scope) ignoredUnder(ctx context.Context, rel string) (map[string]bool, bool, error) {
	if s.tree == nil {
		return nil, false, nil
	}
	args := []string{"ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory"}
	if rel != "" {
		args = append(args, "--", rel+"/")
	}
	// A name in the folder is a path here, not a pattern.
	out, cut, err := s.git(ctx, gitx.Opts{Max: changes.MaxOutput, Env: []string{"GIT_LITERAL_PATHSPECS=1"}}, args...)
	if err != nil {
		return nil, false, err
	}
	set := map[string]bool{}
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			set[strings.TrimSuffix(p, "/")] = true
		}
	}
	return set, cut, nil
}

// underIgnored says p is, or is inside, one of set.
func underIgnored(set map[string]bool, p string) bool {
	for ; p != "." && p != ""; p = path.Dir(p) {
		if set[p] {
			return true
		}
	}
	return false
}

// Read is the chunk of file rel from offset: ignored allows a git-ignored
// one. It answers name and size alone for a file that isn't text.
func (s *Scope) Read(ctx context.Context, rel string, offset int64, ignored bool) (proto.FilesReadResult, error) {
	return s.read(ctx, rel, offset, ignored, false)
}

// ReadBytes is Read with the Data of a file that isn't text too, for the
// app that hands it to Quick Look or an app of the user's.
func (s *Scope) ReadBytes(ctx context.Context, rel string, offset int64, ignored bool) (proto.FilesReadResult, error) {
	return s.read(ctx, rel, offset, ignored, true)
}

func (s *Scope) read(ctx context.Context, rel string, offset int64, ignored, binary bool) (proto.FilesReadResult, error) {
	var res proto.FilesReadResult
	rel, err := CleanPath(rel)
	if err != nil {
		return res, err
	}
	if rel == "" {
		return res, fmt.Errorf("%w: the top is a folder", ErrNotFile)
	}
	if offset < 0 {
		return res, fmt.Errorf("%w: offset %d", ErrPath, offset)
	}
	if err := s.farGate(ctx, rel); err != nil {
		return res, err
	}
	binary = binary && !s.far
	most := int64(proto.MaxFilesRead)
	if s.far {
		most = proto.PeerWorktreeMaxRead
	}
	real, err := s.resolve(rel)
	if err != nil {
		return res, err
	}
	f, fi, err := s.openGated(ctx, real, ignored)
	if err != nil {
		return res, err
	}
	defer f.Close()
	if !fi.Mode().IsRegular() {
		return res, fmt.Errorf("%w: %s", ErrNotFile, rel)
	}
	res = proto.FilesReadResult{Name: path.Base(rel), Path: rel, Size: fi.Size(), MTime: fi.ModTime(), Offset: offset,
		Truncated: fi.Size() > most}
	head := make([]byte, min(fi.Size(), sniff))
	n, err := f.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return res, fmt.Errorf("%w: %s: %v", ErrGone, rel, err)
	}
	if IsBinary(head[:n]) {
		res.Binary = true
		if !binary {
			return res, nil
		}
	}
	if s.far {
		// All a chunk could come from: a chunk from inside a key shows no mark.
		all := make([]byte, min(fi.Size(), most))
		n, err := f.ReadAt(all, 0)
		if err != nil && err != io.EOF {
			return res, fmt.Errorf("%w: %s: %v", ErrGone, rel, err)
		}
		if holdsPrivateKey(all[:n]) {
			return proto.FilesReadResult{}, fmt.Errorf("%w: %s", ErrWithheld, rel)
		}
	}
	want := min(fi.Size(), most) - offset
	if want <= 0 {
		return res, nil
	}
	buf := make([]byte, min(want, proto.LibraryChunk))
	n, err = f.ReadAt(buf, offset)
	if n < len(buf) && err != nil && err != io.EOF {
		return res, fmt.Errorf("%w: %s: %v", ErrGone, rel, err)
	}
	if s.far && ChunkBinary(buf[:n]) {
		// Text at its head only: what isn't text doesn't cross.
		res.Binary = true
		return res, nil
	}
	res.Data = buf[:n]
	return res, nil
}

// IsBinary says a file's first bytes aren't text: a NUL, or not UTF-8
// but for a character cut at their end.
func IsBinary(head []byte) bool {
	if slices.Contains(head, 0) {
		return true
	}
	for range utf8.UTFMax {
		r, n := utf8.DecodeLastRune(head)
		if r != utf8.RuneError || n > 1 {
			break
		}
		head = head[:len(head)-n]
	}
	return !utf8.Valid(head)
}

// openGated opens real path rel, git-ignored or not as allowed, and holds
// what it opened to the same rules: rel was the real path a moment ago.
func (s *Scope) openGated(ctx context.Context, rel string, ignored bool) (*os.File, fs.FileInfo, error) {
	if err := s.gate(ctx, rel, ignored); err != nil {
		return nil, nil, err
	}
	if s.beforeOpen != nil {
		s.beforeOpen()
	}
	f, fi, err := s.open(rel)
	if err != nil {
		return nil, nil, err
	}
	at, err := s.openedAt(f, fi, rel)
	switch {
	case err != nil:
	case s.far && fileLinked(f, fi):
		err = fmt.Errorf("%w: %s", ErrWithheld, rel)
	case s.far && at != rel:
		// Swapped since, or another spelling of it: either way, not there.
		err = fmt.Errorf("%w: %s", ErrGone, rel)
	case at != rel:
		err = s.gate(ctx, at, ignored)
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}

// openedAt is where the file opened as rel is, relative to the root. A
// system that can't say from the file is asked for the path again, which
// must still lead to that file.
func (s *Scope) openedAt(f *os.File, fi fs.FileInfo, rel string) (string, error) {
	if got, ok := handlePath(f); ok {
		return s.inside(got)
	}
	at, err := s.resolve(rel)
	if err != nil {
		return "", err
	}
	r, err := os.OpenRoot(s.Root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoRoot, err)
	}
	defer r.Close()
	name := filepath.FromSlash(at)
	if at == "" {
		name = "."
	}
	if now, err := r.Stat(name); err != nil || !os.SameFile(fi, now) {
		return "", fmt.Errorf("%w: %s", ErrGone, rel)
	}
	if s.far && !spelledAsStored(r, at) {
		return "", fmt.Errorf("%w: %s", ErrGone, rel)
	}
	return at, nil
}

// open opens real path rel by the root, without blocking on a FIFO: the
// os.Root refuses a link leading out, and the open file is asked where it
// is, for one leading to .git swapped in since it was resolved.
func (s *Scope) open(rel string) (*os.File, fs.FileInfo, error) {
	r, err := os.OpenRoot(s.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrNoRoot, err)
	}
	defer r.Close()
	name := filepath.FromSlash(rel)
	if rel == "" {
		name = "."
	}
	f, err := fsx.OpenInNoBlock(r, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, fmt.Errorf("%w: %s", ErrGone, rel)
	case err != nil && strings.Contains(err.Error(), "escapes"):
		return nil, nil, fmt.Errorf("%w: %s", ErrLinkOut, rel)
	case err != nil:
		return nil, nil, fmt.Errorf("%w: %v", ErrNotFile, err)
	}
	if got, ok := handlePath(f); ok {
		if _, err := s.inside(got); err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("%w: %s", err, rel)
		}
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("%w: %s", ErrGone, rel)
	}
	return f, fi, nil
}

// Options say what List lists (proto.FilesListParams).
type Options struct {
	Ignored bool
	Query   string
	Changed bool
}

// List lists folder rel, or with o.Query or o.Changed the files found
// under it, with cs's marks (nil: none).
func (s *Scope) List(ctx context.Context, rel string, o Options, cs *changes.Changeset) (proto.FilesListResult, error) {
	res := proto.FilesListResult{Root: s.Root, Entries: []proto.FileEntry{}, Git: s.tree != nil}
	rel, err := CleanPath(rel)
	if err != nil {
		return res, err
	}
	if len(o.Query) > proto.MaxFilesQuery {
		return res, fmt.Errorf("%w: a search of over %d bytes", ErrPath, proto.MaxFilesQuery)
	}
	if s.far {
		o.Ignored = false
		if err := s.farGate(ctx, rel); err != nil {
			return res, err
		}
	}
	res.Path = rel
	m := marksOf(cs)
	if cs != nil {
		res.Files, res.Added, res.Deleted, res.Truncated = cs.Totals.Files, cs.Totals.Add, cs.Totals.Del, cs.Truncated
	}
	if o.Query != "" || o.Changed {
		err = s.find(ctx, rel, o, m, &res)
	} else {
		err = s.folder(ctx, rel, o, m, &res)
	}
	if s.far && err == nil {
		s.holdBack(ctx, &res)
	}
	return res, err
}

// marks are git's marks by real path, and the folders that hold them.
type marks struct {
	status map[string]string
	dirs   map[string]bool
}

func marksOf(cs *changes.Changeset) marks {
	m := marks{status: map[string]string{}, dirs: map[string]bool{}}
	if cs == nil {
		return m
	}
	add := func(p, st string) {
		m.status[p] = st
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			m.dirs[d] = true
		}
	}
	for _, f := range cs.Files {
		switch f.Status {
		case "D":
			add(f.Path, proto.FileDeleted)
		case "A", "?":
			add(f.Path, proto.FileAdded)
		case "R":
			add(f.Path, proto.FileAdded)
			if f.Old != "" {
				add(f.Old, proto.FileDeleted)
			}
		default:
			add(f.Path, proto.FileModified)
		}
	}
	return m
}

// folder lists one folder: folders first, by name.
func (s *Scope) folder(ctx context.Context, rel string, o Options, m marks, res *proto.FilesListResult) error {
	real, err := s.resolve(rel)
	missing := errors.Is(err, ErrGone)
	if err != nil && !(missing && (m.dirs[rel])) {
		// A folder of deleted files alone isn't there, but they are shown.
		return err
	}
	if missing {
		real = rel
	}
	seen := map[string]bool{}
	if !missing {
		f, fi, err := s.openGated(ctx, real, o.Ignored)
		if err != nil {
			return err
		}
		defer f.Close()
		if !fi.IsDir() {
			return fmt.Errorf("%w: %s", ErrNotFile, rel)
		}
		ents, err := f.ReadDir(maxScan)
		if err != nil && err != io.EOF {
			return fmt.Errorf("%w: %s: %v", ErrGone, rel, err)
		}
		res.Truncated = res.Truncated || len(ents) == maxScan
		ign, cut, err := s.ignoredUnder(ctx, real)
		if err != nil {
			return fmt.Errorf("%w: git can't say: %v", ErrIgnored, err)
		}
		if cut && s.far {
			return fmt.Errorf("%w: git can't say: too many", ErrIgnored)
		}
		res.Truncated = res.Truncated || cut
		r, err := os.OpenRoot(s.Root)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNoRoot, err)
		}
		defer r.Close()
		for _, e := range ents {
			if hiddenName(e.Name()) {
				continue
			}
			realChild := path.Join(real, e.Name())
			seen[realChild] = true
			// Inside an ignored folder, git names the folder alone.
			isIgn := underIgnored(ign, realChild)
			if isIgn && !o.Ignored {
				continue
			}
			ent := s.entry(r, path.Join(rel, e.Name()), realChild, m)
			ent.Ignored = isIgn
			res.Entries = append(res.Entries, ent)
		}
	}
	// What git has deleted is still listed.
	for p, st := range m.status {
		if st == proto.FileDeleted && path.Dir(p) == cmpDot(real) && !seen[p] {
			seen[p] = true
			res.Entries = append(res.Entries, proto.FileEntry{Name: path.Base(p), Path: path.Join(rel, path.Base(p)), Kind: proto.FileKindFile, Status: proto.FileDeleted})
		}
	}
	for d := range m.dirs {
		if path.Dir(d) == cmpDot(real) && !seen[d] {
			seen[d] = true
			res.Entries = append(res.Entries, proto.FileEntry{Name: path.Base(d), Path: path.Join(rel, path.Base(d)), Kind: proto.FileKindDir, Status: proto.FileDeleted, Changed: true})
		}
	}
	slices.SortFunc(res.Entries, func(a, b proto.FileEntry) int {
		if ad, bd := a.Kind == proto.FileKindDir, b.Kind == proto.FileKindDir; ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	capEntries(res, proto.MaxFilesEntries)
	return nil
}

// cmpDot is path.Dir's spelling of the top.
func cmpDot(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// capEntries cuts res's entries to n, and to what a line carries.
func capEntries(res *proto.FilesListResult, n int) {
	total := 0
	for i, e := range res.Entries {
		total += len(e.Name) + len(e.Path) + 128
		if i >= n || total > maxListBytes {
			res.Entries, res.Truncated = res.Entries[:i], true
			return
		}
	}
}

// entry is the file or folder at real path realRel, named rel.
func (s *Scope) entry(r *os.Root, rel, realRel string, m marks) proto.FileEntry {
	e := proto.FileEntry{Name: path.Base(rel), Path: rel, Kind: proto.FileKindOther, Status: m.status[realRel], Changed: m.dirs[realRel]}
	fi, err := r.Lstat(filepath.FromSlash(realRel))
	if err != nil {
		if e.Status == proto.FileDeleted {
			e.Kind = proto.FileKindFile
		}
		return e
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		e.Link = true
		to, err := s.resolve(rel)
		if err != nil {
			e.LinkOut = true
			return e
		}
		if fi, err = r.Stat(filepath.FromSlash(to)); err != nil {
			e.LinkOut = true
			return e
		}
	}
	e.MTime = fi.ModTime()
	switch {
	case fi.IsDir():
		e.Kind = proto.FileKindDir
	case fi.Mode().IsRegular():
		e.Kind, e.Size = proto.FileKindFile, fi.Size()
	}
	return e
}

// find lists the files under rel git marks, or whose path holds the
// query: whole paths, flat, by path. A link's path isn't looked through.
func (s *Scope) find(ctx context.Context, rel string, o Options, m marks, res *proto.FilesListResult) error {
	real, err := s.resolve(rel)
	switch {
	case errors.Is(err, ErrGone) && m.dirs[rel]:
		real = rel // a folder of deleted files
	case err != nil:
		return err
	case rel != real:
		return fmt.Errorf("%w: a search doesn't go through a link", ErrNotFile)
	}
	if err := s.gate(ctx, real, o.Ignored); err != nil {
		return err
	}
	var paths []string
	if o.Changed {
		for p := range m.status {
			paths = append(paths, p)
		}
	} else if paths, err = s.allPaths(ctx, o.Ignored, res); err != nil {
		return err
	}
	var ign map[string]bool
	if o.Ignored && s.tree != nil {
		var cut bool
		if ign, cut, err = s.ignoredUnder(ctx, ""); err != nil {
			return fmt.Errorf("%w: git can't say: %v", ErrIgnored, err)
		}
		res.Truncated = res.Truncated || cut
	}
	q := strings.ToLower(o.Query)
	var hits []string
	for _, p := range paths {
		if real != "" && !strings.HasPrefix(p, real+"/") {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(p), q) {
			continue
		}
		if !slices.ContainsFunc(strings.Split(p, "/"), hiddenName) {
			hits = append(hits, p)
		}
	}
	slices.Sort(hits)
	hits = slices.Compact(hits)
	if len(hits) > proto.MaxFilesFound {
		hits, res.Truncated = hits[:proto.MaxFilesFound], true
	}
	r, err := os.OpenRoot(s.Root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoRoot, err)
	}
	defer r.Close()
	for _, p := range hits {
		e := s.entry(r, p, p, m)
		e.Ignored = underIgnored(ign, p)
		res.Entries = append(res.Entries, e)
	}
	capEntries(res, proto.MaxFilesFound)
	return nil
}

// allPaths are the root's file paths: git's, ignored ones when asked; a
// plain folder's by a walk that doesn't follow links.
func (s *Scope) allPaths(ctx context.Context, ignored bool, res *proto.FilesListResult) ([]string, error) {
	var out []string
	if s.tree != nil {
		args := []string{"ls-files", "-z", "--cached", "--others"}
		if !ignored {
			args = append(args, "--exclude-standard")
		}
		text, cut, err := s.git(ctx, gitx.Opts{}, args...)
		if err != nil {
			return nil, fmt.Errorf("%w: git can't say: %v", ErrIgnored, err)
		}
		res.Truncated = res.Truncated || cut
		if cut {
			// A cut list ends in a partial path.
			text = text[:strings.LastIndexByte(text, 0)+1]
		}
		for p := range strings.SplitSeq(text, "\x00") {
			if p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	}
	err := filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		case p == s.Root:
			return nil
		case hiddenName(d.Name()):
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		case len(out) >= maxScan:
			res.Truncated = true
			return fs.SkipAll
		case d.IsDir():
			return nil
		}
		if rel, ok := fsx.Rel(s.Root, p); ok {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}

// Stamp is a digest of the root's git status (changes.Fingerprint: its
// changed files' sizes and times too) and of each folder's entries, by
// reading no file: see proto.FilesStampResult. A folder that doesn't
// answer counts as such.
func (s *Scope) Stamp(ctx context.Context, folders []string) string {
	fp := ""
	if s.tree != nil {
		fp = changes.FingerprintTree(ctx, s.tree)
	}
	return s.stamp(fp, folders, s.farIgnored(ctx))
}

// stamp is the digest of fingerprint fp and folders' entries; ign is what
// a far scope leaves out of them (farIgnored).
func (s *Scope) stamp(fp string, folders []string, ign map[string]bool) string {
	h := sha256.New()
	if s.tree != nil {
		io.WriteString(h, fp)
	}
	for _, d := range folders {
		fmt.Fprintf(h, "\x00%s\x00", d)
		s.stampFolder(h, d, ign)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// stampFolder writes folder rel's entries, sorted by the file system or
// not as it reads them, into w: names, sizes, times and modes. To a far
// scope an ignored or withheld folder, or one spelled other than the file
// system does, doesn't answer, an ignored entry doesn't count and a withheld
// one counts by its name.
func (s *Scope) stampFolder(w io.Writer, rel string, ign map[string]bool) {
	rel, err := CleanPath(rel)
	if err != nil || s.farName(rel) != nil {
		return
	}
	real, err := s.resolve(rel)
	if err != nil {
		return
	}
	if s.far && (ign == nil || underIgnored(ign, real) || s.withheld(real)) {
		return
	}
	f, fi, err := s.open(real)
	if err != nil {
		return
	}
	defer f.Close()
	if !fi.IsDir() {
		return
	}
	if s.far {
		if at, err := s.openedAt(f, fi, real); err != nil || at != real {
			return
		}
	}
	ents, _ := f.ReadDir(maxScan)
	slices.SortFunc(ents, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, e := range ents {
		if hiddenName(e.Name()) {
			continue
		}
		if s.far {
			if underIgnored(ign, path.Join(real, e.Name())) {
				continue
			}
			if withheldName(e.Name()) {
				fmt.Fprintf(w, "%s\x00", e.Name())
				continue
			}
			if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() && fsx.Links(fi) > 1 {
				fmt.Fprintf(w, "%s\x00", e.Name())
				continue
			}
		}
		if fi, err := e.Info(); err == nil {
			fmt.Fprintf(w, "%s %d %d %d\x00", e.Name(), fi.Size(), fi.ModTime().UnixNano(), fi.Mode())
		}
	}
}

// Cache keeps each root's changes for as long as its fingerprint stays:
// a tree relisting its open folders reads git in full once. Its zero
// value is ready.
type Cache struct {
	mu     sync.Mutex
	seen   map[string]seenChanges
	stamps map[string]stampSeen
}

// stampSeen is a root's last fingerprint for a stamp and what it took.
type stampSeen struct {
	fp   string
	ign  map[string]bool // a far scope's (farIgnored)
	at   time.Time
	took time.Duration
}

const (
	// stampShare keeps stamps to about 1/stampShare of the time spent in
	// git, however often they're asked for and by however many clients: a
	// repo whose status takes a second answers a stamp from git every 10 s.
	stampShare = 10
	// maxStampRest bounds how old a stamp's fingerprint gets.
	maxStampRest = time.Minute
)

// Stamp is s's stamp (Scope.Stamp), its git status at most as often as
// stampShare allows; the folders' entries are always read.
func (c *Cache) Stamp(ctx context.Context, s *Scope, folders []string) string {
	if s.tree == nil {
		return s.stamp("", folders, nil)
	}
	// A near scope keeps no ignored files for a far one to find.
	key := s.Root
	if s.far {
		key += "\x00far"
	}
	start := time.Now()
	c.mu.Lock()
	had, ok := c.stamps[key]
	c.mu.Unlock()
	if ok && had.fp != "" && start.Sub(had.at) < min(had.took*stampShare, maxStampRest) {
		return s.stamp(had.fp, folders, had.ign)
	}
	fp := changes.FingerprintTree(ctx, s.tree)
	ign := s.farIgnored(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stamps == nil || len(c.stamps) >= maxCached {
		c.stamps = map[string]stampSeen{}
	}
	c.stamps[key] = stampSeen{fp: fp, ign: ign, at: start, took: time.Since(start)}
	return s.stamp(fp, folders, ign)
}

type seenChanges struct {
	fp string
	cs *changes.Changeset
}

// maxCached bounds the roots a Cache holds.
const maxCached = 64

// Changes is s's changes since its base; nil for a plain folder.
func (c *Cache) Changes(ctx context.Context, s *Scope) (*changes.Changeset, error) {
	if s.tree == nil {
		return nil, nil
	}
	fp := changes.FingerprintTree(ctx, s.tree)
	c.mu.Lock()
	had, ok := c.seen[s.Root]
	c.mu.Unlock()
	if ok && fp != "" && had.fp == fp {
		return had.cs, nil
	}
	cs, err := changes.Read(ctx, s.Root, changes.Options{Repo: s.repo, Base: s.base})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil || len(c.seen) >= maxCached {
		c.seen = map[string]seenChanges{}
	}
	c.seen[s.Root] = seenChanges{fp, cs}
	return cs, nil
}
