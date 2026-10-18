package session

// Priority prompts (docs/SPEC.md §8.6, Priority prompts): tm thread prompt
// --now and --interrupt, for a thread that never goes idle. Claude Code
// (2.1.302) and Codex (0.162) both take a prompt typed mid-turn at the
// next tool boundary, in the running turn; the queue's routes (Claude's
// mod, `codex queue`) only run a prompt once the turn ends.

import (
	"fmt"
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

// PromptNow delivers text ahead of the queue. While the agent works and
// its prompt box is empty with no dialog on screen, the text is pasted
// into the running turn (ViaSteered), or, with interrupt, the turn is
// stopped for it the way the agent's manifest says ([inject] interrupt,
// interrupt_paste; ViaInterrupted). An agent without an interrupt key gets
// the prompt steered, and why says so. Otherwise (idle, blocked, a draft
// in the box, a dialog) it goes as Prompt sends it, and why says why.
// A queued prompt with the same text that no mod took yet is dropped, so
// a prompt sent again with --now doesn't run twice.
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
	case rt.observed:
		// No launch of ours: its keys aren't known.
		via, err = s.Prompt(text)
		return via, "the agent was started by hand", err
	case st != agent.StateWorking:
		via, err = s.Prompt(text)
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
	case rt.man != nil && rt.man.Inject.EmptyRule != "" && !rt.emptyBox:
		why = HeldBox
	}
	if why != "" {
		rt.mu.Unlock()
		via, err = s.Prompt(text)
		return via, why, err
	}
	kept := rt.prompts[:0]
	for _, p := range rt.prompts {
		if p.text != text || p.taken {
			kept = append(kept, p)
		}
	}
	if len(kept) != len(rt.prompts) {
		rt.heldSince, rt.heldWhy = time.Time{}, "" // the head may have changed
	}
	rt.prompts = kept
	rt.emptyBox = false // until the screen says so again
	key, after := "", false
	if interrupt && rt.man != nil {
		key, after = rt.man.Inject.Interrupt, rt.man.Inject.InterruptPaste != agent.InterruptPasteBefore
	}
	if key != "" && after {
		rt.promptSeq++
		id := fmt.Sprintf("%s-%d", rt.promptGen, rt.promptSeq)
		rt.prompts = append([]queuedPrompt{{id: id, text: text, at: time.Now(), paste: true}}, rt.prompts...)
	}
	rt.mu.Unlock()
	switch {
	case key == "":
		s.paste(text, "")
		if interrupt {
			return ViaSteered, "the agent has no interrupt key", nil
		}
		return ViaSteered, "", nil
	case after:
		s.Input([]byte(key))
	default:
		s.paste(text, key)
	}
	s.cfg.Logf("session %s: interrupted the agent's turn for a prompt", s.cfg.ID)
	return ViaInterrupted, "", nil
}
