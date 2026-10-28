package peer

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Files over links (docs/SPEC.md §16.13): the server a file was offered
// to (near) pulls it with peer.file.fetch on a stream id it picked; the
// one that offered it (far) answers with binary frames, paced, and keeps
// at most fileWindow bytes unacked. The stream closes when the last byte
// is acked or either end aborts; a dropped link drops it too, and the
// next fetch starts from the bytes the near end has. Frame types say
// the direction, but for abort, which either end sends: each end's
// stream ids are its own, and the TLS client's have the top bit clear,
// the server's set, so an abort names one stream on either side.

// CapFile is the link capability of peer.file.fetch and its frames.
const CapFile = "file"

// File stream frame types. Far to near:
const (
	frameFileData = 30 // u64 offset, then the bytes
	frameFileEnd  = 31 // u64 size, 32-byte SHA-256
)

// Near to far, and either way:
const (
	frameFileAck   = 32 // u64 bytes written so far
	frameFileAbort = 33 // a fixed word (proto.FileCancelled …)
)

// File stream limits: the far end keeps at most fileWindow bytes
// unacked and the near end acks every fileAckEvery; either end gives up
// on a stream that hears nothing for fileStall. A server serves at most
// maxFileStreams streams at once, over all its links. A mux keeps its
// own window and stall (tests shorten them).
const (
	fileWindow   = 128 << 10
	fileAckEvery = 32 << 10
	fileStall    = 60 * time.Second
	maxFileStreams = 2
	// fileData is the most of a file one data frame carries.
	fileData = frameChunk - 8
	// The pace backs off when acks take fileSlowRTT longer than the
	// link's idle round trip: halved, down to fileMinRate, and up by a
	// tenth a second once they recover.
	fileSlowRTT = 150 * time.Millisecond
	fileMinRate = 64 << 10
)

// FileHost is what the server lends a link for files it offered.
type FileHost interface {
	// ServeFile opens transfer id, which this server offered to the
	// paired server c, to send from offset: proto.ErrUnknownFile when
	// there is no such offer to c (or it was cancelled), ErrFileGone
	// when its copy was deleted, ErrFileBusy when a stream serves it
	// already.
	ServeFile(c caller.Caller, id string, offset int64) (FileSource, error)
}

// FileSource is one file a stream serves.
type FileSource interface {
	io.ReaderAt
	Size() int64
	Sum() [32]byte
	// Kbps is the pace the stream starts at, KiB a second.
	Kbps() int
	// Acked takes the near end's word that n bytes are on its disk.
	Acked(n int64)
	// Close ends the stream, once: code "" when the near end acked the
	// last byte, else why it ended (an abort's word, or "link").
	Close(code string)
}

// FileAbort is a file stream ended by an abort, either end's: Code is
// the word (proto.FileStalled …).
type FileAbort struct{ Code string }

func (e *FileAbort) Error() string { return "the file stream was aborted: " + e.Code }

// fileWords are the words an abort may say; another is bad-content.
var fileWords = map[string]bool{proto.FileCancelled: true, proto.FileBadContent: true, proto.FileTooBig: true,
	proto.FileStalled: true, proto.FileNoSpace: true, proto.FileStopping: true}

func abortWord(p []byte) string {
	if w := string(p); fileWords[w] {
		return w
	}
	return proto.FileBadContent
}

// fileMux is a session's file streams: those it serves and those it
// opened.
type fileMux struct {
	s      *session
	mu     sync.Mutex
	served map[uint32]*fileServed
	opened map[uint32]*fileConn
	// top is this end's bit on the ids it picks.
	top   uint32
	next  uint32
	ended bool
	// slots bounds the streams the server serves over all its links.
	slots chan struct{}
	// window and stall are fileWindow and fileStall, set before the
	// session serves.
	window int
	stall  time.Duration
}

func newFileMux(s *session) *fileMux {
	return &fileMux{s: s, served: map[uint32]*fileServed{}, opened: map[uint32]*fileConn{},
		window: fileWindow, stall: fileStall}
}

