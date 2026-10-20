package server

// Which way a mod session's conversation got its context (docs/SPEC.md
// §7.8). The mod gives it as the `terminatr` context block, fetched by
// its prompt.context hook (GET /v1/context), and its classic.SessionStart
// hook marks each start before the command hooks run (POST
// /v1/context/started): Claude runs settings hooks as the innermost link
// of the same chain. Another mod seated before user plugins can skip both
// hooks (an organization's managed plugin that sends classic.* and
// prompt.context on past the user tier), and then the command hooks,
// which always run, give the context themselves: SessionStart when the
// mark is missing, UserPromptSubmit when a turn went by without the
// block. Whichever gives it first is the only one: once a command hook
// gave it, GET /v1/context answers 204, and once the block was served
// the command hooks give nothing.
//
// A conversation starts at the launch and again after SessionEnd (a
// /clear ends one) and PreCompact, the command hooks that come before
// the new one's context is read.

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// How a conversation got its context (convState.via).
const (
	viaNone  = ""
	viaBlock = "block" // the mod's context block
	viaHook  = "hook"  // a command hook's additionalContext
	viaKept  = "kept"  // a resume: what the conversation already carries
)

// skippedBy is who skipped the mod's hooks: Claude doesn't tell the mod
// it skipped, so not who.
const skippedBy = "another mod"

// hookContextLimit is the most additionalContext Claude keeps in the
// conversation (2.1.304: 10,000 characters); past it the text goes to a
// file and the model sees a preview. Counted in bytes, never fewer.
const hookContextLimit = 10_000

// convState is a mod session's current conversation, for its context.
type convState struct {
	via string
	// marked: the mod's classic.SessionStart ran for the start under way;
	// started: it ran for this conversation's start.
	marked, started bool
	// prompts counts the conversation's prompts without its context.
	prompts int
	// heard: the mod reported its state, so it runs; skipped: its hooks
	// were skipped, recorded once heard.
	heard, skipped, recorded bool
}

// convLocked is session id's conversation state; s.mu held.
func (s *Server) convLocked(id string) *convState {
	if s.convs == nil {
		s.convs = map[string]*convState{}
	}
	c := s.convs[id]
	if c == nil {
		c = &convState{}
		s.convs[id] = c
	}
	return c
}

// newConvLocked starts session id's state over at a launch; s.mu held.
func (s *Server) newConvLocked(id string) {
	delete(s.convs, id)
}

// convHook follows the command hooks that end a conversation (OnHook).
func (s *Server) convHook(sess *session.Session, event string, _ map[string]any) {
	if event == "SessionEnd" || event == "PreCompact" {
		s.endConv(sess.ID())
	}
}

// endConv ends session id's conversation: the next one has no context yet.
func (s *Server) endConv(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convLocked(id)
	c.via, c.marked, c.started, c.prompts = viaNone, false, false, 0
}

// markedStart records that the mod's classic.SessionStart ran (POST
// /v1/context/started).
func (s *Server) markedStart(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.convLocked(id).marked = true
}

// blockContext says whether the mod's prompt.context may add the
// context block (GET /v1/context): not when a command hook gave the
// context already.
func (s *Server) blockContext(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convLocked(id)
	if c.via == viaHook {
		return false
	}
	c.via = viaBlock
	return true
}

// modHeard records that session id's mod reports its state, and a skip
// seen before it did.
func (s *Server) modHeard(id string) {
	s.mu.Lock()
	c := s.convLocked(id)
	c.heard = true
	record := c.skipped && !c.recorded
	c.recorded = c.recorded || record
	s.mu.Unlock()
	if record {
		s.hooksSkipped(id)
	}
}

// sawSkip records that session id's mod hooks were skipped; it is told
// once the mod is known to run (a mod that isn't loaded is no skip).
func (s *Server) sawSkip(id, hook string) {
	s.mu.Lock()
	c := s.convLocked(id)
	first := !c.skipped
	c.skipped = true
	record := c.heard && !c.recorded
	c.recorded = c.recorded || record
	s.mu.Unlock()
	if first {
		s.log.Printf("session %s: the mod's %s hook didn't run; tm's command hooks give the context", id, hook)
	}
	if record {
		s.hooksSkipped(id)
	}
}

