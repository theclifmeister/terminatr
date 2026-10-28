package e2e

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// escapeHeld types keys into th until its ordinary permission prompt
// is answered (the keys reach the dialog), then raises an escape's
// prompt and types the same keys again: they must not answer it, which
// stays for the user on the computer.
func escapeHeld(t *testing.T, env *Env, th *Session, thread string, keys func(string)) {
	t.Helper()
	env.MustCLI("thread", "prompt", thread, "run thread-block", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")
	if !Poll(agentWait, func() bool {
		keys("1")
		return Poll(2*time.Second, func() bool { i, _ := env.Info(th); return i.State != "blocked" })
	}) {
		t.Fatalf("the keys never answered an ordinary prompt:\n%s", env.Screen(th))
	}
	env.WaitState(th, "idle", agentWait)

	env.MustCLI("thread", "prompt", thread, "run thread-escape", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")
	for range 3 {
		keys("1")
		time.Sleep(500 * time.Millisecond)
	}
	if i, _ := env.Info(th); i.State != "blocked" || !i.Escape || strings.Contains(env.Screen(th), "ESCAPED") {
		t.Fatalf("keys answered the escape's prompt: %+v\n%s", i, env.Screen(th))
	}
}

// TestDeviceKeysEscape (T836): a phone at write types into a thread's
// pane (pane.input) and sends it keys (session.keys); either answers an
// ordinary permission prompt, neither an escape's, which the computer's
// app still answers.
func TestDeviceKeysEscape(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	writeConfig(t, env, "[projects.demo]\nguard = false\n")
	th := startThread(t, env, projDir)
	phone, wk := phoneLink(t, env, "Test phone", device.LevelWrite)
	phone.Sign = apptest.Signer(wk)

	var pr appapi.PaneResult
	pane, err := phone.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: th.ID}, &pr)
	if err != nil {
		t.Fatalf("pane.vt: %v", err)
	}
	defer pane.Cancel()
	go func() {
		var ev appapi.PaneEvent
		for {
			if _, err := pane.Next(&ev); err != nil {
				return
			}
		}
	}()
	escapeHeld(t, env, th, "t-0001", func(k string) {
		if err := phone.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte(k)}, nil); err != nil {
			t.Fatal(err)
		}
	})
	for range 3 {
		err := phone.Call(proto.MethodSessionKeys, appapi.KeysParams{ID: th.ID, Data: "1"}, nil)
		if appCode(err) != proto.ErrDeviceEscapeOff {
			t.Fatalf("session.keys from the phone to an escape's prompt: %v", err)
		}
	}
	if i, _ := env.Info(th); i.State != "blocked" {
		t.Fatalf("session.keys answered the escape's prompt: %+v", i)
	}

	b := runBridge(t, env)
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Escape: true}, nil); err != nil {
		t.Fatalf("approve on the computer, confirmed: %v", err)
	}
	env.WaitFor(th, "ESCAPED", agentWait)
}

// TestRemotePaneKeysEscape (T836): studio's app types into mini's
// thread through a pane mini's switch lets it write: the keys answer an
// ordinary permission prompt, never an escape's.
func TestRemotePaneKeysEscape(t *testing.T) {
	mini, projDir, _ := tickerEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\nguard = false\n")

	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "panes", "studio", "write")
	th := startThread(t, mini, projDir)
	remote := "mini/" + th.ID

	b := runBridge(t, studio)
	var last appapi.PeerList
	if !Poll(30*time.Second, func() bool {
		if err := b.Call(proto.MethodPeerList, nil, &last); err != nil {
			t.Fatal(err)
		}
		for _, p := range last.Peers {
			for _, pr := range p.Projects {
				if slices.ContainsFunc(pr.Sessions, func(s appapi.PeerSession) bool { return s.ID == remote }) {
					return p.Pane
				}
			}
		}
		time.Sleep(time.Second)
		return false
	}) {
		t.Fatalf("peer.list never listed %s: %+v", remote, last)
	}
	var pr appapi.PaneResult
	pane, err := b.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: remote}, &pr)
	if err != nil || pr.Session.ViewOnly {
		t.Fatalf("pane.vt at write: %+v %v", pr.Session, err)
	}
	defer pane.Cancel()
	go func() {
		var ev appapi.PaneEvent
		for {
			if _, err := pane.Next(&ev); err != nil {
				return
			}
		}
	}()
	escapeHeld(t, mini, th, "t-0001", func(k string) {
		if err := b.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte(k)}, nil); err != nil {
			t.Fatal(err)
		}
	})
}
