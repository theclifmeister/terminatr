package modproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConnRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(nil, &buf)
	_ = c.Request(1, MethodEvent, Event{Name: "clock.minute", Data: json.RawMessage(`{"now":1}`)})
	_ = c.Notify(MethodLog, Log{Level: "info", Text: "hi"})
	_ = c.Reply(1, nil)
	_ = c.ReplyError(2, CodeMethodNotFound, "no")
	if strings.Count(buf.String(), "\n") != 4 {
		t.Fatalf("not one line each:\n%s", buf.String())
	}

	r := NewConn(strings.NewReader(buf.String()+"\n{bad\n"+`{"jsonrpc":"1.0","method":"x"}`+"\n"+`{"jsonrpc":"2.0"}`+"\n"), io.Discard)
	m, err := r.Read()
	if err != nil || !m.IsRequest() || *m.ID != 1 || m.Method != MethodEvent {
		t.Fatalf("request %+v %v", m, err)
	}
	if m, _ = r.Read(); !m.IsNotification() || m.Method != MethodLog {
		t.Fatalf("notification %+v", m)
	}
	if m, _ = r.Read(); !m.IsResponse() || string(m.Result) != "null" {
		t.Fatalf("null reply %+v", m)
	}
	if m, _ = r.Read(); !m.IsResponse() || m.Error == nil || m.Error.Code != CodeMethodNotFound {
		t.Fatalf("error reply %+v", m)
	}
	// Bad lines are reported, and reading goes on.
	for i := 0; i < 3; i++ {
		var fe *FrameError
		if _, err := r.Read(); !errors.As(err, &fe) {
			t.Fatalf("bad line %d: %v", i, err)
		}
	}
	if _, err := r.Read(); err != io.EOF {
		t.Fatalf("end: %v", err)
	}
}

func TestSendRefusesAnOverlongLine(t *testing.T) {
	c := NewConn(nil, io.Discard)
	if err := c.Notify(MethodLog, Log{Text: strings.Repeat("x", MaxLine)}); err == nil {
		t.Fatal("sent a line over MaxLine")
	}
}

// TestCallAnswerBeforeExit: a host that answers a call and exits at once
// (shutdown) could give the call ErrClosed: when the caller came to wait
// only after the answer and the end of the host's stdout were both in,
// select picked either (TestClientEventLimit in internal/modhost, T283).
// The writer holds the caller after its request until the end is in.
func TestCallAnswerBeforeExit(t *testing.T) {
	for i := 0; i < 50; i++ {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		ended := make(chan struct{})
		go func() {
			host := NewConn(inR, outW)
			if m, err := host.Read(); err == nil {
				_ = host.Reply(*m.ID, nil)
			}
			outW.Close()
		}()
		c := NewClient(outR, heldWriter{inW, ended}, nil, nil)
		go func() { <-c.Done(); close(ended) }()
		if err := c.Shutdown(context.Background()); err != nil {
			t.Fatalf("try %d: %v", i, err)
		}
		inW.Close()
	}
}

// heldWriter returns from each write only once ended is closed.
type heldWriter struct {
	w     io.Writer
	ended chan struct{}
}

func (h heldWriter) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	<-h.ended
	return n, err
}
