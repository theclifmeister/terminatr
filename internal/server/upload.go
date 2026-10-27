package server

// session.upload (docs/SPEC.md §3.8, Attaching files): a file the human
// gives a session's prompt in the Mac app's composer. The app sends its
// bytes, never a path; the server keeps the chunks in memory until the
// last is in, checks the bytes are of the type the name says, and only
// then writes the file into the session's project's uploads/, the
// folder only the human and tm write, which the coordinator reads and
// threads read by absolute path. Its ref is the stored file's name, and
// session.prompt's Files names it after the prompt's text by its path:
// agents take a file by its path in the prompt (Claude reads an image
// or a PDF so).

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Bounds of the uploads in flight: how many at once, their declared
// bytes in all, and how long one waits for its next chunk.
const (
	maxUploads      = 8
	maxUploadsBytes = 64 << 20
	uploadIdle      = 10 * time.Minute
	// maxUploadName bounds a stored file's name, less its time stamp.
	maxUploadName = 80
)

// uploadTypes are the types session.upload takes, by extension: the
// images and PDFs agents read, and text. A file of any other name is
// refused.
var uploadTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".tsv": "text/tab-separated-values",
	".log": "text/plain", ".json": "application/json", ".yaml": "application/yaml", ".yml": "application/yaml",
	".toml": "application/toml", ".xml": "application/xml", ".html": "text/html", ".diff": "text/x-diff",
	".patch": "text/x-diff",
}

// uploadType is the media type of a file named name, "" for a name
// session.upload refuses.
func uploadType(name string) string {
	if strings.HasPrefix(name, ".") {
		return ""
	}
	return uploadTypes[strings.ToLower(filepath.Ext(name))]
}

// uploadName is name as uploads/ stores it: its base, with only
// letters, digits, '.', '-' and '_' (others become '-'), no leading
// dot, at most maxUploadName bytes keeping the extension.
func uploadName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	dash := false
	for _, r := range name {
		ok := r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
		if !ok {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = true
			continue
		}
		b.WriteRune(r)
		dash = false
	}
	out := strings.TrimLeft(b.String(), ".-")
	if len(out) > maxUploadName {
		ext := filepath.Ext(out)
		if len(ext) > 10 {
			ext = ""
		}
		out = out[:maxUploadName-len(ext)] + ext
	}
	return out
}

// sniffs says whether data is of type typ: an image's or a PDF's
// signature, or text (UTF-8 without NUL) for the rest.
func sniffs(typ string, data []byte) bool {
	if strings.HasPrefix(typ, "image/") || typ == "application/pdf" {
		return http.DetectContentType(data) == typ
	}
	return utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
}

// uploads are the uploads in flight.
type uploads struct {
	mu sync.Mutex
	m  map[string]*upload
}

// upload is a file being taken: for session of project, stored as
// name, of size bytes, data those in so far.
type upload struct {
	session, project, name, typ string
	size                        int64
	data                        []byte
	at                          time.Time
}

// sweep drops the uploads idle past uploadIdle; under u.mu.
func (u *uploads) sweep(now time.Time) {
	for id, up := range u.m {
		if now.Sub(up.at) > uploadIdle {
			delete(u.m, id)
		}
	}
}

