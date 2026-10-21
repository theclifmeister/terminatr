package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/modstore"
)

// modsRow selects the General tab's row labelled label.
func modsRow(t *testing.T, m *dash, label string) {
	t.Helper()
	sv := m.top().(*settingsView)
	for i, r := range sv.tabs[0].rows {
		if r.label == label {
			sv.tabs[0].sel = i
			return
		}
	}
	t.Fatalf("no %q setting", label)
}

// TestSettingsForm: a setting written as a dotted key is refused with
// the popup's usual message, naming no file.
func TestSettingsForm(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	path, _ := config.Path()
	if err := os.WriteFile(path, []byte("ui.needs_first = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 80, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",")
	modsRow(t, m, "Needs you first")
	act(t, m, src, "enter")
	if data, _ := os.ReadFile(path); string(data) != "ui.needs_first = false\n" {
		t.Fatalf("file changed:\n%s", data)
	}
	if !strings.Contains(screen(m), "it stays as it is") {
		t.Fatalf("no refusal:\n%s", screen(m))
	}
}

func TestPanelBadges(t *testing.T) {
	d := &panel{w: 60}
	d.badges([]modstore.Badge{
		{Mod: "lab", Key: "a", Spans: []modstore.Span{{Text: "docs", Role: "info"}}},
		{Mod: "lab", Key: "b", Spans: []modstore.Span{{Text: "no-check", Role: "warn"}}},
		{Mod: "other", Key: "a", Spans: []modstore.Span{{Text: "x"}}},
	})
	if len(d.lines) != 2 {
		t.Fatalf("lines = %q", d.lines)
	}
	first := ansi.Strip(d.lines[0])
	if !strings.Contains(first, "docs  no-check") || strings.Contains(first, "mod: ") || strings.TrimSpace(ansi.Strip(d.lines[1])) != "x" {
		t.Fatalf("lines = %q", d.lines)
	}
}
