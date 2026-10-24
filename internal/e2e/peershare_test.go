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
// inbox hears of either. Asked again, from studio's Network popup (S),
// mini's user accepts in its console's (A, api, Y): api is shared, and
// shows in studio's mirror of mini and its tree.
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

	// Ask in studio's Network popup: S on mini, for acme, with a note.
	studio.FakeClaude()
	mini.FakeClaude()
	ws := studio.Window(120, 30)
	ws.WaitFor("● server ok", wait)
	ws.Prefix("N")
	ws.WaitFor("◉ mini · connected", wait)
	ws.Keys(keyDown, keyDown)
	ws.WaitFor("S ask to share", wait)
	ws.Type("S")
	ws.WaitFor("which of yours is it for?", wait)
	ws.Keys(keyEnter)
	ws.WaitFor("what it's for (optional)", wait)
	ws.Keys("the API docs", keyEnter)
	m := regexp.MustCompile(`asked mini to share a project \((S-[0-9A-F]{4}-\d+), for acme\)`).FindStringSubmatch(ws.WaitFor("; its user answers there", wait))
	if m == nil {
		t.Fatalf("no request asked:\n%s", ws.Screen())
	}
	id = m[1]
	ws.WaitFor("waiting on mini's user", wait)
	if !Poll(30*time.Second, func() bool { return waiting(mini, id, false) }) {
		t.Fatalf("the second request never arrived: %+v", shareRequests(mini))
	}
	if err := call(t, mini, proto.MethodPeerShareAnswer, proto.PeerShareAnswerParams{ID: id, Accept: true, Project: "nosuch"}, nil); err == nil {
		t.Fatal("accepted with a project that isn't there")
	}

	// Accept in mini's console: the request is a need, A picks api, the
	// share confirm's Y shares it; api shows in studio's mirror and tree.
	wm := mini.Window(120, 30)
	wm.WaitFor("● server ok", wait)
	wm.Prefix("N")
	wm.WaitFor("⚑ asks you to share a project (for its acme)", wait)
	wm.Type("!")
	wm.WaitFor("A choose a project · X decline", wait)
	wm.Type("A")
	wm.WaitFor("Share a project with studio?", wait)
	wm.Keys(keyEnter)
	wm.WaitFor("Share api with every paired server?", wait)
	wm.Type("Y")
	wm.WaitFor("api is shared with every paired server, for studio's request "+id, wait)
	if !Poll(30*time.Second, func() bool {
		pm, _ := mirrored(t, studio, "mini")
		return slices.ContainsFunc(pm.Projects, func(mp proto.PeerMirrorProject) bool { return mp.Project == "api" })
	}) {
		t.Fatal("api never showed in studio's mirror of mini")
	}
	theirs := regexp.MustCompile(`api +shared with you`)
	ws.WaitUntil("api under mini in the tree", wait, func(sc string) bool { return theirs.MatchString(sc) })
	if !Poll(30*time.Second, func() bool { return shareState(t, studio, "mini", id) == proto.ShareIsAccepted }) {
		t.Fatalf("studio never heard: %s", shareState(t, studio, "mini", id))
	}
	for _, env := range []*Env{studio, mini} {
		if out := env.MustCLI("inbox", "list", "--project", map[*Env]string{studio: "acme", mini: "api"}[env]); strings.Contains(out, "share") {
			t.Fatalf("an inbox heard of a share request:\n%s", out)
		}
	}
}
