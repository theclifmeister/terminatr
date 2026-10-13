package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// probeTheme gives every role a colour of its own, so text the console
// draws in no role (the terminal's default colour) shows as such.
const probeTheme = `
[dark]
head      = { fg = ["#000001", "#000001"], bold = true }
sel       = { fg = ["#000002", "#000002"], bg = ["#100002", "#100002"] }
sel_focus = { fg = ["#000003", "#000003"], bg = ["#100003", "#100003"] }
faint     = { fg = ["#000004", "#000004"] }
accent    = { fg = ["#000005", "#000005"] }
title     = { fg = ["#000006", "#000006"], bold = true }
bad       = { fg = ["#000007", "#000007"] }
warn      = { fg = ["#000008", "#000008"] }
good      = { fg = ["#000009", "#000009"] }
info      = { fg = ["#00000a", "#00000a"] }
plain     = { fg = ["#00000b", "#00000b"] }
`

// withProbeTheme makes probeTheme this console's theme for the test.
func withProbeTheme(t *testing.T) {
	t.Helper()
	tt, err := parseTheme([]byte(probeTheme))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COLORTERM", "truecolor")
	if err := setTheme(tt, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setTheme(builtinTheme, true) })
}

// unthemed is the text of s drawn in no theme role: runs of visible
// characters without a foreground colour, which the terminal draws in
// its own default colour (white, or grey under a bare faint); one entry
// per line that has any.
func unthemed(s string) []string {
	var out []string
	for n, line := range strings.Split(s, "\n") {
		var fg bool
		var run, bad strings.Builder
		flush := func() {
			if t := strings.TrimSpace(run.String()); t != "" {
				if bad.Len() > 0 {
					bad.WriteString(" | ")
				}
				bad.WriteString(t)
			}
			run.Reset()
		}
		state := byte(0)
		for len(line) > 0 {
			seq, width, nb, newState := ansi.DecodeSequence(line, state, nil)
			state = newState
			line = line[nb:]
			if width == 0 {
				if isSGR(seq) {
					fg = sgrFg(seq, fg)
					flush()
				}
				continue
			}
			if !fg {
				run.WriteString(seq)
			} else {
				flush()
			}
		}
		flush()
		if bad.Len() > 0 {
			out = append(out, fmt.Sprintf("line %d: %s", n+1, bad.String()))
		}
	}
	return out
}

func TestUnthemedProbe(t *testing.T) {
	cases := map[string][]string{
		"\x1b[38;2;0;0;1mok\x1b[m":  nil,
		"plain":                     {"line 1: plain"},
		"\x1b[38;2;0;0;1ma\x1b[m b": {"line 1: b"},
		"\x1b[2mdim\x1b[m":          {"line 1: dim"},
		"\x1b[31mred\x1b[39m x":     {"line 1: x"},
	}
	for in, want := range cases {
		if got := unthemed(in); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("unthemed(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDashboardThemed: every bit of text the dashboard draws, with its
// sidebar, details panel, footer and the popups over it (the settings'
// tabs, the help, the project info, the tasks, a new task), is in a
// theme role, so a theme colours all of it.
func TestDashboardThemed(t *testing.T) {
	withProbeTheme(t)
	for _, keys := range [][]string{
		nil, {"down", "down"}, {","}, {",", "2"}, {",", "3"}, {"?"}, {"i"}, {"t"}, {"n"}, {"a"},
	} {
		src := &fakeSource{data: testData()}
		m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "beta"}})
		m.setData(src.data)
		press(m, keys...)
		for _, l := range unthemed(m.render()) {
			t.Errorf("after %q: %s", keys, l)
		}
	}
}

// TestPanelsThemed: as TestDashboardThemed, the attach client's chrome:
// the info panel beside a thread, the coordinator's panel and the
// status bar.
func TestPanelsThemed(t *testing.T) {
	withProbeTheme(t)
	for name, c := range map[string]*client{"info panel": infoClient(t, 200), "coordinator panel": coordClient(t, 200)} {
		c.infoLayout()
		b, _ := c.appendInfo(nil, false)
		for _, l := range unthemed(string(b)) {
			t.Errorf("%s: %s", name, l)
		}
		c.status()
		for _, l := range unthemed(c.statusText) {
			t.Errorf("%s status bar: %s", name, l)
		}
	}
}

// TestSidebarThemed: as TestDashboardThemed, the sidebar's every kind of
// row.
func TestSidebarThemed(t *testing.T) {
	withProbeTheme(t)
	rows := goldenTree()
	side := sideLines(rows, 40, 30, sideFooter(rows, 40, ServerInfo{}, false, false), false)
	for _, l := range unthemed(strings.Join(side, "\n")) {
		t.Error(l)
	}
}

// hardColour is a colour the console's code would draw with outside the
// theme: lipgloss's colours and its bare style, x/ansi's, and a raw SGR
// colour (30-37, 90-97, 38;…).
var hardColour = regexp.MustCompile(`lipgloss\.(Color|ANSIColor|NewStyle|Red|Green|Yellow|Blue|Cyan|Magenta|White|Black|Bright\w*)\b|ansi\.(BasicColor|IndexedColor|TrueColor|RGBColor)\b|\\x1b\[[0-9;]*(3[0-8]|9[0-7])m`)

// TestNoHardColours: the console draws only with its theme's roles
// (th()); only theme.go turns a theme's data into colours.
func TestNoHardColours(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "theme.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(data), "\n") {
			if m := hardColour.FindString(l); m != "" {
				t.Errorf("%s:%d: %s draws outside the theme: use a role, th().Plain and the like", f, i+1, m)
			}
		}
	}
}
