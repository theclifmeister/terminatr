package tui

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// commandDash is a dashboard with three mods' commands: lab's relabel
// on g and its sweep on j (the core's list.down), beta-only's on G, and
// a second mod's g, which lost it to lab.
func commandDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src := &fakeSource{data: testData()}
	no := false
	src.data.ModCommands = []mod.ModCommand{
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "relabel", Title: "Relabel", Key: "g"}},
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "sweep", Title: "Sweep", Key: "j"}},
		{Mod: "lab", Name: "Labeller", Command: mod.Command{ID: "quiet", Title: "Quiet", Menu: &no}},
		{Mod: "only", Name: "Only", Projects: []string{"beta"}, Command: mod.Command{ID: "go", Title: "Go beta", Key: "G"}},
		{Mod: "late", Name: "Late", Conflict: "key conflict: g is lab:relabel's", Command: mod.Command{ID: "sync", Title: "Sync"}},
	}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	return src, m
}

// TestModCommandKeys: a command's key runs it with the project shown and
// the selected row; the core keeps its keys; a command of a mod that
// doesn't run for the project isn't sent.
func TestModCommandKeys(t *testing.T) {
	src, m := commandDash(t)
	sel := m.sel
	run(m, press(m, "g"))
	if len(src.commands) != 1 {
		t.Fatalf("commands %+v", src.commands)
	}
	c := src.commands[0]
	if c.Mod != "lab" || c.Command != "relabel" || c.Project != "alpha" || !c.Console || c.Selection == nil || c.Selection.Project != "alpha" {
		t.Fatalf("sent %+v %+v", c, c.Selection)
	}
	if !strings.Contains(m.msg, "ran Relabel") {
		t.Fatalf("msg %q", m.msg)
	}
	// j stays the core's list.down.
	press(m, "j")
	if len(src.commands) != 1 || m.sel == sel {
		t.Fatalf("j ran a command (%d) or didn't move (%s)", len(src.commands), m.sel)
	}
	press(m, "G")
	if len(src.commands) != 1 || !strings.Contains(m.msg, "doesn't run for alpha") {
		t.Fatalf("a beta-only command ran in alpha: %+v, %q", src.commands, m.msg)
	}
	// Safe mode: no user mod's command.
	src.data.NoMods = true
	m.setData(src.data)
	press(m, "g")
	if len(src.commands) != 1 {
		t.Fatal("a user mod's command ran in safe mode")
	}
}

// TestModCommandMenuAndHelp: the ≡ menu lists the commands that run for
// the project and have an entry, each marked as the mod's with its key;
// the help lists them all, and the Mods tab names the key conflicts.
func TestModCommandMenuAndHelp(t *testing.T) {
	src, m := commandDash(t)
	// A full view of the command's title is one entry, the command's
	// (standup's view and S, T301 B9); another full view stays.
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{Views: []proto.ModView{
		{Mod: "lab", Name: "Lab", View: "relabel", Title: "Relabel", Slot: "full"},
		{Mod: "lab", Name: "Lab", View: "board", Title: "Board", Slot: "full"}}}})
	var labels []string
	for _, it := range m.dashItems() {
		if strings.HasSuffix(it.label, " · lab") || strings.HasSuffix(it.label, " · late") {
			labels = append(labels, it.label+"="+it.key)
		}
	}
	if got, want := strings.Join(labels, " | "), "Board · lab= | Relabel · lab=g | Sweep · lab= | Sync · late="; got != want {
		t.Fatalf("menu\n got %s\nwant %s", got, want)
	}
	for _, it := range m.dashItems() {
		if it.label == "Relabel · lab" {
			run(m, it.run(m))
		}
	}
	if len(src.commands) != 1 || src.commands[0].Command != "relabel" {
		t.Fatalf("menu entry sent %+v", src.commands)
	}
	help := strings.Join(keyLines(200, m.commandHelp()), "\n")
	for _, want := range []string{"Mod commands", "Relabel · Labeller (tm mod run lab relabel)", "g prefix+g", "Sweep · Labeller (tm mod run lab sweep); its key j is taken", "in a mod's view"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "Go beta") {
		t.Errorf("help lists a command of a mod limited to another project:\n%s", help)
	}
	if got := strings.Join(m.keyConflicts("lab"), "; "); got != "key conflict: j is the core's list.down" {
		t.Errorf("lab's conflicts %q", got)
	}
	if got := strings.Join(m.keyConflicts("late"), "; "); got != "key conflict: g is lab:relabel's" {
		t.Errorf("late's conflicts %q", got)
	}
}

// TestModHelpScope: the help lists the commands of the mods that run for
// the project shown, none in safe mode, and no mod's group without any.
func TestModHelpScope(t *testing.T) {
	src, m := commandDash(t)
	// A disabled mod isn't in the registry's commands at all.
	if h := strings.Join(keyLines(200, m.commandHelp()), "\n"); strings.Contains(h, "tm mod run off") {
		t.Fatalf("a disabled mod's command is listed:\n%s", h)
	}
	m.over = &Over{Project: "beta"}
	if h := strings.Join(keyLines(200, m.commandHelp()), "\n"); !strings.Contains(h, "Go beta · Only") || !strings.Contains(h, "G prefix+G") {
		t.Fatalf("beta's help lacks its command:\n%s", h)
	}
	m.over = nil
	src.data.NoMods = true
	m.setData(src.data)
	h := strings.Join(keyLines(200, m.commandHelp()), "\n")
	if strings.Contains(h, "Mod commands:") || strings.Contains(h, "Relabel") {
		t.Fatalf("safe mode lists mods' commands:\n%s", h)
	}
}
