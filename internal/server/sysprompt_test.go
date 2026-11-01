package server

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// TestSystemPromptLaunch: Claude and Codex, coordinator and thread, get
// their rules in the system prompt (a coordinator PROJECT.md too), and
// the record is stamped with what it was launched with (T915).
func TestSystemPromptLaunch(t *testing.T) {
	p := testPaths(t)
	cleanThreadTemp(t, "demo", "t-0001")
	t.Setenv("HOME", t.TempDir())
	proj := newWatchProject(t)
	if err := os.WriteFile(proj.Path("PROJECT.md"), []byte("---\ngoal: g\n---\n# Instructions\n\nAlways say kumquat.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(t.TempDir(), "brief.md")
	os.WriteFile(brief, []byte("# Thread t-0001 of project demo\n\nBrief text.\n"), 0o600)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(os.Stderr, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	launch := func(rec SessionRecord) *preparedLaunch {
		t.Helper()
		pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
		if perr != nil {
			t.Fatal(perr)
		}
		t.Cleanup(pl.abandon)
		return pl
	}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: "demo", Cwd: proj.Dir}
	thr := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: t.TempDir(), Brief: brief}

	pl := launch(coord)
	file := filepath.Join(pl.rt, promptFile)
	if i := slices.Index(pl.argv, "--append-system-prompt-file"); i < 0 || pl.argv[i+1] != file {
		t.Errorf("claude coordinator argv: %q", pl.argv)
	}
	b, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(b), "tm skill coordinator v") || !strings.Contains(string(b), "# PROJECT.md\n") || !strings.Contains(string(b), "Always say kumquat.") {
		t.Errorf("coordinator system prompt:\n%s", b)
	}
	if st := pl.l.rec.Prompt; st == nil || st.Version != version.Version || st.Rules == "" || st.Project == "" {
		t.Errorf("coordinator stamp: %+v", st)
	}

	pl = launch(thr)
	file = filepath.Join(pl.rt, promptFile)
	if i := slices.Index(pl.argv, "--append-system-prompt-file"); i < 0 || pl.argv[i+1] != file {
		t.Errorf("claude thread argv: %q", pl.argv)
	}
	b, _ = os.ReadFile(file)
	if !strings.HasPrefix(string(b), "# Thread t-0001 of project demo\n\nBrief text.\n\n# Your rules\n\ntm skill thread v") || strings.Contains(string(b), "# PROJECT.md") {
		t.Errorf("thread system prompt:\n%s", b)
	}
	if st := pl.l.rec.Prompt; st == nil || st.Rules == "" || st.Project != "" {
		t.Errorf("thread stamp: %+v", st)
	}

	coord.Agent, thr.Agent = "codex", "codex"
	for _, rec := range []SessionRecord{coord, thr} {
		pl = launch(rec)
		i := slices.IndexFunc(pl.argv, func(a string) bool { return strings.HasPrefix(a, "developer_instructions=") })
		if i < 0 || !strings.Contains(pl.argv[i], "tm skill "+rec.Role+" v") || pl.l.rec.Prompt == nil {
			t.Errorf("codex %s: %q", rec.Role, pl.argv)
		}
	}

	// A shell session has none.
	pl = launch(SessionRecord{ID: "s-3", Role: proto.RoleShell, Agent: "claude", Cwd: t.TempDir()})
	if slices.Contains(pl.argv, "--append-system-prompt-file") || pl.l.rec.Prompt != nil {
		t.Errorf("shell: %q %+v", pl.argv, pl.l.rec.Prompt)
	}
}