// startContext is a mod session's SessionStart answer: a line saying
// where the context is when the mod's hook ran, else the context itself.
func (s *Server) startContext(id string, payload map[string]any, full func() ([]byte, error)) ([]byte, error) {
	src, _ := payload["source"].(string)
	resume := src == "resume" || src == "fork"
	s.mu.Lock()
	c := s.convLocked(id)
	marked, via := c.marked, c.via
	c.marked = false
	if marked {
		c.started = true
		if resume && via == viaNone {
			c.via = viaKept
		}
	}
	s.mu.Unlock()
	b, err := full()
	if err != nil || len(b) == 0 {
		return b, err
	}
	switch {
	case marked && resume:
		if old, stale := s.staleContext(id); stale {
			return append([]byte(replacedContext(old)), b...), nil
		}
		return []byte(modContextLine), nil
	case marked, via == viaBlock && !resume:
		return []byte(modContextLine), nil
	}
	s.mu.Lock()
	s.convLocked(id).via = viaHook
	s.mu.Unlock()
	s.sawSkip(id, "classic.SessionStart")
	return s.fallbackContext(id, b), nil
}

// promptContext is a mod session's UserPromptSubmit answer: nothing
// while the conversation has its context or the mod's block is due (its
// first prompt after a SessionStart the mod saw: Claude may read the
// blocks after this hook), else the context.
func (s *Server) promptContext(id string, full func() ([]byte, error)) ([]byte, error) {
	s.mu.Lock()
	c := s.convLocked(id)
	if c.via != viaNone {
		s.mu.Unlock()
		return nil, nil
	}
	if c.started && c.prompts == 0 {
		c.prompts++
		s.mu.Unlock()
		return nil, nil
	}
	skipped := c.started
	s.mu.Unlock()
	b, err := full()
	if err != nil || len(b) == 0 {
		return b, err
	}
	s.mu.Lock()
	s.convLocked(id).via = viaHook
	s.mu.Unlock()
	if skipped {
		s.sawSkip(id, "prompt.context")
	}
	return s.fallbackContext(id, b), nil
}

// fallbackContext is context b as a command hook gives it, headed by
// why; when it is past hookContextLimit, the commands that print it.
func (s *Server) fallbackContext(id string, b []byte) []byte {
	const head = "terminatr: another Claude Code mod skipped terminatr's own, so this conversation has no `terminatr` context block. "
	if full := head + "Your role's rules and your task's state, in its place:\n\n" + string(b); len(full) <= hookContextLimit {
		return []byte(full)
	}
	s.mu.Lock()
	r := s.records[id]
	s.mu.Unlock()
	if r.Role == proto.RoleCoordinator {
		return []byte(head + "Run `tm skill coordinator` now for your rules, then `tm context` for the project's state.")
	}
	out := head + "Run `tm skill thread` now for your rules"
	if r.Brief != "" {
		out += ", then read your brief again: " + r.Brief
	}
	return []byte(out + "; then continue with your task's unchecked steps.")
}

// hooksSkipped shows that session id's mod hooks were skipped: on its
// row (session.list, tm doctor) and, once per project while the server
// runs, in the coordinator's inbox.
func (s *Server) hooksSkipped(id string) {
	s.mu.Lock()
	sess, r := s.sessions[id], s.records[id]
	tell := r.Project != "" && !s.skipTold[r.Project]
	if tell {
		if s.skipTold == nil {
			s.skipTold = map[string]bool{}
		}
		s.skipTold[r.Project] = true
	}
	s.mu.Unlock()
	if sess != nil {
		sess.SetHooksSkipped(skippedBy)
	}
	if !tell {
		return
	}
	p, err := project.Open(r.Project)
	if err != nil {
		return
	}
	ref := r.ID
	who := caller.Caller{Kind: caller.Coordinator, Project: r.Project}
	if r.Role == proto.RoleThread && r.Thread != "" {
		ref = r.Thread
		who = caller.Caller{Kind: caller.Thread, Project: r.Project, Thread: r.Thread}
	}
	if err := p.Journal(who, "hooks.skipped", ref, "terminatr hooks skipped by "+skippedBy); err != nil {
		s.log.Printf("session %s: journal: %v", id, err)
	}
	summary := fmt.Sprintf("%s: terminatr hooks skipped by %s (likely a plugin the organization manages, seated before user plugins). "+
		"tm gives the role's rules and state through its command hooks instead, so nothing is lost; "+
		"other Claude sessions on this machine are likely the same. tm doctor lists them.", ref, skippedBy)
	if _, err := p.AddItem("hooks-skipped", ref, strings.TrimSpace(summary), false); err != nil {
		s.log.Printf("session %s: inbox: %v", id, err)
	}
}
