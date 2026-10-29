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
	"slices"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (e peerExport) Needs(c caller.Caller, slug string) ([]proto.PeerNeed, error) {
	if c.Kind != caller.Peer {
		return nil, proto.Errorf(proto.ErrRefused, "peer.watch is a paired server's")
	}
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
