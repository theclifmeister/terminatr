package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestFeaturesPrograms: Settings → all projects → Features: Programs is
// off by default and asks each way. On says what a director does; off
// names the programs going dormant and the directors it stops. N keeps
// it as it was, Y switches it through the server.
func TestFeaturesPrograms(t *testing.T) {
	src, m := popupData(t)
	src.programs = ProgramsState{Programs: []program.Summary{{Slug: "acme", Members: []string{"alpha", "beta"}}}}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	keyPress(m, ",")
	keyPress(m, "2")
	sv := m.top().(*settingsView)
	at := slices.IndexFunc(sv.tabs[1].rows, func(r setting) bool { return r.label == "Programs" })
	if at < 0 || sv.tabs[1].rows[at].section != "Features" {
		t.Fatal("no Features: Programs row in all projects")
	}
	for range at {
		keyPress(m, "down")
	}
	if got := screen(m); !strings.Contains(got, "dormant: acme (1 program is kept)") {
		t.Fatalf("the off row doesn't name the dormant program:\n%s", got)
	}
	keyPress(m, "enter")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Turn programs on" || !strings.Contains(cv.question, "acme (alpha, beta)") {
		t.Fatalf("on asks %+v", m.top())
	}
	keyPress(m, "N")
	if len(src.switched) != 0 || m.programs.On {
		t.Fatalf("N switched: %v", src.switched)
	}
	keyPress(m, "enter")
	drain(t, m, keyPress(m, "Y"))
	if !slices.Equal(src.switched, []bool{true}) || !m.programs.On {
		t.Fatalf("Y didn't switch on: %v", src.switched)
	}

	// Off names the running director.
	m.data.Sessions = append(m.data.Sessions, proto.SessionInfo{ID: "s-40", Role: proto.RoleDirector, Project: "acme"})
	keyPress(m, "enter")
	cv, ok = m.top().(*confirmView)
	if !ok || cv.title != "Turn programs off" || !strings.Contains(cv.question, "acme (alpha, beta) go dormant") || !strings.Contains(cv.question, "director acme") {
		t.Fatalf("off asks %+v", m.top())
	}
	drain(t, m, keyPress(m, "Y"))
	if !slices.Equal(src.switched, []bool{true, false}) || m.programs.On {
		t.Fatalf("Y didn't switch off: %v", src.switched)
	}
}

// TestProgramsSwitched: the footer says what the switch did.
func TestProgramsSwitched(t *testing.T) {
	if got := ProgramsSwitched(false, proto.ProgramsSwitchResult{Changed: true, Stopped: []string{"director acme"}}); got != "programs off; stopped director acme, to resume when you turn programs on" {
		t.Fatal(got)
	}
	if got := ProgramsSwitched(true, proto.ProgramsSwitchResult{}); got != "programs are already on" {
		t.Fatal(got)
	}
}
