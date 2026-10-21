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
// projects, the project, tm's default; tm 0.37's switch lists
// the rules it unfolds into; a drop marks all projects' rule dropped;
// what doesn't apply on the platform is marked skipped, not left out.
func TestAccessRuleSources(t *testing.T) {
	testPaths(t)
	newWatchProject(t)
	writeConfig(t, `[defaults]
sandbox_escapes = false
guard_allow = [{ rule = "force-push", command = "git push -f origin tm/*" }]

[defaults.sandbox]
browser_downloads = true
excluded_commands = ["docker compose *", "make e2e"]

[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }

[projects.demo]
guard_mode = "learn"

[projects.demo.sandbox]
allow = ["Bash(npx playwright *)"]
local_ports = true
drop = ["make e2e"]
`)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME"), Socket: "/run/tm.sock"}}}
	set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: t.TempDir(), goos: "linux", noEscapes: true}, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ kind, value, source, skipped string }{
		{proto.AccessExcluded, "docker compose *", proto.AccessFromAll, ""},
		{proto.AccessAllow, "Bash(npx playwright *)", "demo", ""},
		{proto.AccessLocalPorts, "on", "demo", "doesn't apply on Linux"},
		{proto.AccessHost, "registry.npmjs.org", proto.AccessFromAll, "doesn't apply on Linux"},
		{proto.AccessGuide, "browser-downloads", proto.AccessFromAll, ""}, // plain text: every platform
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
	if r := findRule(set.Rules, proto.AccessExcluded, "make e2e"); r == nil || !r.Dropped || r.Given() || r.Source != proto.AccessFromAll {
		t.Errorf("dropped rule: %+v", r)
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
	// The project's coordinator sees its thread-to-come and its threads'
	// launch records (on either socket), never the agent's own files;
	// another project's coordinator sees nothing.
	for _, onHuman := range []bool{false, true} {
		coord := callCtx{human: onHuman, who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator, Project: "demo"} }}
		for _, id := range []string{"", "t-0001"} {
			res, perr := show(coord, id)
			if perr != nil {
				t.Errorf("coordinator (human socket %v) refused thread %q: %v", onHuman, id, perr)
			} else if findRule(res.Rules, proto.AccessAllow, "Bash(git log:*)") != nil {
				t.Errorf("coordinator was shown the agent's own files (thread %q)", id)
			}
		}
		foreign := callCtx{human: onHuman, who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator, Project: "other"} }}
		for _, id := range []string{"", "t-0001"} {
			if _, perr := show(foreign, id); perr == nil {
				t.Errorf("another project's coordinator (human socket %v) saw thread %q", onHuman, id)
			}
		}
	}
	unplaced := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Coordinator} }}
	if _, perr := show(unplaced, ""); perr == nil {
		t.Error("a coordinator of no project saw the project's")
	}

	res, perr = show(human, "")
	if perr != nil || res.Recorded || findRule(res.Rules, proto.AccessWorktree, "<worktree>") == nil {
		t.Errorf("project: %+v %v", res, perr)
	}
}

