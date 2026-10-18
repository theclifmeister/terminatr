package session

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// working starts a turn (UserPromptSubmit) and waits for it, and, with
// empty, for the screen rules to have read the empty box.
func working(t *testing.T, s *Session, empty bool) {
	t.Helper()
	if _, err := s.Hook("UserPromptSubmit", map[string]any{"prompt": "go"}); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "working", func() bool {
		st, _ := s.AgentState()
		return st.State == agent.StateWorking
	})
	if !empty {
		return
	}
	testwait.Eventually(t, "the empty box read", func() bool {
		rt := s.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return rt.emptyBox
	})
}

// TestPromptNowSteers: while the agent works with an empty box, --now
// pastes at once, and a queued copy of the same text is dropped.
func TestPromptNowSteers(t *testing.T) {
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, claudeLike(t), "", time.Hour, resolved)
	working(t, s, true)
	if _, err := s.Prompt("steer-me"); err != nil {
		t.Fatal(err)
	}
	via, why, err := s.PromptNow("steer-me", false)
	if err != nil || via != ViaSteered || why != "" {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "the paste", func() bool { return strings.Contains(screen(t, s), "steer-me") })
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("the queued copy wasn't dropped: %+v", st)
	}
}

// TestPromptNowDraftQueues: a draft in the box is never typed over; the
// prompt is queued and why says so.
func TestPromptNowDraftQueues(t *testing.T) {
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, claudeLike(t), "draft", time.Hour, resolved)
	working(t, s, false)
	testwait.Eventually(t, "the screen read", func() bool {
		via, why, err := s.PromptNow("not-now", false)
		if err != nil {
			t.Fatal(err)
		}
		if via == ViaSteered {
			t.Fatal("pasted over a draft")
		}
		return via == "queued" && why == HeldBox
	})
	never(t, 300*time.Millisecond, "pasted over a draft", func() bool { return strings.Contains(screen(t, s), "not-now") })
}

// TestPromptNowIdle: an idle agent gets the prompt the usual way.
func TestPromptNowIdle(t *testing.T) {
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, claudeLike(t), "", time.Hour, resolved)
	via, why, err := s.PromptNow("idle-one", true)
	if err != nil || via != "queued" || why != "the agent is idle" {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "the paste", func() bool { return strings.Contains(screen(t, s), "idle-one") })
}

// TestPromptInterruptBefore: Claude's way: the prompt is pasted, then the
// interrupt key (the program echoes Esc as ^[).
func TestPromptInterruptBefore(t *testing.T) {
	defer func(d time.Duration) { interruptDelay = d }(interruptDelay)
	interruptDelay = 50 * time.Millisecond
	a := claudeLike(t)
	if a.m.Inject.Interrupt != "\x1b" || a.m.Inject.InterruptPaste != agent.InterruptPasteBefore {
		t.Fatalf("claude's manifest: interrupt %q, paste %q", a.m.Inject.Interrupt, a.m.Inject.InterruptPaste)
	}
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, a, "", time.Hour, resolved)
	working(t, s, true)
	via, _, err := s.PromptNow("stop-and-do", true)
	if err != nil || via != ViaInterrupted {
		t.Fatalf("PromptNow: %q %v", via, err)
	}
	testwait.Eventually(t, "the paste, then Esc", func() bool {
		sc := screen(t, s)
		i := strings.Index(sc, "stop-and-do")
		return i >= 0 && strings.Contains(sc[i:], "^[")
	})
}

// TestPromptInterruptAfter: Codex's way: the key first; the prompt heads
// the queue and is pasted once the agent is idle.
func TestPromptInterruptAfter(t *testing.T) {
	a := claudeLike(t)
	a.m.Inject.InterruptPaste = agent.InterruptPasteAfter
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, a, "", time.Hour, resolved)
	working(t, s, true)
	if _, err := s.Prompt("older"); err != nil {
		t.Fatal(err)
	}
	via, _, err := s.PromptNow("after-stop", true)
	if err != nil || via != ViaInterrupted {
		t.Fatalf("PromptNow: %q %v", via, err)
	}
	testwait.Eventually(t, "Esc", func() bool { return strings.Contains(screen(t, s), "^[") })
	if strings.Contains(screen(t, s), "after-stop") {
		t.Fatal("pasted before the turn stopped")
	}
	if st, _ := s.AgentState(); st.Queued != 2 {
		t.Fatalf("queued: %+v", st)
	}
	if _, err := s.Hook("Stop", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "the paste once idle", func() bool { return strings.Contains(screen(t, s), "after-stop") })
	if sc := screen(t, s); strings.Contains(sc, "older") && strings.Index(sc, "older") < strings.Index(sc, "after-stop") {
		t.Fatal("the older prompt went first")
	}
}

// TestPromptInterruptNoKey: an agent without an interrupt key gets the
// prompt steered, and why says so.
func TestPromptInterruptNoKey(t *testing.T) {
	a := claudeLike(t)
	a.m.Inject.Interrupt, a.m.Inject.InterruptPaste = "", ""
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, a, "", time.Hour, resolved)
	working(t, s, true)
	via, why, err := s.PromptNow("no-key", true)
	if err != nil || via != ViaSteered || why != "the agent has no interrupt key" {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
}
