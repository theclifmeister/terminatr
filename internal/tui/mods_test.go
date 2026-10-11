package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/modannot"
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

// TestModsSettings: the General tab lists Agent mod (off) and Agent mod band (on)
// with plain words; enter and space change them in place, saved to the
// settings file, which keeps its comments; the band row says it needs
// Agent mod while that is off; no key or file name shows.
func TestModsSettings(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	path, _ := config.Path()
	if err := os.WriteFile(path, []byte("# mine\n[agent_mod]\nenabled = false # early access\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{data: testData()}
	src.data.AgentModBand = true
	m := newDash(DashOptions{Source: src, Width: 120, Height: 80, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",")
	// The box scrolls: take it from the top to the bottom.
	out := screen(m)
	for range 40 {
		press(m, "down")
		out += "\n" + screen(m)
	}
	// The help text wraps to the box's width: its words, one space apart.
	words := strings.Join(strings.Fields(strings.ReplaceAll(out, "│", " ")), " ")
	for _, want := range []string{"Agent mod  ", "Agent mod band", "early access", "Claude Code 2.1.289", "after the change", "the feed still runs", "needs Agent mod on"} {
		if !strings.Contains(out, want) && !strings.Contains(words, want) {
			t.Errorf("settings lack %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"enabled", "[agent_mod]", "config.toml", "band ="} {
		if strings.Contains(out, bad) {
			t.Errorf("settings name %q:\n%s", bad, out)
		}
	}

	modsRow(t, m, "Agent mod")
	act(m, src, "enter")
	if cfg, _ := config.Load(); !cfg.AgentMod || !cfg.AgentModBand {
		t.Fatalf("enter: mods %v, band %v", cfg.AgentMod, cfg.AgentModBand)
	}
	if data, _ := os.ReadFile(path); string(data) != "# mine\n[agent_mod]\nenabled = true # early access\n" {
		t.Fatalf("file:\n%s", data)
	}
	if out := screen(m); strings.Contains(out, "needs Agent mod on") {
		t.Fatalf("band still says it needs Agent mod:\n%s", out)
	}

	modsRow(t, m, "Agent mod band")
	act(m, src, "space")
	if cfg, _ := config.Load(); !cfg.AgentMod || cfg.AgentModBand {
		t.Fatalf("space: mods %v, band %v", cfg.AgentMod, cfg.AgentModBand)
	}
	if data, _ := os.ReadFile(path); string(data) != "# mine\n[agent_mod]\nenabled = true # early access\nband = false\n" {
		t.Fatalf("file:\n%s", data)
	}
	modsRow(t, m, "Agent mod")
	act(m, src, "enter")
	if cfg, _ := config.Load(); cfg.AgentMod || cfg.AgentModBand {
		t.Fatalf("enter again: mods %v, band %v", cfg.AgentMod, cfg.AgentModBand)
	}
}

// TestModsSettingsForm: a setting written as a dotted key is refused
// with the popup's usual message, naming no file.
func TestModsSettingsForm(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	path, _ := config.Path()
	if err := os.WriteFile(path, []byte("agent_mod.enabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 80, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",")
	modsRow(t, m, "Agent mod")
	act(m, src, "enter")
	if data, _ := os.ReadFile(path); string(data) != "agent_mod.enabled = false\n" {
		t.Fatalf("file changed:\n%s", data)
	}
	if !strings.Contains(screen(m), "it stays as it is") {
		t.Fatalf("no refusal:\n%s", screen(m))
	}
}

func TestPanelBadges(t *testing.T) {
	d := &panel{w: 60}
	d.badges([]modannot.Badge{
		{Mod: "lab", Key: "a", Spans: []modannot.Span{{Text: "docs", Role: "info"}}},
		{Mod: "lab", Key: "b", Spans: []modannot.Span{{Text: "no-check", Role: "warn"}}},
		{Mod: "other", Key: "a", Spans: []modannot.Span{{Text: "x"}}},
	})
	if len(d.lines) != 2 {
		t.Fatalf("lines = %q", d.lines)
	}
	first := ansi.Strip(d.lines[0])
	if !strings.Contains(first, "docs  no-check") || !strings.Contains(first, "mod: lab") || !strings.Contains(ansi.Strip(d.lines[1]), "mod: other") {
		t.Fatalf("lines = %q", d.lines)
	}
}
