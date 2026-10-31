package session

// Priority prompts (docs/SPEC.md §8.6, Priority prompts): tm thread prompt
// --now and --interrupt, for a thread that never goes idle. Claude Code
// (2.1.302) and Codex (0.162) both take a prompt typed mid-turn at the
// next tool boundary, in the running turn; the queue's routes (the paste
// injector, `codex queue`) only run a prompt once the turn ends.

import (
	"fmt"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// What PromptNow did with a prompt, beside "channel" and "queued".
const (
	// ViaSteered: pasted into the running turn; the agent reads it at its
	// next tool boundary.
	ViaSteered = "steered"
	// ViaInterrupted: the turn was stopped for it. Pasted before the
	// interrupt key, the agent sends it as its next turn; else it heads
	// the queue, pasted once the agent is idle.
	ViaInterrupted = "interrupted"
)

// interruptDelay spaces the interrupt key from the Enter that queued the
// prompt before it, so the agent has queued it first. A variable for
// tests.
var interruptDelay = time.Second

// steerSep goes before a steer pasted after tm's unread steers in the box.
const steerSep = "\n\n"

// NotInterruptedError says PromptNow with interrupt left the running turn
// alone, and neither sent nor queued the prompt. Why is HeldBox (a draft
// tm didn't type), HeldDialog or HeldEscape, or that the agent was
// started by hand.
type NotInterruptedError struct{ Why string }

func (e *NotInterruptedError) Error() string {
	return "the running turn was not interrupted (" + e.Why + "): nothing was sent or queued"
}

// PromptNow delivers text ahead of the queue. While the agent works with
// no dialog on screen and nothing in its prompt box but the steers tm
// pasted itself, the text is pasted into the running turn (ViaSteered),
// or, with interrupt, the turn is stopped for it the way the agent's
// manifest says ([inject] interrupt, interrupt_paste; ViaInterrupted).
// Text already steered into this turn isn't pasted again. An agent
// without an interrupt key gets the prompt steered, and why says so. An
// idle agent gets it as Prompt sends it, and why says so. Otherwise (a
// draft in the box, a dialog, an agent started by hand) a steer goes as
// Prompt sends it, and why says why, while an interrupt is refused
// (NotInterruptedError): the turn runs on, and nothing is queued.
// A queued prompt with the same text is dropped, so
// a prompt sent again with --now doesn't run twice. Its prompts are a
// client's: one it queues is kept (PromptOptions.Keep).
func (s *Session) PromptNow(text string, interrupt bool) (via, why string, err error) {
	rt := s.agentRT()
	if rt == nil {
		return "", "", ErrNoAgent
	}
	st := rt.tr.State().State
	switch {
	case st == agent.StateExited:
		return "", "", ErrExited
	case rt.a.Injector() == agent.InjectNone:
		return "", "", fmt.Errorf("agent %s takes no prompts", rt.a.Name())
	case rt.observed && interrupt && st != agent.StateIdle:
		// No launch of ours: its keys aren't known.
		return "", "", &NotInterruptedError{Why: "the agent was started by hand"}
	case rt.observed:
		via, err = s.PromptWith(text, PromptOptions{Keep: true})
		return via, "the agent was started by hand", err
	case interrupt && st == agent.StateBlocked:
		return "", "", &NotInterruptedError{Why: HeldDialog}
	case st != agent.StateWorking:
		via, err = s.PromptWith(text, PromptOptions{Keep: true})
		return via, "the agent is " + string(st), err
	}
	escape := rt.escapeOpen()
	rt.mu.Lock()
	switch {
	case escape:
		// Its prompt may open at any moment: nothing is pasted now.
		why = HeldEscape
	case rt.blocker:
		why = HeldDialog
	case rt.man != nil && rt.man.Inject.EmptyRule != "" && !rt.emptyBox && len(rt.steers) == 0:
		why = HeldBox // a draft: kept, never typed over
	}
	if why != "" {
		rt.mu.Unlock()
		if interrupt {
			return "", "", &NotInterruptedError{Why: why}
		}
		via, err = s.PromptWith(text, PromptOptions{Keep: true})
		return via, why, err
	}
	kept := rt.prompts[:0]
	for _, p := range rt.prompts {
		if p.text != text {
			kept = append(kept, p)
		}
	}
	if len(kept) != len(rt.prompts) {
		rt.heldSince, rt.heldWhy = time.Time{}, "" // the head may have changed
	}
	rt.prompts = kept
	steered := slices.Contains(rt.steers, text)
	// Claude holds unread steers in its box and sends them as one prompt
	// with no separator (T668): a blank line keeps each its own message.
	// Codex took them at once, its box empty again: nothing to join.
	paste := text
	if len(rt.steers) > 0 && !rt.emptyBox {
		paste = steerSep + text
	}
	key, after := "", false
	if interrupt && rt.man != nil {
		key, after = rt.man.Inject.Interrupt, rt.man.Inject.InterruptPaste != agent.InterruptPasteBefore
	}
	switch {
	case key != "" && after:
		// The turn's unread steers are left unanswered.
		rt.steers, rt.steerShown = nil, false
		rt.promptSeq++
		id := fmt.Sprintf("%s-%d", rt.promptGen, rt.promptSeq)
		rt.prompts = append([]queuedPrompt{{id: id, text: text, at: time.Now(), keep: true}}, rt.prompts...)
	case !steered:
		rt.steers = append(rt.steers, text)
		rt.emptyBox = false // until the screen says so again
	}
	rt.mu.Unlock()
	switch {
	case key == "" && steered:
		why = "already steered into this turn"
		if interrupt {
			why += "; the agent has no interrupt key"
		}
		return ViaSteered, why, nil
	case key == "":
		s.paste(paste, "")
		if interrupt {
			return ViaSteered, "the agent has no interrupt key", nil
		}
		return ViaSteered, "", nil
	case after, steered:
		// Claude sends what it queued, the steer among it, once stopped.
		s.input([]byte(key))
	default:
		s.paste(paste, key)
	}
	s.cfg.Logf("session %s: interrupted the agent's turn for a prompt", s.cfg.ID)
	return ViaInterrupted, "", nil
}

// forgetSteers: keys were typed into the prompt box, so what it holds
// may be a draft.
func (s *Session) forgetSteers() {
	rt := s.agentRT()
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.steers, rt.steerShown = nil, false
	rt.mu.Unlock()
}
