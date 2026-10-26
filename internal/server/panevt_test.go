package server

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// openPaneVT opens a pane.vt stream on a connection of its own and
// returns it with the answer.
func openPaneVT(t *testing.T, p client.Paths, id string) (*client.Client, proto.PaneVTResult) {
	t.Helper()
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	var res proto.PaneVTResult
	if err := c.Call(proto.MethodPaneVT, proto.PaneVTParams{Session: id}, &res); err != nil {
		t.Fatal(err)
	}
	return c, res
}

// paneEvents reads a pane.vt stream's events into a channel.
func paneEvents(c *client.Client) <-chan proto.PaneVTEvent {
	ch := make(chan proto.PaneVTEvent, 1024)
	go func() {
		defer close(ch)
		for {
			var ev proto.PaneVTEvent
			if err := c.ReadLine(&ev); err != nil {
				return
			}
			ch <- ev
		}
	}()
	return ch
}

// TestPaneVTStream: a client of any build gets a repaint its own
// emulator reads, then output and resizes; its keys reach the pane, and
// the server holds at most the window past its acks.
func TestPaneVTStream(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	if caps := mustHello(t, p).Caps; !slices.Contains(caps, proto.CapPaneVT) {
		t.Fatalf("hello caps %v, want %s", caps, proto.CapPaneVT)
	}
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir(), Cols: 80, Rows: 24}, &started)
	id := started.Session.ID
	call(t, c, proto.MethodSessionKeys, proto.SessionKeysParams{ID: id, Data: "echo before-attach\r"}, nil)
	testwait.Eventually(t, "the echo", func() bool {
		var rr proto.SessionReadResult
		call(t, c, proto.MethodSessionRead, proto.SessionReadParams{ID: id}, &rr)
		return strings.Count(rr.Text, "before-attach") >= 2
	})

	v, res := openPaneVT(t, p, id)
	if res.Session.ID != id || res.Window != proto.PaneVTWindow {
		t.Fatalf("answer %+v", res)
	}
	events := paneEvents(v)
	first := <-events
	if first.Event != proto.EventPaneRepaint || first.Cols != 80 || first.Rows != 24 {
		t.Fatalf("first event %+v, want an 80×24 repaint", first)
	}
	mirror, err := emu.New(first.Cols, first.Rows)
	if err != nil {
		t.Fatal(err)
	}
	defer mirror.Close()
	mirror.Write(first.Data)
	if got, _ := mirror.Screen(); !strings.Contains(got, "before-attach") {
		t.Fatalf("the repaint doesn't show the screen:\n%s", got)
	}

	v.WriteLine(proto.PaneVTInput{Input: []byte("echo via-pane-vt\r")})
	v.WriteLine(proto.PaneVTInput{Size: &proto.PaneSize{Cols: 100, Rows: 30}})
	resized := false
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream ended")
			}
			switch ev.Event {
			case proto.EventPaneOutput:
				mirror.Write(ev.Data)
				v.WriteLine(proto.PaneVTInput{Ack: len(ev.Data)})
			case proto.EventPaneResize:
				mirror.Resize(ev.Cols, ev.Rows)
				resized = ev.Cols == 100 && ev.Rows == 30
			case proto.EventPaneRepaint:
				t.Fatalf("a repaint mid-stream: %+v", ev)
			}
		case <-deadline:
			got, _ := mirror.Screen()
			t.Fatalf("no echo or resize (resized=%v):\n%s", resized, got)
		}
		// The command line and its echo.
		if got, _ := mirror.Screen(); resized && strings.Count(got, "via-pane-vt") >= 2 {
			break
		}
	}

	// A fresh repaint on request.
	v.WriteLine(proto.PaneVTInput{Repaint: true})
	for ev := range events {
		if ev.Event == proto.EventPaneRepaint {
			if ev.Cols != 100 || ev.Rows != 30 {
				t.Fatalf("repaint %+v", ev)
			}
			break
		}
	}
	v.WriteLine(proto.PaneVTInput{Detach: true})
	for range events {
	}
}

// TestPaneVTWindow: without acks the server stops at the window, and an
// ack lets the rest come.
func TestPaneVTWindow(t *testing.T) {
	defer func(w int) { paneVTWindow = w }(paneVTWindow)
	paneVTWindow = 4 << 10
	p := testPaths(t)
	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir(), Cols: 80, Rows: 24}, &started)
	id := started.Session.ID

	v, _ := openPaneVT(t, p, id)
	events := paneEvents(v)
	first := <-events
	taken := len(first.Data)
	v.WriteLine(proto.PaneVTInput{Ack: taken})
	// About 40 KiB of output, ten windows.
	v.WriteLine(proto.PaneVTInput{Input: []byte("i=0; while [ $i -lt 500 ]; do echo line-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done\r")})
	got := 0
	quiet := time.After(2 * time.Second)
wait:
	for {
		select {
		case ev := <-events:
			got += len(ev.Data)
		case <-quiet:
			break wait
		}
	}
	// What the window allows, plus at most one chunk sent before the
	// window filled.
	if got == 0 || got > paneVTWindow+paneVTChunk {
		t.Fatalf("took %d bytes unacked, window %d", got, paneVTWindow)
	}
	v.WriteLine(proto.PaneVTInput{Ack: got})
	more := false
	timeout := time.After(5 * time.Second)
	for !more {
		select {
		case ev := <-events:
			more = len(ev.Data) > 0
		case <-timeout:
			t.Fatal("nothing after the ack")
		}
	}
}

// TestPaneVTRefusals: an unknown session, a pane of no paired server and
// an agent's caller are refused.
func TestPaneVTRefusals(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range []string{"", "s-404", "mini/s-1"} {
		var res proto.PaneVTResult
		if err := c.Call(proto.MethodPaneVT, proto.PaneVTParams{Session: id}, &res); err == nil {
			t.Errorf("pane.vt %q: answered %+v", id, res)
		}
		c.Close()
		if c, err = client.Connect(p, false); err != nil {
			t.Fatal(err)
		}
	}
}
