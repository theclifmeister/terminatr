package program

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// pairedFixture is routeFixture with mini paired and its acme-ios an
// accepted member of acme that granted features.
func pairedFixture(t *testing.T, conf string, features ...string) (*Program, string) {
	t.Helper()
	g, _ := routeFixture(t, conf)
	root := os.Getenv(home.Env)
	if _, _, err := peer.EnsureIdentity(peer.DirIn(root)); err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := peer.SavePeers(peer.DirIn(root), []peer.Peer{{Alias: "mini", Key: pub, Relay: "ws://127.0.0.1:1"}}); err != nil {
		t.Fatal(err)
	}
	st := map[string]any{"invites": []map[string]any{{"id": "P-ABCD-1", "out": true, "project": "acme", "peer": "mini/acme-ios",
		"features": features, "granted": features, "state": link.ReqAccepted, "created": "2026-11-01T00:00:00Z"}}}
	b, _ := json.Marshal(st)
	os.MkdirAll(filepath.Join(root, "state"), 0o700)
	if err := os.WriteFile(filepath.Join(root, "state", "links.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return g, root
}

// TestRoutePaired: a route files local tasks here and queues the paired
// member's, pending until its server answers; its after refs are read
// as that server will, and refs it can't take are refused first.
func TestRoutePaired(t *testing.T) {
	g, _ := pairedFixture(t, routeConfig, "tasks", "messages", "status")
	cfg, _ := config.Load()
	req := RouteRequest{Umbrella: "SSO", Tasks: []RouteTask{
		{Key: "api", Project: "acme-api", Title: "OIDC"},
		{Key: "ios", Project: "mini/acme-ios", Title: "Login screen", After: []string{"api", "T3", "mini/acme-ios/T4"}},
	}}
	res, err := g.Route(director, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tasks) != 2 || res.Tasks[1].Result != link.SentPending || res.Tasks[1].Ref != "" || !strings.HasPrefix(res.Tasks[1].Message, "M-") {
		t.Fatalf("routed %+v", res.Tasks)
	}
	sent := link.Sent("acme")
	if len(sent) != 1 || sent[0].Member != "mini/acme-ios" || sent[0].Umbrella != "acme/T1" || sent[0].Key != "ios" {
		t.Fatalf("sent %+v", sent)
	}
	u, _ := g.Tasks().Get(1)
	if !strings.Contains(u.Notes, "mini/acme-ios (pending "+sent[0].ID+")") || strings.Join(u.Parts, ",") != "acme-api/T1" {
		t.Fatalf("umbrella notes %q", u.Notes)
	}
	if b, _ := os.ReadDir(filepath.Join(os.Getenv(home.Env), "state", "link", "outbox", "mini", "program")); len(b) != 1 {
		t.Fatalf("outbox %v", b)
	}
	for name, tc := range map[string]struct {
		req  RouteRequest
		code string
	}{
		"pending ref":  {RouteRequest{Tasks: []RouteTask{{Key: "ios", Project: "mini/acme-ios", Title: "a"}, {Project: "acme-api", Title: "b", After: []string{"ios"}}}}, "pending-ref"},
		"other server": {RouteRequest{Tasks: []RouteTask{{Project: "mini/acme-ios", Title: "a", After: []string{"lab/x/T1"}}}}, "remote-ref"},
		"bad notes":    {RouteRequest{Tasks: []RouteTask{{Project: "mini/acme-ios", Title: "a", Notes: "## Open"}}}, "invalid-notes"},
		"long title":   {RouteRequest{Tasks: []RouteTask{{Project: "mini/acme-ios", Title: strings.Repeat("x", 201)}}}, "too-long"},
	} {
		if _, err := g.Route(director, cfg, tc.req); code(err) != tc.code {
			t.Errorf("%s: got %v, want %s", name, err, tc.code)
		}
	}
	// A local task may come after a paired member's.
	if _, err := g.Route(director, cfg, RouteRequest{Tasks: []RouteTask{{Project: "acme-web", Title: "c", After: []string{"mini/acme-ios/T4"}}}}); err != nil {
		t.Fatalf("after a paired task: %v", err)
	}
}

// TestEditSendStartPaired: a change, a message and a start to a paired
// member go over the link, each within what it granted.
func TestEditSendStartPaired(t *testing.T) {
	g, _ := pairedFixture(t, strings.Replace(routeConfig, "[programs.acme]\n", "[programs.acme]\nauthority = \"delegate\"\n", 1), "tasks", "messages")
	cfg, _ := config.Load()
	prio := "must"
	sent, err := g.EditRemote(director, cfg, "mini/acme-ios/T4", tasks.ProgramChange{Priority: &prio, After: []string{"T2,mini/acme-ios/T3"}})
	if err != nil || sent.Kind != "edit" || sent.Ref != "T4" {
		t.Fatalf("edit %+v %v", sent, err)
	}
	if _, err := g.EditRemote(director, cfg, "mini/acme-ios/T4", tasks.ProgramChange{}); code(err) != "invalid-edit" {
		t.Errorf("empty edit: %v", err)
	}
	if _, err := g.EditRemote(caller.Caller{Kind: caller.Coordinator, Project: "acme-api"}, cfg, "mini/acme-ios/T4", tasks.ProgramChange{Priority: &prio}); code(err) != "director-only" {
		t.Errorf("coordinator: %v", err)
	}
	m, err := g.Send(director, cfg, "", "mini/acme-ios", link.Opts{Text: "hello"})
	if err != nil || m.State != link.StateQueued {
		t.Fatalf("send %+v %v", m, err)
	}
	if _, err := g.StartRemote(director, cfg, StartRequest{Ref: "mini/acme-ios/T4"}, "", ""); code(err) != "not-granted" {
		t.Errorf("starts not granted: %v", err)
	}
	if _, err := g.StartRemote(director, cfg, StartRequest{Ref: "mini/acme-ios/T4", OverCap: true}, "", ""); code(err) != "human-only" {
		t.Errorf("over cap: %v", err)
	}
}

// TestStartPairedAuthority: under propose a director's start of a paired
// member's task needs the user's word; under delegate it goes as such.
func TestStartPairedAuthority(t *testing.T) {
	g, _ := pairedFixture(t, routeConfig, "tasks", "messages", "starts")
	cfg, _ := config.Load()
	if _, err := g.StartRemote(director, cfg, StartRequest{Ref: "mini/acme-ios/T4"}, "", ""); code(err) != "needs-approval" {
		t.Fatalf("propose: %v", err)
	}
	if s, err := g.StartRemote(director, cfg, StartRequest{Ref: "mini/acme-ios/T4", Approved: true}, "claude", ""); err != nil || s.Kind != "start" {
		t.Fatalf("approved: %+v %v", s, err)
	}
}