// frame takes a file frame from the reader.
func (m *fileMux) frame(stream uint32, typ byte, payload []byte) {
	m.mu.Lock()
	c, sv := m.opened[stream], m.served[stream]
	m.mu.Unlock()
	switch typ {
	case frameFileData, frameFileEnd:
		if c != nil {
			c.take(typ, payload)
		}
	case frameFileAck:
		if sv != nil {
			sv.ack(payload)
		}
	case frameFileAbort:
		switch {
		case c != nil:
			c.take(typ, payload)
		case sv != nil:
			sv.stop(abortWord(payload))
		}
	}
}

// end ends every stream: the session is over.
func (m *fileMux) end() {
	m.mu.Lock()
	m.ended = true
	ss, cs := m.served, m.opened
	m.served, m.opened = map[uint32]*fileServed{}, map[uint32]*fileConn{}
	m.mu.Unlock()
	for _, sv := range ss {
		sv.stop("link")
	}
	for _, c := range cs {
		c.drop()
	}
}

// The far end.

type fileServed struct {
	m    *fileMux
	id   uint32
	src  FileSource
	slot chan struct{}
	pace *pacer
	// idle is the link's round trip without a file, for the backoff.
	idle func() time.Duration

	mu    sync.Mutex
	cond  *sync.Cond
	acked int64
	heard time.Time
	sent  []sentAt // ends of frames not yet acked, oldest first
	done  bool
	why   string
}

type sentAt struct {
	end int64
	at  time.Time
}

// serve starts stream id from src at offset, taking one of the server's
// slots, or refuses it busy.
func (m *fileMux) serve(id uint32, src FileSource, offset int64) error {
	sv := &fileServed{m: m, id: id, src: src, acked: offset, heard: time.Now(), pace: newPacer(src.Kbps() << 10),
		idle: m.s.idleRTT}
	sv.cond = sync.NewCond(&sv.mu)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.ended:
		return errNotUp
	case m.served[id] != nil:
		return proto.Errorf(proto.ErrBadParams, "stream %d is open", id)
	}
	if m.slots != nil {
		select {
		case m.slots <- struct{}{}:
			sv.slot = m.slots
		default:
			return proto.Errorf(proto.ErrFileBusy, "this server sends %d files at once already", maxFileStreams)
		}
	}
	m.served[id] = sv
	go sv.run(offset)
	return nil
}

// stop ends the stream, once, for why ("" when it was all acked).
func (sv *fileServed) stop(why string) {
	sv.mu.Lock()
	if sv.done {
		sv.mu.Unlock()
		return
	}
	sv.done, sv.why = true, why
	sv.cond.Broadcast()
	sv.mu.Unlock()
	sv.m.mu.Lock()
	if sv.m.served[sv.id] == sv {
		delete(sv.m.served, sv.id)
	}
	sv.m.mu.Unlock()
	if sv.slot != nil {
		<-sv.slot
	}
	sv.src.Close(why)
}

// ack takes the near end's count of bytes written.
func (sv *fileServed) ack(p []byte) {
	if len(p) != 8 {
		return
	}
	n := int64(binary.BigEndian.Uint64(p))
	t := time.Now()
	sv.mu.Lock()
	if n <= sv.acked || n > sv.src.Size() {
		sv.mu.Unlock()
		return
	}
	sv.acked, sv.heard = n, t
	var rtt time.Duration
	for len(sv.sent) > 0 && sv.sent[0].end <= n {
		rtt = t.Sub(sv.sent[0].at)
		sv.sent = sv.sent[1:]
	}
	sv.cond.Broadcast()
	sv.mu.Unlock()
	if rtt > 0 {
		sv.pace.rtt(rtt, sv.idle())
	}
	sv.src.Acked(n)
}

// wait waits until the window has room for more past off (or, with
// last, until the near end acked off itself); false once the stream
// ended, which a stall ends too.
func (sv *fileServed) wait(off int64, last bool) bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	stall := sv.m.stall
	timer := time.AfterFunc(stall, func() {
		sv.mu.Lock()
		sv.cond.Broadcast()
		sv.mu.Unlock()
	})
	defer timer.Stop()
	for !sv.done {
		if last && sv.acked >= off || !last && off-sv.acked < int64(sv.m.window) {
			return true
		}
		if time.Since(sv.heard) >= stall {
			sv.mu.Unlock()
			sv.m.s.sendFrame(sv.id, frameFileAbort, []byte(proto.FileStalled))
			sv.stop(proto.FileStalled)
			sv.mu.Lock()
			return false
		}
		sv.cond.Wait()
		if time.Since(sv.heard) < stall {
			timer.Reset(stall - time.Since(sv.heard))
		}
	}
	return false
}

