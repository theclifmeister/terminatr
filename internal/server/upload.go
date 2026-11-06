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
	"context"
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

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
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

// Bounds of what paired servers upload (docs/SPEC.md §16.12), within those
// above: one holds at most half the bytes and half the uploads, so another
// has room; together they leave room for a file and an upload of this
// server's own human; and one stores at most peerUploadBudget an hour. Theirs
// wait less for a next chunk, and a dropped link drops them at once.
const (
	maxPeerUploads        = maxUploads / 2
	maxPeerUploadsBytes   = maxUploadsBytes / 2
	maxPeersUploads       = maxUploads - 1
	maxPeersUploadsBytes  = maxUploadsBytes - proto.MaxUpload
	peerUploadIdle        = 2 * time.Minute
	peerUploadBudget      = 1 << 30
	peerUploadBudgetEvery = time.Hour
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
	// budgets are what each paired server declared this hour.
	budgets map[string]*uploadBudget
}

// uploadBudget is the bytes a paired server has declared since at.
type uploadBudget struct {
	at    time.Time
	bytes int64
}

// upload is a file being taken: for session of project, stored as
// name, of size bytes, data those in so far.
type upload struct {
	// dir is the session's project's folder (its program's for a
	// director), where uploads/ is.
	session, dir, name, typ string
	size                    int64
	data                    []byte
	at                      time.Time
	// peer is the paired server's alias whose upload it is, "" for this
	// server's own human; unwatch stops watching its link.
	peer    string
	unwatch func() bool
}

// idle is how long up waits for its next chunk.
func (up *upload) idle() time.Duration {
	if up.peer != "" {
		return peerUploadIdle
	}
	return uploadIdle
}

// watchLink drops upload id when gone ends.
func (u *uploads) watchLink(id string, gone context.Context) {
	stop := context.AfterFunc(gone, func() { u.drop(id) })
	u.mu.Lock()
	defer u.mu.Unlock()
	if up := u.m[id]; up != nil {
		up.unwatch = stop
	} else {
		stop()
	}
}

// remove takes upload id out; under u.mu.
func (u *uploads) remove(id string) {
	if up := u.m[id]; up != nil && up.unwatch != nil {
		up.unwatch()
	}
	delete(u.m, id)
}

// drop forgets upload id: its link dropped.
func (u *uploads) drop(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.remove(id)
}

// dropPeer forgets the uploads of the paired server alias.
func (u *uploads) dropPeer(alias string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for id, up := range u.m {
		if up.peer == alias {
			u.remove(id)
		}
	}
}

// sweep drops the uploads idle past uploadIdle; under u.mu.
func (u *uploads) sweep(now time.Time) {
	for id, up := range u.m {
		if now.Sub(up.at) > up.idle() {
			u.remove(id)
		}
	}
}

