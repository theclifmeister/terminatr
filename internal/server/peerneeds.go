package server

// Paired needs (docs/SPEC.md §16.16): a shared project's needs in full,
// for a paired server whose needs switch here is view or answer. The
// link's dispatcher asks only at that level (internal/peer); here they
// are built from what the user here sees: the sessions' menus and
// permission dialogs, the coordinator's open questions and the
// project's NEEDS YOU.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

func (e peerExport) Needs(c caller.Caller, slug string) ([]proto.PeerNeed, error) {
	if c.Kind != caller.Peer {
		return nil, proto.Errorf(proto.ErrRefused, "peer.watch is a paired server's")
	}
	return e.needsOf(slug)
}

// needsOf are shared project slug's needs now.
func (e peerExport) needsOf(slug string) ([]proto.PeerNeed, error) {
	if !slices.Contains(sharedProjects(), slug) {
		return nil, errNotShared(slug)
	}
	p, err := project.Open(slug)
	if err != nil {
		return nil, errNotShared(slug)
	}
	sessions := e.sessions()
	w, err := e.state(slug, sessions)
	if err != nil {
		return nil, errNotShared(slug)
	}
	asks, _ := p.Questions()
	approval := e.approval
	if approval == nil {
		approval = func(string) *proto.PeerApproval { return nil }
	}
	return peerNeeds(slug, w, sessions, asks, approval), nil
}

// Act is peer.act's, for c, the paired server's user (caller.ForPeerHuman):
// the need must still be one of the shared project's, the one they saw
// (its id), never an escape's, and the act one it takes with its guard
// holding (the question's text, the option's, the task's status). Then
// the act runs as this server's own does, with its checks, as c.
func (e peerExport) Act(c caller.Caller, p proto.PeerAct) (proto.PeerActResult, error) {
	if c.Kind != caller.Human || c.Peer == "" {
		return proto.PeerActResult{}, proto.Errorf(proto.ErrRefused, "peer.act is a paired server's user's")
	}
	ns, err := e.needsOf(p.Project)
	if err != nil {
		return proto.PeerActResult{}, err
	}
	i := slices.IndexFunc(ns, func(n proto.PeerNeed) bool { return n.ID == p.Need })
	if i < 0 {
		return proto.PeerActResult{}, errNeedChanged
	}
	n := ns[i]
	if n.Escape {
		return proto.PeerActResult{}, errEscapeHere
	}
	if !slices.Contains(proto.ActsOf(n), p.Act) {
		return proto.PeerActResult{}, proto.Errorf(proto.ErrBadParams, "a %s need takes no %s", n.Kind, p.Act)
	}
	p, perr := actGuard(n, p)
	if perr != nil {
		return proto.PeerActResult{}, perr
	}
	if e.act == nil {
		return proto.PeerActResult{}, proto.Errorf(proto.ErrRefused, "this server takes no acts on its needs")
	}
	res, perr := e.act(c, n, p)
	if perr != nil {
		return proto.PeerActResult{}, perr
	}
	res.Act = p.Act
	return res, nil
}

// Refusals of peer.act that say the need moved on, or is the user's
// here alone.
var (
	errNeedChanged = proto.Errorf(proto.ErrNeedChanged, "that need changed: look again")
	errEscapeHere  = proto.Errorf(proto.ErrEscapeHere, "that asks to run a command outside the sandbox: only this server's user answers it, here")
)

// actGuard checks p's guard against need n as it is now: the act must
// name what the user saw, else the need changed. The paired server
// cleaned the words it showed (one line, cut: internal/peer), so they
// are compared so; p comes back with this server's own words, which the
// act sends.
func actGuard(n proto.PeerNeed, p proto.PeerAct) (proto.PeerAct, *proto.Error) {
	switch p.Act {
	case proto.ActAnswer:
		qs := n.Question.Questions
		if p.Index < 0 || p.Index >= len(qs) {
			return p, proto.Errorf(proto.ErrBadParams, "the menu has %d question(s), not %d", len(qs), p.Index+1)
		}
		it := qs[p.Index]
		if p.Question == "" || !sameWords(p.Question, it.Question) {
			return p, errNeedChanged
		}
		j := slices.IndexFunc(it.Options, func(o proto.QuestionOption) bool { return sameWords(o.Label, p.Answer) })
		if j < 0 {
			return p, proto.Errorf(proto.ErrBadParams, "%q is not one of that question's options", textsafe.Line(p.Answer, 80))
		}
		p.Question, p.Answer = it.Question, it.Options[j].Label
	case proto.ActApprove:
		opts := n.Approval.Options
		if p.Choice < 1 || p.Choice > len(opts) {
			return p, proto.Errorf(proto.ErrBadParams, "the dialog has %d option(s), not %d", len(opts), p.Choice)
		}
		if p.Option == "" || !sameWords(p.Option, opts[p.Choice-1]) {
			return p, errNeedChanged
		}
		p.Option = opts[p.Choice-1]
	case proto.ActAccept, proto.ActSendBack:
		if p.Status == "" || p.Status != n.Status {
			return p, errNeedChanged
		}
	}
	return p, nil
}