// run sends the file from off, then the end frame, and waits for the
// last ack.
func (sv *fileServed) run(off int64) {
	size := sv.src.Size()
	buf := make([]byte, 8+fileData)
	for off < size {
		if !sv.wait(off, false) {
			return
		}
		n := int(min(int64(fileData), size-off))
		binary.BigEndian.PutUint64(buf, uint64(off))
		if _, err := sv.src.ReadAt(buf[8:8+n], off); err != nil && !errors.Is(err, io.EOF) {
			sv.m.s.sendFrame(sv.id, frameFileAbort, []byte(proto.FileStopping))
			sv.stop("read")
			return
		}
		sv.pace.wait(n)
		if sv.isDone() {
			return
		}
		if err := sv.m.s.sendFrame(sv.id, frameFileData, buf[:8+n]); err != nil {
			sv.stop("link")
			return
		}
		off += int64(n)
		sv.mu.Lock()
		sv.sent = append(sv.sent, sentAt{off, time.Now()})
		sv.mu.Unlock()
	}
	sum := sv.src.Sum()
	end := binary.BigEndian.AppendUint64(nil, uint64(size))
	if err := sv.m.s.sendFrame(sv.id, frameFileEnd, append(end, sum[:]...)); err != nil {
		sv.stop("link")
		return
	}
	if sv.wait(size, true) {
		sv.stop("")
	}
}

func (sv *fileServed) isDone() bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.done
}

// pacer is a sender's token bucket: rate bytes a second, at most a
// frame's worth ahead, backing off on slow acks (fileSlowRTT).
type pacer struct {
	mu     sync.Mutex
	top    float64 // the setting
	rate   float64
	tokens float64
	at     time.Time
	// slowed is when the pace last halved; it climbs back after that.
	slowed time.Time
	now    func() time.Time
	sleep  func(time.Duration)
}

func newPacer(rate int) *pacer {
	r := float64(max(rate, fileMinRate))
	return &pacer{top: r, rate: r, tokens: fileData, now: time.Now, sleep: time.Sleep}
}

// refill brings the bucket and the rate's recovery up to now; mu held.
func (p *pacer) refill() {
	t := p.now()
	if p.at.IsZero() {
		p.at = t
	}
	el := t.Sub(p.at).Seconds()
	if el <= 0 {
		return
	}
	if p.rate < p.top && t.Sub(p.slowed) >= time.Second {
		p.rate = min(p.top, p.rate*pow11(el))
	}
	p.tokens = min(p.tokens+el*p.rate, fileData)
	p.at = t
}

// pow11 is 1.1 to the power of s seconds, for s up to a few.
func pow11(s float64) float64 {
	r := 1.0
	for ; s >= 1; s-- {
		r *= 1.1
	}
	return r * (1 + 0.1*s)
}

// wait takes n bytes' worth, sleeping until the bucket has them.
func (p *pacer) wait(n int) {
	p.mu.Lock()
	p.refill()
	p.tokens -= float64(n)
	short := -p.tokens / p.rate
	p.mu.Unlock()
	if short > 0 {
		p.sleep(time.Duration(short * float64(time.Second)))
	}
}

// rtt takes an ack's round trip against the link's idle one: more than
// fileSlowRTT over halves the pace, at most once a round trip.
func (p *pacer) rtt(d, idle time.Duration) {
	if d <= idle+fileSlowRTT {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.now()
	if t.Sub(p.slowed) < d {
		return
	}
	p.refill()
	p.rate = max(p.rate/2, fileMinRate)
	p.slowed = t
}

// pace is the rate now, bytes a second.
func (p *pacer) pace() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refill()
	return p.rate
}

// The near end.

// fileConn is a stream this server opened: its frames come on the
// reader and wait in frames for fetch, which writes them off the
// reader. An honest far end never has more than its window, an end and
// an abort in flight, so a full queue is a far end that broke the rules.
type fileConn struct {
	id      uint32
	frames  chan fileFrame
	gone    chan struct{}
	once    sync.Once
	overrun bool
	mu      sync.Mutex
}

