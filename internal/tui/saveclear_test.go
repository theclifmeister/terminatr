package tui

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestSideSaveClear: past the context hint a running coordinator's row
// offers prefix+C in its key bar; plain c on the row types through, and
// prefix+C on the dashboard saves and clears the selected project's
// coordinator with no question.
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
	if foot := bar(); !strings.Contains(foot, "prefix+C save and clear") {
		t.Errorf("footer past the hint: %q", foot)
	}
	// Plain c types through to the coordinator (typethrough.go): it
	// clears nothing.
	drain(t, m, keyPress(m, "c"), reflect.TypeOf(typeSettledMsg{}))
	if len(src.saveCleared) != 0 || !m.typing {
		t.Fatalf("plain c cleared %v, typing %v", src.saveCleared, m.typing)
	}
	drain(t, m, m.prefixCommand("C"))
	if !slices.Equal(src.saveCleared, []string{"alpha"}) || !strings.Contains(m.msg, "asked alpha's coordinator to save what it learned") {
		t.Fatalf("prefix+C: cleared %v msg %q", src.saveCleared, m.msg)
	}
	src.data.Sessions[0].Context = 20_000
	m.setData(src.data)
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
		if strings.Contains(ansi.Strip(l), "prefix+C or click: save and clear") {
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

// TestSaveClearNotPlainC: plain c is neither the sidebar's nor the
// info panel's key any more, and prefix+C is a locked prefix command.
func TestSaveClearNotPlainC(t *testing.T) {
	if op, ok := panelOp("c"); ok {
		t.Errorf("panel c: op %d", op)
	}
	if op, ok := sideOp("c"); ok {
		t.Errorf("sidebar c: op %d", op)
	}
	if !prefixStep(chord{}, true, uv.Key{Code: 'C', Text: "C"}, false).saveClear {
		t.Error("prefix+C isn't save and clear")
	}
	m, _, _ := foldDash(t)
	if _, ok := m.detailsKey("c"); ok {
		t.Error("the details panel took c")
	}
}
