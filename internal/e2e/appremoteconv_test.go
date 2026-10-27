package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestAppBridgeRemoteConversation (T773): the app on studio reads mini's
// thread's conversation with transcript.watch on "mini/<id>", relayed by
// studio's server over the link under mini's switches: panes off
// refuses it; view streams the fake agent's items and refuses a prompt;
// write types the app's prompt into mini's thread; and mini turning its
// conversations switch off ends the open stream with its reason, and
// refuses the next.
func TestAppBridgeRemoteConversation(t *testing.T) {
	mini, _, _ := threadEnv(t)
	for _, kv := range mini.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			os.WriteFile(filepath.Join(v, "thread-chat.toml"), []byte(chatScript), 0o644)
		}
	}
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
	if !slices.Contains(b.Hello.Server.Caps, proto.CapTranscriptRemote) {
		t.Fatalf("caps %v", b.Hello.Server.Caps)
	}
	listed := func(want func(appapi.Peer) bool) appapi.Peer {
		t.Helper()
		var last appapi.PeerList
		if !Poll(45*time.Second, func() bool {
			if err := b.Call(proto.MethodPeerList, nil, &last); err != nil {
				t.Fatal(err)
			}
			for _, p := range last.Peers {
				for _, pr := range p.Projects {
					if slices.ContainsFunc(pr.Sessions, func(s appapi.PeerSession) bool { return s.ID == remote }) && want(p) {
						return true
					}
				}
			}
			time.Sleep(time.Second) // the mirror reads mini again
			return false
		}) {
			t.Fatalf("peer.list never listed %s as wanted: %+v", remote, last)
		}
		return last.Peers[slices.IndexFunc(last.Peers, func(p appapi.Peer) bool { return p.Alias == "mini" })]
	}
	if p := listed(func(p appapi.Peer) bool { return p.Transcript }); p.Conversations != proto.ConversationsOn {
		t.Errorf("conversations %q, want on by default", p.Conversations)
	}

	// Panes off (the default): no conversation either.
	var tr appapi.TranscriptResult
	if _, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: remote}, &tr); appCode(err) != proto.ErrConversationsOff {
		t.Fatalf("transcript.watch with panes off: %v", err)
	}

	// View: the conversation streams, view-only; a prompt is refused.
	mini.MustCLI("peer", "panes", "studio", "view")
	st, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: remote}, &tr)
	if err != nil || tr.Session.ID != remote || tr.Session.Server != "mini" || !tr.Session.ViewOnly || tr.Status.State != proto.TranscriptMapped {
		t.Fatalf("transcript.watch at view: %+v %v", tr, err)
	}
	defer st.Cancel()
	var ev appapi.TranscriptEvent
	if name, err := st.Next(&ev); err != nil || name != proto.EventTranscriptReset {
		t.Fatalf("first event %q %v", name, err)
	}
	var pr appapi.PromptResult
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: remote, Text: "not at view"}, &pr); appCode(err) != proto.ErrPanesOff {
		t.Fatalf("session.prompt at view: %+v %v", pr, err)
	}

	mini.MustCLI("thread", "prompt", "t-0001", "run thread-chat", "--project", "demo")
	var items []appapi.TranscriptItem
	has := func(kind, tool, text string) bool {
		return slices.ContainsFunc(items, func(it appapi.TranscriptItem) bool { return it.Kind == kind && it.Tool == tool && it.Text == text })
	}
	deadline := time.Now().Add(agentWait)
	for !(has(proto.ItemPrompt, "", "run thread-chat") && has(proto.ItemTool, "Bash", "go test ./...") && has(proto.ItemText, "", "CHAT-DONE")) {
		if time.Now().After(deadline) {
			t.Fatalf("items so far %+v", items)
		}
		ev = appapi.TranscriptEvent{}
		name, err := st.Next(&ev)
		if err != nil || name == proto.EventTranscriptClosed {
			t.Fatalf("the stream ended (%q %v); items so far %+v", name, err, items)
		}
		items = append(items, ev.Items...)
	}

	// Write: the app's prompt is typed into mini's thread.
	mini.MustCLI("peer", "panes", "studio", "write")
	got := func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "hello from studio's app") }
	if !Poll(agentWait, func() bool {
		if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: remote, Text: "hello from studio's app"}, &pr); err != nil {
			// The open pane follows the switch a moment later.
			time.Sleep(500 * time.Millisecond)
			return false
		}
		return Poll(5*time.Second, func() bool { return slices.ContainsFunc(mini.FakeRecords("prompt"), got) })
	}) {
		t.Fatal("the app's prompt never reached mini's thread")
	}
	if pr.Via != "typed" {
		t.Errorf("via %q", pr.Via)
	}
	if slices.ContainsFunc(mini.FakeRecords("prompt"), func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "not at view") }) {
		t.Fatal("a prompt sent at view reached mini's agent")
	}

	// Conversations off on mini: the open stream ends with its reason,
	// and the next is refused.
	out := mini.MustCLI("peer", "conversations", "studio", "off")
	if !strings.Contains(out, "Conversations off for studio") {
		t.Errorf("tm peer conversations: %q", out)
	}
	if out := mini.MustCLI("peer", "list"); !strings.Contains(out, "panes: write, conversations off") {
		t.Errorf("tm peer list: %q", out)
	}
	closed := closedReason(t, st)
	if closed != proto.ConversationsOff {
		t.Fatalf("closed %q, want %s", closed, proto.ConversationsOff)
	}
	if _, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: remote}, &tr); appCode(err) != proto.ErrConversationsOff ||
		!strings.Contains(err.Error(), "mini doesn't share conversations with this server") {
		t.Fatalf("transcript.watch with conversations off: %v", err)
	}
	listed(func(p appapi.Peer) bool { return p.Conversations == proto.ConversationsNot })
	mini.AssertAlive(th)
}

// closedReason reads st to its transcript.closed and gives its reason.
func closedReason(t *testing.T, st *apptest.Stream) string {
	t.Helper()
	deadline := time.Now().Add(agentWait)
	for time.Now().Before(deadline) {
		var ev appapi.TranscriptEvent
		name, err := st.Next(&ev)
		if err != nil {
			t.Fatalf("the stream ended before transcript.closed: %v", err)
		}
		if name == proto.EventTranscriptClosed {
			return ev.Reason
		}
	}
	t.Fatal("no transcript.closed")
	return ""
}
