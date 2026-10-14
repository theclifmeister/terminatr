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