// sameWords says whether a paired server's words are these, as it
// showed them: on one line, cut as its mirror cuts a need's.
func sameWords(theirs, ours string) bool {
	const most = 200
	return textsafe.Line(theirs, most) == textsafe.Line(ours, most)
}

// peerAct runs act p on need n of its project for c, the paired
// server's user, as this server's own acts run: session.answer,
// session.approve, task.accept, task.send-back or questions.open, each
// with its checks, on the human's side but never an escape's (c is
// remote). A prompt whose escape may be on screen (session.EscapeHeld)
// takes no answer from there either.
func (s *Server) peerAct(c caller.Caller, n proto.PeerNeed, p proto.PeerAct) (proto.PeerActResult, *proto.Error) {
	cc := callCtx{method: proto.MethodPeerAct, human: true, who: func() caller.Caller { return c }}
	var none proto.PeerActResult
	if p.Act == proto.ActAnswer || p.Act == proto.ActApprove {
		sess, perr := s.session(n.Session)
		if perr != nil {
			return none, errNeedChanged
		}
		if sess.EscapeHeld() || sess.Info().Escape {
			return none, errEscapeHere
		}
	}
	switch p.Act {
	case proto.ActAnswer:
		res, perr := s.answer(proto.SessionAnswerParams{ID: n.Session, Index: p.Index, Answer: p.Answer, Question: p.Question}, cc)
		if perr != nil {
			return none, perr
		}
		return proto.PeerActResult{Remaining: res.(proto.SessionAnswerResult).Remaining}, nil
	case proto.ActApprove:
		ap := proto.SessionApproveParams{ID: n.Session, Choice: p.Choice, Check: true}
		res, perr := s.approve(ap, cc)
		if perr != nil {
			return none, perr
		}
		if res.(proto.SessionApproveResult).Option != p.Option {
			return none, errNeedChanged
		}
		ap.Check = false
		if res, perr = s.approve(ap, cc); perr != nil {
			return none, perr
		}
		return proto.PeerActResult{Option: res.(proto.SessionApproveResult).Option}, nil
	case proto.ActAccept, proto.ActSendBack:
		kind := map[string]string{proto.ActAccept: project.KindAccept, proto.ActSendBack: project.KindSendBack}[p.Act]
		res, perr := s.taskAction(kind, proto.TaskActionParams{Project: p.Project, Task: n.Task, Note: p.Note}, cc)
		if perr != nil {
			return none, perr
		}
		return proto.PeerActResult{Asked: res.(proto.TaskAskResult).Asked}, nil
	case proto.ActQuestions:
		res, perr := s.openQuestions(proto.QuestionsOpenParams{Project: p.Project})
		if perr != nil {
			return none, perr
		}
		r := res.(proto.QuestionsOpenResult)
		s.journalIn(p.Project, c, "questions.open", r.Session, fmt.Sprintf("%d open", r.Open))
		return proto.PeerActResult{Open: r.Open}, nil
	}
	return none, proto.Errorf(proto.ErrBadParams, "%q is no act", p.Act)
}

// peerApproval is the permission dialog on session id's screen, as
// session.approve reads it; nil while the screen shows none.
func (s *Server) peerApproval(id string) *proto.PeerApproval {
	sess, perr := s.session(id)
	if perr != nil {
		return nil
	}
	x, ok := sess.Explain()
	if !ok || x.Screen == nil || x.Screen.State != agent.StateBlocked || x.Screen.Reason != "permission" {
		return nil
	}
	screen, err := sess.Read(false)
	if err != nil {
		return nil
	}
	a := &proto.PeerApproval{Options: agent.DialogOptions(screen.Text)}
	if len(a.Options) == 0 {
		return nil
	}
	_, a.Question = agent.DialogOption(screen.Text, 1)
	m := agent.ManifestOf(sess.Agent())
	for _, o := range a.Options {
		a.Scopes = append(a.Scopes, string(agent.OptionScope(m, o)))
	}
	return a
}

