package e2e

// A paired phone over its device link (docs/SPEC.md §16.15): the app
// API the server runs on its side of the link, reached as the phone
// reaches it, through a relay with both keys pinned.

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// phoneLink pairs a phone called name with env's server through an
// in-process relay, as tm device pair leaves it (with a write key), sets
// its level with tm device access and dials its link: the phone's app
// API client, signing its writes with the write key it returns.
func phoneLink(t *testing.T, env *Env, name, level string) (*apptest.Client, *ecdsa.PrivateKey) {
	t.Helper()
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")
	srv, _, err := peer.EnsureIdentity(peer.DirIn(env.Home))
	if err != nil {
		t.Fatal(err)
	}
	_, k, _ := ed25519.GenerateKey(nil)
	wk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wkb, _ := wk.PublicKey.Bytes()
	if _, err := device.Add(device.DirIn(env.Home), k.Public().(ed25519.PublicKey), wkb, name, url); err != nil {
		t.Fatal(err)
	}
	env.MustCLI("device", "access", name, level)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := device.Dial(ctx, peer.IdentityOf(k), srv.Key.Public().(ed25519.PublicKey), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c, err := apptest.New(conn, conn)
	if err != nil {
		t.Fatal(err)
	}
	return c, wk
}

// TestSmokeDeviceAnswerEscape (T791): a phone at answer approves a thread's
// permission prompt over its device link, signed by its write key, and
// the journal says it was the phone. A prompt to run a command outside
// the sandbox is refused to it, device-escape-off, checked or
// confirmed, signed or not, and stays for the user on the computer,
// whose app still allows it. An unsigned write never reaches the
// server.
func TestSmokeDeviceAnswerEscape(t *testing.T) {
	env, projDir, _ := tickerEnv(t)
	writeConfig(t, env, "[projects.demo]\nguard = false\n")
	th := startThread(t, env, projDir)
	phone, wk := phoneLink(t, env, "Test phone", device.LevelAnswer)
	if h := phone.Hello; h.Level != device.LevelAnswer || !h.Sign || !slices.Contains(h.Methods, proto.MethodSessionApprove) ||
		slices.Contains(h.Methods, proto.MethodSessionPrompt) {
		t.Fatalf("hello %+v", h)
	}

	// An ordinary prompt: unsigned it is refused, signed it goes through.
	env.MustCLI("thread", "prompt", "t-0001", "run thread-block", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")
	var check appapi.ApproveResult
	if err := phone.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Check: true}, &check); err != nil || check.Sent || check.Option == "" {
		t.Fatalf("unsigned check: %+v %v", check, err)
	}
	if err := phone.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID}, nil); appCode(err) != appapi.ErrSignature {
		t.Fatalf("unsigned approve: %v", err)
	}
	phone.Sign = apptest.Signer(wk)
	var res appapi.ApproveResult
	if err := phone.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID}, &res); err != nil || !res.Sent || res.Escape {
		t.Fatalf("signed approve: %+v %v", res, err)
	}
	env.WaitState(th, "idle", agentWait)

	// An escape: never the phone's.
	env.MustCLI("thread", "prompt", "t-0001", "run thread-escape", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	waitReason(t, env, th, "permission")
	for _, p := range []appapi.ApproveParams{{ID: th.ID, Check: true}, {ID: th.ID, Escape: true}, {ID: th.ID}} {
		if err := phone.Call(proto.MethodSessionApprove, p, nil); appCode(err) != proto.ErrDeviceEscapeOff {
			t.Fatalf("approve %+v of an escape from the phone: %v", p, err)
		}
	}
	if i, _ := env.Info(th); i.State != "blocked" {
		t.Fatalf("the escape's prompt was answered: %+v", i)
	}
	b := runBridge(t, env)
	if err := b.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: th.ID, Escape: true}, nil); err != nil {
		t.Fatalf("approve on the computer, confirmed: %v", err)
	}
	env.WaitFor(th, "ESCAPED", agentWait)

	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	if !strings.Contains(string(journal), "human (iphone: Test phone) thread.approve t-0001 ") {
		t.Errorf("journal lacks the phone's approve:\n%s", journal)
	}
}
