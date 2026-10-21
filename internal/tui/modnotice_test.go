package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/proto"
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
	if got != "\x1b]777;notify;terminatr · ping;T3, needs you: in review\a" {
		t.Fatalf("osc 777 = %q", got)
	}
	if got := OSNotice("iTerm.app", n); !strings.HasPrefix(got, "\x1b]9;terminatr · ping") || strings.Count(got, "\x1b") != 1 {
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
	case bindingMsg:
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
		Buttons: []modnotify.Button{{ID: "task.accept", Label: "accept", Project: "alpha", Task: 2}}}}
	cmd := func() tea.Cmd { d := base.Load(); return m.setData(d) }()
	var raw string
	for _, msg := range drive(m, cmd) {
		if r, ok := msg.(tea.RawMsg); ok {
			if s, _ := r.Msg.(string); strings.HasPrefix(s, "\x1b]") {
				raw = s
			}
		}
	}
	if !strings.Contains(raw, "terminatr · ping") || len(src.claimed) != 1 {
		t.Fatalf("raw %q claimed %v", raw, src.claimed)
	}
	m.setData(base.Load())
	if len(src.claimed) != 1 {
		t.Fatal("drawn twice")
	}

	keyPress(m, "M")
	nv, ok := m.top().(*noticesView)
	if !ok {
		t.Fatalf("M opened %T", m.top())
	}
	now := time.Now()
	nv.now, nv.stableAt = func() time.Time { return now }, now
	if out := screen(m); !strings.Contains(out, "ping") || !strings.Contains(out, "1 accept T2") {
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
	drive(m, m.pressNotice("ping", modnotify.Button{ID: "task.accept", Project: "alpha", Task: 2}))
	if _, ok := m.top().(*confirmView); ok || len(base.asked) != 0 {
		t.Fatalf("asked for a task not in review: %T", m.top())
	}
}

// TestModNoticeAnswer: an answer button shows the option it answers
// with, and its press opens tm's own confirmation of that answer to the
// live question; only y sends it.
func TestModNoticeAnswer(t *testing.T) {
	base, m := popupData(t)
	src := &noticeFake{fakeSource: base}
	m.src = src
	for i := range base.data.Sessions {
		if base.data.Sessions[i].ID == "s-9" {
			base.data.Sessions[i].Question = &proto.Question{Questions: []proto.QuestionItem{
				{Question: "Ship it?", Options: []proto.QuestionOption{{Label: "Yes"}, {Label: "No"}}},
			}}
		}
	}
	b := modnotify.Button{ID: "question.answer", Label: "answer", Session: "s-9", Question: "Ship it?", Option: "Yes"}
	base.data.ModNotices = []modnotify.Notice{{ID: "n1", Mod: "ping", Title: "t-0003 asks", Buttons: []modnotify.Button{b}}}
	drive(m, m.setData(base.Load()))
	keyPress(m, "M")
	if out := screen(m); !strings.Contains(out, `1 answer "Yes"`) {
		t.Fatalf("view:\n%s", out)
	}
	m.pop()
	drive(m, m.pressNotice("ping", b))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.HasPrefix(cv.question, `Answer alpha t-0003's question "Ship it?" with "Yes"?`) || len(base.answers) != 0 {
		t.Fatalf("button opened %T %+v, answers %v", m.top(), m.top(), base.answers)
	}
	drive(m, keyPress(m, "Y"))
	if len(base.answers) != 1 || base.answers[0] != "s-9 0 Ship it? → Yes" {
		t.Fatalf("answers %v", base.answers)
	}
}
