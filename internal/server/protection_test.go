package server

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Protection open, a new install's default: a thread without its
// sandbox and with its agent's own prompts, the guard keeping its floor
// only; guarded adds the guard's rules back, not the sandbox.
func TestProtectionOpen(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home := os.Getenv("TERMINATR_HOME")
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: client.Paths{Home: home}}}
	wt := t.TempDir()
	cfg := filepath.Join(home, "config.toml")
	writeConfig(t, "[projects."+p.Slug+"]\nprotection = \"open\"\nguard_mode = \"learn\"\n\n[projects."+p.Slug+".sandbox]\nhosts = [\"registry.npmjs.org\"]\nexcluded_commands = [\"docker *\"]\n")

	noSandbox, noEscapes := protectFlags(config.ProtectOpen, "claude")
	if !noSandbox || noEscapes {
		t.Fatalf("open claude: no sandbox %v, no escapes %v", noSandbox, noEscapes)
	}
	if ns, _ := protectFlags(config.ProtectOpen, "codex"); ns {
		t.Error("a Codex thread loses its sandbox at open")
	}
	if ns, ne := protectFlags(config.ProtectStrict, "claude"); ns || !ne {
		t.Errorf("strict: no sandbox %v, no escapes %v", ns, ne)
	}

	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt,
		NoSandbox: noSandbox, Protection: config.ProtectOpen}
	g := s.guardRulesFor(thread)
	if !g.On || g.Rules != nil || g.Learn || g.Mods != nil || g.AskEscapes || g.AskFirst != nil ||
		!slices.Contains(g.Floor, "force-push") || !slices.Contains(g.NoWrite, cfg) || !slices.Contains(g.NoWrite, p.Dir) {
		t.Fatalf("open thread's guard %+v", g)
	}
	if d := g.Judge("Bash", map[string]any{"command": "git push origin main"}); d != nil {
		t.Errorf("open: push to main refused: %+v", d)
	}
	if d := g.Judge("Write", map[string]any{"file_path": filepath.Join(p.Dir, "CONTEXT.md")}); d == nil || !d.Floor {
		t.Errorf("open: a write to the project folder passes: %+v", d)
	}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: p.Slug, Cwd: p.Dir}
	if g := s.guardRulesFor(coord); g.Rules != nil || !slices.Contains(g.NoWrite, cfg) {
		t.Fatalf("open coordinator's guard %+v", g)
	}

	lc := loadLaunchConfig()
	set, err := s.resolveAccess(withProtection(accessInput{role: proto.RoleThread, slug: p.Slug, agent: "claude", goos: config.OSMac}, lc.safety(p.Slug)), lc)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]proto.AccessRule{}
	for _, r := range set.Rules {
		seen[r.Kind] = r
		switch r.Kind {
		case proto.AccessHost, proto.AccessExcluded, proto.AccessEscapes:
			if r.Skipped != proto.AccessNoSandbox {
				t.Errorf("%s %s at open: skipped %q", r.Kind, r.Value, r.Skipped)
			}
		case proto.AccessGuardMode:
			if r.Skipped != proto.AccessProtectFloor {
				t.Errorf("guard mode at open: skipped %q", r.Skipped)
			}
		}
	}
	if r := seen[proto.AccessProtection]; r.Value != config.ProtectOpen || r.Source != p.Slug {
		t.Errorf("protection rule %+v", r)
	}
	if r := seen[proto.AccessSandbox]; r.Value != "off" || r.Skipped != "" {
		t.Errorf("sandbox rule %+v", r)
	}
	if y, ns, _ := launchFlags(set); y || !ns {
		t.Errorf("launch flags: yolo %v, no sandbox %v", y, ns)
	}
	if l, _, _ := launchSandbox(set); len(l.Domains) != 0 || len(l.Excluded) != 0 {
		t.Errorf("open launch gets sandbox lists %+v", l)
	}
	if res := explainCommand(set, "docker ps"); res.Sandbox != proto.AccessOutside || res.SandboxWhy != proto.AccessNoSandbox || res.Prompt == proto.PromptNone {
		t.Errorf("explain at open: %+v", res)
	}

	// The coordinator's tm access writes pass the floor only while the
	// project lets it change its access.
	if g := s.guardRulesFor(coord); g.AccessWrites {
		t.Error("access writes without coordinator_access")
	} else if d := g.Judge("Bash", map[string]any{"command": "tm access add host x.dev " + p.Slug}); d == nil || !d.Floor {
		t.Errorf("coordinator's access write passes without coordinator_access: %+v", d)
	}
	writeConfig(t, "[projects."+p.Slug+"]\ncoordinator_access = true\n")
	if g := s.guardRulesFor(coord); !g.AccessWrites {
		t.Error("no access writes with coordinator_access")
	} else if d := g.Judge("Bash", map[string]any{"command": "tm access add host x.dev " + p.Slug}); d != nil {
		t.Errorf("coordinator's access write refused: %+v", d)
	} else if d := g.Judge("Bash", map[string]any{"command": "tm access set protection open " + p.Slug}); d == nil || !d.Floor {
		t.Errorf("coordinator's protection change passes: %+v", d)
	}
	if g := s.guardRulesFor(thread); g.AccessWrites {
		t.Error("a thread gets access writes")
	}

	// Guarded: the guard's rules, still no sandbox.
	writeConfig(t, "[projects."+p.Slug+"]\nprotection = \"guarded\"\n")
	if g := s.guardRulesFor(thread); len(g.Rules) == 0 || !slices.Contains(g.Rules, "worktree-only") {
		t.Errorf("guarded thread's rules %q", g.Rules)
	}
	lc = loadLaunchConfig()
	set, _ = s.resolveAccess(withProtection(accessInput{role: proto.RoleThread, slug: p.Slug, agent: "claude", goos: config.OSMac}, lc.safety(p.Slug)), lc)
	if _, ns, _ := launchFlags(set); !ns {
		t.Error("guarded thread gets its sandbox")
	}
}

// The pin: an install with projects from before protection keeps
// sandboxed, once; a new install stays open.
func TestPinProtection(t *testing.T) {
	testPaths(t)
	home := os.Getenv("TERMINATR_HOME")
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, f) }
	pinProtection(logf)
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err == nil {
		t.Fatal("a new install got a config.toml")
	}
	if _, err := os.Stat(filepath.Join(home, "state", "protection-pinned")); err != nil {
		t.Fatalf("no mark: %v", err)
	}
	os.Remove(filepath.Join(home, "state", "protection-pinned"))
	p := newWatchProject(t)
	pinProtection(logf)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Safety(p.Slug); s.Protection != config.ProtectSandboxed {
		t.Fatalf("pinned at %q", s.Protection)
	}
	lines, _, _ := p.JournalTail(5)
	if !strings.Contains(strings.Join(lines, "\n"), `protection "sandboxed"`) {
		t.Errorf("journal %q", lines)
	}
	// Once: a later open stays open.
	writeConfig(t, "[defaults]\nprotection = \"open\"\n")
	os.Remove(filepath.Join(home, "config.toml"))
	pinProtection(logf)
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err == nil {
		t.Error("pinned twice")
	}
}
