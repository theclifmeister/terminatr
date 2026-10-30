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
	for _, name := range []string{"long-100x40.screen", "long-60x20.screen", "long-60x14.screen"} {
		b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, o := range agent.DialogOptions(string(b)) {
			got = append(got, menuLabel(o))
		}
		if want := []string{"Drop Keys from chat (Recommended)", "Add Keys to terminal", "Type something.", "Chat about this"}; !slices.Equal(got, want) {
			t.Errorf("%s: options %q, want %q", name, got, want)
		}
	}
}
