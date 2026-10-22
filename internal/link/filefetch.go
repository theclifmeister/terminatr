package link

// The data half of a file to a paired server's project (docs/SPEC.md
// §16.13). The receiving server's fetcher pulls each accepted file over
// the link (peer.file.fetch), one at a time per pair of projects and two
// at a time in all, into state/link/incoming/<X-id>.part, a private
// folder outside every project; the part is kept across a dropped link
// or a restart, and the next fetch goes on from its end. Checked by size
// and hash, it moves beside its message. The sending server serves only
// the copy it took of an offer it made to that server (fileHost).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Limits of the fetcher.
const (
	// filesPerServer is how many files the fetcher pulls at once.
	filesPerServer = 2
	// diskSlack is what a fetch leaves free: the rest of the file, a
	// tenth more and this.
	diskSlack = 1 << 30
	// saveEvery is how often a running transfer's progress is written.
	saveEvery = time.Second
)

// Logf logs a transfer's start and end, with its ids, bytes and time,
// never its name; nil logs nothing. The server sets it.
var Logf func(format string, a ...any)

func logf(format string, a ...any) {
	if Logf != nil {
		Logf(format, a...)
	}
}

// fileKick wakes the fetcher: a file was accepted.
var fileKick = make(chan struct{}, 1)

// KickFiles asks the fetcher to look now rather than at its next poll.
func KickFiles() {
	select {
	case fileKick <- struct{}{}:
	default:
	}
}

// FileTransport is the server's links as the fetcher needs them.
type FileTransport interface {
	// FileReady says the link to alias is up and its tm takes files.
	FileReady(alias string) bool
	FetchFile(ctx context.Context, alias, id string, offset int64, w io.Writer) (peer.FileEnd, error)
}

// streams are the transfers with a stream open in this process: the
// fetches by id, and the ids served.
var (
	streamsMu sync.Mutex
	fetching  = map[string]context.CancelCauseFunc{}
	serving   = map[string]bool{}
	// abortServed ends the served streams match picks (the links'
	// AbortServed); the server sets it with SetServedAborter.
	abortServed func(match func(peer.FileSource) bool, word string)
)

// SetServedAborter gives the file host the links' way to end a stream
// it serves: a cancel or a receipt ends it at once.
func SetServedAborter(f func(match func(peer.FileSource) bool, word string)) {
	streamsMu.Lock()
	defer streamsMu.Unlock()
	abortServed = f
}

// abortStream ends transfer id's live stream in this process, either
// way, with word ("" ends a served one without telling the other end).
func abortStream(id, word string) {
	streamsMu.Lock()
	cancel, served, abort := fetching[id], serving[id], abortServed
	streamsMu.Unlock()
	if cancel != nil {
		cause := word
		if cause == "" {
			cause = proto.FileStopping
		}
		cancel(&peer.FileAbort{Code: cause})
	}
	if served && abort != nil {
		abort(func(s peer.FileSource) bool { sf, ok := s.(*servedFile); return ok && sf.id == id }, word)
	}
}

// partPath is where transfer id's bytes wait until they are checked.
func partPath(id string) (string, error) { return linkState("incoming", id+".part") }

// dropPart removes transfer id's part, if any.
func dropPart(id string) {
	if p, err := partPath(id); err == nil {
		os.Remove(p)
	}
}

// RunFiles fetches the accepted files from paired servers until ctx
// ends: on each kick and every Poll.
func RunFiles(ctx context.Context, t FileTransport) {
	tick := time.NewTicker(Poll)
	defer tick.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		startFetches(ctx, t, &wg)
		select {
		case <-ctx.Done():
			return
		case <-fileKick:
		case <-tick.C:
		}
	}
}

