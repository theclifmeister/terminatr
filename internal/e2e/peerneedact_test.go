package e2e

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPeerNeedAct (T839): mini shares demo, whose thread asks a question,
// then a permission, then an escape's permission, and which has a task in
// review. studio's user acts on them through studio's app bridge
// (peer.need-act), a probe paired with mini calls the link itself:
//   - at view, mini takes no act (needs-off);
//   - at answer, a stale guard is refused (need-changed); the question is
//     answered and the dialog approved, journaled on mini as
//     "human (peer: studio)";
//   - the probe's accept goes through once, its replayed nonce not
//     (bad-nonce);
//   - an escape's prompt is refused (escape-here) and stays for mini's
//     user.
func TestPeerNeedAct(t *testing.T) {
	mini, projDir, _ := tickerEnv(t)
	scripts := ""
	for _, kv := range mini.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			scripts = v
		}
	}
	os.WriteFile(filepath.Join(scripts, "thread-ask.toml"), []byte(askScript), 0o644)
	writeConfig(t, mini, "[projects.demo]\nguard = false\nshare = true\n")

	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")
	pairDirect(t, studio, mini, "studio", "mini", url)
	// The probe: a paired server of mini's in this process, to call the
	// link's methods itself.
	probeDir := t.TempDir()
	probeID, _, err := peer.EnsureIdentity(probeDir)
	if err != nil {
		t.Fatal(err)
	}
	miniID, _, _ := peer.EnsureIdentity(peer.DirIn(mini.Home))
	pub := func(i *peer.Identity) ed25519.PublicKey { return i.Key.Public().(ed25519.PublicKey) }
	ps, _ := peer.LoadPeers(peer.DirIn(mini.Home))
	if err := peer.SavePeers(peer.DirIn(mini.Home), append(ps, peer.Peer{Alias: "probe", Key: pub(probeID), Relay: url})); err != nil {
		t.Fatal(err)
	}
	if err := peer.SavePeers(probeDir, []peer.Peer{{Alias: "mini", Key: pub(miniID), Relay: url}}); err != nil {
		t.Fatal(err)
	}

	th := startThread(t, mini, projDir)
	mini.MustCLI("task", "add", "Rate limits", "--status", "review", "--project", "demo")
	mini.MustCLI("peer", "needs", "studio", "view")
	mini.MustCLI("peer", "needs", "probe", "answer")
	studio.MustCLI("server", "start")
	b := runBridge(t, studio)
	probe := peer.Start(peer.Options{Dir: probeDir, Version: "test", Logf: t.Logf, Backoff: func(int) time.Duration { return 50 * time.Millisecond }})
	t.Cleanup(probe.Close)

	// need waits for the first need of kind on studio's mirror of demo
	// that ok takes, with mini's switch for studio at level.
	need := func(what, level, kind string, ok func(appapi.PeerNeed) bool) appapi.PeerNeed {
		t.Helper()
		var found appapi.PeerNeed
		if !Poll(60*time.Second, func() bool {
			var l appapi.PeerList
			if b.Call(proto.MethodPeerList, nil, &l) != nil {
				return false
			}
			for _, p := range l.Peers {
				if p.Alias != "mini" || p.Needs != level {
					continue
				}
				for _, pp := range p.Projects {
					for _, n := range pp.Needs {
						if pp.Project == "demo" && n.Kind == kind && (ok == nil || ok(n)) {
							found = n
							return true
						}
					}
				}
			}
			return false
		}) {
			t.Fatalf("studio never saw %s", what)
		}
		return found
	}
	act := func(p appapi.PeerNeedActParams) (appapi.PeerNeedActResult, error) {
		p.Peer, p.Project = "mini", "demo"
		var res appapi.PeerNeedActResult
		err := b.Call(proto.MethodPeerNeedAct, p, &res)
		return res, err
	}

	// The question, seen at view: no act.
	mini.MustCLI("thread", "prompt", "t-0001", "run thread-ask", "--project", "demo")
	mini.WaitState(th, "blocked", agentWait)
	waitReason(t, mini, th, "question")
	q := need("the question", proto.NeedsView, proto.NeedQuestion, func(n appapi.PeerNeed) bool { return n.Question != nil })
	answer := appapi.PeerNeedActParams{Need: q.ID, Act: proto.ActAnswer, Question: q.Question.Questions[0].Question, Answer: "Blue"}
	if _, err := act(answer); appCode(err) != proto.ErrNeedsOff {
		t.Fatalf("an answer at view: %v", err)
	}

	mini.MustCLI("peer", "needs", "studio", "answer")
	q = need("the question at answer", proto.NeedsAnswer, proto.NeedQuestion, func(n appapi.PeerNeed) bool { return slices.Contains(n.Acts, proto.ActAnswer) })
	stale := answer
	stale.Question = "Which color should the header be?"
	if _, err := act(stale); appCode(err) != proto.ErrNeedChanged {
		t.Fatalf("an answer to a question no longer asked: %v", err)
	}
	if _, err := act(answer); err != nil {
		t.Fatalf("answer: %v", err)
	}
	mini.WaitFor(th, "→ Blue", agentWait)
	// The menu's next question is a new need: the old id is stale.
	if _, err := act(answer); appCode(err) != proto.ErrNeedChanged {
		t.Fatalf("the same answer again: %v", err)
	}
	mini.WaitFor(th, "What should the button say?", agentWait)
	second := func(it appapi.QuestionItem) bool { return it.Question == "What should the button say?" && !it.Answered }
	q = need("the second question", proto.NeedsAnswer, proto.NeedQuestion, func(n appapi.PeerNeed) bool {
		return n.Question != nil && slices.ContainsFunc(n.Question.Questions, second)
	})
	i := slices.IndexFunc(q.Question.Questions, second)
	if _, err := act(appapi.PeerNeedActParams{Need: q.ID, Act: proto.ActAnswer, Index: i, Question: q.Question.Questions[i].Question, Answer: "Go"}); err != nil {
		t.Fatalf("answer 2: %v", err)
	}
	mini.WaitFor(th, "ASKED", agentWait)
	mini.WaitState(th, "idle", agentWait)

	// A dialog: another option's words are stale; its own go through.
	mini.MustCLI("thread", "prompt", "t-0001", "run thread-block", "--project", "demo")
	mini.WaitState(th, "blocked", agentWait)
	waitReason(t, mini, th, "permission")
	a := need("the dialog", proto.NeedsAnswer, proto.NeedApproval, func(n appapi.PeerNeed) bool { return n.Approval != nil })
	if _, err := act(appapi.PeerNeedActParams{Need: a.ID, Act: proto.ActApprove, Choice: 1, Option: a.Approval.Options[len(a.Approval.Options)-1]}); appCode(err) != proto.ErrNeedChanged {
		t.Fatalf("approve with another option's words: %v", err)
	}
	res, err := act(appapi.PeerNeedActParams{Need: a.ID, Act: proto.ActApprove, Choice: 1, Option: a.Approval.Options[0]})
	if err != nil || res.Option != a.Approval.Options[0] {
		t.Fatalf("approve: %+v %v", res, err)
	}
	mini.WaitState(th, "idle", agentWait)

	// The probe's accept, once: its nonce replayed is refused.
	r := need("the task in review", proto.NeedsAnswer, proto.NeedReview, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var nonce proto.PeerNonce
	if !Poll(30*time.Second, func() bool { return probe.Call(ctx, "mini", proto.MethodPeerNonce, struct{}{}, &nonce) == nil }) {
		t.Fatal("the probe got no nonce")
	}
	accept := proto.PeerActParams{PeerAct: proto.PeerAct{Project: "demo", Need: r.ID, Act: proto.ActAccept, Status: r.Status}, Nonce: nonce.Nonce, Device: "Probe phone"}
	var ar proto.PeerActResult
	if err := probe.Call(ctx, "mini", proto.MethodPeerAct, accept, &ar); err != nil || !ar.Asked {
		t.Fatalf("the probe's accept: %+v %v", ar, err)
	}
	if err := probe.Call(ctx, "mini", proto.MethodPeerAct, accept, &ar); peerCode(err) != proto.ErrBadNonce {
		t.Fatalf("a replayed nonce: %v", err)
	}

	// An escape's prompt: refused, and it stays.
	mini.MustCLI("thread", "prompt", "t-0001", "run thread-escape", "--project", "demo")
	mini.WaitState(th, "blocked", agentWait)
	waitReason(t, mini, th, "permission")
	e := need("the escape", proto.NeedsAnswer, proto.NeedApproval, func(n appapi.PeerNeed) bool { return n.Escape })
	if e.Approval != nil || len(e.Acts) != 0 {
		t.Fatalf("the escape crossed with its dialog: %+v", e)
	}
	if _, err := act(appapi.PeerNeedActParams{Need: e.ID, Act: proto.ActApprove, Choice: 1, Option: "Yes"}); appCode(err) != proto.ErrEscapeHere {
		t.Fatalf("approve of an escape: %v", err)
	}
	time.Sleep(time.Second)
	if i, _ := mini.Info(th); i.State != "blocked" || strings.Contains(mini.Screen(th), "ESCAPED") {
		t.Fatalf("the escape's prompt was answered: %+v\n%s", i, mini.Screen(th))
	}

	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	for _, want := range []string{"human (peer: studio) session.answer", "→ Blue", "human (peer: studio) thread.approve t-0001",
		"human (peer: probe › iphone: Probe phone)"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("mini's journal lacks %q:\n%s", want, journal)
		}
	}
}

// peerCode is a link call's refusal code.
func peerCode(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}
