package server

import (
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// findRule is set's first rule of kind and value, nil for none.
func findRule(rules []proto.AccessRule, kind, value string) *proto.AccessRule {
	for i, r := range rules {
		if r.Kind == kind && r.Value == value {
			return &rules[i]
		}
	}
	return nil
}

// TestAccessRuleSources: each rule says where it comes from: tm, all
// projects, the project, tm's default; a preset lists what it opens;
// what doesn't apply on the platform is marked skipped, not left out.
func TestAccessRuleSources(t *testing.T) {
	testPaths(t)
	newWatchProject(t)
	writeConfig(t, `[defaults]
sandbox_escapes = false
guard_allow = [{ rule = "force-push", command = "git push -f origin tm/*" }]

[defaults.sandbox]
browser_downloads = true
excluded_commands = ["docker compose *"]

[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }

[projects.demo]
guard_mode = "learn"

[projects.demo.sandbox]
allow = ["Bash(npx playwright *)"]
`)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME"), Socket: "/run/tm.sock"}}}
	set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: t.TempDir(), goos: "linux", noEscapes: true}, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ kind, value, source, skipped string }{
		{proto.AccessExcluded, "docker compose *", proto.AccessFromAll, ""},
		{proto.AccessAllow, "Bash(npx playwright *)", "demo", ""},
		{proto.AccessPreset, "browser_downloads", proto.AccessFromAll, "doesn't apply on Linux"},
		{proto.AccessHost, "registry.npmjs.org", proto.AccessFromAll, "doesn't apply on Linux"},
		{proto.AccessGuide, "browser-downloads", proto.AccessFromAll, "doesn't apply on Linux"},
		{proto.AccessEscapes, "off", proto.AccessFromAll, ""},
		{proto.AccessYolo, "off", proto.AccessFromDefault, ""},
		{proto.AccessGuardMode, "learn", "demo", ""},
		{proto.AccessGuardAllow, "git push -f origin tm/*", proto.AccessFromAll, ""},
		{proto.AccessSecret, "NODE_AUTH_TOKEN", proto.AccessFromAll, ""},
		{proto.AccessSocket, "/run/tm.sock", proto.AccessFromTM, ""},
	} {
		r := findRule(set.Rules, c.kind, c.value)
		if r == nil {
			t.Errorf("no %s %q in %+v", c.kind, c.value, set.Rules)
			continue
		}
		if r.Source != c.source || r.Skipped != c.skipped {
			t.Errorf("%s %q: source %q skipped %q, want %q %q", c.kind, c.value, r.Source, r.Skipped, c.source, c.skipped)
		}
	}
	if r := findRule(set.Rules, proto.AccessAllow, "Bash(npx playwright *)"); r == nil || !slices.Equal(r.Agents, []string{"claude"}) {
		t.Errorf("allow rule's agents: %+v", r)
	}
	if r := findRule(set.Rules, proto.AccessGuardMode, "learn"); r == nil || !r.Live {
		t.Errorf("guard rules aren't live: %+v", r)
	}
	if r := findRule(set.Rules, proto.AccessSecret, "NODE_AUTH_TOKEN"); r == nil || r.Note != "gh-auth-token" {
		t.Errorf("secret's source: %+v", r)
	}
}

