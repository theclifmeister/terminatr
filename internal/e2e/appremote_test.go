package e2e

import (
	"bytes"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestAppBridgeRemotePane (T742): the app on studio lists mini's thread
// from peer.list, as "mini/<id>", and opens its pane through pane.vt,
// which studio's server proxies over the link under mini's switch: off
// refuses it, view shows it and drops keys, type lets the app's keys
// reach mini's agent, and off again closes the open pane in studio's
// words.
func TestAppBridgeRemotePane(t *testing.T) {
	mini, _, _ := threadEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\n")

	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	remote := "mini/" + th.ID

	b := runBridge(t, studio)
	if !slices.Contains(b.Hello.Server.Caps, proto.CapPaneVTRemote) {
		t.Fatalf("caps %v", b.Hello.Server.Caps)
	}
	var last appapi.PeerList
	if !Poll(30*time.Second, func() bool {
		if err := b.Call(proto.MethodPeerList, nil, &last); err != nil {
			t.Fatal(err)
		}
		for _, p := range last.Peers {
			for _, pr := range p.Projects {
				if slices.ContainsFunc(pr.Sessions, func(s appapi.PeerSession) bool { return s.ID == remote && s.Thread == "t-0001" }) {
					return p.Pane
				}
			}
		}
		time.Sleep(time.Second) // the mirror reads mini again
		return false
	}) {
		t.Fatalf("peer.list never listed %s: %+v", remote, last)
	}

	// Off (the default): refused, saying how to turn it on.
	var pr appapi.PaneResult
	if _, err := b.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: remote}, &pr); appCode(err) != proto.ErrPanesOff {
		t.Fatalf("pane.vt with panes off: %v", err)
	}

	// No transcript crosses a link, whatever the switch.
	var tr appapi.TranscriptResult
	if _, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: remote}, &tr); appCode(err) != proto.ErrRefused {
		t.Fatalf("transcript.watch of a remote session: %v", err)
	}

	// View: the pane shows, view-only; keys are dropped.
	mini.MustCLI("peer", "panes", "studio", "view")
	pane, err := b.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: remote}, &pr)
	if err != nil || pr.Session.ID != remote || pr.Session.Server != "mini" || !pr.Session.ViewOnly {
		t.Fatalf("pane.vt at view: %+v %v", pr.Session, err)
	}
	defer pane.Cancel()
	var ev appapi.PaneEvent
	if name, err := pane.Next(&ev); err != nil || name != proto.EventPaneRepaint || len(ev.Data) == 0 {
		t.Fatalf("first event %q %v", name, err)
	}
	if err := b.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte("dropped at view\r")}, nil); err != nil {
		t.Fatal(err)
	}

	// Type: the app's keys reach mini's agent (the open pane follows
	// the switch at once).
	mini.MustCLI("peer", "panes", "studio", "write")
	got := func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "hello from the app") }
	if !Poll(agentWait, func() bool {
		if err := b.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte("hello from the app\r")}, nil); err != nil {
			t.Fatal(err)
		}
		return Poll(2*time.Second, func() bool { return slices.ContainsFunc(mini.FakeRecords("prompt"), got) })
	}) {
		t.Fatal("the app's keys never reached mini's thread")
	}
	if slices.ContainsFunc(mini.FakeRecords("prompt"), func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "dropped at view") }) {
		t.Fatal("a key typed at view reached mini's agent")
	}

	// Off again: the pane closes, in studio's words.
	mini.MustCLI("peer", "panes", "studio", "off")
	deadline := time.Now().Add(agentWait)
	for {
		ev = appapi.PaneEvent{}
		name, err := pane.Next(&ev)
		if err != nil {
			t.Fatalf("the stream ended before pane.closed: %v", err)
		}
		if name == proto.EventPaneClosed {
			if !strings.Contains(ev.Reason, "mini turned its panes off for this server") {
				t.Fatalf("closed: %q", ev.Reason)
			}
			break
		}
		if name == proto.EventPaneOutput && bytes.Contains(ev.Data, []byte("dropped at view")) {
			t.Fatal("the pane echoed a key typed at view")
		}
		if time.Now().After(deadline) {
			t.Fatal("no pane.closed")
		}
	}
	mini.AssertAlive(th)
}