// begin takes a new upload, refused past the bounds.
func (u *uploads) begin(up *upload) (string, *proto.Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep(up.at)
	var held, peersHeld, peerHeld int64
	peers, peer := 0, 0
	for _, o := range u.m {
		held += o.size
		if o.peer != "" {
			peersHeld += o.size
			peers++
		}
		if up.peer != "" && o.peer == up.peer {
			peerHeld += o.size
			peer++
		}
	}
	full := len(u.m) >= maxUploads || held+up.size > maxUploadsBytes
	if up.peer != "" {
		full = full || peers >= maxPeersUploads || peersHeld+up.size > maxPeersUploadsBytes ||
			peer >= maxPeerUploads || peerHeld+up.size > maxPeerUploadsBytes
	}
	if full {
		return "", proto.Errorf(proto.ErrRefused, "too many files uploading at once; try again when they are in")
	}
	if up.peer != "" {
		b := u.budgets[up.peer]
		if b == nil || up.at.Sub(b.at) > peerUploadBudgetEvery {
			b = &uploadBudget{at: up.at}
			if u.budgets == nil {
				u.budgets = map[string]*uploadBudget{}
			}
			u.budgets[up.peer] = b
		}
		if b.bytes+up.size > peerUploadBudget {
			return "", proto.Errorf(proto.ErrRefused, "too many files from your server this hour; try again later")
		}
		b.bytes += up.size
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

// add appends data at offset to upload id of session, peer's ("" for this
// server's own human), and hands the upload back, removed, once all its bytes are in.
func (u *uploads) add(id, session, peer string, offset int64, data []byte, now time.Time) (int64, *upload, *proto.Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep(now)
	up := u.m[id]
	if up == nil || up.session != session || up.peer != peer {
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: no upload %q of session %s (it waited too long?): send the file again", id, session)
	}
	if offset != int64(len(up.data)) {
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: chunk at %d, but %d bytes are in", offset, len(up.data))
	}
	if int64(len(up.data)+len(data)) > up.size {
		u.remove(id)
		return 0, nil, proto.Errorf(proto.ErrBadParams, "session.upload: more than the %d bytes it said", up.size)
	}
	up.data = append(up.data, data...)
	up.at = now
	got := int64(len(up.data))
	if got < up.size {
		return got, nil, nil
	}
	u.remove(id)
	return got, up, nil
}

// upload is session.upload's answer. A paired server's session goes to that
// server, which holds the checks.
func (s *Server) upload(p proto.SessionUploadParams, cc callCtx) (any, *proto.Error) {
	if len(p.Data) > proto.UploadChunk {
		return nil, proto.Errorf(proto.ErrBadParams, "session.upload: a chunk is at most %d bytes", proto.UploadChunk)
	}
	if _, _, ok := remoteID(p.Session); ok {
		return s.remotes.upload(s.remoteCanon(p.Session), p)
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		return nil, perr
	}
	return s.takeChunk(sess.Info(), p, cc.who(), "", nil)
}

// takeChunk takes a chunk of a file for session info's prompt, stores the
// file once its bytes are in, and answers where the upload stands. who is
// journaled; peer is the paired server's alias, "" for this server's human,
// and gone, with one, ends with its link.
func (s *Server) takeChunk(info proto.SessionInfo, p proto.SessionUploadParams, who caller.Caller, peer string, gone context.Context) (proto.SessionUploadResult, *proto.Error) {
	var none proto.SessionUploadResult
	now := time.Now()
	id := p.Upload
	if id == "" {
		if p.Offset != 0 {
			return none, proto.Errorf(proto.ErrBadParams, "session.upload: a new upload starts at 0")
		}
		if info.Project == "" {
			return none, proto.Errorf(proto.ErrRefused, "session %s is in no project: files go to a project's uploads/", p.Session)
		}
		name := uploadName(p.Name)
		typ := uploadType(name)
		if typ == "" {
			return none, proto.Errorf(proto.ErrBadParams, "%s: tm takes images (PNG, JPEG, GIF, WebP), PDFs and text files", shownName(p.Name))
		}
		if p.Size <= 0 || p.Size > proto.MaxUpload {
			return none, proto.Errorf(proto.ErrBadParams, "%s: a file is 1 byte to %d MB", name, proto.MaxUpload>>20)
		}
		dir, err := sessionDir(info)
		if err != nil {
			if peer != "" {
				return none, proto.Errorf(proto.ErrRefused, "session %s takes no files", p.Session)
			}
			return none, proto.Errorf(proto.ErrRefused, "session %s: %v", p.Session, err)
		}
		var perr *proto.Error
		id, perr = s.uploads.begin(&upload{session: p.Session, dir: dir, name: name, typ: typ, size: p.Size, at: now, peer: peer})
		if perr != nil {
			return none, perr
		}
		if gone != nil {
			s.uploads.watchLink(id, gone)
		}
	}
	got, up, perr := s.uploads.add(id, p.Session, peer, p.Offset, p.Data, now)
	if perr != nil {
		return none, perr
	}
	res := proto.SessionUploadResult{Upload: id, Received: got}
	if up == nil {
		return res, nil
	}
	if !sniffs(up.typ, up.data) {
		return none, proto.Errorf(proto.ErrBadParams, "%s isn't a %s file", up.name, up.typ)
	}
	ref, err := storeUpload(filepath.Join(up.dir, "uploads"), up.name, up.data, now)
	if err != nil {
		s.log.Printf("session.upload %s: %v", p.Session, err)
		// A path stays here: a paired server reads these words.
		if peer != "" {
			return none, proto.Errorf(proto.ErrInternal, "storing %s failed", up.name)
		}
		return none, proto.Errorf(proto.ErrInternal, "storing %s: %v", up.name, err)
	}
	s.journalSession(info, who, "session.upload", sessionRef(info), ref+" ("+strconv.FormatInt(up.size, 10)+" bytes)")
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

// promptFiles is text with the uploads refs name in folder's uploads/
// (the session's project's, sessionDir), each on an "Attached: <path>" line after it: what an agent
// reads them by.
func promptFiles(folder, text string, refs []string) (string, *proto.Error) {
	paths, perr := promptPaths(folder, refs)
	if perr != nil {
		return "", perr
	}
	return attachedText(text, paths), nil
}

// promptPaths are the paths of the uploads refs name in folder's uploads/. A
// ref is a file's name there, as session.upload answered it: no folder, no
// link, a type it takes.
func promptPaths(folder string, refs []string) ([]string, *proto.Error) {
	if len(refs) > proto.MaxPromptFiles {
		return nil, proto.Errorf(proto.ErrBadParams, "a prompt takes at most %d files", proto.MaxPromptFiles)
	}
	if folder == "" {
		return nil, proto.Errorf(proto.ErrRefused, "the session is in no project: it takes no files")
	}
	dir := filepath.Join(folder, "uploads")
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == "" || ref != filepath.Base(ref) || strings.ContainsAny(ref, `/\`) || uploadName(ref) != ref || uploadType(ref) == "" {
			return nil, proto.Errorf(proto.ErrBadParams, "bad file ref %q", ref)
		}
		path := filepath.Join(dir, ref)
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return nil, proto.Errorf(proto.ErrFileGone, "%s is no longer in the project's uploads: attach it again", ref)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// attachedText is text with each of paths on an "Attached: <path>" line after
// it.
func attachedText(text string, paths []string) string {
	text = strings.TrimRight(text, " \t\r\n")
	if len(paths) == 0 {
		return text
	}
	if text != "" {
		text += "\n\n"
	}
	return text + "Attached: " + strings.Join(paths, "\nAttached: ")
}