// TestStartupBlock: with the rules in the system prompt the SessionStart
// block leaves them out, says where they are, fits under Claude's cap,
// and says what to re-read when tm, the rules or PROJECT.md changed
// since the session started (T915).
func TestStartupBlock(t *testing.T) {
	testPaths(t)
	proj := newWatchProject(t)
	big := "# Context\n\n" + strings.Repeat("A line of living context that goes on for a while.\n", 600)
	if err := os.WriteFile(proj.Path("CONTEXT.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	pm, _ := os.ReadFile(proj.Path("PROJECT.md"))
	rules, _ := roleRules("coordinator", "demo")
	stamp := &PromptStamp{Version: version.Version, Rules: textHash([]byte(rules)), Project: textHash(pm)}
	block := func(r SessionRecord) string {
		t.Helper()
		b, err := contextFor(r, filepath.Join(t.TempDir(), "ticker.json"))()
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	coord := SessionRecord{Role: proto.RoleCoordinator, Project: "demo", Prompt: stamp}
	got := block(coord)
	if !strings.HasPrefix(got, "Your rules (`tm skill coordinator`) and PROJECT.md are in your system prompt") || strings.Contains(got, "tm skill coordinator v") {
		t.Errorf("coordinator block head:\n%.400s", got)
	}
	if len(got) > project.StartupBudget || !strings.HasSuffix(got, "\n"+project.StartupTail) || !strings.Contains(got, "lines: tm context --part context]") {
		t.Errorf("coordinator block: %d bytes (budget %d), ends %q", len(got), project.StartupBudget, got[max(len(got)-400, 0):])
	}
	for _, drift := range []string{"tm was upgraded", "rules changed", "PROJECT.md changed"} {
		if strings.Contains(got, drift) {
			t.Errorf("no drift, yet %q", drift)
		}
	}

	// Without a stamp (an adopted session, an agent without a system
	// prompt) the rules come first, as before.
	if got := block(SessionRecord{Role: proto.RoleCoordinator, Project: "demo"}); !strings.HasPrefix(got, "tm skill coordinator v") {
		t.Errorf("no stamp:\n%.200s", got)
	}

	old := *stamp
	old.Version, old.Rules = "0.1.0", "0000"
	coord.Prompt = &old
	if got := block(coord); !strings.Contains(got, "tm was upgraded since this session started (0.1.0 → "+version.Version+"): run `tm skill coordinator` once") {
		t.Errorf("upgrade:\n%.600s", got)
	}
	mod := *stamp
	mod.Rules = "0000"
	coord.Prompt = &mod
	if got := block(coord); !strings.Contains(got, "Your rules changed since this session started") || strings.Contains(got, "upgraded") {
		t.Errorf("rules changed:\n%.600s", got)
	}
	coord.Prompt = stamp
	os.WriteFile(proj.Path("PROJECT.md"), append(pm, "\nA new instruction.\n"...), 0o644)
	if got := block(coord); !strings.Contains(got, "PROJECT.md changed since this session started: read "+proj.Path("PROJECT.md")+" once") {
		t.Errorf("PROJECT.md changed:\n%.600s", got)
	}

	trules, _ := roleRules("thread", "demo")
	thr := SessionRecord{Role: proto.RoleThread, Project: "demo", Thread: "t-0001", Brief: "/b/brief.md",
		Prompt: &PromptStamp{Version: version.Version, Rules: textHash([]byte(trules))}}
	got = block(thr)
	if !strings.HasPrefix(got, "Your brief and your rules (`tm skill thread`) are in your system prompt") || strings.Contains(got, "tm skill thread v") ||
		strings.Contains(got, "PROJECT.md changed") || len(got) > project.StartupBudget {
		t.Errorf("thread block:\n%s", got)
	}
	thr.Prompt = nil
	if got := block(thr); !strings.HasPrefix(got, "tm skill thread v") {
		t.Errorf("thread, no stamp:\n%.200s", got)
	}
}

// TestArgvTooLong: only Windows's command line is near a system
// prompt's size.
func TestArgvTooLong(t *testing.T) {
	long := []string{"codex", "-c", "developer_instructions=" + strings.Repeat("x", winCmdLine)}
	if !argvTooLong("windows", long) || argvTooLong("darwin", long) || argvTooLong("windows", long[:2]) {
		t.Error("argvTooLong")
	}
}
