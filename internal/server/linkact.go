package server

// link.act (docs/SPEC.md §7.9, §16.13): what a project's link waits on
// the user for, answered as the human through the human socket, with
// the checks the Messages tab's and tm link's make; internal/link keeps
// them and journals each as the caller.

import (
	"fmt"
	"slices"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// linkAct takes p.Action on p.ID of project p.Project's links.
func (s *Server) linkAct(p proto.LinkActParams, c callCtx) (any, *proto.Error) {
	if !slices.Contains(proto.LinkActs, p.Action) {
		return nil, proto.Errorf(proto.ErrBadParams, "link.act: %q is no action (%v)", p.Action, proto.LinkActs)
	}
	if p.Action != proto.LinkAccept && (p.Allow != "" || p.Ask) {
		return nil, proto.Errorf(proto.ErrBadParams, "link.act: only accept takes allow and ask")
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	words, err := linkAction(c, pr, p)
	if err != nil {
		return nil, codeErr(err)
	}
	s.kick()
	return proto.LinkActResult{Words: words}, nil
}

// linkAction runs it and says what came of it.
func linkAction(c callCtx, pr *project.Project, p proto.LinkActParams) (string, error) {
	who := c.who()
	switch p.Action {
	case proto.LinkApprove:
		m, err := link.Approve(who, pr, p.ID, false)
		if err != nil {
			return "", err
		}
		if !m.Due.IsZero() {
			return fmt.Sprintf("queued %s for %s: over the link's rate, it goes in order %s", m.ID, link.Shown(m.Peer), link.DueWords(m)), nil
		}
		return "sent " + m.ID + " to " + link.Shown(m.Peer), nil
	case proto.LinkDrop:
		m, err := link.Drop(who, pr, p.ID, false)
		if err != nil {
			return "", err
		}
		return "dropped " + m.ID + "; it is never sent", nil
	case proto.LinkAcceptFile:
		t, err := link.AcceptFile(who, pr, p.ID)
		if err != nil {
			return "", err
		}
		if t.State == link.FileReceived {
			return "accepted " + t.Name + ": it is in place", nil
		}
		return "accepted " + t.Name + ": it comes over the link", nil
	case proto.LinkDeclineFile:
		t, err := link.DeclineFile(who, pr, p.ID)
		if err != nil {
			return "", err
		}
		return "declined " + t.Name + "; " + link.Shown(t.Peer) + " hears of it", nil
	case proto.LinkCancelFile:
		t, err := link.CancelFile(who, pr, p.ID)
		if err != nil {
			return "", err
		}
		return "cancelled " + t.Name + "; " + link.Shown(t.Peer) + " hears of it", nil
	}
	r, err := link.Answer(who, pr, p.ID, p.Action == proto.LinkAccept, p.Allow, p.Ask)
	if err != nil {
		return "", err
	}
	if r.State == link.ReqAccepted {
		return fmt.Sprintf("linked %s with %s, %s on this side; %s hears of it", r.Project, link.Shown(r.Peer),
			allowWords(r.Answer, r.Ask), link.Shown(r.Peer)), nil
	}
	return fmt.Sprintf("declined %s's link request; nothing here changed, %s hears of it", link.Shown(r.Peer), link.Shown(r.Peer)), nil
}

// allowWords is a link entry in words: "both ways, ask first".
func allowWords(allow string, ask bool) string {
	w := map[string]string{config.LinkBoth: "both ways", config.LinkSend: "send only", config.LinkReceive: "receive only"}[allow]
	if w == "" {
		return "off"
	}
	if ask {
		w += ", ask first"
	}
	return w
}
