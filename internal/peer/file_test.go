package peer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// fakeFile is a file the far end serves from memory.
type fakeFile struct {
	data []byte
	kbps int

	mu     sync.Mutex
	acked  int64
	read   int64 // the furthest byte read
	closed []string
}

func (f *fakeFile) ReadAt(p []byte, off int64) (int, error) {
	n := copy(p, f.data[off:])
	f.mu.Lock()
	f.read = max(f.read, off+int64(n))
	f.mu.Unlock()
	return n, nil
}
func (f *fakeFile) Size() int64   { return int64(len(f.data)) }
func (f *fakeFile) Sum() [32]byte { return sha256.Sum256(f.data) }
func (f *fakeFile) Kbps() int     { return f.kbps }
func (f *fakeFile) Acked(n int64) { f.mu.Lock(); f.acked = n; f.mu.Unlock() }
func (f *fakeFile) Close(code string) {
	f.mu.Lock()
	f.closed = append(f.closed, code)
	f.mu.Unlock()
}

func (f *fakeFile) state() (acked, read int64, closed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acked, f.read, slices.Clone(f.closed)
}

// fileFarm serves fakeFiles by id to the paired server studio.
type fileFarm struct {
	mu    sync.Mutex
	files map[string]*fakeFile
	seen  []caller.Caller
}

func (h *fileFarm) ServeFile(c caller.Caller, id string, offset int64) (FileSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, c)
	f := h.files[id]
	if f == nil {
		return nil, proto.Errorf(proto.ErrUnknownFile, "no file %s", id)
	}
	return f, nil
}

func randomFile(n int) *fakeFile {
	b := make([]byte, n)
	rand.Read(b)
	return &fakeFile{data: b, kbps: 2048}
}

// filePair is two sessions over loopback TCP, b serving h's files to
// a's fetches, as the TLS server would; the conns are theirs to cut.
func filePair(t *testing.T, h FileHost) (a, b *session, ca, cb net.Conn) {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	a, b = newSession(ca), newSession(cb)
	a.ping, b.ping = time.Hour, time.Hour
	b.methods = (&dispatcher{files: h, caller: caller.ForPeer("studio")}).methods()
	b.files.slots = make(chan struct{}, maxFileStreams)
	b.files.top = 1 << 31
	go a.serve(func(time.Duration) {})
	go b.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return a, b, ca, cb
}

// TestFileStream: a file larger than the window comes whole, in order,
// with the end frame's size and hash; the far end closes it clean once
// the last byte is acked.
func TestFileStream(t *testing.T) {
	f := randomFile(600 << 10)
	h := &fileFarm{files: map[string]*fakeFile{"X-1": f}}
	a, _, _, _ := filePair(t, h)
	var got bytes.Buffer
	end, err := a.files.fetch(context.Background(), "X-1", 0, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), f.data) || end.Size != f.Size() || end.Sum != f.Sum() {
		t.Fatal("the bytes or the end differ")
	}
	testwait.Eventually(t, "the far end closed it", func() bool { _, _, c := f.state(); return len(c) == 1 })
	if acked, _, closed := f.state(); acked != f.Size() || closed[0] != "" {
		t.Fatalf("acked %d closed %q", acked, closed)
	}
	if len(h.seen) != 1 || h.seen[0].Alias != "studio" {
		t.Fatalf("callers %+v", h.seen)
	}
	if _, err := a.files.fetch(context.Background(), "X-9", 0, &got); err == nil || err.(*proto.Error).Code != proto.ErrUnknownFile {
		t.Fatalf("unknown file: %v", err)
	}
}

// cutWriter cuts the link once at bytes have been written.
type cutWriter struct {
	buf bytes.Buffer
	at  int
	cut func()
}

func (w *cutWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.cut != nil && w.buf.Len() >= w.at {
		w.cut()
		w.cut = nil
	}
	return len(p), nil
}

