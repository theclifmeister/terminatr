package server

// The action binding (the mods design §11; docs/SPEC.md §5.3, Action
// binding): $.actions.list and $.actions.available for a mod, and
// action.done once a human ran an action a mod's row bound. The core's
// table and its precondition check are internal/modaction's; the
// console runs the action itself, as the human, from the human's key.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// modActionMethods adds actions.list and actions.available to b. Neither
// needs a grant to call, but available reads live state: it needs
// project_read and a target in a project the mod runs for
// (modActionScope), so a mod can't learn a task's status, or which
// sessions run, by probing.
func (s *Server) modActionMethods(b *modbroker.Broker) {
	b.Handle("actions.list", "", func(context.Context, *modbroker.Call) (any, error) {
		out := make([]modapi.Action, len(modaction.Actions))
		for i, a := range modaction.Actions {
			out[i] = modapi.Action{ID: a.ID, Label: a.Label, Key: a.Key, On: a.On, Confirm: a.Confirm, Precondition: a.Precondition, Consent: a.Consent}
		}
		return out, nil
	})
	b.Handle("actions.available", "", func(_ context.Context, c *modbroker.Call) (any, error) {
		var a struct {
			ID     string           `json:"id"`
			Target modaction.Target `json:"target"`
		}
		if err := c.Decode(&a); err != nil {
			return nil, err
		}
		spec, ok := modaction.Lookup(a.ID)
		if !ok {
			return false, nil
		}
		if why := modaction.TargetWhy(spec, a.Target); why != "" {
			return nil, modbroker.BadParams("actions.available: %s", why)
		}
		if err := s.modActionScope(c.Mod, spec, a.Target); err != nil {
			if onSession(spec) && !modbroker.IsDenied(err) {
				// A session that isn't running, or not in a project the
				// mod runs for, answers alike: no probing for sessions.
				return false, nil
			}
			return nil, err
		}
		return modaction.Why(a.ID, s.actionState(), a.Target) == "", nil
	})
}

// onSession says whether a's target names a session (session.attach,
// session.clear, question.answer).
func onSession(a modaction.Action) bool {
	return a.On == modaction.OnSession || a.On == modaction.OnQuestion
}

// modActionScope refuses a binding of action a on target t outside mod
// m's reach: it needs project_read, an action that needs a grant of its
// own (modaction.Action.Consent) needs m to list it under [permissions]
// actions with the actions grant (mod.Mod.Binds), and the target's
// project (a session's own project) must be one m runs for. A session
// outside the projects, or not running, is not found. The console asks it before it
// runs a pressed binding (mod.action.check), and action.done and
// $.actions.available check it, so a mod enabled for one project can't
// have the human act on, or attach to, another.
func (s *Server) modActionScope(m mod.Mod, a modaction.Action, t modaction.Target) error {
	if !m.Granted(mod.PermProjectRead) {
		return modbroker.Denied("binding %s needs the %s permission, which %s doesn't have", a.ID, mod.PermProjectRead, m.ID)
	}
	if !m.Binds(a) {
		return modbroker.Denied("binding %s needs it in [permissions] actions, with the %s grant, which %s doesn't have", a.ID, mod.PermActions, m.ID)
	}
	slug := t.Project
	if onSession(a) {
		sess, perr := s.session(t.Session)
		if perr != nil {
			return modbroker.NotFound("no session %s", t.Session)
		}
		if slug = sess.Info().Project; slug == "" {
			return modbroker.NotFound("session %s is not in a project", t.Session)
		}
	}
	if !m.Runs(slug) {
		return modbroker.Denied("%s doesn't run for project %s", m.ID, slug)
	}
	return nil
}

// actionState is the server's live state for a precondition: the
// project's files as they are now, read once per check.
func (s *Server) actionState() modaction.State {
	return &liveActions{s: s, boards: map[string]*tasks.Board{}}
}

type liveActions struct {
	s      *Server
	boards map[string]*tasks.Board
}

func (l *liveActions) Project(slug string) bool {
	_, err := project.Open(slug)
	return err == nil
}

func (l *liveActions) Task(slug, ref string) *tasks.Task {
	b, ok := l.boards[slug]
	if !ok {
		if p, err := project.Open(slug); err == nil {
			b, _ = p.Tasks().Load()
		}
		l.boards[slug] = b
	}
	if b == nil {
		return nil
	}
	for _, t := range b.Tasks {
		if t.Ref() == ref {
			return t
		}
	}
	return nil
}

func (l *liveActions) Asked(slug, ref string) string {
	p, err := project.Open(slug)
	if err != nil {
		return ""
	}
	items, _ := p.Inbox()
	return project.TaskAsked(items, ref)
}

func (l *liveActions) Menu(id string) []modaction.MenuQuestion {
	return menuOf(l.s.asks.of(id))
}

// menuOf is question menu q as a precondition sees it.
func menuOf(q *proto.Question) []modaction.MenuQuestion {
	if q == nil {
		return nil
	}
	out := make([]modaction.MenuQuestion, len(q.Questions))
	for i, it := range q.Questions {
		out[i] = modaction.MenuQuestion{Question: it.Question, Multi: it.MultiSelect, Answered: it.Answered}
		for _, o := range it.Options {
			out[i].Options = append(out[i].Options, o.Label)
		}
	}
	return out
}

