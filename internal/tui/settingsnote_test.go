package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSettingsClosingNoteReachable: on the , popup's last row the tab
// scrolls on to its closing note, at a short height; the screen is
// testdata/settings-all-projects-short.golden (go test -run
// TestSettingsClosingNoteReachable -update rewrites it).
func TestSettingsClosingNoteReachable(t *testing.T) {
	_, m := popupData(t)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	keyPress(m, ",")
	keyPress(m, "2")
	n := len(m.top().(*settingsView).tabs[1].rows)
	for range n + 2 {
		keyPress(m, "down")
	}
	got := screen(m)
	if !strings.Contains(got, "Every project follows these") {
		t.Fatalf("the closing note is out of view on the last row:\n%s", got)
	}
	var b strings.Builder
	for _, l := range strings.Split(got, "\n") {
		b.WriteString(strings.TrimRight(l, " ") + "\n")
	}
	path := filepath.Join("testdata", "settings-all-projects-short.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSettingsClosingNoteReachable -update writes it)", err)
	}
	if b.String() != string(want) {
		t.Errorf("differs from %s:\n%s", path, b.String())
	}
}
