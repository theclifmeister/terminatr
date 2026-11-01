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
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// pickScript asks a lone multi-select, then says it is done.
const pickScript = `
[[step]]
do = "question"
header = "Fruits"
question = "Which fruits go on it?"
options = ["Apple", "Pear", "Plum"]
multi = true

[[step]]
do = "stream"
ms = 50
text = "PICKED"
`

// TestPeerNeedPicks (T904): mini's hello says need-picks, so studio's
// peer.list has picks on it, as the apps read it to tick a paired
// server's multi-select; picks with other labels than theirs are
// refused (need-changed), the ones ticked with their labels answer it
// there.
func TestPeerNeedPicks(t *testing.T) {
	mini, projDir, _ := tickerEnv(t)
	scripts := ""
	for _, kv := range mini.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			scripts = v
		}
	}
	os.WriteFile(filepath.Join(scripts, "thread-ask.toml"), []byte(pickScript), 0o644)
	writeConfig(t, mini, "[projects.demo]\nguard = false\nshare = true\n")

	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	th := startThread(t, mini, projDir)
	mini.MustCLI("peer", "needs", "studio", "answer")
	studio.MustCLI("server", "start")
	b := runBridge(t, studio)

	mini.MustCLI("thread", "prompt", "t-0001", "run thread-ask", "--project", "demo")
	mini.WaitState(th, "blocked", agentWait)
	waitReason(t, mini, th, "question")
	var q appapi.PeerNeed
	var picks bool
	if !Poll(60*time.Second, func() bool {
		var l appapi.PeerList
		if b.Call(proto.MethodPeerList, nil, &l) != nil {
			return false
		}
		for _, p := range l.Peers {
			for _, pp := range p.Projects {
				for _, n := range pp.Needs {
					if p.Alias == "mini" && n.Kind == proto.NeedQuestion && n.Question != nil && slices.Contains(n.Acts, proto.ActAnswer) {
						q, picks = n, p.Picks
						return true
					}
				}
			}
		}
		return false
	}) {
		t.Fatal("studio never saw the multi-select")
	}
	if !picks || !q.Question.Questions[0].MultiSelect {
		t.Fatalf("picks %v, question %+v", picks, q.Question)
	}
	act := appapi.PeerNeedActParams{Peer: "mini", Project: "demo", Need: q.ID, Act: proto.ActAnswer,
		Question: q.Question.Questions[0].Question, Picks: []int{0, 2}, Answer: "Apple, Pear"}
	var res appapi.PeerNeedActResult
	if err := b.Call(proto.MethodPeerNeedAct, act, &res); appCode(err) != proto.ErrNeedChanged {
		t.Fatalf("picks under other labels: %v", err)
	}
	act.Answer = "Apple, Plum"
	if err := b.Call(proto.MethodPeerNeedAct, act, &res); err != nil {
		t.Fatalf("picks: %v", err)
	}
	mini.WaitFor(th, "→ Apple, Plum", agentWait)
	mini.WaitFor(th, "PICKED", agentWait)
}
