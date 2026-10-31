package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

func TestMenuLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Blue":                 "Blue",
		"[ ] Apple":            "Apple",
		"[✔] Apple":            "Apple",
		"Yes  Proceed with it": "Yes",
		"Type something.":      "Type something.",
	} {
		if got := menuLabel(in); got != want {
			t.Errorf("menuLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// longQuestion is T884's: Claude Code v2.1.314 draws it behind "│ " on
// each wrapped line, as testdata/menu's screens captured it in panes
// of 100x40, 60x20 and 60x14 (long-60x18-clipped is the 60x20 one less
// its top two lines).
const longQuestion = "T882's thread found that the terminal view has no composer: tapping the pane opens the keyboard with its own key bar, and the Keys button (with its 'Type into its terminal' strip) exists only in the conversation's composer. What should happen?"

func TestMenuShows(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct {
		screen, question, why string
	}{
		{read("long-100x40.screen"), longQuestion, ""},
		{read("long-60x20.screen"), longQuestion, ""},
		// Its start scrolled off: the rest, from the top line on.
		{read("long-60x18-clipped.screen"), longQuestion, ""},
		// T887: a ~450-character question with quotes, parentheses,
		// semicolons, a slash list and <angle> words, drawn as
		// Claude draws long-60x20 (behind "│ ", wrapped at words):
		// whole, and with its start scrolled off a short pane.
		{read("t886-100x40.screen"), t886Question, ""},
		{read("t886-60x24.screen"), t886Question, ""},
		{read("t886-60x20-clipped.screen"), t886Question, ""},
		// Nothing of it above the options.
		{read("long-60x14.screen"), longQuestion, "pane is too short to show the question above its options"},
		// Another question, or one ending the same way, isn't it.
		{read("long-100x40.screen"), "Which color should the button be?", `screen doesn't show "Which color should the button be?"`},
		{read("long-60x18-clipped.screen"), "Should the key bar move? (with its 'Type into its terminal' strip) exists only in the conversation's composer. What should happen?", "screen doesn't show"},
		{"│ composer. What should happen?\n\n❯ 1. Yes\n  2. No\n", longQuestion, "screen doesn't show"},
		// A menu drawn without bars, and in a box.
		{"Which color should the button be?\n\n❯ 1. Red\n  2. Blue\n", "Which color should the button be?", ""},
		{"│ Which color should │\n│ the button be?      │\n│ ❯ 1. Red            │\n", "Which color should the button be?", ""},
	} {
		why := menuShows(c.screen, c.question)
		if c.why == "" && why != "" || c.why != "" && !strings.HasPrefix(why, c.why) {
			t.Errorf("menuShows(%.40q…) = %q, want %q", c.question, why, c.why)
		}
	}
}

// TestMenuOptionsLong: the long question's options are read off its
// screen, labels and all, wherever the pane cuts it.
func TestMenuOptionsLong(t *testing.T) {
	keys := []string{"Drop Keys from chat (Recommended)", "Add Keys to terminal", "Type something.", "Chat about this"}
	t886 := []string{"Build with defaults (Recommended)", "Defaults + Designs now", "Let me read it first", "Type something.", "Chat about this"}
	for name, want := range map[string][]string{
		"long-100x40.screen": keys, "long-60x20.screen": keys, "long-60x14.screen": keys,
		"t886-100x40.screen": t886, "t886-60x20-clipped.screen": t886,
	} {
		b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, o := range agent.DialogOptions(string(b)) {
			got = append(got, menuLabel(o))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: options %q, want %q", name, got, want)
		}
	}
}

// t886Question is the coordinator's question of T887's screenshot, as
// its AskUserQuestion call sent it.
const t886Question = "T886 (peer project pages) design is ready. Its defaults: switch called 'project pages' (tm peer pages <alias> <project> on|off, set on the server, not from the phone); Board shares only task title/status/steps (no notes); Overview shares the goal and PROJECT.md body but no repo paths; Library shares thread files but not files that came from a third server; Designs deferred (shows 'Not shared'); Memory never. Build it with these defaults?"
