package theme

import (
	"strings"
	"testing"
)

func TestBundled(t *testing.T) {
	tt, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, rs := range []RoleSet{tt.Light, tt.Dark} {
		if m := rs.Missing(); len(m) > 0 {
			t.Fatalf("bundled theme leaves out %v", m)
		}
		if rs.Pane != nil {
			t.Fatal("the built-in theme has a pane palette")
		}
	}
	if _, err := Parse([]byte("[dark]\nnope = 1\n")); err == nil || !strings.Contains(err.Error(), "unknown key dark.nope") {
		t.Fatalf("unknown key: %v", err)
	}
}

// TestAgentColors: an agent gets the theme's colours only under a pane
// palette, each role's truecolor choice, an ANSI one through the palette
// (none where it sets none), a 256-colour index as xterm's, and the
// built-in role where the theme leaves one out (T342).
func TestAgentColors(t *testing.T) {
	tt, err := Parse([]byte(`
[dark]
accent = { fg = ["cyan", "#88C0D0"] }
good   = { fg = ["green"] }
warn   = { fg = ["214"] }
info   = { fg = ["blue"] }
sel    = { fg = ["black"], bg = ["white", "#4c566a"] }
[dark.pane]
fg = "#d8dee9"
bg = "#2e3440"
palette = ["#3b4252", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#88c0d0", "#e5e9f0",
           "#4c566a", "#bf616a", "#a3be8c", "#ebcb8b", "#81a1c1", "#b48ead", "#8fbcbb", "#eceff4"]
[light]
accent = { fg = ["blue"] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if a := AgentColors(tt, false); a != nil {
		t.Fatalf("light has no pane palette, yet %+v", a)
	}
	a := AgentColors(tt, true)
	if a == nil || a.Scheme != "dark" || a.PaneFg != "#d8dee9" || a.PaneBg != "#2e3440" || a.ANSI[6] != "#88c0d0" {
		t.Fatalf("dark: %+v", a)
	}
	for role, want := range map[string]string{
		"accent": "#88c0d0", // the truecolor choice, lower-cased
		"good":   "#a3be8c", // green through the palette
		"warn":   "#ffaf00", // xterm 214
		"info":   "#81a1c1",
	} {
		if got := a.Fg[role]; got != want {
			t.Errorf("%s = %q, want %q", role, got, want)
		}
	}
	if a.Bg["sel"] != "#4c566a" || a.Fg["sel_focus"] != a.Fg["sel"] || a.Bg["sel_focus"] != "#4c566a" {
		t.Errorf("sel %q/%q, sel_focus %q/%q", a.Fg["sel"], a.Bg["sel"], a.Fg["sel_focus"], a.Bg["sel_focus"])
	}
	// Every role is there (a template can name any), the left-out ones
	// the built-in theme's.
	builtin, _ := Bundled()
	for _, r := range builtin.Dark.Roles() {
		if _, ok := a.Fg[r.Name]; !ok {
			t.Errorf("no %s", r.Name)
		}
	}
	if a.Fg["bad"] == "" {
		t.Error("bad: the built-in role's colour through the palette is missing")
	}

	// A palette without ANSI colours: ANSI roles give none.
	tt2, _ := Parse([]byte("[dark]\ngood = { fg = [\"green\"] }\n[dark.pane]\nbg = \"#000000\"\n"))
	if a := AgentColors(tt2, true); a == nil || a.Fg["good"] != "" || a.PaneBg != "#000000" {
		t.Fatalf("bg-only palette: %+v", a)
	}
}

func TestXterm256(t *testing.T) {
	for n, want := range map[int]string{16: "#000000", 21: "#0000ff", 196: "#ff0000", 231: "#ffffff", 232: "#080808", 255: "#eeeeee"} {
		if got := hexOf(xterm256(n)); got != want {
			t.Errorf("%d = %s, want %s", n, got, want)
		}
	}
}
