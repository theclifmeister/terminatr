package session

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

func start(t *testing.T, argv ...string) *Session {
	t.Helper()
	s, err := Start(Config{
		ID:   "s-test",
		Role: proto.RoleShell,
		Argv: argv,
		Cwd:  t.TempDir(),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color", "PS1=$ ", "LANG=C.UTF-8"},
		Cols: 80, Rows: 24,
		Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	return s
}

// never fails the test if bad turns true within d: the negative of
// eventually, for "nothing happens". It polls instead of sleeping the
// whole window, so a failure shows at once; d is a bound on how long a
// regression may hide, not a wait for a result, so keep the clocks it
// guards (PromptHold, ModPollTimeout) well under it.
func never(t *testing.T, d time.Duration, what string, bad func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if bad() {
			t.Fatalf("happened within %v: %s", d, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bad() {
		t.Fatalf("happened within %v: %s", d, what)
	}
}

func screen(t *testing.T, s *Session) string {
	t.Helper()
	r, err := s.Read(false)
	if err != nil {
		t.Fatal(err)
	}
	return r.Text
}

func TestInputAndRead(t *testing.T) {
	s := start(t, "/bin/sh")
	s.Input([]byte("echo hi-$((40+2))\r"))
	testwait.Eventually(t, "echo output", func() bool { return strings.Contains(screen(t, s), "hi-42") })
}

// mirror plays an attach client: it rebuilds the pane from the snapshot
// and then applies the ordered stream.
type mirror struct {
	term    *emu.Terminal
	snaps   int
	closed  string
	updates chan struct{}
}

func runMirror(t *testing.T, sub *Subscriber) *mirror {
	m := &mirror{updates: make(chan struct{}, 1)}
	frames := make(chan [][2][]byte)
	go func() {
		defer close(frames)
		for {
			b, ok := sub.Next()
			if !ok {
				return
			}
			var batch [][2][]byte
			r := bytes.NewReader(b)
			for r.Len() > 0 {
				typ, payload, err := proto.ReadFrame(r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				batch = append(batch, [2][]byte{{byte(typ)}, append([]byte(nil), payload...)})
			}
			frames <- batch
		}
	}()
	go func() {
		for batch := range frames {
			for _, f := range batch {
				m.apply(t, proto.FrameType(f[0][0]), f[1])
			}
			select {
			case m.updates <- struct{}{}:
			default:
			}
		}
	}()
	return m
}

func (m *mirror) apply(t *testing.T, typ proto.FrameType, payload []byte) {
	switch typ {
	case proto.FrameSnapshot:
		term, err := emu.Decode(payload)
		if err != nil {
			t.Error(err)
			return
		}
		if m.term != nil {
			m.term.Close()
		}
		m.term = term
		m.snaps++
	case proto.FrameOutput:
		m.term.Write(payload)
	case proto.FrameResize:
		c, r, err := proto.ParseSize(payload)
		if err != nil {
			t.Error(err)
			return
		}
		m.term.Resize(c, r)
	case proto.FrameClosed:
		m.closed = string(payload)
	}
}

func TestAttachMirrorMatchesServer(t *testing.T) {
	s := start(t, "/bin/sh")
	s.Input([]byte("echo before-attach\r"))
	testwait.Eventually(t, "first output", func() bool { return strings.Contains(screen(t, s), "before-attach") })

	sub, err := s.Attach()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	m := runMirror(t, sub)

	s.Input([]byte("for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do echo line-$i; done\r"))
	testwait.Eventually(t, "loop output", func() bool { return strings.Contains(screen(t, s), "line-30") })
	if err := s.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	s.Input([]byte("echo after-resize\r"))
	testwait.Eventually(t, "post-resize output", func() bool { return strings.Contains(screen(t, s), "after-resize") })

	testwait.Eventually(t, "mirror to match the server", func() bool {
		<-m.updates
		got, _ := m.term.Screen()
		return got == screen(t, s)
	})
	if c, r := m.term.Size(); c != 100 || r != 30 {
		t.Fatalf("mirror size %d×%d, want 100×30", c, r)
	}
	if m.snaps != 1 {
		t.Fatalf("mirror got %d snapshots, want 1", m.snaps)
	}
}

// TestRequestResizeCoalesces: the first request applies at once; a burst
// right after it collapses into one resize to the last size, once the
// quiet time is over.
func TestRequestResizeCoalesces(t *testing.T) {
	old := ResizeQuiet
	ResizeQuiet = 200 * time.Millisecond
	defer func() { ResizeQuiet = old }()

	s := start(t, "/bin/sh")
	sub, err := s.Attach()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	if s.Sized() {
		t.Fatal("a new session is sized")
	}
	if err := s.RequestResize(100, 30); err != nil {
		t.Fatal(err)
	}
	if got := s.Info(); got.Cols != 100 || got.Rows != 30 {
		t.Fatalf("first request not applied at once: %d×%d", got.Cols, got.Rows)
	}
	if !s.Sized() {
		t.Fatal("a resized session isn't sized")
	}
	for _, c := range []uint16{90, 91, 92} {
		if err := s.RequestResize(c, 20); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.Info(); got.Cols != 100 {
		t.Fatalf("a request within the quiet time applied at once: %d×%d", got.Cols, got.Rows)
	}
	testwait.Eventually(t, "the last request", func() bool { got := s.Info(); return got.Cols == 92 && got.Rows == 20 })

	// The stream saw exactly two resizes: 100×30, then 92×20. The
	// subscriber holds everything since attaching.
	var sizes []string
	for len(sizes) < 2 {
		b, ok := sub.Next()
		if !ok {
			t.Fatal("stream ended")
		}
		r := bytes.NewReader(b)
		for r.Len() > 0 {
			typ, payload, err := proto.ReadFrame(r, nil)
			if err != nil {
				t.Fatal(err)
			}
			if typ == proto.FrameResize {
				c, rows, _ := proto.ParseSize(payload)
				sizes = append(sizes, fmt.Sprintf("%dx%d", c, rows))
			}
		}
	}
	if strings.Join(sizes, " ") != "100x30 92x20" {
		t.Fatalf("resizes in the stream: %v, want 100x30 92x20", sizes)
	}
}

func TestSlowClientIsResynced(t *testing.T) {
	old := MaxPending
	MaxPending = 2048
	defer func() { MaxPending = old }()

	s := start(t, "/bin/sh")
	sub, err := s.Attach()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	// Nobody drains the subscriber while this runs.
	// The echoed command line must not match: wait for computed output.
	s.Input([]byte("i=0; while [ $i -lt 2000 ]; do echo flood-$i; i=$((i+1)); done; echo done-$((6*7))\r"))
	testwait.Eventually(t, "flood to finish", func() bool { return strings.Contains(screen(t, s), "done-42") })
	if sub.Resyncs() == 0 {
		t.Fatal("expected at least one resync")
	}
	m := runMirror(t, sub)
	testwait.Eventually(t, "mirror to match after resync", func() bool {
		<-m.updates
		got, _ := m.term.Screen()
		return got == screen(t, s)
	})
}

func TestStopEscalatesToKill(t *testing.T) {
	s := start(t, "/bin/sh", "-c", "trap '' HUP; echo ready; while :; do sleep 1; done")
	testwait.Eventually(t, "ready", func() bool { return strings.Contains(screen(t, s), "ready") })
	t0 := time.Now()
	s.Stop(300 * time.Millisecond)
	select {
	case <-s.Done():
	default:
		t.Fatal("session still running after Stop")
	}
	if d := time.Since(t0); d < 300*time.Millisecond {
		t.Fatalf("stopped after %v; SIGHUP should have been ignored", d)
	}
	if st := s.ExitStatus(); !strings.Contains(st, "killed") {
		t.Fatalf("exit status %q, want killed", st)
	}
}

func TestExitClosesSubscribers(t *testing.T) {
	exited := make(chan string, 1)
	s, err := Start(Config{
		ID: "s-exit", Role: proto.RoleShell,
		Argv: []string{"/bin/sh", "-c", "read x; exit 3"},
		Cwd:  "/", Env: []string{"PATH=/usr/bin:/bin"},
		OnExit: func(s *Session) { exited <- s.ExitStatus() },
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Attach()
	if err != nil {
		t.Fatal(err)
	}
	s.Input([]byte("\r"))
	var last proto.FrameType
	for {
		b, ok := sub.Next()
		if !ok {
			break
		}
		r := bytes.NewReader(b)
		for r.Len() > 0 {
			typ, _, err := proto.ReadFrame(r, nil)
			if err != nil {
				t.Fatal(err)
			}
			last = typ
		}
	}
	if last != proto.FrameClosed {
		t.Fatalf("last frame %d, want FrameClosed", last)
	}
	select {
	case st := <-exited:
		if st != "exit status 3" {
			t.Fatalf("exit status %q", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit not called")
	}
	if _, err := s.Read(false); err != ErrExited {
		t.Fatalf("Read after exit: %v", err)
	}
}

// TestColors: the program's OSC 11 query is answered with the colours a
// client reported, and with new ones once they change.
func TestColors(t *testing.T) {
	s := start(t, "/bin/sh")
	n := 0
	query := func(want string) {
		t.Helper()
		// The answer is 24 bytes; ESC and BEL shown as E and B. The
		// query is done only once stty sane has run and printed the
		// marker (computed, so the echoed command never matches): keys
		// typed while the tty is still raw reach Linux's sh as a line
		// with no newline, and it waits for the rest forever.
		n++
		marker := fmt.Sprintf("sane-%d", 100+n)
		s.Input([]byte(fmt.Sprintf("clear; stty raw -echo; printf '\\033]11;?\\007'; head -c 24 | tr '\\033\\007' EB; stty sane; echo; echo sane-$((100+%d))\r", n)))
		testwait.Eventually(t, want, func() bool {
			sc := screen(t, s)
			return strings.Contains(sc, want) && strings.Contains(sc, marker)
		})
	}
	var c emu.Colors
	c.Bg, c.Known = emu.RGB{R: 0x12, G: 0x34, B: 0x56}, emu.KnownBg
	if err := s.SetColors(c); err != nil {
		t.Fatal(err)
	}
	query("E]11;rgb:1212/3434/5656B")
	c.Bg = emu.RGB{R: 0xfa, G: 0xfa, B: 0xfa}
	if err := s.SetColors(c); err != nil {
		t.Fatal(err)
	}
	query("E]11;rgb:fafa/fafa/fafaB")
}

// TestInputDropsReplies (T686): a console's terminal replies, and the
// focus and mouse reports of modes the program has off, never reach it;
// its keys do.
func TestInputDropsReplies(t *testing.T) {
	s := start(t, "/bin/sh")
	s.Input([]byte("stty -echo; cat -v\r"))
	time.Sleep(300 * time.Millisecond)
	s.Input([]byte("\x1b[I\x1b]11;rgb:0/0/0\x07\x1b[?62;22c\x1b[<35;2;1M\x1b[12;40Rkeys-in\r"))
	testwait.Eventually(t, "the keys", func() bool { return strings.Contains(screen(t, s), "keys-in") })
	if sc := screen(t, s); strings.Contains(sc, "^[") {
		t.Fatalf("a reply or report reached the program:\n%s", sc)
	}
}