// begin takes a new upload, refused past the bounds.
func (u *uploads) begin(up *upload) (string, *proto.Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep(up.at)
	var held int64
	for _, o := range u.m {
		held += o.size
	}
	if len(u.m) >= maxUploads || held+up.size > maxUploadsBytes {
		return "", proto.Errorf(proto.ErrRefused, "too many files uploading at once; try again when they are in")
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	if u.m == nil {
		u.m = map[string]*upload{}
	}
	u.m[id] = up
	return id, nil
}

// add appends data at offset to upload id of session, and hands the
// upload back, removed, once all its bytes are in.
func (u *uploads) add(id, session string, offset int64, data []byte, now time.Time) (int64, *upload, *proto.Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep(now)
	up := u.m[id]
	if up == nil || up.session != session {
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: no upload %q of session %s (it waited too long?): send the file again", id, session)
	}
	if offset != int64(len(up.data)) {
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: chunk at %d, but %d bytes are in", offset, len(up.data))
	}
	if int64(len(up.data)+len(data)) > up.size {
		delete(u.m, id)
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: more than the %d bytes it said", up.size)
	}
	up.data = append(up.data, data...)
	up.at = now
	got := int64(len(up.data))
	if got < up.size {
		return got, nil, nil
	}
	delete(u.m, id)
	return got, up, nil
}

// upload is session.upload's answer.
func (s *Server) upload(p proto.SessionUploadParams, cc callCtx) (any, *proto.Error) {
	if _, _, ok := remoteID(p.Session); ok {
		return nil, proto.Errorf(proto.ErrRefused, "a paired server's session takes no files")
	}
	if len(p.Data) > proto.UploadChunk {
		return nil, proto.Errorf(proto.ErrBadParams, "session.upload: a chunk is at most %d bytes", proto.UploadChunk)
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		return nil, perr
	}
	info := sess.Info()
	now := time.Now()
	id := p.Upload
	if id == "" {
		if p.Offset != 0 {
			return nil, proto.Errorf(proto.ErrBadParams, "session.upload: a new upload starts at 0")
		}
		if info.Project == "" {
			return nil, proto.Errorf(proto.ErrRefused, "session %s is in no project: files go to a project's uploads/", p.Session)
		}
		name := uploadName(p.Name)
		typ := uploadType(name)
		if typ == "" {
			return nil, proto.Errorf(proto.ErrBadParams, "%s: tm takes images (PNG, JPEG, GIF, WebP), PDFs and text files", shownName(p.Name))
		}
		if p.Size <= 0 || p.Size > proto.MaxUpload {
			return nil, proto.Errorf(proto.ErrBadParams, "%s: a file is 1 byte to %d MB", name, proto.MaxUpload>>20)
		}
		id, perr = s.uploads.begin(&upload{session: p.Session, project: info.Project, name: name, typ: typ, size: p.Size, at: now})
		if perr != nil {
			return nil, perr
		}
	}
	got, up, perr := s.uploads.add(id, p.Session, p.Offset, p.Data, now)
	if perr != nil {
		return nil, perr
	}
	res := proto.SessionUploadResult{Upload: id, Received: got}
	if up == nil {
		return res, nil
	}
	if !sniffs(up.typ, up.data) {
		return nil, proto.Errorf(proto.ErrBadParams, "%s isn't a %s file", up.name, up.typ)
	}
	dir, err := project.Dir(up.project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "project %s: %v", up.project, err)
	}
	ref, err := storeUpload(filepath.Join(dir, "uploads"), up.name, up.data, now)
	if err != nil {
		s.log.Printf("session.upload %s: %v", p.Session, err)
		return nil, proto.Errorf(proto.ErrInternal, "storing %s: %v", up.name, err)
	}
	s.journalIn(up.project, cc.who(), "session.upload", sessionRef(info), ref+" ("+strconv.FormatInt(up.size, 10)+" bytes)")
	res.Ref, res.Name, res.Type, res.Size = ref, ref, up.typ, up.size
	return res, nil
}

// shownName is a name the app gave, for an error.
func shownName(name string) string {
	if n := uploadName(name); n != "" {
		return n
	}
	return "the file"
}

// storeUpload writes data into dir as "<UTC time>-<name>", or with
// "-2", "-3"… after the time when that is taken, never through a link,
// and answers the name it took.
func storeUpload(dir, name string, data []byte, now time.Time) (string, error) {
	if err := fsx.MkdirNoLink(dir, 0o700); err != nil {
		return "", err
	}
	stamp := now.UTC().Format("20060102-150405")
	for n := 1; n <= 100; n++ {
		ref := stamp + "-" + name
		if n > 1 {
			ref = stamp + "-" + strconv.Itoa(n) + "-" + name
		}
		path := filepath.Join(dir, ref)
		f, err := fsx.OpenNoLink(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(path)
			return "", werr
		}
		return ref, nil
	}
	return "", fmt.Errorf("no free name for %s", name)
}

// promptFiles is text with the uploads refs name in project's
// uploads/, each on an "Attached: <path>" line after it: what an agent
// reads them by. A ref is a file's name there, as session.upload
// answered it: no folder, no link, a type it takes.
func promptFiles(slug, text string, refs []string) (string, *proto.Error) {
	if len(refs) > proto.MaxPromptFiles {
		return "", proto.Errorf(proto.ErrBadParams, "a prompt takes at most %d files", proto.MaxPromptFiles)
	}
	if slug == "" {
		return "", proto.Errorf(proto.ErrRefused, "the session is in no project: it takes no files")
	}
	dir, err := project.Dir(slug)
	if err != nil {
		return "", proto.Errorf(proto.ErrRefused, "project %s: %v", slug, err)
	}
	dir = filepath.Join(dir, "uploads")
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == "" || ref != filepath.Base(ref) || strings.ContainsAny(ref, `/\`) || uploadName(ref) != ref || uploadType(ref) == "" {
			return "", proto.Errorf(proto.ErrBadParams, "bad file ref %q", ref)
		}
		path := filepath.Join(dir, ref)
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return "", proto.Errorf(proto.ErrFileGone, "%s is no longer in the project's uploads: attach it again", ref)
		}
		lines = append(lines, "Attached: "+path)
	}
	text = strings.TrimRight(text, " \t\r\n")
	if len(lines) == 0 {
		return text, nil
	}
	if text != "" {
		text += "\n\n"
	}
	return text + strings.Join(lines, "\n"), nil
}