// TestAccessLaunchRecord: a thread's launch keeps its rules in its
// folder; access.show gives them back, marking what config.toml changed
// since (restart to apply) but never the guard's live rules, lists the
// agent's own files' rules, and lets a thread see only its own.
func TestAccessLaunchRecord(t *testing.T) {
	testPaths(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", t.TempDir())
	p := newWatchProject(t)
	wt := t.TempDir()
	if r, err := thread.Create(p, thread.Record{Title: "x", Agent: "claude", Worktree: wt, State: "running"}); err != nil || r.ID != "t-0001" {
		t.Fatal(r, err)
	}
	os.MkdirAll(wt+"/.claude", 0o755)
	os.WriteFile(wt+"/.claude/settings.local.json", []byte(`{"permissions":{"allow":["Bash(git log:*)"]}}`), 0o600)
	writeConfig(t, `[projects.demo.sandbox]
excluded_commands = ["npx playwright *"]
`)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(os.Stderr, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	accessNow = func() time.Time { return time.Date(2026, 10, 18, 14, 2, 0, 0, time.UTC) }
	t.Cleanup(func() { accessNow = time.Now })
	rec := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: wt}
	pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(pl.abandon)
	s.recordLaunch(pl.l.rec, pl.access)
	got, ok := readLaunchRecord(p, "t-0001")
	if !ok || got.Session != "s-1" || got.Thread != "t-0001" || got.Launched.IsZero() || findRule(got.Rules, proto.AccessExcluded, "npx playwright *") == nil {
		t.Fatalf("record %v: %+v", ok, got)
	}

	human := callCtx{human: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Human} }}
	show := func(c callCtx, id string) (proto.AccessShowResult, *proto.Error) {
		res, perr := s.accessShow(proto.AccessShowParams{Project: "demo", Thread: id}, c)
		if perr != nil {
			return proto.AccessShowResult{}, perr
		}
		return res.(proto.AccessShowResult), nil
	}
	res, perr := show(human, "t-0001")
	if perr != nil || !res.Recorded || res.Changes != 0 {
		t.Fatalf("unchanged: %+v %v", res, perr)
	}
	if r := findRule(res.Rules, proto.AccessAllow, "Bash(git log:*)"); r == nil || r.Where != proto.AccessAgent || r.Source != ".claude/settings.local.json" {
		t.Errorf("agent file rule: %+v", r)
	}

	writeConfig(t, `[projects.demo]
guard_mode = "learn"

[projects.demo.sandbox]
excluded_commands = ["make bench"]
`)
	res, _ = show(human, "t-0001")
	if res.Changes != 2 {
		t.Errorf("changes %d: %+v", res.Changes, res.Rules)
	}
	if r := findRule(res.Rules, proto.AccessExcluded, "npx playwright *"); r == nil || r.Change != proto.AccessRemoved {
		t.Errorf("removed: %+v", r)
	}
	if r := findRule(res.Rules, proto.AccessExcluded, "make bench"); r == nil || r.Change != proto.AccessAdded {
		t.Errorf("added: %+v", r)
	}
	if r := findRule(res.Rules, proto.AccessGuardMode, "learn"); r == nil || r.Change != "" {
		t.Errorf("guard mode, live: %+v", r)
	}

	self := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"} }}
	if own, perr := show(self, "t-0001"); perr != nil {
		t.Errorf("own thread refused: %v", perr)
	} else if findRule(own.Rules, proto.AccessAllow, "Bash(git log:*)") != nil {
		t.Error("a thread was shown the agent's own files")
	}
	if _, perr := show(self, ""); perr == nil {
		t.Error("a thread saw the project's")
	}
	// On the human socket, a caller in an agent's session (an
	// unsandboxed command) is that agent, as gateHuman has it.
	escaped := callCtx{human: true, who: self.who}
	if res, perr := show(escaped, ""); perr == nil {
		t.Errorf("an escaped thread saw the project's: %+v", res)
	}
	if own, perr := show(escaped, "t-0001"); perr != nil || findRule(own.Rules, proto.AccessAllow, "Bash(git log:*)") != nil {
		t.Errorf("an escaped thread's own: %v, agent files shown %v", perr, perr == nil)
	}
	coord := callCtx{human: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator, Project: "demo"} }}
	if _, perr := show(coord, "t-0001"); perr == nil {
		t.Error("the coordinator on the human socket saw a thread's")
	}
	other := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator, Project: "demo"} }}
	if _, perr := show(other, "t-0001"); perr == nil {
		t.Error("the coordinator saw a thread's")
	}

	res, perr = show(human, "")
	if perr != nil || res.Recorded || findRule(res.Rules, proto.AccessWorktree, "<worktree>") == nil {
		t.Errorf("project: %+v %v", res, perr)
	}
}

// TestAccessExplain: an excluded command runs outside the sandbox,
// asking first unless an allow rule covers it; a compound command stays
// inside; yolo asks nothing; the guard's verdict comes with it.
func TestAccessExplain(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	writeConfig(t, `[projects.demo.sandbox]
excluded_commands = ["npx playwright *", "docker compose *"]
allow = ["Bash(npx playwright *)"]
`)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}, agents: reg, log: log.New(os.Stderr, "", 0)}
	explain := func(cmd string) proto.AccessExplainResult {
		t.Helper()
		res, perr := s.accessExplain(proto.AccessExplainParams{Project: "demo", Command: cmd})
		if perr != nil {
			t.Fatal(perr)
		}
		return res.(proto.AccessExplainResult)
	}
	if r := explain("npx playwright test"); r.Sandbox != proto.AccessOutside || r.Prompt != proto.PromptNone || r.SandboxFrom != "demo" || r.Guard != "allowed" || !strings.Contains(r.Agents, "claude") {
		t.Errorf("playwright: %+v", r)
	}
	if r := explain("docker compose up"); r.Sandbox != proto.AccessOutside || r.Prompt != proto.PromptAsks {
		t.Errorf("docker: %+v", r)
	}
	if r := explain("npx playwright test > out.txt"); r.Sandbox != proto.AccessInside {
		t.Errorf("compound: %+v", r)
	}
	if r := explain("git push --force origin main"); r.Guard != "refused" {
		t.Errorf("force-push: %+v", r)
	}
	writeConfig(t, "[projects.demo]\nyolo = true\n")
	// Yolo: no sandbox, no prompts, the guard's floor only (T574 case 4).
	if r := explain("ls"); r.Sandbox != proto.AccessOutside || r.SandboxFrom != "demo" || r.Prompt != proto.PromptNone || r.PromptFrom != "demo" {
		t.Errorf("yolo: %+v", r)
	}
	if r := explain("git push --force origin main"); r.Guard != "refused" {
		t.Errorf("yolo force-push (the floor): %+v", r)
	}
	if r := explain("git push origin main"); r.Guard != "allowed" {
		t.Errorf("yolo push-default (not the floor): %+v", r)
	}
}