type fileFrame struct {
	typ     byte
	payload []byte
}

const fileQueue = 32

func (c *fileConn) take(typ byte, payload []byte) {
	select {
	case c.frames <- fileFrame{typ, payload}:
	default:
		c.mu.Lock()
		c.overrun = true
		c.mu.Unlock()
		c.drop()
	}
}

// drop ends the stream from this end's side: the link went, or the far
// end overran it.
func (c *fileConn) drop() { c.once.Do(func() { close(c.gone) }) }

// FileEnd is a fetched file as the far end's end frame said it.
type FileEnd struct {
	Size int64
	Sum  [32]byte
}

// fetch pulls transfer id from offset into w, in order: the result says
// the size and hash the far end offered, then the frames bring the
// bytes. It returns when the end frame came and its last byte was
// acked; an abort is a *FileAbort, a dropped link errNotUp.
func (m *fileMux) fetch(ctx context.Context, id string, offset int64, w io.Writer) (FileEnd, error) {
	c := &fileConn{frames: make(chan fileFrame, fileQueue), gone: make(chan struct{})}
	m.mu.Lock()
	if m.ended {
		m.mu.Unlock()
		return FileEnd{}, errNotUp
	}
	m.next = (m.next + 1) &^ (1 << 31)
	if m.next == 0 {
		m.next = 1
	}
	c.id = m.next | m.top
	m.opened[c.id] = c
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.opened[c.id] == c {
			delete(m.opened, c.id)
		}
		m.mu.Unlock()
	}()
	var res proto.PeerFileFetchResult
	if err := m.s.call(ctx, proto.MethodPeerFileFetch, proto.PeerFileFetchParams{ID: id, Stream: c.id, Offset: offset}, &res); err != nil {
		return FileEnd{}, err
	}
	var want [32]byte
	if b, err := hex.DecodeString(res.SHA256); err != nil || len(b) != 32 || res.Size < offset {
		m.abort(c.id, proto.FileBadContent)
		return FileEnd{}, &FileAbort{proto.FileBadContent}
	} else {
		copy(want[:], b)
	}
	got, unacked := offset, 0
	stall := time.NewTimer(m.stall)
	defer stall.Stop()
	ack := func() error {
		return m.s.sendFrame(c.id, frameFileAck, binary.BigEndian.AppendUint64(nil, uint64(got)))
	}
	for {
		select {
		case <-ctx.Done():
			word := proto.FileStopping
			var ab *FileAbort
			if errors.As(context.Cause(ctx), &ab) && fileWords[ab.Code] {
				word = ab.Code
			}
			m.abort(c.id, word)
			return FileEnd{}, ctx.Err()
		case <-c.gone:
			c.mu.Lock()
			over := c.overrun
			c.mu.Unlock()
			if over {
				m.abort(c.id, proto.FileBadContent)
				return FileEnd{}, &FileAbort{proto.FileBadContent}
			}
			return FileEnd{}, errNotUp
		case <-stall.C:
			m.abort(c.id, proto.FileStalled)
			return FileEnd{}, &FileAbort{proto.FileStalled}
		case f := <-c.frames:
			stall.Reset(m.stall)
			switch f.typ {
			case frameFileAbort:
				return FileEnd{}, &FileAbort{abortWord(f.payload)}
			case frameFileData:
				if len(f.payload) < 8 || int64(binary.BigEndian.Uint64(f.payload)) != got {
					m.abort(c.id, proto.FileBadContent)
					return FileEnd{}, &FileAbort{proto.FileBadContent}
				}
				data := f.payload[8:]
				if got+int64(len(data)) > res.Size {
					m.abort(c.id, proto.FileTooBig)
					return FileEnd{}, &FileAbort{proto.FileTooBig}
				}
				if _, err := w.Write(data); err != nil {
					// w's own word, or the disk's
					ab := &FileAbort{proto.FileNoSpace}
					errors.As(err, &ab)
					m.abort(c.id, ab.Code)
					return FileEnd{}, ab
				}
				got += int64(len(data))
				if unacked += len(data); unacked >= fileAckEvery {
					unacked = 0
					if err := ack(); err != nil {
						return FileEnd{}, errNotUp
					}
				}
			case frameFileEnd:
				var e FileEnd
				if len(f.payload) == 40 {
					e.Size = int64(binary.BigEndian.Uint64(f.payload))
					copy(e.Sum[:], f.payload[8:])
				}
				if len(f.payload) != 40 || e.Size != res.Size || e.Sum != want || got != e.Size {
					m.abort(c.id, proto.FileBadContent)
					return FileEnd{}, &FileAbort{proto.FileBadContent}
				}
				if err := ack(); err != nil {
					return FileEnd{}, errNotUp
				}
				return e, nil
			}
		}
	}
}