// startFetches starts each accepted transfer that may run now: its link
// up, its pair and the server with a free slot, and the disk with room.
// The others wait, saying why.
func startFetches(ctx context.Context, t FileTransport, wg *sync.WaitGroup) {
	var todo []Transfer
	readFiles(func(st *fileStore) {
		for _, x := range st.Transfers {
			if !x.Out && x.Active() {
				if _, _, remote := config.Remote(x.Peer); remote {
					todo = append(todo, *x)
				}
			}
		}
	})
	if len(todo) == 0 {
		return
	}
	slices.SortStableFunc(todo, func(a, b Transfer) int { return a.Accepted.Compare(b.Accepted) })
	streamsMu.Lock()
	running := len(fetching)
	busy := map[string]bool{}
	for _, x := range todo {
		if fetching[x.ID] != nil {
			busy[x.Project+">"+x.Peer] = true
		}
	}
	streamsMu.Unlock()
	waits := map[string]string{}
	for _, x := range todo {
		streamsMu.Lock()
		on := fetching[x.ID] != nil
		streamsMu.Unlock()
		if on {
			continue
		}
		alias, _, _ := config.Remote(x.Peer)
		pair := x.Project + ">" + x.Peer
		switch {
		case !t.FileReady(alias):
			waits[x.ID] = "link"
			continue
		case busy[pair] || running >= filesPerServer:
			waits[x.ID] = "turn"
			continue
		case !roomFor(x):
			waits[x.ID] = "disk"
			continue
		}
		busy[pair] = true
		running++
		fctx, cancel := context.WithCancelCause(ctx)
		streamsMu.Lock()
		fetching[x.ID] = cancel
		streamsMu.Unlock()
		wg.Add(1)
		go func(x Transfer) {
			defer wg.Done()
			defer func() {
				streamsMu.Lock()
				delete(fetching, x.ID)
				streamsMu.Unlock()
				cancel(nil)
				KickFiles() // the next may go
			}()
			fetch(fctx, t, x)
		}(x)
	}
	if len(waits) > 0 {
		withFiles(func(st *fileStore) error {
			for id, why := range waits {
				for _, x := range st.Transfers {
					if x.ID == id && !x.Out && x.Active() && (x.State != FileWaiting || x.Wait != why) {
						x.State, x.Wait, x.Rate = FileWaiting, why, 0
					}
				}
			}
			return nil
		})
	}
}

// roomFor says the disk under the incoming folder has room for the
// rest of x, a tenth more and diskSlack; true when that can't be told.
func roomFor(x Transfer) bool {
	d, err := linkState("incoming")
	if err != nil {
		return true
	}
	if err := fsx.EnsurePrivateDir(d); err != nil {
		return true
	}
	free := fsx.Free(d)
	rest := x.Size - x.Offset
	return free < 0 || free >= rest+rest/10+diskSlack
}

// partWriter writes a fetch's bytes to the part, hashing them, and
// saves the transfer's progress every saveEvery.
type partWriter struct {
	f     *os.File
	h     hash.Hash
	x     *Transfer
	off   int64
	saved time.Time
	from  int64 // off at saved, for the pace
}

func (w *partWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.h.Write(p[:n])
	w.off += int64(n)
	if t := now(); t.Sub(w.saved) >= saveEvery {
		rate := float64(w.off-w.from) / t.Sub(w.saved).Seconds()
		w.saved, w.from = t, w.off
		// A cancel or an expiry from another tm shows here.
		if !progress(w.x.Project, w.x.ID, w.off, rate) && err == nil {
			err = &peer.FileAbort{Code: proto.FileCancelled}
		}
	}
	return n, err
}

// progress writes a running transfer's bytes so far and pace, and says
// whether it still runs.
func progress(slug, id string, off int64, rate float64) bool {
	on := false
	withFiles(func(st *fileStore) error {
		if x := st.find(slug, id); x != nil && x.Active() {
			x.Offset, x.Rate, on = off, rate, true
		}
		return nil
	})
	return on
}

