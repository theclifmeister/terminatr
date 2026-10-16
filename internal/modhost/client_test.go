package modhost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modproto"
)

// The server's side, modproto.Client, against a real host: the event
// call returns at ctx's end, the late answer is dropped, and the host's
// exit is ErrClosed.
func TestClientEventLimit(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	code := make(chan int, 1)
	go func() {
		code <- Run(Options{Name: "main.js", In: inR, Out: outW, Bind: testBind, Script: `
			$.on("mod.ready", (data, meta) => $.log(meta.name, meta.project, meta.seq, meta.dropped, data.projects.length));
			$.on("hang", async () => { await $.call("never"); });
			$.on("ask", async () => { $.log("got", await $.call("project.get", {slug: "tm"})); });
		`})
		outW.Close()
	}()
	logs := make(chan string, 10)
	var c *modproto.Client
	c = modproto.NewClient(outR, inW, func(m *modproto.Message) {
		if m.Method == "project.get" {
			_ = c.Conn().Reply(*m.ID, "ok")
		} // "never" is never answered
	}, func(m *modproto.Message) {
		if m.Method == modproto.MethodLog {
			logs <- logText(m)
		}
	})
	ctx := context.Background()
	if r, err := c.Hello(ctx, modproto.Hello{Mod: "test"}); err != nil || len(r.Events) != 3 {
		t.Fatalf("hello %+v %v", r, err)
	}
	if _, err := c.Event(ctx, modproto.Event{Name: "mod.ready", Project: "tm", Seq: 7, Dropped: 2, Data: json.RawMessage(`{"projects":[1]}`)}); err != nil {
		t.Fatal(err)
	}
	if got := <-logs; got != "mod.ready tm 7 2 1" {
		t.Fatalf("log %q", got)
	}
	hctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := c.Event(hctx, modproto.Event{Name: "hang"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hang: %v", err)
	}
	// The next event goes through while the hung one still waits.
	if r, err := c.Event(ctx, modproto.Event{Name: "ask"}); err != nil || r.Handlers != 1 {
		t.Fatalf("ask %+v %v", r, err)
	}
	if got := <-logs; got != "got ok" {
		t.Fatalf("log %q", got)
	}
	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := <-code; got != modproto.ExitOK {
		t.Fatalf("exit %d", got)
	}
	<-c.Done()
	if _, err := c.Event(ctx, modproto.Event{Name: "ask"}); !errors.Is(err, modproto.ErrClosed) {
		t.Fatalf("after exit: %v", err)
	}
}
