package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAccessView: e on a thread's row shows what it launched with, each
// rule with its source, RESTART TO APPLY on what changed since, the
// agent's own files' rules shown only, the settings file by that name;
// e on the project popup's Sandbox tab shows what a new thread gets.
func TestAccessView(t *testing.T) {
	src, m := popupData(t)
	src.access = proto.AccessShowResult{Recorded: true, Changes: 2, AccessSet: proto.AccessSet{Agent: "claude", OS: "darwin",
		Launched: time.Date(2026, 10, 18, 14, 2, 0, 0, time.UTC),
		Rules: []proto.AccessRule{
			{Where: proto.AccessInside, Kind: proto.AccessWorktree, Value: "/wt/t-0002", Source: proto.AccessFromTM, Note: "the thread's worktree"},
			{Where: proto.AccessInside, Kind: proto.AccessNoWriteFile, Value: "/h/.terminatr/config.toml", Source: proto.AccessFromTM, Note: proto.AccessSettingsNote},
			{Where: proto.AccessOutside, Kind: proto.AccessExcluded, Value: "npx playwright *", Source: "alpha", Agents: []string{"claude"}, Change: proto.AccessRemoved},
			{Where: proto.AccessInside, Kind: proto.AccessHost, Value: "registry.npmjs.org", Source: proto.AccessFromAll, Change: proto.AccessAdded},
			{Where: proto.AccessGuard, Kind: proto.AccessGuardMode, Value: "learn", Source: "alpha", Live: true},
			{Where: proto.AccessAgent, Kind: proto.AccessAllow, Value: "Bash(git log:*)", Source: ".claude/settings.local.json"},
		}}}
	m.sel = "th:alpha:t-0002"
	keyPress(m, "e")
	if !slices.Equal(src.accessShown, []string{"alpha t-0002"}) {
		t.Fatalf("asked for %q", src.accessShown)
	}
	sc := whole(m)
	for _, want := range []string{"Effective access · alpha · t-0002 (claude, macOS)", "launched", "excluded npx playwright *",
		"RESTART TO APPLY: removed since launch", "RESTART TO APPLY: added since launch", "the agent's own file, shown only",
		"(your settings file)", "2 changes since launch: tm thread restart t-0002 applies them", "guard-mode learn"} {
		if !strings.Contains(sc, want) {
			t.Errorf("view lacks %q:\n%s", want, sc)
		}
	}
	if strings.Contains(sc, "config.toml") {
		t.Errorf("the view names the settings file:\n%s", sc)
	}
	keyPress(m, "esc")

	src.access = proto.AccessShowResult{AccessSet: proto.AccessSet{OS: "linux"}}
	keyPress(m, "a")
	keyPress(m, "5")
	keyPress(m, "e")
	if got := src.accessShown[len(src.accessShown)-1]; got != "alpha " {
		t.Errorf("Sandbox tab asked for %q", got)
	}
	if sc := whole(m); !strings.Contains(sc, "Effective access · alpha · a new thread (Linux)") || !strings.Contains(sc, "What a new thread of alpha gets") {
		t.Errorf("project view:\n%s", sc)
	}
}
