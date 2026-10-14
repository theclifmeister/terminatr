package server

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// linkedServers pairs two peer directories through an in-process relay
// and starts both: mini exports ex, studio reads it. It returns
// studio's Manager, whose link to "mini" is up.
func linkedServers(t *testing.T, ex peer.Exporter) *peer.Manager {
	t.Helper()
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewUnstartedServer(rs)
	hs.StartTLS()
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	tc := hs.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	url := "wss" + strings.TrimPrefix(hs.URL, "https")

	dirs := [2]string{filepath.Join(t.TempDir(), "studio"), filepath.Join(t.TempDir(), "mini")}
	var ids [2]*peer.Identity
	for i, d := range dirs {
		id, _, err := peer.EnsureIdentity(d)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	for i, alias := range []string{"mini", "studio"} {
		other := ids[1-i]
		if err := peer.SavePeers(dirs[i], []peer.Peer{{Alias: alias, Key: other.Key.Public().(ed25519.PublicKey), Relay: url}}); err != nil {
			t.Fatal(err)
		}
	}
	start := func(dir string, ex peer.Exporter, tc *tls.Config) *peer.Manager {
		m := peer.Start(peer.Options{Dir: dir, Version: "test", Logf: t.Logf, RelayTLS: tc, Export: ex,
			Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
		t.Cleanup(m.Close)
		return m
	}
	studio := start(dirs[0], nil, tc)
	start(dirs[1], ex, tc)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := studio.Status(); st != nil && len(st.Peers) == 1 && st.Peers[0].State == proto.LinkUp {
			if !slices.Contains(st.Peers[0].Caps, peer.CapWatch) {
				t.Fatalf("mini exports but its hello says %v", st.Peers[0].Caps)
			}
			return studio
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the link never came up: %+v", studio.Status())
	return nil
}

// TestPeerExportNeverLeaks: through a real link, a paired server sees
// the shared, active projects only, each project's sessions without
// argv, working directory, pid, agent session id, title or question
// text, and links only to other shared projects. An unshared project
// answers exactly as one that doesn't exist, and turning share off hides
// a project at once.
func TestPeerExportNeverLeaks(t *testing.T) {
	testPaths(t)
	root, _ := home.Dir()
	for _, o := range []project.Options{{Slug: "web", Goal: "the web shop"}, {Slug: "api"}, {Slug: "secret-proj", Goal: "SECRET-GOAL"}, {Slug: "dormant"}, {Slug: "attic"}} {
		if _, err := project.New(o); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []struct {
		slug, key string
		v         bool
	}{{"web", "share", true}, {"api", "share", true}, {"dormant", "share", true}, {"dormant", "active", false},
		{"attic", "share", true}, {"attic", "archived", true}, {"secret-proj", "yolo", true}} {
		if err := config.SetProject(s.slug, s.key, s.v); err != nil {
			t.Fatal(err)
		}
	}
	for _, other := range []string{"api", "secret-proj", "dormant"} {
		if err := config.SetLinkPair("web", other, config.Link{Allow: config.LinkBoth}, true); err != nil {
			t.Fatal(err)
		}
	}
	web, _ := project.Open("web")
	if _, err := web.Tasks().Add(human, []tasks.NewTask{{Title: "Next up", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktrees", "web", "t-0001")
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: "web", Argv: []string{"claude", "--token", "SECRET-ARGV"},
			Cwd: worktree, PID: 4242, AgentSID: "SECRET-AGENT-SID", Title: "SECRET-TITLE", State: "blocked", Model: "opus",
			Question:      &proto.Question{Questions: []proto.QuestionItem{{Question: "SECRET-QUESTION"}}},
			Cells:         []proto.ModCell{{Mod: "SECRET-MOD", Column: "c"}},
			RemoteControl: true, Context: 50, ContextWindow: 100},
		{ID: "s-2", Role: proto.RoleThread, Project: "secret-proj", Thread: "t-0001", Argv: []string{"claude", "SECRET-OTHER"}, Cwd: worktree},
		{ID: "s-3", Role: proto.RoleShell, Argv: []string{"zsh"}, Cwd: root},
	}
	ticker := filepath.Join(t.TempDir(), "ticker.json")
	ex := peerExport{
		sessions: func() []proto.SessionInfo { return sessions },
		state: func(slug string, ss []proto.SessionInfo) (proto.ProjectWatch, error) {
			p, err := project.Open(slug)
			if err != nil {
				return proto.ProjectWatch{}, err
			}
			return ProjectWatchOf(p, ss, ticker), nil
		},
		changes: func() (<-chan struct{}, func()) { return make(chan struct{}), func() {} },
	}
	studio := linkedServers(t, ex)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Every answer as it crossed, and each refusal with the slug the
	// caller sent (its own words) taken out.
	var crossed []string
	call := func(method string, params any) (json.RawMessage, error) {
		t.Helper()
		var raw json.RawMessage
		err := studio.Call(ctx, "mini", method, params, &raw)
		crossed = append(crossed, string(raw))
		if err != nil {
			words := err.Error()
			if p, ok := params.(proto.PeerWatchParams); ok && p.Project != "" {
				words = strings.ReplaceAll(words, p.Project, "X")
			}
			crossed = append(crossed, words)
		}
		return raw, err
	}

	raw, err := call(proto.MethodPeerProjects, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ps proto.PeerProjects
	json.Unmarshal(raw, &ps)
	var slugs []string
	for _, p := range ps.Projects {
		slugs = append(slugs, p.Project)
	}
	if !slices.Equal(slugs, []string{"api", "web"}) {
		t.Fatalf("offered %v, want the shared active projects api and web", slugs)
	}
	if w := ps.Projects[1]; w.Goal != "the web shop" || w.Ready != 1 {
		t.Errorf("web: %+v", w)
	}

	raw, err = call(proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web"})
	if err != nil {
		t.Fatal(err)
	}
	var pw proto.PeerWatch
	json.Unmarshal(raw, &pw)
	if len(pw.Sessions) != 1 || pw.Sessions[0].ID != "s-1" || !pw.Sessions[0].Asking || pw.Sessions[0].Model != "opus" || !pw.Sessions[0].RemoteControl {
		t.Errorf("sessions: %+v", pw.Sessions)
	}
	if len(pw.Watch.Links) != 1 || pw.Watch.Links[0].Peer != "api" {
		t.Errorf("links: %+v, want only api's", pw.Watch.Links)
	}
	if len(pw.Watch.Ready) != 1 || pw.Watch.Context == nil || pw.Watch.Context.Percent != 50 {
		t.Errorf("watch: %+v", pw.Watch)
	}

	// Unshared, inactive, archived, missing or malformed: one answer.
	var refusals []string
	for _, slug := range []string{"secret-proj", "dormant", "attic", "nope", "../secret-proj", ""} {
		_, err := call(proto.MethodPeerWatch, proto.PeerWatchParams{Project: slug})
		var pe *proto.Error
		if !errors.As(err, &pe) {
			t.Fatalf("%q: %v", slug, err)
		}
		if slug == "" {
			if pe.Code != proto.ErrBadParams {
				t.Errorf("no slug: %v", err)
			}
			continue
		}
		if pe.Code != proto.ErrNotShared {
			t.Errorf("%q: %v, want not-shared", slug, err)
		}
		refusals = append(refusals, strings.Replace(pe.Message, slug, "X", 1))
	}
	if len(slices.Compact(slices.Clone(refusals))) != 1 {
		t.Errorf("an unshared project answers unlike a missing one: %q", refusals)
	}

	// The user turns share off: gone at once.
	if err := config.SetProject("web", "share", false); err != nil {
		t.Fatal(err)
	}
	if _, err := call(proto.MethodPeerWatch, proto.PeerWatchParams{Project: "web"}); err == nil {
		t.Error("an unshared project still answers")
	}
	raw, _ = call(proto.MethodPeerProjects, nil)
	json.Unmarshal(raw, &ps)
	if len(ps.Projects) != 1 || ps.Projects[0].Project != "api" {
		t.Errorf("after share off: %+v", ps.Projects)
	}

	all := strings.Join(crossed, "\n")
	cfg, _ := os.ReadFile(filepath.Join(root, "config.toml"))
	for _, never := range []string{"secret-proj", "dormant", "attic", "SECRET", root, "worktrees", "config.toml",
		`"argv"`, `"cwd"`, `"pid"`, `"agent_session_id"`, `"cells"`, `"repos"`, `"dir"`} {
		if strings.Contains(all, never) {
			t.Errorf("%q crossed:\n%s", never, all)
		}
	}
	if len(cfg) == 0 || strings.Contains(all, string(cfg)) {
		t.Error("the settings file crossed, or the test wrote none")
	}
}
