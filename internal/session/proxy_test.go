package session

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// consoleMirror plays a console attached to a proxy: a snapshot starts
// it, output and resizes follow, a closed frame ends it.
type consoleMirror struct {
	t      *testing.T
	term   *emu.Terminal
	closed string
}

func (m *consoleMirror) apply(b []byte) {
	for len(b) >= 5 {
		typ := proto.FrameType(b[0])
		n := int(b[1])<<24 | int(b[2])<<16 | int(b[3])<<8 | int(b[4])
		p := b[5 : 5+n]
		b = b[5+n:]
		switch typ {
		case proto.FrameSnapshot:
			term, err := emu.Decode(p)
			if err != nil {
				m.t.Fatal(err)
			}
			m.term = term
		case proto.FrameOutput:
			m.term.Write(p)
		case proto.FrameResize:
			c, r, _ := proto.ParseSize(p)
			m.term.Resize(c, r)
		case proto.FrameClosed:
			m.closed = string(p)
		}
	}
}

// TestProxy: a proxy pane is empty until its first repaint, then serves
// consoles as a session does; a later repaint resyncs them with a
// snapshot, and Close ends them with its reason.
func TestProxy(t *testing.T) {
	p := NewProxy("mini/s-1", t.Logf)
	if _, err := p.Attach(); err == nil {
		t.Fatal("attached before the first repaint")
	}
	select {
	case <-p.Ready():
		t.Fatal("ready before the first repaint")
	default:
	}
	p.Output([]byte("dropped"))
	p.Repaint(40, 10, []byte("\x1b[1mhello\x1b[0m\r\nworld"))
	<-p.Ready()
	sub, err := p.Attach()
	if err != nil {
		t.Fatal(err)
	}
	m := &consoleMirror{t: t}
	b, _ := sub.Next()
	m.apply(b)
	p.Output([]byte(" and more"))
	p.Resize(50, 12)
	b, _ = sub.Next()
	m.apply(b)
	got, _ := m.term.Screen()
	want, _ := p.Screen()
	if got != want || !strings.Contains(got, "world and more") || strings.Contains(got, "dropped") {
		t.Fatalf("console %q, proxy %q", got, want)
	}
	if c, r := m.term.Size(); c != 50 || r != 12 {
		t.Errorf("size %dx%d", c, r)
	}
	// A new repaint: the console resyncs to it.
	p.Repaint(30, 8, []byte("fresh"))
	b, _ = sub.Next()
	m.apply(b)
	if got, _ := m.term.Screen(); !strings.HasPrefix(got, "fresh") || strings.Contains(got, "world") {
		t.Fatalf("after a repaint %q", got)
	}
	if p.Subscribers() != 1 {
		t.Errorf("subscribers %d", p.Subscribers())
	}
	p.Close("session exited on mini")
	b, _ = sub.Next()
	m.apply(b)
	if m.closed != "session exited on mini" {
		t.Errorf("closed %q", m.closed)
	}
	if _, ok := sub.Next(); ok {
		t.Error("the stream goes on after Close")
	}
}
