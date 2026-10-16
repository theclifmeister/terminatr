package server

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestPollStream: a poll stream sends what its look finds on a wake
// and on its poll, nothing for a look that finds nothing, and ends
// after the look that ends it, or once the client hangs up.
func TestPollStream(t *testing.T) {
	srv, cli := net.Pipe()
	defer cli.Close()
	wake := make(chan struct{})
	looks := 0
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		pollStream(srv, make(chan struct{}), wake, time.Hour, func() (any, bool) {
			looks++
			switch looks {
			case 1:
				return nil, false
			case 2:
				return proto.AskEvent{Event: "one"}, false
			}
			return proto.AskEvent{Event: "last"}, true
		})
	}()
	br := bufio.NewReader(cli)
	for _, want := range []string{"one", "last"} {
		wake <- struct{}{}
		if want == "one" {
			wake <- struct{}{} // the first look finds nothing
		}
		var ev proto.AskEvent
		if err := proto.ReadLine(br, &ev); err != nil || ev.Event != want {
			t.Fatalf("event %+v %v, want %s", ev, err, want)
		}
	}
	<-ended

	// The client leaving ends it, the poll ticking meanwhile.
	gone := make(chan struct{})
	ended = make(chan struct{})
	go func() {
		defer close(ended)
		pollStream(srv, gone, nil, time.Millisecond, func() (any, bool) { return nil, false })
	}()
	close(gone)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("still streaming after the client left")
	}
}

// TestLastSent: a watch sends a state only when it differs from the
// last one sent.
func TestLastSent(t *testing.T) {
	var l lastSent
	if !l.changed(map[string]int{"a": 1}) {
		t.Fatal("first state not new")
	}
	if l.changed(map[string]int{"a": 1}) {
		t.Fatal("same state again is new")
	}
	if !l.changed(map[string]int{"a": 2}) {
		t.Fatal("changed state not new")
	}
}
