package server

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestSessionsWatch: sessions.watch answers the list, then sends it
// again as the server changes it, woken without waiting for its poll.
func TestSessionsWatch(t *testing.T) {
	p := testPaths(t)
	t.Setenv(envWatchPoll, "1h")
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var first proto.SessionListResult
	st, err := client.OpenStream(p, proto.MethodSessionsWatch, nil, &first)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if len(first.Sessions) != 0 || first.Version == "" {
		t.Fatalf("first list %+v", first)
	}
	got := make(chan proto.SessionsWatchEvent, 16)
	go func() {
		defer close(got)
		for {
			ev, err := client.NextEvent(st, func(ev proto.SessionsWatchEvent) bool { return ev.Event == proto.EventSessionsChanged })
			if err != nil {
				return
			}
			got <- ev
		}
	}()
	next := func(what string, ok func(proto.SessionListResult) bool) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case ev, open := <-got:
				if !open {
					t.Fatalf("stream ended waiting for %s", what)
				}
				if ok(ev.SessionListResult) {
					return
				}
			case <-deadline:
				t.Fatalf("no line with %s within 2s", what)
			}
		}
	}
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	id := started.Session.ID
	next("the session", func(l proto.SessionListResult) bool { return len(l.Sessions) == 1 && l.Sessions[0].ID == id })
	call(t, c, proto.MethodSessionStop, proto.SessionIDParams{ID: id}, nil)
	next("no session", func(l proto.SessionListResult) bool { return len(l.Sessions) == 0 })
}

// TestSessionsKey: an agent's spinner frame in its title changes
// nothing; its words do.
func TestSessionsKey(t *testing.T) {
	for title, want := range map[string]string{
		"✳ Fix the build": "Fix the build",
		"⠂ Fix the build": "Fix the build",
		"◐ Fix the build": "Fix the build",
		"Fix the build":   "Fix the build",
		"A title":         "A title",
		"zsh":             "zsh",
		"":                "",
	} {
		if got := titleKey(title); got != want {
			t.Errorf("titleKey(%q) = %q, want %q", title, got, want)
		}
	}
	a := proto.SessionListResult{Sessions: []proto.SessionInfo{{ID: "s-1", Title: "⠂ Fix", State: "working"}}}
	b := proto.SessionListResult{Sessions: []proto.SessionInfo{{ID: "s-1", Title: "⠐ Fix", State: "working"}}}
	var last lastSent
	last.changed(sessionsKey(a))
	if last.changed(sessionsKey(b)) {
		t.Error("a spinner frame changed the list")
	}
	b.Sessions[0].State = "blocked"
	if !last.changed(sessionsKey(b)) {
		t.Error("a state change didn't")
	}
	if a.Sessions[0].Title != "⠂ Fix" {
		t.Error("sessionsKey changed the list it was given")
	}
}
