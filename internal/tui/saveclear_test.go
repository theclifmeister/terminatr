package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestSideSaveClear: c on a running coordinator's row saves and clears
// it (coordinator.clear), and the footer says so; past the context hint
// the row's key bar offers it; elsewhere c says what it is for.
func TestSideSaveClear(t *testing.T) {
	m, src, _ := foldDash(t)
	src.data.ContextHint = 40
	src.data.Sessions[0].Context, src.data.Sessions[0].ContextWindow = 100_000, 200_000
	m.setData(src.data)
	m.sideSel = "c:alpha"
	bar := func() string {
		i := slices.IndexFunc(m.tree(), func(r treeRow) bool { return r.key() == "c:alpha" })
		r := m.tree()[i]
		return ansi.Strip(sideKeyBar(&r, true, false, 200))
	}
	if foot := bar(); !strings.Contains(foot, "c save and clear") {
		t.Errorf("footer past the hint: %q", foot)
	}
	drain(t, m, keyPress(m, "c"))
	if !slices.Equal(src.saveCleared, []string{"alpha"}) || !strings.Contains(m.msg, "asked alpha's coordinator to save what it learned") {
		t.Fatalf("c: cleared %v msg %q", src.saveCleared, m.msg)
	}
	m.sideSel = "p:alpha"
	drain(t, m, keyPress(m, "c"))
	if len(src.saveCleared) != 1 || !strings.Contains(m.msg, "c on a running coordinator") {
		t.Fatalf("c on a project: cleared %v msg %q", src.saveCleared, m.msg)
	}
	src.data.Sessions[0].Context = 20_000
	m.setData(src.data)
	m.sideSel = "c:alpha"
	if foot := bar(); strings.Contains(foot, "save and clear") {
		t.Errorf("footer below the hint: %q", foot)
	}
}

// TestCoordSaveClear: past the context hint the coordinator's info panel
// names the key, and a click on the line saves and clears; a paired
// server's panel names none. The words say what came of it.
func TestCoordSaveClear(t *testing.T) {
	w := &proto.ProjectWatch{Project: "demo", Context: &proto.WatchContext{Tokens: 90_000, Window: 200_000, Percent: 45, Threshold: 40, Hint: true}}
	lines, hits := coordLines(w, 60, time.Now())
	found := false
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "c or click: save and clear") {
			found = hits[i].kind == hitSaveClear
		}
	}
	if !found {
		t.Fatalf("no save-and-clear line with its click:\n%s", ansi.Strip(strings.Join(lines, "\n")))
	}
	if text, _, hint := contextWords(w.Context, true); strings.Contains(hint, "click") || text == "" {
		t.Errorf("read-only hint %q", hint)
	}
	if _, _, hint := contextWords(&proto.WatchContext{Percent: 10, Threshold: 40}, false); hint != "" {
		t.Errorf("hint below the threshold: %q", hint)
	}
	if got := saveClearWords("demo", proto.CoordinatorClearResult{Session: "s-1"}, nil); !strings.Contains(got, "asked demo's coordinator") {
		t.Errorf("words %q", got)
	}
	refused := &proto.Error{Code: proto.ErrRefused, Message: "the coordinator's agent (codex) has no clear prompt ([inject] clear)"}
	if got := saveClearWords("demo", proto.CoordinatorClearResult{}, refused); got != "not cleared: "+refused.Message {
		t.Errorf("words %q", got)
	}
	if got := saveClearWords("demo", proto.CoordinatorClearResult{}, errors.New("no server")); got != "not cleared: no server" {
		t.Errorf("words %q", got)
	}
}

// TestPanelSaveClearKey: c is the panel's save-clear key, rebindable,
// and the details panel leaves it to the list.
func TestPanelSaveClearKey(t *testing.T) {
	if op, ok := panelOp("c"); !ok || op != panelSaveClear {
		t.Fatalf("c: op %d ok %v", op, ok)
	}
	if op, ok := sideOp("c"); !ok || op != sideSaveClear {
		t.Fatalf("sidebar c: op %d ok %v", op, ok)
	}
	m, _, _ := foldDash(t)
	if _, ok := m.detailsKey("c"); ok {
		t.Error("the details panel took c")
	}
}
