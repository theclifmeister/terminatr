package server

// session.file (docs/SPEC.md §3.3, Transcript): the bytes of a file a
// session's conversation puts in front of the user, for the Mac app's
// attachment cards. The app never names a path: it names a file by the
// Ref transcript.watch gave it, which is the transcript line's offset,
// a hash of the line and the file's place in it. The server reads that
// line again from the session's transcript and serves only what the
// line names there (agent.Transcript.FileAt): a call of the files tool's
// path, symlinks evaluated, a regular file of a type tm sends and at most
// proto.MaxSentFile bytes; or an image pasted into a prompt. The human's
// only, and never a paired server's: transcript.watch isn't either.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// lineSum is how many hex digits of a line's SHA-256 its ref keeps.
const lineSum = 16

// lineRef is the prefix of the refs of the files of line, the
// transcript's line at off: "<off>.<sum>.", then each file's own
// "<block>.<n>".
func lineRef(line []byte, off int64) string {
	sum := sha256.Sum256(line)
	return fmt.Sprintf("%d.%s.", off, hex.EncodeToString(sum[:])[:lineSum])
}

// fileRef is a parsed ref.
type fileRef struct {
	off      int64
	sum      string
	block, n int
}

func parseRef(ref string) (fileRef, bool) {
	parts := strings.Split(ref, ".")
	if len(parts) != 4 || len(parts[1]) != lineSum {
		return fileRef{}, false
	}
	var r fileRef
	var err error
	if r.off, err = strconv.ParseInt(parts[0], 10, 64); err != nil || r.off < 0 {
		return fileRef{}, false
	}
	r.sum = parts[1]
	if r.block, err = strconv.Atoi(parts[2]); err != nil || r.block < 0 {
		return fileRef{}, false
	}
	if r.n, err = strconv.Atoi(parts[3]); err != nil || r.n < 0 {
		return fileRef{}, false
	}
	return r, true
}

// errGone is session.file's answer for a file, or the line naming it,
// that is no longer there.
func errGone(format string, args ...any) *proto.Error {
	return proto.Errorf(proto.ErrFileGone, format, args...)
}

func (s *Server) sessionFile(p proto.SessionFileParams) (any, *proto.Error) {
	if p.Session == "" || p.Ref == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "session.file needs a session and a ref")
	}
	ref, ok := parseRef(p.Ref)
	if !ok || p.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "session.file: bad ref %q", p.Ref)
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		return nil, perr
	}
	var tr *agent.Transcript
	if a := sess.Agent(); a != nil {
		if m := agent.ManifestOf(a); m != nil {
			tr = m.Transcript
		}
	}
	root, path := sess.Transcript()
	if tr == nil || tr.Files == nil || path == "" {
		return nil, errGone("the session's conversation names no files")
	}
	line, err := transcriptLine(root, path, ref.off)
	if err != nil {
		return nil, errGone("the transcript's line is no longer there")
	}
	if sum := sha256.Sum256(line); hex.EncodeToString(sum[:])[:lineSum] != ref.sum {
		return nil, errGone("the transcript's line is no longer there")
	}
	lf, err := tr.FileAt(line, ref.block, ref.n)
	if err != nil {
		return nil, errGone("%v", err)
	}
	if lf.Path == "" {
		res := proto.SessionFileResult{Name: "image", Type: lf.Type, Size: int64(len(lf.Data)), Sendable: len(lf.Data) <= proto.MaxSentFile}
		return chunk(res, bytes.NewReader(lf.Data), p)
	}
	return sentFile(lf.Path, p)
}

// transcriptLine is the whole line of the transcript at off: at most
// transcriptRead bytes, ended by a newline.
func transcriptLine(root, path string, off int64) ([]byte, error) {
	f, fi, err := session.OpenTranscript(root, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if off >= fi.Size() {
		return nil, io.EOF
	}
	if off > 0 {
		// A line starts after a newline.
		var prev [1]byte
		if _, err := f.ReadAt(prev[:], off-1); err != nil || prev[0] != '\n' {
			return nil, fmt.Errorf("no line at %d", off)
		}
	}
	data := make([]byte, min(fi.Size()-off, transcriptRead))
	n, err := f.ReadAt(data, off)
	if n == 0 && err != nil {
		return nil, err
	}
	i := bytes.IndexByte(data[:n], '\n')
	if i < 0 {
		return nil, fmt.Errorf("no whole line at %d", off)
	}
	return data[:i], nil
}

// sentFile is session.file's answer for a sent file's path: absolute,
// symlinks evaluated, a regular file opened without following a link
// swapped in or blocking on a FIFO, of a type tm sends and at most
// proto.MaxSentFile bytes for its bytes; a stat answers any regular
// file's name and size.
func sentFile(path string, p proto.SessionFileParams) (any, *proto.Error) {
	if !filepath.IsAbs(path) {
		return nil, proto.Errorf(proto.ErrRefused, "%s: not an absolute path", path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errGone("%s is no longer there", path)
		}
		return nil, proto.Errorf(proto.ErrRefused, "%s: %v", path, err)
	}
	li, err := os.Lstat(real)
	if err != nil {
		return nil, errGone("%s is no longer there", path)
	}
	if !li.Mode().IsRegular() {
		return nil, proto.Errorf(proto.ErrRefused, "%s is not a regular file", path)
	}
	// The type is the name's the transcript gives and the file's own:
	// a link named .png to a key is no image.
	typ := agent.SentFileType(path)
	if agent.SentFileType(real) != typ {
		typ = ""
	}
	res := proto.SessionFileResult{Name: filepath.Base(path), Type: typ, Size: li.Size(), MTime: li.ModTime(),
		Sendable: typ != "" && li.Size() <= proto.MaxSentFile}
	if p.Stat {
		return res, nil
	}
	if !res.Sendable {
		return nil, proto.Errorf(proto.ErrRefused, "%s: tm doesn't send a file of this type or size", path)
	}
	r, err := os.OpenRoot(filepath.Dir(real))
	if err != nil {
		return nil, errGone("%s is no longer there", path)
	}
	defer r.Close()
	f, err := fsx.OpenInNoBlock(r, filepath.Base(real))
	if err != nil {
		return nil, errGone("%s is no longer there", path)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(fi, li) {
		return nil, proto.Errorf(proto.ErrRefused, "%s changed while it was opened", path)
	}
	res.Size, res.MTime = fi.Size(), fi.ModTime()
	if res.Size > proto.MaxSentFile {
		return nil, proto.Errorf(proto.ErrRefused, "%s: tm doesn't send a file of this type or size", path)
	}
	return chunk(res, f, p)
}

// chunk is res with the chunk of r at p.Offset: at most
// proto.SentFileChunk bytes, none for a stat.
func chunk(res proto.SessionFileResult, r io.ReaderAt, p proto.SessionFileParams) (any, *proto.Error) {
	if p.Stat {
		return res, nil
	}
	if !res.Sendable {
		return nil, proto.Errorf(proto.ErrRefused, "tm doesn't send a file of this type or size")
	}
	if p.Offset > res.Size {
		return nil, proto.Errorf(proto.ErrBadParams, "session.file: offset %d is past the end (%d)", p.Offset, res.Size)
	}
	buf := make([]byte, min(res.Size-p.Offset, proto.SentFileChunk))
	n, err := r.ReadAt(buf, p.Offset)
	if n < len(buf) && err != nil {
		return nil, errGone("the file changed while it was read")
	}
	res.Offset, res.Data = p.Offset, buf[:n]
	return res, nil
}
