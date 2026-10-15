package tui

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestAdoptSession: T on an agent session outside the projects asks the
// current project's coordinator to adopt it (docs/SPEC.md §4, Adopt);
// on a shell or a project's session it only says why.
func TestAdoptSession(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)

	m.sel = "s:s-3" // a plain shell
	press(m, "T")
	if !strings.Contains(m.msg, "no agent runs in s-3") || len(src.adopted) != 0 {
		t.Fatalf("T on a shell: %q %v", m.msg, src.adopted)
	}
	m.sel = "p:beta"
	press(m, "T")
	if !strings.Contains(m.msg, "select one") {
		t.Fatalf("T on a project: %q", m.msg)
	}

	m.sel = "s:s-4"
	if keys := m.footKeys(); !strings.Contains(keys, "T adopt") {
		t.Errorf("footer lacks T adopt: %q", keys)
	}
	if items := m.rowItems(row{key: "s:s-4", session: "s-4"}); items[len(items)-1].label != "adopt as a thread" {
		t.Errorf("row menu: %+v", items)
	}
	press(m, "T")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Adopt s-4" || !strings.HasPrefix(cv.question, "Adopt s-4 as a thread of beta?") || !strings.HasSuffix(cv.question, "claude in /y") {
		t.Fatalf("T opened %T %+v", m.top(), cv)
	}
	press(m, "n")
	if m.msg != "s-4 not adopted" || len(src.adopted) != 0 {
		t.Fatalf("n: %q %v", m.msg, src.adopted)
	}
	press(m, "T")
	drain(t, m, press(m, "y"))
	if len(src.adopted) != 1 || src.adopted[0] != "beta s-4" || m.msg != "asked the coordinator to adopt s-4" {
		t.Fatalf("y: %q %v", m.msg, src.adopted)
	}
}

// TestAdoptWithdraw (T335): T on a session the user already asked to
// adopt offers to take the go-ahead back.
func TestAdoptWithdraw(t *testing.T) {
	src := &fakeSource{data: testData()}
	for i := range src.data.Projects {
		if src.data.Projects[i].Slug == "beta" {
			src.data.Projects[i].Items = []project.Item{{ID: "x", Kind: project.KindAdopt, Subject: "s-4"}}
		}
	}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.sel = "s:s-4"
	press(m, "T")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Withdraw s-4" || !strings.HasPrefix(cv.question, "Withdraw your go-ahead to adopt s-4 as a thread of beta?") {
		t.Fatalf("T opened %T %+v", m.top(), cv)
	}
	press(m, "n")
	if m.msg != "s-4 still asked" || len(src.withdrawn) != 0 {
		t.Fatalf("n: %q %v", m.msg, src.withdrawn)
	}
	press(m, "T")
	drain(t, m, press(m, "y"))
	if len(src.withdrawn) != 1 || src.withdrawn[0] != "beta s-4" || len(src.adopted) != 0 || m.msg != "withdrew the go-ahead to adopt s-4" {
		t.Fatalf("y: %q %v %v", m.msg, src.withdrawn, src.adopted)
	}
}