// fetch pulls transfer x once: from the part's end, to its end or until
// the stream stops. A whole, checked file lands; anything else says
// whether the transfer waits for the next try or failed for good.
func fetch(ctx context.Context, t FileTransport, x Transfer) {
	alias, slug, _ := config.Remote(x.Peer)
	part, err := partPath(x.ID)
	if err != nil {
		return
	}
	if err := fsx.EnsurePrivateDir(filepath.Dir(part)); err != nil {
		return
	}
	f, err := fsx.OpenNoLink(part, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	h := sha256.New()
	off, err := f.Seek(0, io.SeekEnd)
	if err != nil || off > x.Size {
		f.Truncate(0)
		off = 0
	}
	if off > 0 {
		if _, err := io.Copy(h, io.NewSectionReader(f, 0, off)); err != nil {
			f.Truncate(0)
			off, h = 0, sha256.New()
		}
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return
	}
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Active() {
			c.State, c.Wait, c.Offset = FileReceiving, "", off
		}
		return nil
	})
	start := now()
	logf("link file %s from %s: receiving from byte %d of %d", x.ID, x.Peer, off, x.Size)
	w := &partWriter{f: f, h: h, x: &x, off: off, saved: start, from: off}
	end, err := t.FetchFile(ctx, alias, x.ID, off, w)
	if err == nil {
		err = f.Sync()
	}
	if err == nil && (w.off != x.Size || end.Size != x.Size || hex.EncodeToString(h.Sum(nil)) != x.SHA256 || hex.EncodeToString(end.Sum[:]) != x.SHA256) {
		err = &peer.FileAbort{Code: proto.FileBadContent}
	}
	took := now().Sub(start).Round(time.Second)
	if err == nil {
		logf("link file %s from %s: received %d bytes in %s", x.ID, x.Peer, x.Size, took)
		landFetched(f, part, x, slug, took)
		return
	}
	logf("link file %s from %s: stopped at byte %d after %s: %v", x.ID, x.Peer, w.off, took, err)
	var ab *peer.FileAbort
	var pe *proto.Error
	switch {
	case ctx.Err() != nil, errors.As(err, &ab) && ab.Code == proto.FileCancelled:
		// Cancelled here or there, expired, or the server stops: the
		// state says which, or the cancel through the outbox will.
		progress(x.Project, x.ID, w.off, 0)
	case errors.As(err, &ab) && (ab.Code == proto.FileBadContent || ab.Code == proto.FileTooBig):
		f.Truncate(0)
		retryOrFail(x, slug)
	case errors.As(err, &ab) && ab.Code == proto.FileStalled:
		stalled(x, slug, w.off)
	case errors.As(err, &ab) && ab.Code == proto.FileNoSpace:
		wait(x, "disk", w.off)
	case errors.As(err, &pe) && (pe.Code == proto.ErrUnknownFile || pe.Code == proto.ErrFileGone):
		failFetched(x, slug, pe.Code)
	case errors.As(err, &pe) && pe.Code == proto.ErrFileBusy:
		wait(x, "turn", w.off)
	default:
		// The link dropped, the other side paused or unlinked it, or
		// stops: the part waits for the next fetch.
		wait(x, "link", w.off)
	}
}

// wait puts x back to waiting for why, with the bytes it has.
func wait(x Transfer, why string, off int64) {
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Active() {
			c.State, c.Wait, c.Offset, c.Rate = FileWaiting, why, off, 0
			if why != "turn" {
				c.Stalls = 0
			}
		}
		return nil
	})
}

// stalled counts a stall: the part waits for the next fetch, until
// MaxStalls in a row fail it.
func stalled(x Transfer, slug string, off int64) {
	n := 0
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Active() {
			c.Stalls++
			n = c.Stalls
			c.State, c.Wait, c.Offset, c.Rate = FileWaiting, "link", off, 0
		}
		return nil
	})
	if n >= MaxStalls {
		failFetched(x, slug, proto.FileStalled)
	}
}