// peerNeeds are project slug's needs in full: each session's open menu
// or permission dialog (an escape's listed bare: approval answers no
// escape, so it isn't read), the coordinator's open questions, then the
// project's NEEDS YOU, a thread's question only where its session's
// menu didn't say it already.
func peerNeeds(slug string, w proto.ProjectWatch, sessions []proto.SessionInfo, asks []project.Question,
	approval func(id string) *proto.PeerApproval) []proto.PeerNeed {
	titles := map[string]string{}
	for _, t := range w.Threads {
		titles[t.ID] = t.Title
		if t.Task != nil {
			titles[t.ID] = t.Task.Title
		}
	}
	var out []proto.PeerNeed
	asked := map[string]bool{} // threads whose session asks already
	for _, info := range sessions {
		if info.Project != slug || info.Server != "" {
			continue
		}
		n := proto.PeerNeed{Session: info.ID, Role: info.Role, Thread: info.Thread, Task: info.Task, Title: titles[info.Thread]}
		switch {
		case info.Question != nil && openQuestion(info.Question) != "":
			q := *info.Question
			n.Kind, n.ID, n.Since, n.Escape = proto.NeedQuestion, "question:"+info.ID+":"+menuKey(q), q.Since, q.Escape
			if !q.Escape {
				q.Questions = slices.Clone(q.Questions)
				n.Question = &q
			}
		case info.State == string(agent.StateBlocked) && info.Reason == "question":
			// The agent's own menu, which tm didn't read: its text is
			// in the session only.
			n.Kind, n.ID, n.Text = proto.NeedQuestion, "question:"+info.ID, questionNoText
		case info.State == string(agent.StateBlocked) && info.Reason == "permission":
			n.Kind, n.ID, n.Escape = proto.NeedApproval, "approval:"+info.ID, info.Escape
			if !info.Escape {
				if a := approval(info.ID); a != nil {
					n.Approval = a
					n.ID += ":" + key(append([]string{a.Question}, a.Options...)...)
				}
			}
		default:
			continue
		}
		if info.Thread != "" {
			asked[info.Thread] = true
		}
		out = append(out, n)
	}
	for _, q := range asks {
		item := proto.QuestionItem{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
		for _, o := range q.Options {
			item.Options = append(item.Options, proto.QuestionOption{Label: o.Label, Description: o.Description})
		}
		out = append(out, proto.PeerNeed{ID: "ask:" + q.ID, Kind: proto.NeedAsk, Role: proto.RoleCoordinator, Task: q.Task,
			Question: &proto.Question{Questions: []proto.QuestionItem{item}}, Since: q.Asked})
	}
	reports := map[string]int{}
	for _, t := range w.Threads {
		reports[t.ID] = t.Reports
	}
	for _, wn := range w.NeedsYou {
		n := proto.PeerNeed{Task: wn.Task, Title: wn.Title, Status: wn.Status, Thread: wn.Thread, Session: wn.Session,
			PR: wn.PR, PRURL: wn.PRURL, PRNumber: wn.PRNumber, Mergeable: wn.Mergeable, Asked: wn.Asked, Reports: reports[wn.Thread]}
		ref := wn.Task
		if ref == "" {
			ref = wn.Thread
		}
		switch wn.Why {
		case proto.WhyQueue:
			n.Kind, n.ID, n.Text, n.Title = proto.NeedQueue, "queue:"+wn.Session, wn.Title, ""
		case proto.WhyQuestion:
			if asked[wn.Thread] {
				continue
			}
			n.Kind, n.ID, n.Text = proto.NeedQuestion, "question:"+ref+":"+key(wn.Question), wn.Question
		case proto.WhyReview:
			n.Kind, n.ID = proto.NeedReview, "review:"+ref+":"+strconv.Itoa(n.Reports)
		case proto.WhyCI:
			n.Kind, n.ID = proto.NeedCI, "ci:"+ref
		case proto.WhyBlocked:
			n.Kind, n.ID = proto.NeedBlocked, "blocked:"+ref
		default:
			continue
		}
		out = append(out, n)
	}
	return out
}

// menuKey names a menu: when it opened, else its questions.
func menuKey(q proto.Question) string {
	if !q.Since.IsZero() {
		return strconv.FormatInt(q.Since.UnixNano(), 36)
	}
	var qs []string
	for _, it := range q.Questions {
		qs = append(qs, it.Question)
	}
	return key(qs...)
}

// key is a short name for words: a need's id changes when they do.
func key(words ...string) string {
	h := sha256.Sum256([]byte(strings.Join(words, "\x00")))
	return hex.EncodeToString(h[:6])
}
