package filetree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

const (
	// maxDiffOutput bounds git's answer: a file's and its old version's
	// lines at MaxFilesDiff each, and their hunks' heads.
	maxDiffOutput = 4 << 20
	// maxDiffWire keeps an answer well within a 1 MiB line: the hunks and
	// the removed lines, as JSON writes them.
	maxDiffWire = 768 << 10
	// hunkWire is what a hunk's numbers and names take on the wire.
	hunkWire = 64
)

// Diff is file rel's changes since the base, as marks of cs say (nil, a
// plain folder: none): where lines were added and which were removed, for
// a console to lay over the text Read gives. The same rules as Read's hold
// of a file that's there, and of a deleted one all but opening it.
//
// A file that's there is never handed to git as new: an untracked file is
// the file Read gives, all of it added, and git's --no-index would follow a
// link swapped into its path. Tracked paths git reads itself, by its index,
// not following links.
func (s *Scope) Diff(ctx context.Context, rel string, ignored bool, cs *changes.Changeset) (proto.FilesDiffResult, error) {
	var res proto.FilesDiffResult
	rel, err := CleanPath(rel)
	if err != nil {
		return res, err
	}
	if rel == "" {
		return res, fmt.Errorf("%w: the top is a folder", ErrNotFile)
	}
	res.Name, res.Path = path.Base(rel), rel
	if err := s.farName(rel); err != nil {
		return proto.FilesDiffResult{}, err
	}
	real, err := s.resolve(rel)
	missing := errors.Is(err, ErrGone)
	switch {
	case missing:
		real = rel // deleted, if cs says so
		if err := s.gate(ctx, real, ignored); err != nil {
			return res, err
		}
	case err != nil:
		return res, err
	default:
		if err := s.diffStat(ctx, real, ignored, &res); err != nil {
			return res, err
		}
	}
	f, renamedAway := changedFile(cs, real)
	if f == nil && !renamedAway {
		if missing {
			return res, fmt.Errorf("%w: %s", ErrGone, rel)
		}
		return res, nil // no changes
	}
	if s.far && f != nil && f.Old != "" {
		// What it was renamed from answers too: its old lines would cross.
		if err := s.gate(ctx, f.Old, false); err != nil {
			return proto.FilesDiffResult{}, err
		}
	}
	res.Base = cs.Base
	if f != nil {
		res.Added, res.Deleted = f.Add, f.Del
		res.Binary = res.Binary || f.Binary
	}
	switch {
	case missing:
		if f != nil && f.Status != "D" {
			return res, fmt.Errorf("%w: %s", ErrGone, rel)
		}
		res.Status = proto.FileDeleted
	case f == nil:
		return res, nil // a file back at a renamed file's old path, untracked or not
	case f.Status == "A" || f.Status == "?":
		res.Status = proto.FileAdded
		return res, nil
	case f.Status == "T":
		// A file for a link or the reverse: the two have no lines in common.
		res.Status = proto.FileModified
		return res, nil
	default:
		res.Status, res.From = proto.FileModified, f.Old
	}
	old := real
	if res.From != "" {
		old = res.From
	}
	oldSize := s.blobSize(ctx, cs.From, old)
	if missing {
		res.Size = oldSize
	}
	if res.Binary {
		return res, nil
	}
	if max(res.Size, oldSize) > proto.MaxFilesDiff {
		res.TooBig = true
		return res, nil
	}
	if err := s.diffHunks(ctx, cs.From, real, &res); err != nil {
		return res, err
	}
	if s.far && hunksHoldKey(res.Hunks) {
		return proto.FilesDiffResult{}, fmt.Errorf("%w: %s", ErrWithheld, rel)
	}
	return res, nil
}

// diffStat fills in the size, time and text of real path rel, opened by
// Read's rules.
func (s *Scope) diffStat(ctx context.Context, rel string, ignored bool, res *proto.FilesDiffResult) error {
	f, fi, err := s.openGated(ctx, rel, ignored)
	if err != nil {
		return err
	}
	defer f.Close()
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotFile, rel)
	}
	res.Size, res.MTime = fi.Size(), fi.ModTime()
	head := make([]byte, min(fi.Size(), sniff))
	n, err := f.ReadAt(head, 0)
	if err != nil && err != io.EOF {
		return fmt.Errorf("%w: %s: %v", ErrGone, rel, err)
	}
	res.Binary = IsBinary(head[:n])
	return nil
}

