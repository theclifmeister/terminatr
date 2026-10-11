package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

type noticeFake struct {
	*fakeSource
	claimed, dismissed []string
}

func (f *noticeFake) ClaimNotice(id string) bool { f.claimed = append(f.claimed, id); return true }
func (f *noticeFake) DismissNotice(id string)    { f.dismissed = append(f.dismissed, id) }

func TestOSNotice(t *testing.T) {
	n := modnotify.Notice{Mod: "ping", Title: "T3; needs\x1b you", Text: "in review"}
	got := OSNotice("ghostty", n)
	if got != "\x1b]777;notify;terminatr · mod: ping;T3, needs you: in review\a" {
		t.Fatalf("osc 777 = %q", got)
	}
	if got := OSNotice("iTerm.app", n); !strings.HasPrefix(got, "\x1b]9;terminatr · mod: ping") || strings.Count(got, "\x1b") != 1 {
		t.Fatalf("osc 9 = %q", got)
	}
}

// drive feeds m the messages cmd makes, a batch's too.
func drive(m *dash, cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, drive(m, c)...)
		}
	case nil:
	case noticeBoardMsg:
		out = append(out, msg)
		_, c := m.Update(msg)
		drive(m, c)
	default:
		out = append(out, msg)
	}
	return out
}

// TestModNoticeButton: a new notification is claimed and drawn once, N
// lists it, a press right after a change is ignored, and a button opens
// the core's own confirmation for the task as the board has it.
func TestModNoticeButton(t *testing.T) {
	base, m := popupData(t)
	src := &noticeFake{fakeSource: base}
	m.src = src
	base.board.Tasks[1].Status = tasks.Review
	base.data.ModNotices = []modnotify.Notice{{ID: "n1", Mod: "ping", Title: "T2 is in review", Level: "warn",
		Buttons: []modnotify.Button{{ID: "task.accept", Label: "Accept", Project: "alpha", Task: 2}}}}
	cmd := func() tea.Cmd { d := base.Load(); return m.setData(d) }()
	var raw string
	for _, msg := range drive(m, cmd) {
		if r, ok := msg.(tea.RawMsg); ok {
			if s, _ := r.Msg.(string); strings.HasPrefix(s, "\x1b]") {
				raw = s
			}
		}
	}
	if !strings.Contains(raw, "mod: ping") || len(src.claimed) != 1 {
		t.Fatalf("raw %q claimed %v", raw, src.claimed)
	}
	m.setData(base.Load())
	if len(src.claimed) != 1 {
		t.Fatal("drawn twice")
	}

	keyPress(m, "N")
	nv, ok := m.top().(*noticesView)
	if !ok {
		t.Fatalf("N opened %T", m.top())
	}
	now := time.Now()
	nv.now, nv.stableAt = func() time.Time { return now }, now
	if out := screen(m); !strings.Contains(out, "mod: ping") || !strings.Contains(out, "1 Accept T2") {
		t.Fatalf("view:\n%s", out)
	}
	keyPress(m, "1")
	if m.top() != overlay(nv) || !strings.Contains(m.msg, "just changed") {
		t.Fatalf("press at once: %T %q", m.top(), m.msg)
	}
	now = now.Add(time.Second)
	drive(m, keyPress(m, "1"))
	cv, ok := m.top().(*confirmView)
	if !ok || cv.question != "Accept T2? The coordinator marks T2 Ship it done." {
		t.Fatalf("button opened %T %+v", m.top(), m.top())
	}
	if len(base.asked) != 0 || len(src.dismissed) != 1 {
		t.Fatalf("asked %v dismissed %v", base.asked, src.dismissed)
	}
}

// TestModNoticePrecondition: a button for a task no longer in review
// gets the core's refusal, not an ask.
func TestModNoticePrecondition(t *testing.T) {
	base, m := popupData(t)
	m.src = &noticeFake{fakeSource: base}
	drive(m, m.pressNotice(modnotify.Button{ID: "task.accept", Project: "alpha", Task: 2}))
	if _, ok := m.top().(*confirmView); ok || len(base.asked) != 0 {
		t.Fatalf("asked for a task not in review: %T", m.top())
	}
}