// TestAccessExplain: an excluded command runs outside the sandbox,
// asking first unless it runs without asking or an old allow rule covers
// it; a compound command stays
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
		res, perr := s.accessExplain(proto.AccessExplainParams{Project: "demo", Command: cmd}, callCtx{human: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Human} }})
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
	// One rule, one choice (T574 case 1): asks first, the guard asking;
	// or without asking.
	writeConfig(t, `[projects.demo.sandbox]
excluded_commands = ["make e2e", { command = "pnpm test *", ask = false }]
`)
	if r := explain("make e2e"); r.Sandbox != proto.AccessOutside || r.Prompt != proto.PromptAsks || r.PromptFrom != "demo" || r.Guard != "asks" {
		t.Errorf("asks first: %+v", r)
	}
	if r := explain("pnpm test --run"); r.Sandbox != proto.AccessOutside || r.Prompt != proto.PromptNone || !strings.Contains(r.PromptWhy, "without asking") || r.Guard != "allowed" {
		t.Errorf("without asking: %+v", r)
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

// TestAccessExplainBareExcluded (T632): all projects' excluded commands
// without a * take only the bare command, as Claude matches them, so
// explain keeps docker ps inside for a thread and a thread to come
// alike, saying why, and show says it beside the rule; with " *" it
// runs outside.
func TestAccessExplainBareExcluded(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	p := newWatchProject(t)
	if r, err := thread.Create(p, thread.Record{Title: "x", Agent: "claude", Worktree: t.TempDir(), State: "running"}); err != nil || r.ID != "t-0001" {
		t.Fatal(r, err)
	}
	writeConfig(t, `[defaults.sandbox]
excluded_commands = ["docker", "git push"]
`)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}, agents: reg, log: log.New(os.Stderr, "", 0)}
	human := callCtx{human: true, who: func() caller.Caller { return caller.Caller{Kind: caller.Human} }}
	explain := func(thread, cmd string) proto.AccessExplainResult {
		t.Helper()
		res, perr := s.accessExplain(proto.AccessExplainParams{Project: "demo", Thread: thread, Command: cmd}, human)
		if perr != nil {
			t.Fatal(perr)
		}
		return res.(proto.AccessExplainResult)
	}
	for _, thread := range []string{"", "t-0001"} {
		if r := explain(thread, "docker"); r.Sandbox != proto.AccessOutside || r.SandboxFrom != proto.AccessFromAll || r.Prompt != proto.PromptAsks {
			t.Errorf("%q bare docker: %+v", thread, r)
		}
		if r := explain(thread, "docker ps"); r.Sandbox != proto.AccessInside || !strings.Contains(r.SandboxWhy, `excluded command "docker" takes only docker itself`) {
			t.Errorf("%q docker ps: %+v", thread, r)
		}
		if r := explain(thread, "git push --dry-run origin HEAD"); r.Sandbox != proto.AccessInside || !strings.Contains(r.SandboxWhy, "git push * takes it") {
			t.Errorf("%q git push: %+v", thread, r)
		}
		if r := explain(thread, "docker ps | head"); r.Sandbox != proto.AccessInside || strings.Contains(r.SandboxWhy, "takes only") {
			t.Errorf("%q compound: %+v", thread, r)
		}
	}
	res, perr := s.accessShow(proto.AccessShowParams{Project: "demo"}, human)
	if perr != nil {
		t.Fatal(perr)
	}
	if r := findRule(res.(proto.AccessShowResult).Rules, proto.AccessExcluded, "docker"); r == nil || !slices.ContainsFunc(r.Remarks(), func(m string) bool { return strings.HasPrefix(m, "only docker itself") }) {
		t.Errorf("show: %+v", r)
	}
	writeConfig(t, `[defaults.sandbox]
excluded_commands = ["docker *", "git push *"]
`)
	if r := explain("t-0001", "docker ps"); r.Sandbox != proto.AccessOutside || r.Prompt != proto.PromptAsks {
		t.Errorf("docker *: %+v", r)
	}
	if r := explain("t-0001", "git push --dry-run origin HEAD"); r.Sandbox != proto.AccessOutside {
		t.Errorf("git push *: %+v", r)
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

// TestAccessExplainGate: the human and the project's coordinator explain;
// another project's coordinator, a thread, a mod and an escaped agent
// don't.
func TestAccessExplainGate(t *testing.T) {
	cases := []struct {
		name  string
		human bool
		who   caller.Caller
		ok    bool
	}{
		{"human", true, caller.Caller{Kind: caller.Human}, true},
		{"coordinator", false, caller.Caller{Kind: caller.Coordinator, Project: "demo"}, true},
		{"coordinator, human socket", true, caller.Caller{Kind: caller.Coordinator, Project: "demo"}, true},
		{"other project's coordinator", false, caller.Caller{Kind: caller.Coordinator, Project: "other"}, false},
		{"coordinator of no project", false, caller.Caller{Kind: caller.Coordinator}, false},
		{"thread", false, caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}, false},
		{"thread, human socket", true, caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}, false},
		{"mod", false, caller.Caller{Kind: caller.Mod, Project: "demo"}, false},
	}
	for _, c := range cases {
		who := c.who
		if _, perr := mayExplainAccess(callCtx{human: c.human, who: func() caller.Caller { return who }}, "demo"); (perr == nil) != c.ok {
			t.Errorf("%s: %v, want ok %v", c.name, perr, c.ok)
		}
	}
}