// retryOrFail starts x again from its first byte once, after bytes that
// didn't match; the second time it fails bad-content.
func retryOrFail(x Transfer, slug string) {
	again := false
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Active() && !c.Retried {
			c.Retried, again = true, true
			c.State, c.Wait, c.Offset, c.Rate = FileWaiting, "", 0, 0
		}
		return nil
	})
	if !again {
		failFetched(x, slug, proto.FileBadContent)
		return
	}
	dropPart(x.ID)
	KickFiles()
}

// failFetched ends x for good with code: the part goes, and the sending
// side hears of it through the outbox.
func failFetched(x Transfer, slug, code string) {
	ended := false
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Open() {
			c.end(FileFailed, code)
			ended = true
		}
		return nil
	})
	if !ended {
		return
	}
	dropPart(x.ID)
	alias, _, _ := config.Remote(x.Peer)
	queueFileMsg(alias, doneMsg(x.Project, slug, x.ID, code))
	if p, err := project.Open(x.Project); err == nil {
		p.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-failed", x.Peer, x.ID+" ("+code+")")
	}
}

// landFetched moves x's checked part beside its message, 0644, and
// sends the receipt.
func landFetched(f *os.File, part string, x Transfer, slug string, took time.Duration) {
	p, err := project.Open(x.Project)
	if err == nil {
		err = f.Chmod(0o644)
	}
	name := ""
	if err == nil {
		name, err = place(p, x.Peer, x.Message, x.Name, part)
	}
	if err != nil {
		logf("link file %s from %s: can't place it: %v", x.ID, x.Peer, err)
		wait(x, "disk", x.Size)
		return
	}
	var in Transfer
	withFiles(func(st *fileStore) error {
		if c := st.find(x.Project, x.ID); c != nil && c.Open() {
			c.Name, c.Offset = name, c.Size
			c.end(FileReceived, "")
			in = *c
		}
		return nil
	})
	if in.ID == "" {
		// Cancelled while it landed: it isn't kept.
		os.Remove(filepath.Join(fileDir(p, x.Peer, x.Message), name))
		return
	}
	update(p, x.Peer, x.Message, func(m *Message) { m.File = name })
	alias, _, _ := config.Remote(x.Peer)
	queueFileMsg(alias, doneMsg(p.Slug, slug, x.ID, ""))
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.file-received", x.Peer, fmt.Sprintf("%s (%s, %s)", x.ID, MB(x.Size), took))
	if in.Auto {
		receivedItem(p, &in)
	}
}

// fileHost serves the files this server offered paired servers.
type fileHost struct{}

// Files is the server's peer.FileHost.
func Files() peer.FileHost { return fileHost{} }

