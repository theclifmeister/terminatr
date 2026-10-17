package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestHelloTextIsCleaned(t *testing.T) {
	h := Hello{LinkProtocol: 1, Version: "v0.28.0\x1b]52;c;AAAA\x07\x1b[2J", Caps: []string{"pane", "x\x1b[31m", strings.Repeat("a", 99), "msg"}}.clean()
	if strings.ContainsAny(h.Version, "\x1b\x07") {
		t.Fatalf("version %q", h.Version)
	}
	if len(h.Caps) != 2 || h.Caps[0] != "pane" || h.Caps[1] != "msg" {
		t.Fatalf("caps %q", h.Caps)
	}
	many := make([]string, 100)
	for i := range many {
		many[i] = "cap"
	}
	if got := (Hello{Caps: many}).clean().Caps; len(got) != maxCaps {
		t.Fatalf("%d caps", len(got))
	}
}

func TestLinkErrorIsCleaned(t *testing.T) {
	var logged []string
	l := &link{logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	l.set(proto.LinkOffline, proto.LinkReasonRelay, errors.New("closed: \x1b]0;owned\x07\x1b[31mfake (4400)"), time.Time{})
	if e := l.status().Error; strings.ContainsAny(e, "\x1b\x07") || e == "" {
		t.Fatalf("error %q", e)
	}
	if len(logged) == 0 || strings.ContainsAny(strings.Join(logged, ""), "\x1b\x07") {
		t.Fatalf("log %q", logged)
	}
}

const osc52 = "\x1b]52;c;ZWNobyBvd25lZA==\a"

// hasEscape reports whether any string in v holds a control character.
func hasEscape(t *testing.T, v any) bool {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(b), `\u001b`) || strings.Contains(string(b), `\u0007`) || strings.Contains(string(b), `\u202e`)
}

// A peer's projects arrive cleaned: a slug that isn't one is dropped,
// and no string keeps an escape sequence or a bidi override.
func TestPeerProjectsAreCleaned(t *testing.T) {
	ps := cleanProjects(proto.PeerProjects{Projects: []proto.PeerProject{
		{Project: "web", Goal: "ship\u202e" + osc52},
		{Project: "x" + osc52},
		{Project: strings.Repeat("a", 65)},
	}})
	if len(ps.Projects) != 1 || ps.Projects[0].Project != "web" || hasEscape(t, ps) {
		t.Fatalf("%+v", ps)
	}
}

// A peer's watch arrives cleaned: threads and sessions whose ids aren't
// ids are dropped, other ids blanked, and every string, however deep,
// loses its control and format characters.
func TestPeerWatchIsCleaned(t *testing.T) {
	w := cleanWatch(proto.PeerWatch{
		Watch: proto.ProjectWatch{
			Project:  "web" + osc52,
			NeedsYou: []proto.WatchNeed{{Why: "review", Task: "T1" + osc52, Thread: "t-0001", Title: "a\nb" + osc52, Question: "q" + osc52}},
			Inbox:    []proto.WatchItem{{ID: "i1", Kind: "k" + osc52, Summary: "s" + osc52}},
			Threads: []proto.WatchThread{
				{ID: "t-0001", Title: "fix" + osc52, State: "blocked", Reason: "r" + osc52, Task: &proto.WatchTask{ID: "T1", Status: "started" + osc52}, PR: "#7" + osc52},
				{ID: "t-0002" + osc52},
			},
			Ready: []proto.WatchTodo{{Task: "T2", Title: "x" + osc52}},
			Cells: []proto.ModCell{{Mod: "m" + osc52}},
		},
		Sessions: []proto.PeerSession{
			{ID: "s-1", Project: "web", Thread: "t-0001", State: "blocked", Reason: "why" + osc52, Model: "m" + osc52, Current: "c\u202e"},
			{ID: "s-2" + osc52, Project: "web"},
			{ID: "s-3", Project: "p" + osc52},
		},
	})
	if hasEscape(t, w) {
		b, _ := json.Marshal(w)
		t.Fatalf("%s", b)
	}
	if len(w.Watch.Threads) != 1 || len(w.Sessions) != 1 || w.Watch.NeedsYou[0].Task != "" || w.Watch.NeedsYou[0].Thread != "t-0001" {
		t.Fatalf("%+v", w)
	}
	if strings.Contains(w.Watch.NeedsYou[0].Title, "\n") || w.Watch.Threads[0].Task.ID != "T1" {
		t.Fatalf("%+v", w.Watch)
	}
}

// A peer's error answer is one short line in its own code, or refused.
func TestPeerErrorIsCleaned(t *testing.T) {
	e := cleanError(&proto.Error{Code: "bad" + osc52, Message: "no\x1b[2J\nway" + strings.Repeat("x", 300)})
	if e.Code != proto.ErrRefused || hasEscape(t, e) || strings.Contains(e.Message, "\n") || len([]rune(e.Message)) > maxErrorRunes {
		t.Fatalf("%+v", e)
	}
	if e := cleanError(&proto.Error{Code: proto.ErrNotShared, Message: "gone"}); e.Code != proto.ErrNotShared || e.Message != "gone" {
		t.Fatalf("%+v", e)
	}
}

// A pane level the peer makes up is ignored, not stored and served.
func TestMirrorIgnoresBadPanes(t *testing.T) {
	m := &Mirror{peers: map[string]*mirrorPeer{"m": {link: proto.PeerLink{Alias: "mini", ID: "m"}, panes: proto.PanesView}}}
	m.SetPanes("m", "view"+osc52)
	if p := m.peers["m"].panes; p != proto.PanesView || m.gen != 0 {
		t.Fatalf("panes %q, gen %d", p, m.gen)
	}
}
