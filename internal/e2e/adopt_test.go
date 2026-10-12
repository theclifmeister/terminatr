package e2e

// T40: tm thread adopt (docs/SPEC.md §9, Adopt). An agent session the
// user started outside the projects becomes a thread: its tm calls are
// the thread's, its todos and report reach the project, and resolve
// treats it as a started thread, keeping a repository's own checkout.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

const adoptScript = `
[[step]]
do = "todo_create"
subject = "alpha"
description = "first"
active_form = "Doing alpha"

[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" task status T1 review > "$OUT/adopt-taskstatus" 2>&1; echo "exit $?" >> "$OUT/adopt-taskstatus"'

[[step]]
do = "run"
cmd = 'printf "## Report\nAdopted and done.\n\n## Next\nReview it\n" | "$TERMINATR_BIN" report > "$OUT/adopt-report" 2>&1; echo "exit $?" >> "$OUT/adopt-report"'

[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" done > "$OUT/adopt-done" 2>&1; echo "exit $?" >> "$OUT/adopt-done"'
`

// coordAdoptScripts are a coordinator's adopts: of the session the test
// names in $OUT/sid, with the flag that no longer stands for the user's
// word, and of a session that doesn't exist; then, once the user asked,
// of that session again.
var coordAdoptScripts = map[string]string{
	"coord-adopt": `
[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" thread adopt "$(cat "$OUT/sid")" --approved-by-user > "$OUT/adopt-unasked" 2>&1; echo "exit $?" >> "$OUT/adopt-unasked"'

[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" thread adopt s-99 > "$OUT/adopt-missing" 2>&1; echo "exit $?" >> "$OUT/adopt-missing"'
`,
	"coord-adopt-asked": `
[[step]]
do = "run"
cmd = '"$TERMINATR_BIN" thread adopt "$(cat "$OUT/sid")" > "$OUT/adopt-asked" 2>&1; echo "exit $?" >> "$OUT/adopt-asked"'
`,
}

// TestThreadAdoptNeedsTheUser (T315): the coordinator adopts only the
// session the user asked it to on the human socket (T on the dashboard),
// whatever start_threads and --approved-by-user say; unasked, its refusal
// is the same for a session that doesn't exist. The go-ahead is used up.
func TestThreadAdoptNeedsTheUser(t *testing.T) {
	env, projDir, out := threadEnv(t)
	env.scripts(coordAdoptScripts)
	writeConfig(t, env, "[projects.demo]\nstart_threads = \"auto\"\n")
	repo := firstRepo(t, projDir)
	env.Trust(repo)
	s := env.StartAgent("claude", repo)
	env.WaitState(s, "idle", agentWait)
	os.WriteFile(filepath.Join(out, "sid"), []byte(s.ID), 0o644)

	coord := &Session{ID: strings.TrimSpace(env.MustCLI("project", "open", "demo"))}
	env.WaitState(coord, "idle", agentWait)
	env.Prompt(coord, "run coord-adopt")
	unasked, missing := readOut(t, out, "adopt-unasked"), readOut(t, out, "adopt-missing")
	if !strings.Contains(unasked, "needs-approval") || !strings.Contains(unasked, "hasn't asked to adopt "+s.ID) || !strings.Contains(unasked, "exit 1") {
		t.Fatalf("unasked adopt: %q", unasked)
	}
	if !strings.Contains(missing, "needs-approval") || strings.Contains(missing, "no session") || !strings.Contains(missing, "exit 1") {
		t.Fatalf("adopt of a missing session: %q", missing)
	}
	if info, _ := env.Info(s); info.Role != "shell" {
		t.Fatalf("adopted unasked: %+v", info)
	}

	// The user's T: the go-ahead on the human socket, which a client in
	// an agent's session doesn't try (a run of this suite from a thread).
	t.Setenv(caller.EnvSession, "")
	c, err := server.Dial(server.Paths{Socket: env.Socket}, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: s.ID, Project: "demo"}, nil); err != nil {
		t.Fatalf("session.adopt.ask: %v", err)
	}
	env.WaitState(coord, "idle", agentWait)
	env.Prompt(coord, "run coord-adopt-asked")
	if got := readOut(t, out, "adopt-asked"); !strings.Contains(got, "adopted session "+s.ID+" as t-0001") {
		t.Fatalf("asked adopt: %q", got)
	}
	if info, _ := env.Info(s); info.Role != "thread" || info.Thread != "t-0001" {
		t.Fatalf("session after adopt: %+v", info)
	}
	var asked proto.SessionAdoptAskResult
	if err := c.Call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: s.ID, Project: "demo", Check: true}, &asked); err != nil || asked.Asked {
		t.Fatalf("the go-ahead outlived the adopt: %+v %v", asked, err)
	}
	if j, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md")); !strings.Contains(string(j), "coordinator thread.adopt t-0001") || !strings.Contains(string(j), "(asked by the user)") {
		t.Errorf("journal:\n%s", j)
	}
}

