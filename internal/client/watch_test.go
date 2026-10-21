package client

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestNextEvent: a stream's reader skips the events its caller doesn't
// want, decodes each line afresh, and ends with the stream.
func TestNextEvent(t *testing.T) {
	lines := `{"event":"view.digest"}` + "\n" +
		`{"event":"watch.changed","watch":{"pr":"#1 open"}}` + "\n" +
		`{"event":"watch.changed"}` + "\n"
	c := &Client{br: bufio.NewReader(strings.NewReader(lines))}
	var skipped []string
	keep := func(ev proto.WatchEvent) bool {
		if ev.Event != proto.EventWatchChanged {
			skipped = append(skipped, ev.Event)
			return false
		}
		return true
	}
	ev, err := NextEvent(c, keep)
	if err != nil || ev.Event != proto.EventWatchChanged || ev.Watch.PR != "#1 open" || len(skipped) != 1 {
		t.Fatalf("first: %+v %v, skipped %q", ev, err, skipped)
	}
	if ev, err = NextEvent(c, keep); err != nil || ev.Event != proto.EventWatchChanged || ev.Watch.PR != "" {
		t.Fatalf("second: %+v %v", ev, err)
	}
	if _, err = NextEvent(c, keep); err != io.EOF {
		t.Fatalf("end: %v, want EOF", err)
	}
}