// abort tells the far end this end gave up on stream id.
func (m *fileMux) abort(id uint32, word string) { m.s.sendFrame(id, frameFileAbort, []byte(word)) }

// abortServed aborts the stream that serves for src's transfer: the
// sender cancelled it, or the far end's receipt came. word "" ends it
// without telling the near end.
func (m *fileMux) abortServed(match func(FileSource) bool, word string) {
	m.mu.Lock()
	var hit []*fileServed
	for _, sv := range m.served {
		if match(sv.src) {
			hit = append(hit, sv)
		}
	}
	m.mu.Unlock()
	for _, sv := range hit {
		if word != "" {
			m.s.sendFrame(sv.id, frameFileAbort, []byte(word))
		}
		sv.stop(word)
	}
}

// fileFetch answers a paired server's peer.file.fetch: only for a file
// this server offered it (FileHost), from an offset within the file.
func (d *dispatcher) fileFetch(ctx context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerFileFetchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.file.fetch: %v", err)
	}
	if p.ID == "" || p.Stream == 0 || p.Offset < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.file.fetch needs an id, a stream and an offset")
	}
	s := sessionOf(ctx)
	if s == nil {
		return nil, errNotUp
	}
	src, err := d.files.ServeFile(d.caller, p.ID, p.Offset)
	if err != nil {
		return nil, err
	}
	if p.Offset > src.Size() {
		src.Close(proto.FileBadContent)
		return nil, proto.Errorf(proto.ErrBadParams, "peer.file.fetch: offset %d is past the file's %d bytes", p.Offset, src.Size())
	}
	if err := s.files.serve(p.Stream, src, p.Offset); err != nil {
		src.Close("refused")
		return nil, err
	}
	sum := src.Sum()
	return proto.PeerFileFetchResult{Size: src.Size(), SHA256: hex.EncodeToString(sum[:])}, nil
}

// FetchFile pulls transfer id, which the paired server alias offered
// this one, from offset into w (docs/SPEC.md §16.13). It returns once
// the stream ended: the file's size and hash as the far end's end frame
// said them, once its last byte is written and acked. ErrNoFiles when
// that server's tm takes no files; *FileAbort when either end aborted;
// a dropped link is any other error, after which a fetch from the bytes
// written resumes it.
func (m *Manager) FetchFile(ctx context.Context, alias, id string, offset int64, w io.Writer) (FileEnd, error) {
	if m == nil {
		return FileEnd{}, errNotUp
	}
	l := m.link(alias)
	if l == nil {
		return FileEnd{}, errNotUp
	}
	l.mu.Lock()
	s, caps := l.sess, l.st.Caps
	l.mu.Unlock()
	if s == nil {
		return FileEnd{}, errNotUp
	}
	if !slices.Contains(caps, CapFile) {
		return FileEnd{}, ErrNoFiles
	}
	return s.files.fetch(ctx, id, offset, w)
}

// AbortServed ends the streams serving transfers match picks, on every
// link, telling the far end word ("" tells nothing).
func (m *Manager) AbortServed(match func(FileSource) bool, word string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	var ss []*session
	for _, r := range m.links {
		r.l.mu.Lock()
		if r.l.sess != nil {
			ss = append(ss, r.l.sess)
		}
		r.l.mu.Unlock()
	}
	m.mu.Unlock()
	for _, s := range ss {
		s.files.abortServed(match, word)
	}
}

// ErrNoFiles means the paired server's tm takes no files (its hello
// doesn't list CapFile).
var ErrNoFiles = errors.New("the paired server's tm takes no files")
