package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// shareRequests are env's share requests waiting, from tm peer requests.
func shareRequests(env *Env) []struct {
	Kind, ID, Peer, State string
	Out                   bool
} {
	var got struct {
		Requests []struct {
			Kind, ID, Peer, State string
			Out                   bool
		} `json:"requests"`
	}
	json.Unmarshal([]byte(env.MustCLI("peer", "requests", "--json")), &got)
	return got.Requests
}

// call runs method on env's server as the human, on the human socket,
// which a client in an agent's session doesn't try (a run of this suite
// from a thread).
func call(t *testing.T, env *Env, method string, params, result any) error {
	t.Helper()
	t.Setenv(caller.EnvSession, "")
	c, err := client.Dial(client.Paths{Socket: env.Socket}, proto.KindControl)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	return c.Call(method, params, result)
}

// mirrored is env's peer.list entry for alias; ok false without one.
func mirrored(t *testing.T, env *Env, alias string) (pm proto.PeerMirror, ok bool) {
	t.Helper()
	var l proto.PeerList
	if call(t, env, proto.MethodPeerList, nil, &l) != nil {
		return pm, false
	}
	for _, p := range l.Peers {
		if p.Alias == alias {
			return p, true
		}
	}
	return pm, false
}

// shareState is request id's state in env's peer.list entry for alias.
func shareState(t *testing.T, env *Env, alias, id string) string {
	t.Helper()
	pm, _ := mirrored(t, env, alias)
	for _, r := range pm.Shares {
		if r.ID == id {
			return r.State
		}
	}
	return ""
}

// TestSmokePeerShareRequest: studio, paired with mini, shares nothing
// and is shared nothing. Its user asks mini's to share a project; mini's
// declines it with tm peer decline, and studio's peer.list says so; no
// inbox hears of either. Asked again,
// mini's user accepts with api through the console's method: api is
// shared, and shows in studio's mirror of mini.
func TestSmokePeerShareRequest(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "new", "api")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	waitPeerState(t, mini, "studio", "up", "")

	idRE := regexp.MustCompile(`\((S-[0-9A-F]{4}-\d+), for acme\)`)
	ask := func() string {
		t.Helper()
		out := studio.MustCLI("peer", "ask-share", "mini", "--for", "acme", "--note", "the API docs")
		m := idRE.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("ask-share: %s", out)
		}
		return m[1]
	}
	waiting := func(env *Env, id string, out bool) bool {
		return slices.ContainsFunc(shareRequests(env), func(r struct {
			Kind, ID, Peer, State string
			Out                   bool
		}) bool {
			return r.ID == id && r.Out == out && r.State == proto.ShareWaiting
		})
	}

	// Ask, decline.
	id := ask()
	if r := studio.CLI("peer", "ask-share", "mini"); r.Code != 1 || !strings.Contains(r.Stderr, "request-pending") {
		t.Fatalf("asked twice: %d %s", r.Code, r.Stderr)
	}
	if !Poll(30*time.Second, func() bool { return waiting(mini, id, false) && waiting(studio, id, true) }) {
		t.Fatalf("the request never arrived: mini %+v, studio %+v", shareRequests(mini), shareRequests(studio))
	}
	if out := mini.MustCLI("peer", "requests"); !strings.Contains(out, `studio asks you to share a project with it, for its acme: "the API docs"; answer in Network (N)`) {
		t.Fatalf("mini's requests:\n%s", out)
	}
	if pm, _ := mirrored(t, mini, "studio"); proto.ShareWaits([]proto.PeerMirror{pm}) != 1 {
		t.Fatalf("mini's peer.list: %+v", pm.Shares)
	}
	mini.MustCLI("peer", "decline", id)
	if !Poll(30*time.Second, func() bool { return shareState(t, studio, "mini", id) == proto.ShareIsDeclined }) {
		t.Fatalf("studio never heard: %s", shareState(t, studio, "mini", id))
	}

	// Ask, accept through the method: api shows in studio's mirror.
	id = ask()
	if !Poll(30*time.Second, func() bool { return waiting(mini, id, false) }) {
		t.Fatalf("the second request never arrived: %+v", shareRequests(mini))
	}
	if err := call(t, mini, proto.MethodPeerShareAnswer, proto.PeerShareAnswerParams{ID: id, Accept: true, Project: "nosuch"}, nil); err == nil {
		t.Fatal("accepted with a project that isn't there")
	}
	var r proto.ShareRequest
	if err := call(t, mini, proto.MethodPeerShareAnswer, proto.PeerShareAnswerParams{ID: id, Accept: true, Project: "api"}, &r); err != nil || r.State != proto.ShareIsAccepted {
		t.Fatalf("accept: %+v %v", r, err)
	}
	if !Poll(30*time.Second, func() bool {
		pm, _ := mirrored(t, studio, "mini")
		return slices.ContainsFunc(pm.Projects, func(mp proto.PeerMirrorProject) bool { return mp.Project == "api" })
	}) {
		t.Fatal("api never showed in studio's mirror of mini")
	}
	if !Poll(30*time.Second, func() bool { return shareState(t, studio, "mini", id) == proto.ShareIsAccepted }) {
		t.Fatalf("studio never heard: %s", shareState(t, studio, "mini", id))
	}
	for _, env := range []*Env{studio, mini} {
		if out := env.MustCLI("inbox", "list", "--project", map[*Env]string{studio: "acme", mini: "api"}[env]); strings.Contains(out, "share") {
			t.Fatalf("an inbox heard of a share request:\n%s", out)
		}
	}
}