// changedFile is real path p's change in cs; renamedAway: it's nil, and p
// is what a renamed file was called.
func changedFile(cs *changes.Changeset, p string) (f *changes.File, renamedAway bool) {
	if cs == nil {
		return nil, false
	}
	for i := range cs.Files {
		if cs.Files[i].Path == p {
			return &cs.Files[i], false
		}
		renamedAway = renamedAway || cs.Files[i].Old == p
	}
	return nil, renamedAway
}

// blobSize is path p's size at commit from, 0 when it has none there.
func (s *Scope) blobSize(ctx context.Context, from, p string) int64 {
	out, _, err := s.git(ctx, gitx.Opts{Max: 64}, "cat-file", "-s", from+":"+p)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	return n
}

// diffHunks reads res's hunks from git: no context lines, a rename's old
// name beside the new, so the changes count from what it was.
func (s *Scope) diffHunks(ctx context.Context, from, real string, res *proto.FilesDiffResult) error {
	args := []string{"diff", "--no-color", "-U0", "--inter-hunk-context=0"}
	if res.From != "" {
		args = append(args, "-M", from, "--", res.From, real)
	} else {
		args = append(args, "--no-renames", from, "--", real)
	}
	out, over, err := s.git(ctx, gitx.Opts{Max: maxDiffOutput, Env: []string{"GIT_LITERAL_PATHSPECS=1"}}, args...)
	if err != nil {
		return fmt.Errorf("%w: git can't say: %v", ErrNoRoot, err)
	}
	most := maxDiffWire
	if s.far {
		most = proto.PeerWorktreeMaxHunks
	}
	res.Hunks, res.Binary, res.Truncated = parseHunks(out, over, most)
	return nil
}

// parseHunks reads a -U0 patch of one file: its hunks, whether git calls it
// binary, and whether they stop short of all it changed: over says git's
// answer was cut, and what's over most bytes on the wire goes too. Lines added are the
// file's own and aren't kept, only counted.
func parseHunks(patch string, over bool, most int) (hunks []proto.FilesHunk, binary, cut bool) {
	lines := strings.Split(patch, "\n")
	wire := 0
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "Binary files ") || l == "GIT binary patch" {
			return nil, true, false
		}
		oldAt, oldN, newAt, newN, ok := hunkHead(l)
		if !ok {
			continue
		}
		h := proto.FilesHunk{Old: oldAt, New: newAt, Added: newN}
		// With nothing removed or added, git's number is the line before.
		if oldN == 0 {
			h.Old++
		}
		if newN == 0 {
			h.New++
		}
		wire += hunkWire
		for left := oldN + newN; left > 0; i++ {
			if i+1 >= len(lines) || over && i+2 >= len(lines) {
				// The cut went through this hunk.
				return hunks, false, true
			}
			text := lines[i+1]
			switch {
			case text == "", text[0] == '\\':
				continue // no newline at the file's end
			case text[0] == '-':
				removed := CleanRemoved(text[1:])
				enc, _ := json.Marshal(removed)
				if wire += len(enc) + 1; wire > most {
					return append(hunks, h), false, true
				}
				h.Removed = append(h.Removed, removed)
			}
			left--
		}
		hunks = append(hunks, h)
		if wire > most {
			return hunks, false, true
		}
	}
	return hunks, false, over
}

// hunkHead reads "@@ -a[,b] +c[,d] @@", git's short form meaning one line.
func hunkHead(l string) (oldAt, oldN, newAt, newN int, ok bool) {
	rest, found := strings.CutPrefix(l, "@@ -")
	if !found {
		return
	}
	spec, _, found := strings.Cut(rest, " @@")
	if !found {
		return
	}
	a, c, found := strings.Cut(spec, " +")
	if !found {
		return
	}
	num := func(s string) (at, n int, good bool) {
		at1, n1, comma := strings.Cut(s, ",")
		at, err := strconv.Atoi(at1)
		if err != nil {
			return 0, 0, false
		}
		if !comma {
			return at, 1, true
		}
		n, err = strconv.Atoi(n1)
		return at, n, err == nil
	}
	var oldOK, newOK bool
	oldAt, oldN, oldOK = num(a)
	newAt, newN, newOK = num(c)
	return oldAt, oldN, newAt, newN, oldOK && newOK
}

// CleanRemoved is a removed line as text to draw: data from the repository,
// so nothing unsafe, as a console cleans a file's lines. A line's own end
// goes.
func CleanRemoved(l string) string {
	return textsafe.Text(strings.ToValidUTF8(strings.TrimSuffix(l, "\r"), "?"))
}
