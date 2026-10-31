package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
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
	if k := s.KeptPrompts(); len(k) != 1 || k[0].Text != "after-stop" {
		t.Fatalf("kept %+v: want PromptNow's prompt only", k)
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

// startSteerBox runs a program that draws Claude's prompt box holding the
// last line it was sent, the way Claude shows a steer until it reads it
// (an empty paste empties the box: the agent read it), and logs the keys
// it gets to a file it returns.
func startSteerBox(t *testing.T, a agent.Agent) (*Session, string) {
	t.Helper()
	return startSteerBoxWith(t, a, `\342\235\257`)
}

// startSteerBoxWith is startSteerBox with the box's pointer as printf
// writes it.
func startSteerBoxWith(t *testing.T, a agent.Agent, pointer string) (*Session, string) {
	t.Helper()
	return startKeysBox(t, a, `printf '\033[2J\033[H\n\342\224\200\342\224\200\342\224\200\n`+pointer+`\302\240%s\n\342\224\200\342\224\200\342\224\200\n' "$1"`)
}

// startHistoryBox is startSteerBox the way Codex takes a steer: the line
// goes above the box, which stays empty.
func startHistoryBox(t *testing.T, a agent.Agent) (*Session, string) {
	t.Helper()
	return startKeysBox(t, a, `printf '\033[2J\033[H%s\n\342\224\200\342\224\200\342\224\200\n\342\235\257\302\240\n\342\224\200\342\224\200\342\224\200\n' "$1"`)
}

// startKeysBox runs a program that draws the screen with draw (a shell
// command given the last line it was sent as $1) and logs the keys it
// gets to a file it returns.
func startKeysBox(t *testing.T, a agent.Agent, draw string) (*Session, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "keys")
	script := `stty -echo -icanon min 1
draw() { ` + draw + `; }
draw ''
tee "$KEYS" | while IFS= read -r l; do draw "$l"; done`
	s, err := Start(Config{
		ID: "s-test", Role: proto.RoleCoordinator, Project: "p",
		Argv: []string{"/bin/sh", "-c", script},
		Cwd:  t.TempDir(),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color", "LANG=C.UTF-8", "KEYS=" + log},
		Cols: 80, Rows: 24,
		Logf:  t.Logf,
		Agent: &AgentConfig{Agent: a, Home: t.TempDir(), PromptHold: time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	if _, err := s.Hook("SessionStart", map[string]any{"source": "startup"}); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "the prompt box", func() bool { return strings.Contains(screen(t, s), "❯") })
	return s, log
}

// shown waits for the box to show text and the screen rules to have read
// it as not empty.
func shown(t *testing.T, s *Session, text string) {
	t.Helper()
	testwait.Eventually(t, text+" in the box", func() bool {
		if !strings.Contains(screen(t, s), "❯ "+text) {
			return false
		}
		rt := s.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return !rt.emptyBox
	})
}

// escs counts the Esc keys in keys, beside paste markers.
func escs(keys string) int {
	return strings.Count(keys, "\x1b") - strings.Count(keys, "\x1b[")
}

func keysGot(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

// TestPromptNowOwnSteerNoDraft reproduces T641 (1): a steer tm pasted
// sits in Claude's box until the agent reads it; a later --now isn't
// held behind it as a draft, the same text isn't pasted twice, and
// nothing is queued.
func TestPromptNowOwnSteerNoDraft(t *testing.T) {
	s, keys := startSteerBox(t, claudeLike(t))
	working(t, s, true)
	if via, why, err := s.PromptNow("first", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("first: %q %q %v", via, why, err)
	}
	shown(t, s, "first")
	if via, why, err := s.PromptNow("second", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("second: %q %q %v", via, why, err)
	}
	shown(t, s, "second")
	if via, why, err := s.PromptNow("second", false); err != nil || via != ViaSteered || why != "already steered into this turn" {
		t.Fatalf("second again: %q %q %v", via, why, err)
	}
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("queued behind tm's own steer: %+v", st)
	}
	never(t, 200*time.Millisecond, "pasted twice", func() bool { return strings.Count(keysGot(t, keys), "second") > 1 })

	// The agent reads them: the box empties, and they are forgotten, so
	// the same text steers again.
	s.paste("", "")
	testwait.Eventually(t, "the steers forgotten", func() bool {
		rt := s.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return len(rt.steers) == 0
	})
	if via, why, err := s.PromptNow("second", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("second, read: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "pasted again", func() bool { return strings.Count(keysGot(t, keys), "second") == 2 })
}

// TestPromptNowDimPointer reproduces T653: mid-turn, Claude 2.1.306
// draws the box's ❯ dim. The box still reads empty, so --now steers (it
// was queued, "prompt box not empty"), and a second --now joins the
// first in the turn: nothing is queued.
func TestPromptNowDimPointer(t *testing.T) {
	s, keys := startSteerBoxWith(t, claudeLike(t), `\033[2m\342\235\257\033[22m`)
	working(t, s, true)
	if via, why, err := s.PromptNow("a", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("a: %q %q %v", via, why, err)
	}
	shown(t, s, "a")
	if via, why, err := s.PromptNow("b", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("b: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "b pasted", func() bool { return strings.Contains(keysGot(t, keys), "b") })
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("a steered prompt was queued too: %+v", st)
	}
}

// TestPromptNowSteersSeparated reproduces T668: Claude sends the steers
// in its box as one prompt, joined with nothing between them, so a steer
// pasted after another starts with a blank line.
func TestPromptNowSteersSeparated(t *testing.T) {
	s, keys := startSteerBox(t, claudeLike(t))
	working(t, s, true)
	if _, _, err := s.PromptNow("steer-a", false); err != nil {
		t.Fatal(err)
	}
	shown(t, s, "steer-a")
	if via, why, err := s.PromptNow("steer-b", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("steer-b: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "steer-b pasted", func() bool { return strings.Contains(keysGot(t, keys), "steer-b") })
	if k := keysGot(t, keys); !strings.Contains(k, "\x1b[200~steer-a\x1b[201~") || !strings.Contains(k, "\x1b[200~"+steerSep+"steer-b\x1b[201~") {
		t.Fatalf("keys: %q", k)
	}
}

// TestPromptNowSteersCodex: Codex takes a steer into its history at once,
// its box empty again, and sends each as a prompt of its own: no
// separator.
func TestPromptNowSteersCodex(t *testing.T) {
	a := claudeLike(t)
	a.m.Inject.InterruptPaste = agent.InterruptPasteAfter
	s, keys := startHistoryBox(t, a)
	working(t, s, true)
	if _, _, err := s.PromptNow("steer-a", false); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "steer-a taken, the box read empty", func() bool {
		if !strings.Contains(screen(t, s), "steer-a") {
			return false
		}
		rt := s.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return rt.emptyBox
	})
	if via, why, err := s.PromptNow("steer-b", false); err != nil || via != ViaSteered || why != "" {
		t.Fatalf("steer-b: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "steer-b pasted", func() bool { return strings.Contains(keysGot(t, keys), "steer-b") })
	if k := keysGot(t, keys); !strings.Contains(k, "\x1b[200~steer-b\x1b[201~") {
		t.Fatalf("keys: %q", k)
	}
}

// TestPromptNowDraftAfterSteer: T641 (3): keys typed into the box after
// tm's steer make it a draft again: kept, and the prompt queued.
func TestPromptNowDraftAfterSteer(t *testing.T) {
	s, keys := startSteerBox(t, claudeLike(t))
	working(t, s, true)
	if _, _, err := s.PromptNow("first", false); err != nil {
		t.Fatal(err)
	}
	shown(t, s, "first")
	if err := s.Input([]byte("mine\r")); err != nil {
		t.Fatal(err)
	}
	shown(t, s, "mine")
	via, why, err := s.PromptNow("later", false)
	if err != nil || via != "queued" || why != HeldBox {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
	never(t, 200*time.Millisecond, "pasted over a draft", func() bool { return strings.Contains(keysGot(t, keys), "later") })
}

// TestPromptInterruptOwnSteer: T641 (2): --interrupt stops the turn past
// tm's own steer. Claude sends what it queued once stopped: the new text
// is pasted before Esc, and text already steered gets Esc alone.
func TestPromptInterruptOwnSteer(t *testing.T) {
	defer func(d time.Duration) { interruptDelay = d }(interruptDelay)
	interruptDelay = 50 * time.Millisecond
	s, keys := startSteerBox(t, claudeLike(t))
	working(t, s, true)
	if _, _, err := s.PromptNow("first", false); err != nil {
		t.Fatal(err)
	}
	shown(t, s, "first")
	if via, why, err := s.PromptNow("stop-now", true); err != nil || via != ViaInterrupted || why != "" {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "the paste, then Esc", func() bool {
		k := keysGot(t, keys)
		i := strings.Index(k, "stop-now")
		return i >= 0 && escs(k[i:]) == 1
	})
	if via, _, err := s.PromptNow("stop-now", true); err != nil || via != ViaInterrupted {
		t.Fatalf("again: %q %v", via, err)
	}
	testwait.Eventually(t, "Esc alone", func() bool { return escs(keysGot(t, keys)) == 2 })
	never(t, 200*time.Millisecond, "pasted twice", func() bool { return strings.Count(keysGot(t, keys), "stop-now") > 1 })
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("queued: %+v", st)
	}
}

// TestPromptInterruptDraftRefused: T641 (2, 3): with a draft tm didn't
// type, --interrupt is refused, saying why; nothing is pasted or queued.
func TestPromptInterruptDraftRefused(t *testing.T) {
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, claudeLike(t), "draft", time.Hour, resolved)
	working(t, s, false)
	testwait.Eventually(t, "the screen read", func() bool {
		rt := s.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return !rt.emptyBox
	})
	via, _, err := s.PromptNow("stop-it", true)
	var ni *NotInterruptedError
	if !errors.As(err, &ni) || ni.Why != HeldBox || via != "" {
		t.Fatalf("PromptNow: %q %v", via, err)
	}
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("queued: %+v", st)
	}
	never(t, 200*time.Millisecond, "typed into the pane", func() bool {
		sc := screen(t, s)
		return strings.Contains(sc, "stop-it") || strings.Contains(sc, "^[")
	})
}

// TestPromptInterruptAfterOwnSteer: Codex's way past a steer tm pasted
// (Codex shows it in its history, the box empty, but a box that shows it
// is no draft either): Esc, and the prompt heads the queue.
func TestPromptInterruptAfterOwnSteer(t *testing.T) {
	a := claudeLike(t)
	a.m.Inject.InterruptPaste = agent.InterruptPasteAfter
	s, keys := startSteerBox(t, a)
	working(t, s, true)
	if via, _, err := s.PromptNow("first", false); err != nil || via != ViaSteered {
		t.Fatalf("first: %q %v", via, err)
	}
	shown(t, s, "first")
	if via, why, err := s.PromptNow("after-stop", true); err != nil || via != ViaInterrupted || why != "" {
		t.Fatalf("PromptNow: %q %q %v", via, why, err)
	}
	testwait.Eventually(t, "Esc", func() bool { return escs(keysGot(t, keys)) == 1 })
	if strings.Contains(keysGot(t, keys), "after-stop") {
		t.Fatal("pasted before the turn stopped")
	}
	if st, _ := s.AgentState(); st.Queued != 1 {
		t.Fatalf("queued: %+v", st)
	}
}

// TestStopTurn: with no prompt, the interrupt key alone stops the turn
// (the phone's Stop, T893); an idle agent, a draft in the box or an
// agent with no interrupt key is refused and nothing is typed.
func TestStopTurn(t *testing.T) {
	var ni *NotInterruptedError
	resolved := make(chan PromptResolution, 1)
	s := startBox(t, claudeLike(t), "", time.Hour, resolved)
	if err := s.StopTurn(); !errors.As(err, &ni) || ni.Why != "the agent is idle" {
		t.Fatalf("idle: %v", err)
	}
	working(t, s, true)
	if err := s.StopTurn(); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "Esc", func() bool { return strings.Contains(screen(t, s), "^[") })
	if st, _ := s.AgentState(); st.Queued != 0 {
		t.Fatalf("queued: %+v", st)
	}

	d := startBox(t, claudeLike(t), "draft", time.Hour, resolved)
	working(t, d, false)
	testwait.Eventually(t, "the screen read", func() bool {
		rt := d.agentRT()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return !rt.emptyBox
	})
	if err := d.StopTurn(); !errors.As(err, &ni) || ni.Why != HeldBox {
		t.Fatalf("draft: %v", err)
	}

	a := claudeLike(t)
	a.m.Inject.Interrupt, a.m.Inject.InterruptPaste = "", ""
	k := startBox(t, a, "", time.Hour, resolved)
	working(t, k, true)
	if err := k.StopTurn(); !errors.As(err, &ni) || ni.Why != "the agent has no interrupt key" {
		t.Fatalf("no key: %v", err)
	}
	never(t, 200*time.Millisecond, "typed into the pane", func() bool {
		return strings.Contains(screen(t, d), "^[") || strings.Contains(screen(t, k), "^[")
	})
}
