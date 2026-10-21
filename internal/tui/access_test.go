package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAccessView: e on a thread's row shows what it launched with, each
// rule with its source, RESTART TO APPLY on what changed since, the
// agent's own files' rules shown only, the settings file by that name;
// E on the project popup's Access tab shows what a new thread gets.
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
			{Where: proto.AccessOutside, Kind: proto.AccessExcluded, Value: "docker", Source: proto.AccessFromAll},
		}}}
	m.sel = "th:alpha:t-0002"
	drain(t, m, keyPress(m, "E"))
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
	if !strings.Contains(sc, "only docker itself, with no arguments: docker * takes it with them") {
		t.Errorf("no remark on a bare excluded command:\n%s", sc)
	}
	if strings.Contains(sc, "config.toml") {
		t.Errorf("the view names the settings file:\n%s", sc)
	}
	keyPress(m, "esc")

	src.access = proto.AccessShowResult{AccessSet: proto.AccessSet{OS: "linux"}}
	keyPress(m, "A")
	keyPress(m, "5")
	drain(t, m, keyPress(m, "E"))
	if got := src.accessShown[len(src.accessShown)-1]; got != "alpha " {
		t.Errorf("Access tab asked for %q", got)
	}
	if sc := whole(m); !strings.Contains(sc, "Effective access · alpha · a new thread (Linux)") || !strings.Contains(sc, "What a new thread of alpha gets") {
		t.Errorf("project view:\n%s", sc)
	}
}

// TestAccessViewEscapes: an agent's own file can hold anything, so no
// escape of a rule reaches the screen, by its value or a remark quoting
// one, and tm advises nothing on the user's own excluded commands.
func TestAccessViewEscapes(t *testing.T) {
	_, m := popupData(t)
	osc := "\x1b]52;c;aGk=\a"
	v := &accessView{res: proto.AccessShowResult{AccessSet: proto.AccessSet{Agent: "claude", OS: "darwin", Rules: []proto.AccessRule{
		{Where: proto.AccessAgent, Kind: proto.AccessExcluded, Value: "make" + osc + "\nfake", Source: ".claude/settings.local.json", Note: proto.AccessOwnEscape},
		{Where: proto.AccessOutside, Kind: proto.AccessExcluded, Value: "x *", Source: "alpha", Refused: "bad " + osc + "\nspoof"},
	}}}}
	raw := v.render(m)
	if !slices.ContainsFunc(strings.Split(ansi.Strip(raw), "\n"), func(l string) bool {
		return strings.Contains(l, "REFUSED: bad") && strings.Contains(l, "spoof")
	}) {
		t.Fatalf("the refused remark isn't one line:\n%s", ansi.Strip(raw))
	}
	if strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\a") || strings.Contains(raw, "only make") {
		t.Errorf("an escape or a remark on the user's own rule reached the screen:\n%q", raw)
	}
}