// ServeFile opens the copy of transfer id this server's project took
// when it offered the file to a project on the paired server c, while
// the offer stands and the link still sends there.
func (fileHost) ServeFile(c caller.Caller, id string, offset int64) (peer.FileSource, error) {
	unknown := proto.Errorf(proto.ErrUnknownFile, "no file %s was offered to your server", id)
	if c.Kind != caller.Peer || !ValidFileID(id) {
		return nil, unknown
	}
	var t Transfer
	readFiles(func(st *fileStore) {
		for _, x := range st.Transfers {
			if x.ID == id && x.Out && strings.HasPrefix(x.Peer, c.Alias+"/") {
				t = *x
			}
		}
	})
	switch t.State {
	case FileQueued, FileOffered, FileAccepted, FileSending:
	default:
		return nil, unknown
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "the server can't read its settings")
	}
	if code, _ := cfg.LinkRefusal(t.Project, t.Peer); code != "" {
		return nil, proto.Errorf(proto.ErrNotLinked, "the link doesn't send to your project now")
	}
	if Paused(t.Project, t.Peer) {
		return nil, proto.Errorf(proto.ErrLinkPaused, "the link is paused")
	}
	p, err := project.Open(t.Project)
	if err != nil {
		return nil, unknown
	}
	f, err := fsx.OpenNoLink(filepath.Join(fileDir(p, t.Peer, t.Message), t.Name), os.O_RDONLY, 0)
	if err != nil {
		return nil, proto.Errorf(proto.ErrFileGone, "the file's copy was deleted")
	}
	fi, err := f.Stat()
	var sum [32]byte
	b, herr := hex.DecodeString(t.SHA256)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != t.Size || herr != nil || len(b) != 32 {
		f.Close()
		return nil, proto.Errorf(proto.ErrFileGone, "the file's copy changed")
	}
	copy(sum[:], b)
	streamsMu.Lock()
	if serving[id] {
		streamsMu.Unlock()
		f.Close()
		return nil, proto.Errorf(proto.ErrFileBusy, "a stream serves %s already", id)
	}
	serving[id] = true
	streamsMu.Unlock()
	kbps := config.DefaultLinkFileKbps
	if s, err := cfg.Safety(t.Project); err == nil && s.LinkFileKbps > 0 {
		kbps = s.LinkFileKbps
	}
	withFiles(func(st *fileStore) error {
		if x := st.find(t.Project, id); x != nil && x.Open() {
			x.State, x.Offset = FileSending, offset
			if x.Accepted.IsZero() {
				x.Accepted = now().UTC().Truncate(time.Second)
			}
		}
		return nil
	})
	logf("link file %s to %s: sending from byte %d of %d", id, t.Peer, offset, t.Size)
	return &servedFile{id: id, project: t.Project, peer: t.Peer, f: f, size: t.Size, sum: sum, kbps: kbps, from: offset, start: now(), saved: now()}, nil
}

// servedFile is a file a stream serves: the sender's copy, open.
type servedFile struct {
	id, project, peer string
	f                 *os.File
	size              int64
	sum               [32]byte
	kbps              int
	from              int64
	start             time.Time

	mu    sync.Mutex
	acked int64
	saved time.Time
	at    int64 // acked at saved
	once  sync.Once
}

func (s *servedFile) ReadAt(p []byte, off int64) (int, error) { return s.f.ReadAt(p, off) }
func (s *servedFile) Size() int64                             { return s.size }
func (s *servedFile) Sum() [32]byte                           { return s.sum }
func (s *servedFile) Kbps() int                               { return s.kbps }

// Acked saves the receiver's progress every saveEvery, off the link's
// reader: the sender's copy shows sending N%. A cancel from another tm
// shows there, and ends the stream.
func (s *servedFile) Acked(n int64) {
	s.mu.Lock()
	s.acked = n
	t := now()
	due := t.Sub(s.saved) >= saveEvery
	var rate float64
	if due {
		rate = float64(n-s.at) / t.Sub(s.saved).Seconds()
		s.saved, s.at = t, n
	}
	s.mu.Unlock()
	if due {
		go func() {
			if !progress(s.project, s.id, n, rate) {
				abortStream(s.id, cancelledWord)
			}
		}()
	}
}

// Close ends the stream: the sender's copy keeps its acked bytes and
// waits for the receipt, or for the next fetch when it stopped short.
func (s *servedFile) Close(code string) {
	s.once.Do(func() {
		s.f.Close()
		streamsMu.Lock()
		delete(serving, s.id)
		streamsMu.Unlock()
		s.mu.Lock()
		acked := s.acked
		s.mu.Unlock()
		withFiles(func(st *fileStore) error {
			if x := st.find(s.project, s.id); x != nil && x.State == FileSending {
				x.Offset, x.Rate = max(acked, x.Offset), 0
				if code != "" {
					x.State = FileAccepted
				}
			}
			return nil
		})
		how := "sent"
		if code != "" {
			how = "stopped (" + code + ")"
		}
		logf("link file %s to %s: %s at byte %d after %s", s.id, s.peer, how, acked, now().Sub(s.start).Round(time.Second))
	})
}
