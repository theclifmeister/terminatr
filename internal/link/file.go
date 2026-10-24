package link

// Files over links (docs/SPEC.md §16.13): a coordinator, or the user,
// sends a file too big for a message's text with one message (tm link
// send … --file). The offer goes with its message: held with it on an
// ask link, queued with it over the rate. The receiving project's user
// accepts it (a link-file item) unless that side's entry takes files
// without asking (files = "auto"), or refuses it (files = "off"). The
// file lands beside its message, messages/<peer>/<M-id>/<name>, where
// the sender keeps the copy it took when it sent it. On this server the
// transfer is a copy; to a paired server the offer, the answer, the
// receipt and a cancel go through the outbox, and the receiving server
// pulls the bytes over the link (filefetch.go). Each transfer is kept
// in state/link/files.json by each side, so restarts resume it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// KindFile is a file over a link: offered to this project and waiting
// for the user (needs_user, done once answered), or, without needing the
// user, one that arrived on its own (files = "auto") or ended here.
const KindFile = "link-file"

// Transfer states, shared by both sides' copies.
const (
	FileHeld     = "held"    // with its message, which waits for the user's OK
	FileQueued   = "queued"  // with its message over the rate, or in the outbox
	FileOffered  = "offered" // the receiving user hasn't answered
	FileAccepted = "accepted"
	// FileWaiting: accepted, but another transfer runs on the link (or
	// the server's other slot), or the receiver's disk is short (Wait).
	FileWaiting     = "waiting"
	FileReceiving   = "receiving"
	FileSending     = "sending"
	FileReceived    = "received"
	FileDeclined    = "declined"
	FileFailed      = "failed" // Code says why
	FileCancelled   = "cancelled"
	FileExpired     = "expired"
	FileUndelivered = "undelivered" // the offer didn't reach the other side (Code)
)

// Limits fixed in v1.
const (
	// OfferExpiry is how long an offer waits for its answer, on both
	// sides; TransferExpiry how long an accepted one may take.
	OfferExpiry    = 7 * 24 * time.Hour
	TransferExpiry = 24 * time.Hour
	// MaxStalls is how many stalls in a row fail a transfer.
	MaxStalls = 5
	// keepEnded is how long an ended transfer stays in the store; its
	// message still names the file.
	keepEnded = 30 * 24 * time.Hour
)