// TestFileResume: cut at random points, each fetch goes on from the
// bytes written, and the whole is byte for byte the file.
func TestFileResume(t *testing.T) {
	f := randomFile(400 << 10)
	h := &fileFarm{files: map[string]*fakeFile{"X-1": f}}
	var all bytes.Buffer
	for i := 0; ; i++ {
		a, _, ca, _ := filePair(t, h)
		var cut [2]byte
		rand.Read(cut[:])
		w := &cutWriter{at: 1 + int(binary.BigEndian.Uint16(cut[:]))%(150<<10), cut: func() { ca.Close() }}
		if i == 3 {
			w.cut = nil
		}
		_, err := a.files.fetch(context.Background(), "X-1", int64(all.Len()), w)
		all.Write(w.buf.Bytes())
		if err == nil {
			break
		}
		if i == 3 {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(all.Bytes(), f.data) {
		t.Fatalf("resumed file differs: %d bytes, want %d", all.Len(), len(f.data))
	}
}

// badFar answers peer.file.fetch for a 64 KiB file, then sends what
// frames does on the stream.
func badFar(t *testing.T, frames func(s *session, stream uint32, data []byte)) *session {
	t.Helper()
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	a, b := newSession(ca), newSession(cb)
	a.ping, b.ping = time.Hour, time.Hour
	data := make([]byte, 64<<10)
	rand.Read(data)
	sum := sha256.Sum256(data)
	b.methods = map[string]handler{proto.MethodPeerFileFetch: func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p proto.PeerFileFetchParams
		json.Unmarshal(raw, &p)
		go frames(sessionOf(ctx), p.Stream, data)
		return proto.PeerFileFetchResult{Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
	}}
	go a.serve(func(time.Duration) {})
	go b.serve(func(time.Duration) {})
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return a
}

func dataFrame(off int64, p []byte) []byte {
	return append(binary.BigEndian.AppendUint64(nil, uint64(off)), p...)
}

// TestFileBadFar: a far end that skips bytes, sends past the size or
// ends with another hash is aborted bad-content (too-big), and the near
// end keeps nothing it shouldn't.
func TestFileBadFar(t *testing.T) {
	for name, tc := range map[string]struct {
		frames func(s *session, id uint32, data []byte)
		code   string
	}{
		"gap": {func(s *session, id uint32, data []byte) {
			s.sendFrame(id, frameFileData, dataFrame(0, data[:100]))
			s.sendFrame(id, frameFileData, dataFrame(200, data[200:300]))
		}, proto.FileBadContent},
		"overrun": {func(s *session, id uint32, data []byte) {
			for off := 0; off < len(data); off += 16 << 10 {
				s.sendFrame(id, frameFileData, dataFrame(int64(off), data[off:off+16<<10]))
			}
			s.sendFrame(id, frameFileData, dataFrame(int64(len(data)), []byte("more")))
		}, proto.FileTooBig},
		"wrong hash": {func(s *session, id uint32, data []byte) {
			for off := 0; off < len(data); off += 16 << 10 {
				s.sendFrame(id, frameFileData, dataFrame(int64(off), data[off:off+16<<10]))
			}
			end := binary.BigEndian.AppendUint64(nil, uint64(len(data)))
			s.sendFrame(id, frameFileEnd, append(end, make([]byte, 32)...))
		}, proto.FileBadContent},
		"aborted": {func(s *session, id uint32, data []byte) {
			s.sendFrame(id, frameFileAbort, []byte("\x1b[2Jwhatever"))
		}, proto.FileBadContent},
		"cancelled": {func(s *session, id uint32, data []byte) {
			s.sendFrame(id, frameFileAbort, []byte(proto.FileCancelled))
		}, proto.FileCancelled},
	} {
		a := badFar(t, tc.frames)
		var got bytes.Buffer
		_, err := a.files.fetch(context.Background(), "X-1", 0, &got)
		var ab *FileAbort
		if !errors.As(err, &ab) || ab.Code != tc.code {
			t.Errorf("%s: %v, want %s", name, err, tc.code)
		}
	}
}

// blockWriter holds each write until it is let go.
type blockWriter struct {
	buf  bytes.Buffer
	gate chan struct{}
}

func (w *blockWriter) Write(p []byte) (int, error) {
	<-w.gate
	return w.buf.Write(p)
}

// TestFileWindow: a near end that doesn't write (so doesn't ack) holds
// the far end to its window; let go, the rest comes.
func TestFileWindow(t *testing.T) {
	old := fileWindow
	fileWindow = 64 << 10
	t.Cleanup(func() { fileWindow = old })
	f := randomFile(512 << 10)
	a, _, _, _ := filePair(t, &fileFarm{files: map[string]*fakeFile{"X-1": f}})
	w := &blockWriter{gate: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := a.files.fetch(context.Background(), "X-1", 0, w)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if _, read, _ := f.state(); read > int64(fileWindow)+fileData {
		t.Fatalf("read %d bytes with nothing acked, window %d", read, fileWindow)
	}
	close(w.gate)
	if err := <-done; err != nil || !bytes.Equal(w.buf.Bytes(), f.data) {
		t.Fatalf("after the gate: %v", err)
	}
}

// TestFileStall: a near end that stops acking is given up as stalled;
// the far end says so, and the near end hears it.
func TestFileStall(t *testing.T) {
	old := fileStall
	fileStall = 300 * time.Millisecond
	t.Cleanup(func() { fileStall = old })
	f := randomFile(512 << 10)
	a, _, _, _ := filePair(t, &fileFarm{files: map[string]*fakeFile{"X-1": f}})
	w := &blockWriter{gate: make(chan struct{})}
	time.AfterFunc(time.Second, func() { close(w.gate) })
	_, err := a.files.fetch(context.Background(), "X-1", 0, w)
	var ab *FileAbort
	if !errors.As(err, &ab) || ab.Code != proto.FileStalled {
		t.Fatalf("near end: %v", err)
	}
	testwait.Eventually(t, "the far end closed it", func() bool { _, _, c := f.state(); return len(c) == 1 })
	if _, _, c := f.state(); c[0] != proto.FileStalled {
		t.Fatalf("far end closed %q", c)
	}
}

// TestFileCancelNear: the near end's cancel aborts the stream with its
// word, and the far end closes with it.
func TestFileCancelNear(t *testing.T) {
	f := randomFile(4 << 20)
	f.kbps = 64
	a, _, _, _ := filePair(t, &fileFarm{files: map[string]*fakeFile{"X-1": f}})
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(200*time.Millisecond, func() { cancel(&FileAbort{Code: proto.FileCancelled}) })
	var got bytes.Buffer
	if _, err := a.files.fetch(ctx, "X-1", 0, &got); err == nil {
		t.Fatal("not cancelled")
	}
	testwait.Eventually(t, "the far end closed it", func() bool { _, _, c := f.state(); return len(c) == 1 })
	if _, _, c := f.state(); c[0] != proto.FileCancelled {
		t.Fatalf("far end closed %q", c)
	}
}

// TestFileSlots: the far end serves maxFileStreams at once, then busy.
func TestFileSlots(t *testing.T) {
	_, b, _, _ := filePair(t, &fileFarm{})
	for i := range maxFileStreams + 1 {
		f := randomFile(1 << 20)
		f.kbps = 64
		err := b.files.serve(uint32(i+1), f, 0)
		var pe *proto.Error
		if i < maxFileStreams && err != nil || i == maxFileStreams && (!errors.As(err, &pe) || pe.Code != proto.ErrFileBusy) {
			t.Fatalf("stream %d: %v", i+1, err)
		}
	}
}

// TestPacer: the bucket holds the pace; a slow ack halves it, down to
// the floor, once a round trip; it climbs back a tenth a second.
func TestPacer(t *testing.T) {
	clock := time.Unix(0, 0)
	var slept time.Duration
	p := newPacer(256 << 10)
	p.now = func() time.Time { return clock }
	p.sleep = func(d time.Duration) { slept += d; clock = clock.Add(d) }
	for range 17 {
		p.wait(16 << 10)
	}
	// 17 frames, one ahead: 16 × 16 KiB at 256 KiB/s is a second.
	if math.Abs(slept.Seconds()-1) > 0.01 {
		t.Fatalf("slept %v for 16 frames at 256 KiB/s", slept)
	}
	p.rtt(100*time.Millisecond, 20*time.Millisecond)
	if p.pace() != 256<<10 {
		t.Fatalf("a fast ack slowed it: %v", p.pace())
	}
	p.rtt(400*time.Millisecond, 20*time.Millisecond)
	p.rtt(400*time.Millisecond, 20*time.Millisecond) // the same round trip
	if p.pace() != 128<<10 {
		t.Fatalf("after a slow ack %v", p.pace())
	}
	for range 5 {
		clock = clock.Add(time.Second)
		p.rtt(900*time.Millisecond, 0)
	}
	if p.pace() != fileMinRate {
		t.Fatalf("not floored: %v", p.pace())
	}
	clock = clock.Add(3 * time.Second)
	if r := p.pace(); math.Abs(r-fileMinRate*1.331) > 1 {
		t.Fatalf("after 3 s: %v, want %v", r, fileMinRate*1.331)
	}
	clock = clock.Add(time.Minute)
	if p.pace() != 256<<10 {
		t.Fatalf("not back to the setting: %v", p.pace())
	}
}

// TestFileCaps: the hello lists file only with a file host.
func TestFileCaps(t *testing.T) {
	m := &Manager{}
	if slices.Contains(m.caps(), CapFile) {
		t.Fatal("file without a host")
	}
	m.opts.Files = &fileFarm{}
	if !slices.Contains(m.caps(), CapFile) {
		t.Fatal("no file with a host")
	}
	if d := (&dispatcher{files: &fileFarm{}}).methods(); d[proto.MethodPeerFileFetch] == nil {
		t.Fatal("no peer.file.fetch")
	}
	if d := (&dispatcher{msgs: nil, panesHere: func(proto.PeerPanesParams) {}}).methods(); d[proto.MethodPeerFileFetch] != nil {
		t.Fatal("peer.file.fetch without a host")
	}
}