func (l *liveActions) Session(id string) bool {
	sess, perr := l.s.session(id)
	if perr != nil {
		return false
	}
	select {
	case <-sess.Done():
		return false
	default:
		return true
	}
}

// modBinding checks, for a console, that enabled mod id may bind action
// on target: the action is bindable, the target of its shape and in the
// mod's reach (modActionScope). Human-only: only a human's console asks.
func (s *Server) modBinding(id, action string, target map[string]string, who caller.Caller) (modaction.Target, *proto.Error) {
	t := modaction.TargetOf(target)
	if who.IsAgent() {
		return t, proto.Errorf("human-only", "only a human's console presses a mod's binding")
	}
	m, ok := s.modNamed(id)
	if !ok || m.State != mod.Enabled {
		return t, proto.Errorf(proto.ErrBadParams, "no enabled mod %q", id)
	}
	spec, ok := modaction.Lookup(action)
	if !ok {
		return t, proto.Errorf(proto.ErrBadParams, "%q is not an action a mod may bind", action)
	}
	if why := modaction.TargetWhy(spec, t); why != "" {
		return t, proto.Errorf(proto.ErrBadParams, "%s", why)
	}
	if err := s.modActionScope(m, spec, t); err != nil {
		return t, proto.Errorf("refused", "%s", err.Error())
	}
	return t, nil
}

// modActionCheck is mod.action.check: whether a pressed binding is in
// its mod's reach, asked before the console runs it. In reach, it
// answers a press token for that console and binding, which the
// console's mod.action.done must present.
func (s *Server) modActionCheck(p proto.ModActionParams, peerPID int, who caller.Caller) (any, *proto.Error) {
	t, perr := s.modBinding(p.Mod, p.Action, p.Target, who)
	if perr != nil {
		return nil, perr
	}
	return proto.ModActionCheckResult{Token: s.presses.issue(peerPID, pressKey(p.Mod, p.Action, t), time.Now())}, nil
}

// modActionDone is mod.action.done from a console: the human ran (ok)
// or was refused an action mod p.Mod's row bound. Human-only: an agent
// can't make a mod believe the human acted. The binding must be in the
// mod's reach (modBinding), and p.Token the press token the same
// console's mod.action.check gave for it, used once: so a done follows
// a check for that very press, and one check makes at most one done.
// The event goes to that mod alone.
func (s *Server) modActionDone(p proto.ModActionDoneParams, peerPID int, who caller.Caller) (any, *proto.Error) {
	t, perr := s.modBinding(p.Mod, p.Action, p.Target, who)
	if perr != nil {
		return nil, perr
	}
	if !s.presses.take(p.Token, peerPID, pressKey(p.Mod, p.Action, t), time.Now()) {
		return nil, proto.Errorf("refused", "no press of %s's %s binding to report: mod.action.check gives the token", p.Mod, p.Action)
	}
	if s.modEv != nil {
		s.modEv.bus.PublishTo(p.Mod, modbus.ActionDone, t.Project, modbus.Done{Action: p.Action, Target: modbus.Target(t), OK: p.OK})
	}
	return struct{}{}, nil
}

// maxPresses is how many press tokens one console may hold at once; a
// new one beyond it drops its oldest.
const maxPresses = 32

// pressTokens are the press tokens mod.action.check gave out and
// mod.action.done hasn't used: each good once, for one binding, from the
// console (peer pid) that asked, for proto.ModActionTokenTTL. The zero
// value is ready.
type pressTokens struct {
	mu     sync.Mutex
	tokens map[string]press
}

type press struct {
	pid  int
	key  string
	made time.Time
}

// pressKey names a binding: mod, action and the target's fields.
func pressKey(mod, action string, t modaction.Target) string {
	return strings.Join([]string{mod, action, t.Project, t.Task, t.Thread, t.Session, t.Question, t.Option}, "\x00")
}

// issue makes a token for console pid's press of binding key.
func (p *pressTokens) issue(pid int, key string, now time.Time) string {
	var b [16]byte
	rand.Read(b[:])
	tok := hex.EncodeToString(b[:])
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokens == nil {
		p.tokens = map[string]press{}
	}
	p.expire(now)
	oldest, n := "", 0
	for t, pr := range p.tokens {
		if pr.pid != pid {
			continue
		}
		n++
		if oldest == "" || pr.made.Before(p.tokens[oldest].made) {
			oldest = t
		}
	}
	if n >= maxPresses {
		delete(p.tokens, oldest)
	}
	p.tokens[tok] = press{pid: pid, key: key, made: now}
	return tok
}

// take uses token tok up: true when it is good for console pid's press
// of binding key. A token shown with the wrong console or binding is
// spent all the same, so it can't be tried again.
func (p *pressTokens) take(tok string, pid int, key string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire(now)
	pr, ok := p.tokens[tok]
	if !ok {
		return false
	}
	delete(p.tokens, tok)
	return pr.pid == pid && pr.key == key
}

// expire drops the tokens older than the TTL; p.mu held.
func (p *pressTokens) expire(now time.Time) {
	for t, pr := range p.tokens {
		if now.Sub(pr.made) >= proto.ModActionTokenTTL {
			delete(p.tokens, t)
		}
	}
}