// Transfer is one file over a link, as one side keeps it.
type Transfer struct {
	// ID is X-<n> on this server, X-<the sending server's fingerprint>-<n>
	// across servers. A local transfer has two copies, one per project.
	ID string `json:"id"`
	// Out: this side sends it. Project is the project here, Peer the
	// other ("mini/api" on a paired server), Message the message it goes
	// with.
	Out     bool   `json:"out"`
	Project string `json:"project"`
	Peer    string `json:"peer"`
	Message string `json:"message"`
	// Name is the file's name in the message's folder; Size its bytes,
	// SHA256 its hash in hex.
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	By     string `json:"by"`
	State  string `json:"state"`
	// Auto: taken without asking (files = "auto").
	Auto     bool      `json:"auto,omitempty"`
	Created  time.Time `json:"created"`
	Accepted time.Time `json:"accepted,omitzero"`
	Ended    time.Time `json:"ended,omitzero"`
	// Offset is the bytes on disk (the receiver) or acked (the sender);
	// Rate the last pace in bytes a second, while it runs.
	Offset int64   `json:"offset,omitempty"`
	Rate   float64 `json:"rate,omitempty"`
	// Stalls counts the stalls in a row; Retried, that the one fresh
	// retry after bad content was used; Wait why it waits ("disk",
	// "link", "turn"); Code why it failed or wasn't delivered.
	Stalls  int    `json:"stalls,omitempty"`
	Retried bool   `json:"retried,omitempty"`
	Wait    string `json:"wait,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Open reports a transfer that hasn't ended.
func (t *Transfer) Open() bool {
	switch t.State {
	case FileReceived, FileDeclined, FileFailed, FileCancelled, FileExpired, FileUndelivered:
		return false
	}
	return true
}

// Active reports a transfer accepted and not ended: it runs or waits
// its turn.
func (t *Transfer) Active() bool {
	switch t.State {
	case FileAccepted, FileWaiting, FileReceiving, FileSending:
		return true
	}
	return false
}

// FileWords are the codes a transfer fails or isn't delivered with, as
// both sides' copies and the sender's link-undelivered item say them;
// {to} is the receiving project, {from} the sending one.
var FileWords = map[string]string{
	"not-linked":    "{to} doesn't take messages from {from}: the user of its server sets that side",
	"link-paused":   "the link is paused on {to}'s server; its user resumes it there",
	"files-off":     "{to} takes no files from {from}: its user sets that side (files)",
	"too-big":       "it is over the size {to}'s side takes (link_file_mb)",
	"bad-message":   "{to}'s server didn't take the offer: a tm before 0.47.0 doesn't take files; send the text, or update that server",
	"expired":       "it wasn't done in time",
	"bad-content":   "the bytes that came didn't match the file's size and hash, twice",
	"stalled":       "the link stalled too often while it was sent",
	"no-space":      "{to}'s server didn't have the disk space for it",
	"gone":          "the sender's copy was deleted",
	"unknown-file":  "the sender's server no longer offers it",
	"shutting-down": "a server stopped while it was sent",
	"no-message":    "the message it went with wasn't delivered",
	"refused":       "{to}'s server refused it",
}

// fileWords is code in words for a transfer from from to to.
func fileWords(code, to, from string) string {
	w, ok := FileWords[code]
	if !ok {
		w = FileWords["refused"]
	}
	return replaceWords(w, to, from)
}

// fileIDRE is a transfer's id; the middle, across servers, is the
// sending server's fingerprint.
var fileIDRE = regexp.MustCompile(`^X-(?:([0-9A-F]{4})-)?[0-9]{1,12}$`)

// ValidFileID reports whether s is a transfer's id.
func ValidFileID(s string) bool { return fileIDRE.MatchString(s) }

// MB is a file's size in the words the views use: "640 bytes", "12.0
// KB", "4.2 MB".
func MB(n int64) string {
	if n < 1<<20 {
		return KB(int(n))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// fileStore is state/link/files.json: every transfer of this server's
// projects, both ways, under its own lock. It is taken inside the
// links' store, never the other way round.
type fileStore struct {
	Transfers []*Transfer `json:"transfers,omitempty"`
}

func linkState(parts ...string) (string, error) {
	d, err := home.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{d, "state", "link"}, parts...)...), nil
}

func filesPath() (string, error) { return linkState("files.json") }

// withFiles runs fn on the file store under its lock and saves it when
// fn returns nil.
func withFiles(fn func(fs *fileStore) error) error {
	file, err := filesPath()
	if err != nil {
		return err
	}
	if err := fsx.EnsurePrivateDir(filepath.Dir(file)); err != nil {
		return err
	}
	unlock, err := mdfile.Lock(file)
	if err != nil {
		return err
	}
	defer unlock()
	st := &fileStore{}
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, st)
	}
	if err := fn(st); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(file, append(b, '\n'), 0o600)
}

// readFiles runs fn on the file store as it is, unlocked: for a look.
func readFiles(fn func(fs *fileStore)) {
	st := &fileStore{}
	if file, err := filesPath(); err == nil {
		if b, err := os.ReadFile(file); err == nil {
			json.Unmarshal(b, st)
		}
	}
	fn(st)
}

// find is project slug's copy of transfer id; nil for none.
func (st *fileStore) find(slug, id string) *Transfer {
	for _, t := range st.Transfers {
		if t.ID == id && t.Project == slug {
			return t
		}
	}
	return nil
}

// add keeps t, dropping transfers that ended long ago.
func (st *fileStore) add(t *Transfer) {
	cut := now().Add(-keepEnded)
	st.Transfers = slices.DeleteFunc(st.Transfers, func(x *Transfer) bool {
		return !x.Open() && !x.Ended.IsZero() && x.Ended.Before(cut)
	})
	st.Transfers = append(st.Transfers, t)
}

// end moves t to state, now, with code.
func (t *Transfer) end(state, code string) {
	t.State, t.Code, t.Ended, t.Rate, t.Wait = state, code, now().UTC().Truncate(time.Second), 0, ""
}

// Transfers are p's transfers with other ("" for all), newest first.
func Transfers(p *project.Project, other string) []Transfer {
	other = Canon(other)
	var out []Transfer
	readFiles(func(st *fileStore) {
		for _, t := range st.Transfers {
			if t.Project == p.Slug && (other == "" || t.Peer == other) {
				out = append(out, *t)
			}
		}
	})
	slices.SortStableFunc(out, func(a, b Transfer) int { return b.Created.Compare(a.Created) })
	return out
}

// TransferOf is p's transfer of message m's file; false for none (a
// message with no file, or one whose transfer was dropped from the
// store long after it ended).
func TransferOf(p *project.Project, m *Message) (Transfer, bool) {
	var t Transfer
	found := false
	if m.Transfer == "" {
		return t, false
	}
	readFiles(func(st *fileStore) {
		if x := st.find(p.Slug, m.Transfer); x != nil {
			t, found = *x, true
		}
	})
	return t, found
}

// FileState is a transfer's state as the views say it: "receiving 38%",
// "failed: stalled", "waiting: disk".
func FileState(t Transfer) string {
	switch t.State {
	case FileReceiving, FileSending:
		if t.Size > 0 {
			return fmt.Sprintf("%s %d%%", t.State, t.Offset*100/t.Size)
		}
	case FileFailed, FileUndelivered:
		if t.Code != "" {
			return t.State + ": " + t.Code
		}
	case FileWaiting:
		if t.Wait != "" {
			return "waiting: " + t.Wait
		}
	case FileOffered:
		if !t.Out {
			return "offered · waits for you"
		}
	case FileAccepted:
		if t.Auto {
			return "accepted (auto)"
		}
	}
	return t.State
}

// FileSummary is the item that asks the user about an offer: fixed
// words, the file's name (cleaned on both sides) and size.
func FileSummary(t *Transfer) string {
	return fmt.Sprintf("%s offers a file with %s (%s, %s); accept or decline it in the Messages tab (or tm link accept|decline %s)", Shown(t.Peer), t.Message, t.Name, MB(t.Size), t.ID)
}

// doneFile moves the link-file items of transfer id that need the user
// to done.
func doneFile(p *project.Project, id string) {
	items, _ := p.Inbox()
	for _, it := range items {
		if it.Kind == KindFile && it.Message == id && it.NeedsUser {
			p.DoneItem(it.ID)
		}
	}
}

// fileDir is the folder of message id's files in p's folder with other.
func fileDir(p *project.Project, other, id string) string {
	return filepath.Join(dir(p, other), id)
}

// source is a file checked for sending, open: its cleaned name and size.
type source struct {
	f    *os.File
	name string
	size int64
}

// checkFile opens the file spec names for p to send: a library file of
// one of p's threads (<thread>/<file>), a file of one of p's messages
// (M-12/<file>), or, for the human only, any file by its absolute path.
// It must be a regular file with one link and at most maxMB.
func checkFile(c caller.Caller, p *project.Project, spec string, maxMB int) (*source, error) {
	var src string
	if filepath.IsAbs(spec) {
		if c.IsAgent() {
			return nil, refuse("bad-attachment", "a coordinator sends a library file (<thread>/<file>) or a file of a message (M-12/<file>); any other file is the user's to send")
		}
		src = filepath.Clean(spec)
	} else {
		id, name, ok := strings.Cut(spec, "/")
		if !ok || name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") || !textsafe.Safe(name) {
			return nil, refuse("bad-attachment", "%q: name a library file as <thread>/<file> (tm library list), or a message's as M-12/<file>", spec)
		}
		switch {
		case thread.ValidID(id):
			src = thread.Path(p, id, "library", name)
		case ValidID(id):
			m, err := Read(p, id)
			if err != nil {
				return nil, refuse("bad-attachment", "%s: %s has no message %s", spec, p.Slug, id)
			}
			if src, err = FilePath(p, m, name); err != nil {
				return nil, refuse("bad-attachment", "%s: %v", spec, err)
			}
		default:
			return nil, refuse("bad-attachment", "%q: name a library file as <thread>/<file> (tm library list), or a message's as M-12/<file>", spec)
		}
	}
	fi, err := os.Lstat(src)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, refuse("bad-attachment", "%s is not a file", spec)
	}
	f, err := fsx.OpenNoLink(src, os.O_RDONLY, 0)
	if err != nil {
		return nil, refuse("bad-attachment", "%s can't be read", spec)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	ofi, err := f.Stat()
	if err != nil || !os.SameFile(fi, ofi) || !ofi.Mode().IsRegular() {
		return nil, refuse("bad-attachment", "%s changed while it was opened", spec)
	}
	if n, err := fsx.FileLinks(f); err != nil || n > 1 {
		return nil, refuse("bad-attachment", "%s has other hard links; a file sent over a link may not", spec)
	}
	if ofi.Size() > int64(maxMB)<<20 {
		return nil, refuse("too-big", "%s is %s; a file over this link is at most %d MB (link_file_mb)", spec, MB(ofi.Size()), maxMB)
	}
	ok = true
	return &source{f: f, name: CleanName(filepath.Base(src)), size: ofi.Size()}, nil
}

// snapshot copies an open source into a private temp file, hashing it:
// the copy the sender serves, whatever happens to the original. The
// caller moves it into place or removes it.
func snapshot(s *source) (tmp, sum string, err error) {
	d, err := linkState("snap")
	if err != nil {
		return "", "", err
	}
	if err := fsx.EnsurePrivateDir(d); err != nil {
		return "", "", err
	}
	out, err := os.CreateTemp(d, "snap-*")
	if err != nil {
		return "", "", err
	}
	tmp = out.Name()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(s.f, s.size+1))
	if err == nil && n != s.size {
		err = refuse("bad-attachment", "%s changed while it was copied", s.name)
	}
	if err == nil {
		err = out.Chmod(0o644)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	return tmp, hex.EncodeToString(h.Sum(nil)), nil
}

// place moves the file at tmp into p's folder of message id with other,
// under name or the first free name-2, name-3 …, and says the name it
// took. The folder is made without following links, and the move
// doesn't replace anything there.
func place(p *project.Project, other, id, name, tmp string) (string, error) {
	d := fileDir(p, other, id)
	if err := os.MkdirAll(dir(p, other), 0o755); err != nil {
		return "", err
	}
	if err := fsx.MkdirNoLink(d, 0o755); err != nil {
		return "", err
	}
	got := name
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(d, got)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if n > 99 {
			return "", fmt.Errorf("%s: no free name for %s", d, name)
		}
		got = numbered(name, n)
	}
	dst := filepath.Join(d, got)
	if err := os.Rename(tmp, dst); err != nil {
		// Another file system: copy, then drop the temp.
		if err := copyNoLink(tmp, dst); err != nil {
			return "", err
		}
		os.Remove(tmp)
	}
	return got, nil
}

// copyNoLink copies src to a new dst, 0644, refusing a dst that exists
// or is a link.
func copyNoLink(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := fsx.OpenNoLink(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// FilePath is where p keeps message m's file name: one of its
// attachments, or the file sent with it once it is there.
func FilePath(p *project.Project, m *Message, name string) (string, error) {
	if slices.Contains(m.Attachments, name) {
		return filepath.Join(fileDir(p, m.Peer, m.ID), name), nil
	}
	if m.File == "" || name != m.File {
		return "", refuse("unknown-file", "%s has no file %q", m.ID, name)
	}
	if t, ok := TransferOf(p, m); ok && !m.Out && t.State != FileReceived {
		return "", refuse("not-received", "%s's file %s isn't here: it is %s", m.ID, name, FileState(t))
	}
	path := filepath.Join(fileDir(p, m.Peer, m.ID), name)
	if _, err := os.Lstat(path); err != nil {
		return "", refuse("not-received", "%s's file %s isn't here", m.ID, name)
	}
	return path, nil
}

// PeerFiles says whether the link to the paired server alias is up and
// whether its tm takes files; nil, without a server, nothing is known
// and an offer is queued anyway. The server sets it.
var PeerFiles func(alias string) (up, files bool)

// fileMode is project slug's files setting for its link to other.
func fileMode(cfg *config.Config, slug, other string) string {
	links, _ := cfg.Links(slug)
	if l, ok := links[other]; ok {
		return l.FilesMode()
	}
	return config.FilesOff
}

// fileMB is project slug's link_file_mb.
func fileMB(cfg *config.Config, slug string) int {
	if s, err := cfg.Safety(slug); err == nil && s.LinkFileMB > 0 {
		return s.LinkFileMB
	}
	return config.DefaultLinkFileMB
}

// prepareFile checks and snapshots the file of a message from p to
// toSlug: this side's files must not be off (nor, on this server, the
// other's), and the file must be within this side's size (and the
// other's). The snapshot is the caller's to place or remove.
func prepareFile(c caller.Caller, cfg *config.Config, p *project.Project, toSlug, spec string, att []attachment) (name, tmp, sum string, size int64, err error) {
	if fileMode(cfg, p.Slug, toSlug) == config.FilesOff {
		return "", "", "", 0, refuse("files-off", "%s sends no files to %s: its link's files are off (F on the link in the Network popup)", p.Slug, toSlug)
	}
	most := fileMB(cfg, p.Slug)
	if alias, _, remote := config.Remote(toSlug); remote && PeerFiles != nil {
		if up, has := PeerFiles(alias); up && !has {
			return "", "", "", 0, refuse("no-file-transfers", "%s's tm takes no files (a tm before 0.47.0): send the text, or ask that server's user to update it", alias)
		}
	}
	if _, _, remote := config.Remote(toSlug); !remote {
		if fileMode(cfg, toSlug, p.Slug) == config.FilesOff {
			return "", "", "", 0, refuse("files-off", "%s takes no files from %s: its link's files are off (F on the link in the Network popup)", toSlug, p.Slug)
		}
		most = min(most, fileMB(cfg, toSlug))
	}
	s, err := checkFile(c, p, spec, most)
	if err != nil {
		return "", "", "", 0, err
	}
	defer s.f.Close()
	if slices.ContainsFunc(att, func(a attachment) bool { return a.name == s.name }) {
		return "", "", "", 0, refuse("bad-attachment", "an attachment and the file are both named %s", s.name)
	}
	tmp, sum, err = snapshot(s)
	if err != nil {
		return "", "", "", 0, err
	}
	return s.name, tmp, sum, s.size, nil
}

// addOut keeps the sending side's copy of message m's transfer, its
// snapshot placed in m's folder; the links' store is locked.
func addOut(from *project.Project, m *Message, tmp, sum, state string) error {
	name, err := place(from, m.Peer, m.ID, m.File, tmp)
	if err != nil {
		return err
	}
	m.File = name
	t := &Transfer{ID: m.Transfer, Out: true, Project: from.Slug, Peer: m.Peer, Message: m.ID, Name: name, Size: m.FileSize, SHA256: sum,
		By: m.By, State: state, Created: m.Created}
	return withFiles(func(st *fileStore) error {
		st.add(t)
		return nil
	})
}

// setOut moves p's copy of transfer id, sending, to state when it is in
// one of from (any, when from is empty).
func setOut(p *project.Project, id, state string, from ...string) {
	withFiles(func(st *fileStore) error {
		if t := st.find(p.Slug, id); t != nil && t.Out && (len(from) == 0 || slices.Contains(from, t.State)) {
			if state == FileOffered || state == FileQueued || state == FileHeld {
				t.State = state
			} else {
				t.end(state, "")
			}
		}
		return nil
	})
}

// offerLocal offers message m's file, sent from from to to on this
// server, once the message is delivered: the receiving copy is offered,
// taken at once on a files = "auto" link, refused on an off one. The
// links' store is locked.
func (s *store) offerLocal(c caller.Caller, from, to *project.Project, m *Message) {
	if m.Transfer == "" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	var out Transfer
	refused := ""
	switch {
	case fileMode(cfg, from.Slug, to.Slug) == config.FilesOff, fileMode(cfg, to.Slug, from.Slug) == config.FilesOff:
		refused = "files-off"
	case m.FileSize > int64(fileMB(cfg, to.Slug))<<20:
		refused = "too-big"
	}
	withFiles(func(st *fileStore) error {
		t := st.find(from.Slug, m.Transfer)
		if t == nil || !t.Out || !t.Open() {
			return nil
		}
		if refused != "" {
			t.end(FileUndelivered, refused)
		} else {
			t.State = FileOffered
		}
		out = *t
		if refused == "" {
			in := *t
			in.Out, in.Project, in.Peer, in.Created = false, to.Slug, from.Slug, now().UTC().Truncate(time.Second)
			st.add(&in)
		}
		return nil
	})
	if out.ID == "" {
		return
	}
	detail := fmt.Sprintf("%s (%s)", out.ID, MB(out.Size))
	from.Journal(c, "link.file-offer", to.Slug, detail)
	if refused != "" {
		fileUndeliveredItem(from, &out, refused)
		return
	}
	if fileMode(cfg, to.Slug, from.Slug) == config.FilesAuto {
		to.Journal(c, "link.file-offered", from.Slug, detail)
		acceptLocal(caller.Caller{Kind: caller.Ticker}, to, from, out.ID, true)
		return
	}
	in := out
	in.Peer = from.Slug
	to.AddMessageItem(KindFile, from.Slug, FileSummary(&in), true, out.ID)
	to.Journal(c, "link.file-offered", from.Slug, detail)
}

// fileUndeliveredItem tells the sending project p that its file didn't
// reach the other side, in fixed words for code.
func fileUndeliveredItem(p *project.Project, t *Transfer, code string) {
	p.AddMessageItem(KindUndelivered, t.Peer, fmt.Sprintf("your file %s with %s to %s wasn't received: %s", t.Name, t.Message, Shown(t.Peer), fileWords(code, t.Peer, p.Slug)), false, t.Message)
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-failed", t.Peer, t.ID+" ("+code+")")
}

// acceptLocal takes transfer id, offered to project to by project from
// on this server: a copy of the sender's snapshot lands beside the
// message, checked by size and hash, and both copies are received. auto
// says no one was asked.
func acceptLocal(c caller.Caller, to, from *project.Project, id string, auto bool) (*Transfer, error) {
	var in Transfer
	err := withFiles(func(st *fileStore) error {
		t := st.find(to.Slug, id)
		if t == nil || t.Out || t.State != FileOffered {
			return refuse("not-offered", "%s doesn't wait for an answer", id)
		}
		t.State, t.Accepted, t.Auto = FileAccepted, now().UTC().Truncate(time.Second), auto
		in = *t
		return nil
	})
	if err != nil {
		return nil, err
	}
	how := id
	if auto {
		how += " (auto)"
	}
	to.Journal(c, "link.file-accept", from.Slug, how)
	start := now()
	code := ""
	name, err := landLocal(to, from, &in)
	if err != nil {
		code = failCode(err)
	}
	withFiles(func(st *fileStore) error {
		for _, slug := range []string{to.Slug, from.Slug} {
			t := st.find(slug, id)
			if t == nil || !t.Open() {
				continue
			}
			if code != "" {
				t.end(FileFailed, code)
				continue
			}
			if !t.Out {
				t.Name = name
			}
			t.Offset = t.Size
			t.end(FileReceived, "")
		}
		if t := st.find(to.Slug, id); t != nil {
			in = *t
		}
		return nil
	})
	doneFile(to, id)
	if code != "" {
		to.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-failed", from.Slug, id+" ("+code+")")
		out := in
		out.Peer = to.Slug
		fileUndeliveredItem(from, &out, code)
		return &in, nil
	}
	update(to, from.Slug, in.Message, func(x *Message) { x.File = name })
	detail := fmt.Sprintf("%s (%s, %s)", id, MB(in.Size), now().Sub(start).Round(time.Second))
	to.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-received", from.Slug, detail)
	from.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-received", to.Slug, detail)
	if auto {
		receivedItem(to, &in)
	}
	return &in, nil
}

// failCode is the fixed word a failure says: bad-content for a copy
// that doesn't match, gone, else refused.
func failCode(err error) string {
	var e *fileErr
	if errors.As(err, &e) {
		return e.code
	}
	return "refused"
}

// fileErr is a transfer's failure with its fixed word.
type fileErr struct{ code string }

func (e *fileErr) Error() string { return e.code }

// landLocal copies the sender's snapshot of t into to's folder of its
// message, checking it, and says the name it took.
func landLocal(to, from *project.Project, t *Transfer) (string, error) {
	src := filepath.Join(fileDir(from, to.Slug, t.Message), t.Name)
	in, err := fsx.OpenNoLink(src, os.O_RDONLY, 0)
	if err != nil {
		return "", &fileErr{"gone"}
	}
	defer in.Close()
	d, err := linkState("incoming")
	if err != nil {
		return "", err
	}
	if err := fsx.EnsurePrivateDir(d); err != nil {
		return "", err
	}
	part := filepath.Join(d, t.ID+"-"+to.Slug+".part")
	out, err := fsx.OpenNoLink(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, t.Size+1))
	if err == nil {
		err = out.Chmod(0o644)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && (n != t.Size || hex.EncodeToString(h.Sum(nil)) != t.SHA256) {
		err = &fileErr{"bad-content"}
	}
	if err != nil {
		os.Remove(part)
		return "", err
	}
	name, err := place(to, from.Slug, t.Message, t.Name, part)
	if err != nil {
		os.Remove(part)
	}
	return name, err
}

// receivedItem tells project p, without needing the user, that a file
// came on its own (files = "auto").
func receivedItem(p *project.Project, t *Transfer) {
	p.AddMessageItem(KindFile, t.Peer, fmt.Sprintf("%s's file %s (%s) with %s was received (the link takes files without asking); read it with tm link read %s --file %s", Shown(t.Peer), t.Name, MB(t.Size), t.Message, t.Message, t.Name), false, t.ID)
}

// humanOnly refuses a transfer's answer to anyone but the human.
func humanOnly(c caller.Caller, what string) error {
	if c.IsAgent() {
		return refuse("human-only", "%s is the user's: tell them, and they answer it in the Messages tab (or tm link accept|decline)", what)
	}
	return nil
}

// AcceptFile takes the file transfer id offered to p, as the human: on
// this server it is copied now; from a paired server the answer goes
// back and this server fetches it (filefetch.go).
func AcceptFile(c caller.Caller, p *project.Project, id string) (*Transfer, error) {
	if err := humanOnly(c, "accepting a file"); err != nil {
		return nil, err
	}
	t, err := offered(p, id)
	if err != nil {
		return nil, err
	}
	if alias, slug, ok := config.Remote(t.Peer); ok {
		return answerRemote(c, p, t, alias, slug, true, false)
	}
	from, err := project.Open(t.Peer)
	if err != nil {
		return nil, refuse("unknown-project", "no project named %s", t.Peer)
	}
	return acceptLocal(c, p, from, id, false)
}

// DeclineFile refuses the file transfer id offered to p, as the human.
func DeclineFile(c caller.Caller, p *project.Project, id string) (*Transfer, error) {
	if err := humanOnly(c, "declining a file"); err != nil {
		return nil, err
	}
	t, err := offered(p, id)
	if err != nil {
		return nil, err
	}
	if alias, slug, ok := config.Remote(t.Peer); ok {
		return answerRemote(c, p, t, alias, slug, false, false)
	}
	return declineLocal(c, p, t)
}

// declineLocal refuses transfer t, offered to p by a project on this
// server: both copies end declined and the sender hears of it.
func declineLocal(c caller.Caller, p *project.Project, t *Transfer) (*Transfer, error) {
	id := t.ID
	var in Transfer
	err := withFiles(func(st *fileStore) error {
		if x := st.find(p.Slug, id); x == nil || x.Out || x.State != FileOffered {
			return refuse("not-offered", "%s was answered meanwhile", id)
		}
		for _, slug := range []string{p.Slug, t.Peer} {
			if x := st.find(slug, id); x != nil && x.Open() {
				x.end(FileDeclined, "")
			}
		}
		if x := st.find(p.Slug, id); x != nil {
			in = *x
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	doneFile(p, id)
	p.Journal(c, "link.file-decline", t.Peer, id)
	if from, err := project.Open(t.Peer); err == nil {
		from.Journal(c, "link.file-declined", p.Slug, id)
		from.AddMessageItem(KindFile, p.Slug, fmt.Sprintf("%s declined your file %s with %s", p.Slug, t.Name, t.Message), false, id)
	}
	return &in, nil
}

// offered is p's incoming transfer id that waits for its answer.
func offered(p *project.Project, id string) (*Transfer, error) {
	if !ValidFileID(id) {
		return nil, refuse("unknown-file", "%q is not a file transfer (X-…)", id)
	}
	var t *Transfer
	readFiles(func(st *fileStore) {
		if x := st.find(p.Slug, id); x != nil {
			c := *x
			t = &c
		}
	})
	if t == nil || t.Out {
		return nil, refuse("unknown-file", "no file %s was offered to %s", id, p.Slug)
	}
	if t.State != FileOffered {
		return nil, refuse("not-offered", "%s was %s", id, FileState(*t))
	}
	return t, nil
}

// CancelFile stops p's transfer id, either way, while it is held,
// queued, offered, waiting or running: the human's, or the coordinator's
// for a file its project sends. The other side hears of it; nothing
// partial is kept.
func CancelFile(c caller.Caller, p *project.Project, id string) (*Transfer, error) {
	if c.Kind == caller.Thread {
		return nil, refuse("coordinator-only", "files over links are the coordinator's")
	}
	if !ValidFileID(id) {
		return nil, refuse("unknown-file", "%q is not a file transfer (X-…)", id)
	}
	var t Transfer
	err := withFiles(func(st *fileStore) error {
		x := st.find(p.Slug, id)
		if x == nil {
			return refuse("unknown-file", "%s has no file transfer %s", p.Slug, id)
		}
		if c.IsAgent() && !x.Out {
			return refuse("human-only", "a file offered to %s is the user's to decline (the Messages tab, or tm link decline %s)", p.Slug, id)
		}
		if !x.Open() {
			return refuse("not-open", "%s was %s", id, FileState(*x))
		}
		x.end(FileCancelled, "")
		t = *x
		if _, _, remote := config.Remote(x.Peer); !remote {
			if o := st.find(x.Peer, id); o != nil && o.Open() {
				o.end(FileCancelled, "")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	doneFile(p, id)
	dropPart(id)
	abortStream(id, cancelledWord)
	p.Journal(c, "link.file-cancel", t.Peer, id)
	if alias, slug, ok := config.Remote(t.Peer); ok {
		queueFileMsg(alias, cancelMsg(p.Slug, slug, id))
	} else if o, err := project.Open(t.Peer); err == nil {
		doneFile(o, id)
		o.Journal(c, "link.file-cancelled", p.Slug, id)
	}
	return &t, nil
}

// cancelledWord is the abort a cancel sends on a live stream.
const cancelledWord = "cancelled"

// dropFile ends message m's transfer with state when its message goes
// no further: dropped while held, or undelivered while queued.
func dropFile(p *project.Project, m *Message, state, code string) {
	if m.Transfer == "" {
		return
	}
	withFiles(func(st *fileStore) error {
		if t := st.find(p.Slug, m.Transfer); t != nil && t.Out && t.Open() {
			t.end(state, code)
		}
		return nil
	})
}

// ExpireFiles ends offers left unanswered for OfferExpiry and transfers
// not done TransferExpiry after they were accepted, on this side; an
// expired offer's item is done. It writes the store only when one
// expired.
func ExpireFiles() {
	t0 := now()
	old := func(t *Transfer) bool {
		switch {
		case t.State == FileOffered || t.State == FileHeld:
			return t0.Sub(t.Created) > OfferExpiry
		case t.Active() && !t.Accepted.IsZero():
			return t0.Sub(t.Accepted) > TransferExpiry
		}
		return false
	}
	some := false
	readFiles(func(st *fileStore) { some = slices.ContainsFunc(st.Transfers, old) })
	if !some {
		return
	}
	var gone []Transfer
	withFiles(func(st *fileStore) error {
		for _, t := range st.Transfers {
			if !old(t) {
				continue
			}
			if t.Active() {
				code := "expired"
				if t.Wait == "disk" {
					code = "no-space"
				}
				t.end(FileFailed, code)
			} else {
				t.end(FileExpired, "")
			}
			gone = append(gone, *t)
		}
		return nil
	})
	for _, t := range gone {
		p, err := project.Open(t.Project)
		if err != nil {
			continue
		}
		dropPart(t.ID)
		abortStream(t.ID, "stalled")
		if !t.Out {
			doneFile(p, t.ID)
		}
		p.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-expired", t.Peer, t.ID)
		alias, slug, remote := config.Remote(t.Peer)
		if remote && !t.Out && t.State == FileFailed {
			queueFileMsg(alias, doneMsg(p.Slug, slug, t.ID, t.Code))
		}
	}
}

// CountOffers counts the transfers in ts offered to this side that wait
// for its user: fit within mb (the receiving side's link_file_mb), over
// it.
func CountOffers(ts []Transfer, mb int) (fit, over int) {
	for _, t := range ts {
		switch {
		case t.Out || t.State != FileOffered:
		case t.Size > int64(mb)<<20:
			over++
		default:
			fit++
		}
	}
	return fit, over
}

// SettleOffers answers the offers that wait for the user on links whose
// files no longer ask (§16.13), slug's from other only unless both are
// "": on an
// auto link each is taken as if it had just arrived, except one over the
// receiving side's link_file_mb, which keeps waiting; on an off link (or
// one that is gone) each is declined. On this server an off sending side
// declines too. It says how many it took and declined.
func SettleOffers(c caller.Caller, slug, other string) (taken, declined int) {
	var waiting []Transfer
	readFiles(func(st *fileStore) {
		for _, t := range st.Transfers {
			if !t.Out && t.State == FileOffered && (slug == "" || t.Project == slug && t.Peer == other) {
				waiting = append(waiting, *t)
			}
		}
	})
	if len(waiting) == 0 {
		return 0, 0
	}
	cfg, err := config.Load()
	if err != nil {
		return 0, 0
	}
	for _, t := range waiting {
		mode := fileMode(cfg, t.Project, t.Peer)
		alias, peerSlug, remote := config.Remote(t.Peer)
		if !remote && mode == config.FilesAuto && fileMode(cfg, t.Peer, t.Project) == config.FilesOff {
			mode = config.FilesOff
		}
		if mode == config.FilesAsk || (mode == config.FilesAuto && t.Size > int64(fileMB(cfg, t.Project))<<20) {
			continue
		}
		p, err := project.Open(t.Project)
		if err != nil {
			continue
		}
		auto := mode == config.FilesAuto
		if remote {
			_, err = answerRemote(c, p, &t, alias, peerSlug, auto, auto)
		} else if from, ferr := project.Open(t.Peer); ferr != nil {
			continue
		} else if auto {
			_, err = acceptLocal(c, p, from, t.ID, true)
		} else {
			_, err = declineLocal(c, p, &t)
		}
		switch {
		case err != nil:
		case auto:
			taken++
		default:
			declined++
		}
	}
	return taken, declined
}
