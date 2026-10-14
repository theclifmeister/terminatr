package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// BenchmarkSidebarHover is one mouse motion over the sidebar and the
// frame that follows it (PERF-03).
func BenchmarkSidebarHover(b *testing.B) {
	t := &testing.T{}
	m, src := sideDash(t)
	src.data.Server = ServerInfo{Version: "1.2.3"}
	m.setData(src.data)
	end := m.sideW() - 1 - sideGap
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Update(tea.MouseMotionMsg{X: end - 6 - i%2, Y: 5})
		m.View()
	}
}

// BenchmarkIdleFrame is one frame with nothing changed.
func BenchmarkIdleFrame(b *testing.B) {
	t := &testing.T{}
	m, _ := sideDash(t)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.View()
	}
}

// TestSettingsRenderReadsAgentsOnce: drawing the settings again and again
// lists the registry once, until a poll (PERF-04).
func TestSettingsRenderReadsAgentsOnce(t *testing.T) {
	m, src := sideDash(t)
	keyPress(m, ",")
	keyPress(m, "2")
	n := src.agentCalls
	for range 5 {
		m.View()
	}
	if got := src.agentCalls - n; got > 1 {
		t.Fatalf("5 frames read the agents %d times", got)
	}
	m.setData(src.data)
	n = src.agentCalls
	m.View()
	m.View()
	if got := src.agentCalls - n; got != 1 {
		t.Fatalf("after a poll: %d reads, want 1", got)
	}
}

// TestHoverFrameReuse: a motion that changes no tooltip shows the same
// frame; anything else draws anew (PERF-03).
func TestHoverFrameReuse(t *testing.T) {
	m, _ := sideDash(t)
	end := m.sideW() - 1 - sideGap
	m.Update(tea.MouseMotionMsg{X: end - 6, Y: 5})
	a := m.View().Content
	m.Update(tea.MouseMotionMsg{X: end - 7, Y: 5})
	if !m.hold || m.View().Content != a {
		t.Fatal("a motion with no new tooltip redrew")
	}
	keyPress(m, "down")
	if m.hold || m.View().Content == a {
		t.Fatal("a key after the motion showed the old frame")
	}
	m.Update(tea.MouseMotionMsg{X: end - 2, Y: 5}) // a glyph: a tooltip
	if m.hold || sideHoverTip() == "" {
		t.Fatal("a new tooltip didn't redraw")
	}
}
