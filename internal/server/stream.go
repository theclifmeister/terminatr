package server

// The skeleton the stream methods share (view.subscribe, session.watch,
// session.ask, project.watch, projects.watch): one answer, then events
// on the same connection until the client hangs up.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// refuse answers req with perr: the stream doesn't start.
func refuse(c net.Conn, req proto.Request, perr *proto.Error) {
	proto.WriteLine(c, proto.Response{ID: req.ID, Error: perr})
}

// answer answers req with result, which the stream's events then
// follow; false when the answer can't be written, and the stream ends.
func answer(c net.Conn, req proto.Request, result any) bool {
	resp := proto.Response{ID: req.ID}
	resp.Result, _ = json.Marshal(result)
	return proto.WriteLine(c, resp) == nil
}

// clientGone is closed when the client of a stream hangs up: it sends
// nothing after its request, so a read returning is it leaving.
func clientGone(br *bufio.Reader) <-chan struct{} {
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, br)
		close(gone)
	}()
	return gone
}

// watchPollEvery is how often a watch looks again between wakes:
// envWatchPoll's, else defaultWatchPoll.
func watchPollEvery() time.Duration {
	if poll := envDuration(envWatchPoll); poll > 0 {
		return poll
	}
	return defaultWatchPoll
}

// pollStream sends the client what look finds, on every wake and every
// poll, until the client hangs up (gone) or look ends the stream: look
// returns the event to send, nil for none, and whether the stream ends
// after it. A failed write ends it too.
func pollStream(c net.Conn, gone <-chan struct{}, wake <-chan struct{}, poll time.Duration, look func() (ev any, done bool)) {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-gone:
			return
		case <-wake:
		case <-t.C:
		}
		ev, done := look()
		if ev != nil {
			if err := proto.WriteLine(c, ev); err != nil {
				return
			}
		}
		if done {
			return
		}
	}
}

// lastSent is the state a watch last sent, marshalled: a look sends only
// what differs from it.
type lastSent []byte

// changed reports whether v differs from what was last sent, and keeps
// it as the last when it does.
func (l *lastSent) changed(v any) bool {
	b, _ := json.Marshal(v)
	if bytes.Equal(b, *l) {
		return false
	}
	*l = b
	return true
}