func TestThreadAdopt(t *testing.T) {
	env, projDir, out := threadEnv(t)
	env.scripts(map[string]string{"adopted-work": adoptScript})
	repo := firstRepo(t, projDir)
	wt := filepath.Join(t.TempDir(), "by-hand")
	if b, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "fix-login", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, b)
	}
	wt, _ = filepath.EvalSymlinks(wt)
	env.Trust(wt, repo)
	env.MustCLI("task", "add", "Fix the login", "--status", "ready", "--project", "demo")

	// What can't be adopted.
	shell := env.Start("shell")
	if r := env.CLI("thread", "adopt", shell.ID, "--project", "demo"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "no agent runs") {
		t.Fatalf("adopt a shell: %+v", r)
	}
	if r := env.CLI("thread", "adopt", "s-99", "--project", "demo"); r.Code != 1 {
		t.Fatalf("adopt a missing session: %+v", r)
	}

	s := env.StartAgent("claude", wt)
	env.WaitState(s, "idle", agentWait)
	got := env.MustCLI("thread", "adopt", s.ID, "--task", "T1", "--project", "demo")
	if !strings.Contains(got, "adopted session "+s.ID+" as t-0001 in "+wt+" on fix-login") {
		t.Fatalf("adopt: %q", got)
	}
	if r := env.CLI("thread", "adopt", s.ID, "--project", "demo"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "in-project") {
		t.Fatalf("adopt twice: %+v", r)
	}

	tdir := filepath.Join(projDir, "threads", "t-0001")
	var rec struct {
		Title, Task, Repo, Branch, Worktree, Session, State string
		Adopted, Checkout                                   bool
	}
	readTOML(t, filepath.Join(tdir, "thread.toml"), &rec)
	if rec.Title != "Fix the login" || rec.Task != "T1" || rec.Repo != repo || rec.Branch != "fix-login" || rec.Worktree != wt ||
		rec.Session != s.ID || rec.State != "running" || !rec.Adopted || rec.Checkout {
		t.Fatalf("record %+v (repo %s)", rec, repo)
	}
	info, _ := env.Info(s)
	if info.Role != "thread" || info.Project != "demo" || info.Thread != "t-0001" {
		t.Fatalf("session after adopt: %+v", info)
	}
	if task := env.MustCLI("task", "show", "T1", "--project", "demo"); !strings.Contains(task, "status: started") || !strings.Contains(task, "thread: t-0001") {
		t.Errorf("task:\n%s", task)
	}
	// It is told it is a thread now, with its brief.
	env.WaitFake("prompt", agentWait, func(r FakeRecord) bool {
		return strings.Contains(r.Str("text"), "You are now thread t-0001") && strings.Contains(r.Str("text"), filepath.Join(tdir, "brief.md"))
	})

	// Its tm calls are the thread's.
	env.WaitState(s, "idle", agentWait)
	env.MustCLI("thread", "prompt", "t-0001", "run adopted-work", "--project", "demo")
	if got := readOut(t, out, "adopt-taskstatus"); !strings.Contains(got, "coordinator-only") {
		t.Errorf("adopted thread set a task status: %q", got)
	}
	if got := readOut(t, out, "adopt-report"); !strings.Contains(got, "stored report 1 for t-0001") {
		t.Errorf("report: %q", got)
	}
	if got := readOut(t, out, "adopt-done"); !strings.Contains(got, "exit 0") {
		t.Errorf("done: %q", got)
	}
	Poll(agentWait, func() bool {
		b, _ := os.ReadFile(filepath.Join(tdir, "STATUS.md"))
		return strings.Contains(string(b), "alpha")
	})
	var list []struct {
		ID, Report string
		Done       bool
		Adopted    bool
	}
	json.Unmarshal([]byte(env.MustCLI("thread", "list", "--json", "--project", "demo")), &list)
	if len(list) != 1 || list[0].Report != "new" || !list[0].Done || !list[0].Adopted {
		t.Fatalf("thread list: %+v", list)
	}

	// A server restart resumes it as the thread's.
	env.MustCLI("server", "restart", "--yes")
	if !Poll(agentWait, func() bool {
		info, ok := env.Info(s)
		return ok && info.Role == "thread" && info.Thread == "t-0001" && info.Agent == "claude"
	}) {
		t.Fatalf("after a server restart: %+v", env.Sessions())
	}

	// Resolve as for a started thread: the clean worktree goes, and the
	// branch, whose commits are all on main.
	res := env.MustCLI("thread", "resolve", "t-0001", "--project", "demo")
	if !strings.Contains(res, "removed worktree "+wt) || !strings.Contains(res, "deleted branch fix-login (merged into origin/main)") {
		t.Fatalf("resolve: %q", res)
	}

	// Adopted in the repository's own checkout: resolve keeps it.
	s2 := env.StartAgent("claude", repo)
	env.WaitState(s2, "idle", agentWait)
	env.MustCLI("thread", "adopt", s2.ID, "--title", "In the checkout", "--project", "demo")
	var rec2 struct {
		Worktree string
		Checkout bool
	}
	readTOML(t, filepath.Join(projDir, "threads", "t-0002", "thread.toml"), &rec2)
	if !rec2.Checkout || rec2.Worktree != repo {
		t.Fatalf("checkout record %+v", rec2)
	}
	res = env.MustCLI("thread", "resolve", "t-0002", "--project", "demo")
	if !strings.Contains(res, "kept checkout "+repo) {
		t.Fatalf("resolve checkout: %q", res)
	}
	if _, err := os.Stat(filepath.Join(repo, "README")); err != nil {
		t.Fatalf("checkout gone: %v", err)
	}
	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	if !strings.Contains(string(journal), "human thread.adopt t-0001 T1 Fix the login (session "+s.ID+")") {
		t.Errorf("journal:\n%s", journal)
	}
}

// firstRepo is the project's first repository, as config.toml lists it
// (T335).
func firstRepo(t *testing.T, projDir string) string {
	t.Helper()
	repos := approvedRepos(t, projDir)
	if len(repos) == 0 {
		t.Fatalf("no repos for %s in config.toml", projDir)
	}
	r, _ := filepath.EvalSymlinks(repos[0])
	return r
}

// approvedRepos is the repo list of the project in projDir, from
// [projects.<slug>] repos in its home's config.toml.
func approvedRepos(t *testing.T, projDir string) []string {
	t.Helper()
	slug, home := filepath.Base(projDir), filepath.Dir(filepath.Dir(projDir))
	var cfg struct {
		Projects map[string]struct{ Repos []string } `toml:"projects"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &cfg); err != nil {
		t.Fatalf("config.toml: %v", err)
	}
	return cfg.Projects[slug].Repos
}
