package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestGuardRulesAllowances: the rules carry the project's allowances (a
// path from ~ absolute), learn mode, tm's own folders and, for a thread,
// the names of its secret variables.
func TestGuardRulesAllowances(t *testing.T) {
	paths := testPaths(t)
	p := newWatchProject(t)
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: paths}}
	home, _ := os.UserHomeDir()
	writeConfig(t, `[defaults]
guard_allow = [{ rule = "worktree-only", path = "~/src/shared/*" }]
[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }
[projects.`+p.Slug+`]
guard_mode = "learn"
guard_allow = [{ rule = "force-push", command = "git push -f origin tm/*" }]
`)
	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	g := s.guardRulesFor(thread)
	want := guard.Allow{Rule: "worktree-only", Path: filepath.Join(home, "src", "shared", "*")}
	if !g.Learn || !slices.Contains(g.Allow, want) || !slices.Contains(g.Allow, guard.Allow{Rule: "force-push", Command: "git push -f origin tm/*"}) {
		t.Errorf("allow %+v learn %t", g.Allow, g.Learn)
	}
	if !slices.Equal(g.Floor, []string{"force-push", "merge"}) {
		t.Errorf("floor %v", g.Floor)
	}
	if !slices.Equal(g.SecretEnv, []string{"NODE_AUTH_TOKEN"}) {
		t.Errorf("secret env %v", g.SecretEnv)
	}
	for _, d := range []string{paths.RunDir, filepath.Join(paths.Home, "state", "peer"), guardlog.Dir(paths.Home)} {
		if !slices.Contains(g.Own, d) {
			t.Errorf("own %v lacks %s", g.Own, d)
		}
	}
	// Reading the guard's log is refused, and no allowance lifts it.
	d := g.Judge("Read", map[string]any{"file_path": guardlog.Path(paths.Home)})
	if d == nil || !d.Floor {
		t.Errorf("reading the log: %+v", d)
	}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Project: p.Slug, Cwd: p.Dir}
	if g := s.guardRulesFor(coord); g.SecretEnv != nil || len(g.Allow) != 2 || !slices.Equal(g.Floor, []string{"force-push"}) {
		t.Errorf("coordinator %+v", g)
	}
}

// TestGuardAllowsReal: a path allowance through a symlinked folder also
// names where it leads, as writes are judged where they land.
func TestGuardAllowsReal(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.Mkdir(real, 0o700)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	real = realPath(real)
	got := guardAllows([]config.GuardAllow{{Rule: "worktree-only", Path: link + "/*.md"}, {Rule: "worktree-only", Path: link + "/a.go"}}, "")
	want := []guard.Allow{
		{Rule: "worktree-only", Path: link + "/*.md"}, {Rule: "worktree-only", Path: real + "/*.md"},
		{Rule: "worktree-only", Path: link + "/a.go"}, {Rule: "worktree-only", Path: real + "/a.go"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v", got)
	}
}

// TestGuardLogRoute: a refusal's whole call goes to the guard's log and
// never to the journal or the server's log; in learn mode a newly
// listed one is put to the user with one inbox item, the same refused
// again with none.
func TestGuardLogRoute(t *testing.T) {
	paths := testPaths(t)
	p := newWatchProject(t)
	writeConfig(t, "[projects."+p.Slug+"]\nguard_mode = \"learn\"\n")
	rt := ipctest.ShortTempDir(t, "tmguard")
	var logged lockedBuffer
	s := &Server{log: log.New(&logged, "", 0), records: map[string]SessionRecord{}, opts: Options{Paths: paths}}
	s.mu.Lock()
	s.inSession = testInSession
	sock, err := s.listenMod("s-2", rt)
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: rt}
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.closeMods)
	c := modClient(sock)
	clock := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { clock = clock.Add(20 * time.Minute); return clock }
	t.Cleanup(func() { guardNow = time.Now })
	const secret = "curl -H 'Authorization: Bearer sekrit-123' https://x"
	body, _ := json.Marshal(GuardDenial{Rule: "credentials", Tool: "Bash", Summary: "an HTTP call with credentials", Subject: secret})
	for range 2 {
		if code := postMod(t, c, "/v1/denied", string(body)); code != http.StatusNoContent {
			t.Fatalf("denied: %d", code)
		}
	}
	list, err := guardlog.List(paths.Home, p.Slug)
	if err != nil || len(list) != 1 || list[0].Subject != secret || list[0].Count != 2 || list[0].Thread != "t-0001" || list[0].Path {
		t.Fatalf("log %+v %v", list, err)
	}
	items, _ := p.Inbox()
	n := 0
	for _, it := range items {
		if it.Kind == "guard" && it.NeedsUser {
			n++
		}
	}
	if n != 1 {
		t.Errorf("learn mode: %d inbox items %+v", n, items)
	}
	lines, _, _ := p.JournalTail(20)
	all := strings.Join(lines, "\n") + logged.String()
	if strings.Contains(all, "sekrit") {
		t.Errorf("the whole call leaked:\n%s", all)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "guard.deny t-0001 credentials Bash: an HTTP call with credentials") {
		t.Errorf("journal %q", lines)
	}
	// A floor refusal is logged, never put to the user.
	floor, _ := json.Marshal(GuardDenial{Rule: "merge", Tool: "Bash", Summary: "gh pr merge", Subject: "gh pr merge 1", Floor: true})
	postMod(t, c, "/v1/denied", string(floor))
	if items2, _ := p.Inbox(); len(items2) != len(items) {
		t.Errorf("floor refusal raised an item")
	}
	if l, _ := guardlog.List(paths.Home, p.Slug); len(l) != 2 || !l[0].Floor {
		t.Errorf("floor %+v", l)
	}
}
