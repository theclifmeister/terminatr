package server

// Watching the session list (sessions.watch): session.list's result,
// sent again whenever it changes, so the apps show an agent's question
// or approval as its dialog opens and drop it as soon as it is
// answered, wherever. Agent state changes wake it at once (s.watch);
// the rest (queues, titles, mods) is caught by the re-read every
// watchPoll.

import (
	"bufio"
	"net"
	"strings"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func (s *Server) serveSessionsWatch(c net.Conn, br *bufio.Reader, req proto.Request) {
	// Join before the first read, so no change between them is missed.
	wake := s.watch.add()
	defer s.watch.remove(wake)
	l := s.sessionList()
	if !answer(c, req, l) {
		return
	}
	var last lastSent
	last.changed(sessionsKey(l))
	pollStream(c, clientGone(br), wake, watchPollEvery(), func() (any, bool) {
		if s.isStopping() {
			return nil, true
		}
		l := s.sessionList()
		if !last.changed(sessionsKey(l)) {
			return nil, false
		}
		return proto.SessionsWatchEvent{Event: proto.EventSessionsChanged, SessionListResult: l}, false
	})
}

// sessionsKey is l as a change is judged: an agent's title without the
// spinner it animates while it works, which would send the list every
// poll for nothing.
func sessionsKey(l proto.SessionListResult) proto.SessionListResult {
	k := l
	k.Sessions = make([]proto.SessionInfo, len(l.Sessions))
	for i, in := range l.Sessions {
		in.Title = titleKey(in.Title)
		k.Sessions[i] = in
	}
	return k
}

// titleKey is title without a leading glyph and its space ("✳ Fix
// the build", "⠂ Fix the build"): an agent's spinner frame.
func titleKey(title string) string {
	glyph, rest, ok := strings.Cut(title, " ")
	if !ok || len([]rune(glyph)) != 1 {
		return title
	}
	r := []rune(glyph)[0]
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return title
	}
	return rest
}
