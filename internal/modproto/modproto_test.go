package modproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConnRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(nil, &buf)
	_ = c.Request(1, MethodEvent, Event{Name: "clock.minute", Payload: json.RawMessage(`{"now":1}`)})
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