// TestAccessGitDirRule: a thread in a repo's worktree shows its git-dir
// grant as one tm rule naming the repo, each path it opens noting it;
// a thread outside a repo has none, and a project's thread to come a
// placeholder. The launch never takes the rule itself.
func TestAccessGitDirRule(t *testing.T) {
	testPaths(t)
	newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	root := realPath(t.TempDir())
	repo, wt := root+"/repo", root+"/wt"
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
		{"-C", repo, "worktree", "add", "-q", "-b", "x", wt},
	} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	rules := func(cwd string) []proto.AccessRule {
		t.Helper()
		set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: cwd, tree: pinTree(repo, cwd), goos: "darwin"}, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		if a := launchAccess(set); slices.Contains(a.Write, repo) {
			t.Errorf("the repo itself launched as writable: %q", a.Write)
		}
		return set.Rules
	}
	got := rules(wt)
	if r := findRule(got, proto.AccessGitDir, repo); r == nil || r.Source != proto.AccessFromTM {
		t.Errorf("no git-dir rule for %s: %+v", repo, got)
	}
	if r := findRule(got, proto.AccessWrite, repo+"/.git/objects"); r == nil || r.Note != "git-dir grant: "+repo {
		t.Errorf("objects: %+v", r)
	}
	for _, r := range rules(t.TempDir()) {
		if r.Kind == proto.AccessGitDir {
			t.Errorf("outside a repo: %+v", r)
		}
	}
	if findRule(rules(""), proto.AccessGitDir, "<its repo>") == nil {
		t.Error("no placeholder for a thread to come")
	}
	// Placeholders are display-only: no kind or value of theirs launches,
	// even given a path rule's kind.
	set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", goos: "darwin"}, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	set.Rules = append(set.Rules, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessWrite, Value: "<repo>/.git", Source: proto.AccessFromTM})
	a := launchAccess(set)
	for _, p := range slices.Concat(a.Read, a.Write, a.WriteFiles, a.NoWrite, a.NoWriteFiles) {
		if strings.HasPrefix(p, "<") {
			t.Errorf("a placeholder launched: %q in %+v", p, a)
		}
	}
}

// TestOwnEscapes: the effective view says how tm treats an excluded
// command of the agent's own files (T574 case 7): an escape it asks
// about, refused with escapes off, one tm excludes too, or not used
// under yolo; its other rules stay shown only.
func TestOwnEscapes(t *testing.T) {
	files := func() []proto.AccessRule {
		return []proto.AccessRule{
			{Where: proto.AccessAgent, Kind: proto.AccessExcluded, Value: "docker", Source: "~/.claude/settings.json"},
			{Where: proto.AccessAgent, Kind: proto.AccessExcluded, Value: "npx playwright *", Source: "~/.claude/settings.json"},
			{Where: proto.AccessAgent, Kind: proto.AccessAllow, Value: "Bash(ls)", Source: "~/.claude/settings.json"},
		}
	}
	set := func(escapes, yolo string) proto.AccessSet {
		return proto.AccessSet{Rules: []proto.AccessRule{
			{Where: proto.AccessOutside, Kind: proto.AccessExcluded, Value: "npx playwright *", Source: "demo"},
			{Where: proto.AccessOutside, Kind: proto.AccessEscapes, Value: escapes},
			{Where: proto.AccessPrompts, Kind: proto.AccessYolo, Value: yolo},
		}}
	}
	got := ownEscapes(files(), set("on", "off"))
	if got[0].Note != proto.AccessOwnEscape || got[1].Note != "tm excludes it too" || got[2].Note != "" {
		t.Errorf("escapes on: %+v", got)
	}
	if rm := got[0].Remarks(); len(rm) != 1 || !strings.Contains(rm[0], "its guard judges") {
		t.Errorf("remarks %q", rm)
	}
	if got := ownEscapes(files(), set("off", "off")); got[0].Note != proto.AccessOwnEscapeOff {
		t.Errorf("escapes off: %+v", got[0])
	}
	if got := ownEscapes(files(), set("on", "on")); got[0].Skipped != proto.AccessYoloNoSandbox || got[0].Note != "" {
		t.Errorf("yolo: %+v", got[0])
	}
}
