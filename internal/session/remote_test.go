package session

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// remoteMirror plays the near server of a remote pane: a repaint starts
// a fresh emulator, output and resizes follow.
type remoteMirror struct {
	t        *testing.T
	term     *emu.Terminal
	repaints int
	closed   string
}

func (m *remoteMirror) apply(b []byte) {
	for len(b) >= 5 {
		typ := proto.FrameType(b[0])
		n := int(binary.BigEndian.Uint32(b[1:5]))
		p := b[5 : 5+n]
		b = b[5+n:]
		switch typ {
		case proto.FrameRepaint:
			cols, rows, err := proto.ParseSize(p[:4])
			if err != nil {
				m.t.Fatal(err)
			}
			if m.term != nil {
				m.term.Close()
			}
			term, err := emu.New(cols, rows)
			if err != nil {
				m.t.Fatal(err)
			}
			m.term = term
			m.term.Write(p[4:])
			m.repaints++
		case proto.FrameOutput:
			m.term.Write(p)
		case proto.FrameResize:
			c, r, _ := proto.ParseSize(p)
			m.term.Resize(c, r)
		case proto.FrameClosed:
			m.closed = string(p)
		case proto.FrameSnapshot:
			m.t.Fatal("a remote stream got a snapshot")
		}
	}
}

// pump sends the subscriber's batches to a channel until it ends.
func pump(sub *Subscriber) <-chan []byte {
	ch := make(chan []byte, 1024)
	go func() {
		defer close(ch)
		for {
			b, ok := sub.Next()
			if !ok {
				return
			}
			ch <- b
		}
	}()
	return ch
}

// drain applies the batches that have come, waiting a little for one.
func (m *remoteMirror) drain(ch <-chan []byte) {
	select {
	case b := <-ch:
		m.apply(b)
	case <-time.After(50 * time.Millisecond):
	}
	for {
		select {
		case b := <-ch:
			m.apply(b)
		default:
			return
		}
	}
}

// TestAttachRemote: a paired server's stream starts with a repaint any
// build can read, then follows the session's output and resizes.
func TestAttachRemote(t *testing.T) {
	s := start(t, "/bin/sh")
	s.Input([]byte("printf '\\033[1mbold\\033[0m %s\\n' x-$((6*7))\r"))
	testwait.Eventually(t, "output", func() bool { return strings.Contains(screen(t, s), "bold x-42") })
	sub, err := s.AttachRemote()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	m := &remoteMirror{t: t}
	b, _ := sub.Next()
	if proto.FrameType(b[0]) != proto.FrameRepaint {
		t.Fatalf("first frame %d, want a repaint", b[0])
	}
	m.apply(b)
	if got, _ := m.term.Screen(); got != screen(t, s) {
		t.Fatalf("after the repaint:\n%q\nwant\n%q", got, screen(t, s))
	}
	ch := pump(sub)
	s.Input([]byte("echo more-$((7*8))\r"))
	s.Resize(100, 30)
	testwait.Eventually(t, "the stream to follow", func() bool {
		m.drain(ch)
		got, _ := m.term.Screen()
		return strings.Contains(got, "more-56") && got == screen(t, s)
	})
	if c, r := m.term.Size(); c != 100 || r != 30 {
		t.Errorf("size %dx%d", c, r)
	}
}

// TestRemoteLagRepaints: a remote stream that falls more than
// MaxRemotePending behind drops its backlog for a repaint, at most one
// per RepaintEvery, and the mirror converges.
func TestRemoteLagRepaints(t *testing.T) {
	oldMax, oldEvery := MaxRemotePending, RepaintEvery
	MaxRemotePending, RepaintEvery = 2048, 300*time.Millisecond
	defer func() { MaxRemotePending, RepaintEvery = oldMax, oldEvery }()

	s := start(t, "/bin/sh")
	sub, err := s.AttachRemote()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	m := &remoteMirror{t: t}
	b, _ := sub.Next()
	m.apply(b)
	t0 := time.Now()
	s.Input([]byte("i=0; while [ $i -lt 3000 ]; do echo flood-$i; i=$((i+1)); done; echo done-$((6*7))\r"))
	testwait.Eventually(t, "flood to finish", func() bool { return strings.Contains(screen(t, s), "done-42") })
	if sub.Resyncs() == 0 {
		t.Fatal("the backlog was never dropped")
	}
	ch := pump(sub)
	testwait.Eventually(t, "the mirror to match", func() bool {
		m.drain(ch)
		got, _ := m.term.Screen()
		return got == screen(t, s)
	})
	// One repaint to start, then at most one per RepaintEvery.
	if most := 2 + int(time.Since(t0)/RepaintEvery); m.repaints > most {
		t.Errorf("%d repaints in %v, at most %d", m.repaints, time.Since(t0), most)
	}
	if m.repaints < 2 {
		t.Errorf("%d repaints: the lag never became one", m.repaints)
	}
}

// TestRemoteResync: Resync queues a repaint in place of the backlog.
func TestRemoteResync(t *testing.T) {
	s := start(t, "/bin/sh")
	sub, err := s.AttachRemote()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	sub.Next()
	time.Sleep(RepaintEvery)
	sub.Resync()
	b, _ := sub.Next()
	if proto.FrameType(b[0]) != proto.FrameRepaint {
		t.Fatalf("after Resync: frame %d", b[0])
	}
}

// TestRemoteRepaintHistory: the stream's first repaint carries the whole
// scrollback, and so does one asked for (prefix+R); one in place of a
// backlog only the last RepaintHistory rows.
func TestRemoteRepaintHistory(t *testing.T) {
	oldMax, oldEvery := MaxRemotePending, RepaintEvery
	MaxRemotePending, RepaintEvery = 4096, 50*time.Millisecond
	defer func() { MaxRemotePending, RepaintEvery = oldMax, oldEvery }()

	s := start(t, "/bin/sh")
	s.Input([]byte("i=0; while [ $i -lt 600 ]; do echo old-$i; i=$((i+1)); done; echo done-$((6*7))\r"))
	testwait.Eventually(t, "the history", func() bool { return strings.Contains(screen(t, s), "done-42") })
	history := func(m *remoteMirror) string {
		text, err := m.term.PlainText()
		if err != nil {
			t.Fatal(err)
		}
		return text
	}

	sub, err := s.AttachRemote()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Detach()
	m := &remoteMirror{t: t}
	b, _ := sub.Next()
	m.apply(b)
	if text := history(m); !strings.Contains(text, "old-0\n") || !strings.Contains(text, "old-599\n") {
		t.Fatalf("the first repaint lacks the scrollback:\n%s", text)
	}

	// A lag: the backlog becomes a repaint with the last rows only.
	s.Input([]byte("i=0; while [ $i -lt 2000 ]; do echo lag-$i; i=$((i+1)); done; echo end-$((7*8))\r"))
	testwait.Eventually(t, "the flood", func() bool { return strings.Contains(screen(t, s), "end-56") })
	if sub.Resyncs() == 0 {
		t.Fatal("the backlog was never dropped")
	}
	time.Sleep(2 * RepaintEvery)
	m.repaints = 0
	ch := pump(sub)
	testwait.Eventually(t, "the lag's repaint", func() bool {
		m.drain(ch)
		got, _ := m.term.Screen()
		return m.repaints > 0 && got == screen(t, s)
	})
	if text := history(m); strings.Contains(text, "old-0\n") {
		t.Errorf("a lag's repaint carried the whole scrollback")
	}

	// prefix+R: the whole scrollback again.
	sub.Resync()
	testwait.Eventually(t, "the asked-for repaint", func() bool {
		m.drain(ch)
		return strings.Contains(history(m), "old-0\n")
	})
}
