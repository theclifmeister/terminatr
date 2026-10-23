package tui

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
)

// ServerSource's LinkSource: the links between projects.

func (s *ServerSource) SetLink(slug, other, allow string) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return link.Set(s.Caller, slug, other, allow, nil)
}

func (s *ServerSource) SetLinkAsk(slug, other string, ask bool) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	return link.SetAsk(s.Caller, slug, other, ask)
}

func (s *ServerSource) SetLinkFiles(slug, other, files string) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	_, _, err := link.SetFiles(s.Caller, slug, other, files)
	return err
}

func (s *ServerSource) LinkFiles(slug, other string) ([]link.Transfer, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return link.Transfers(p, other), nil
}

func (s *ServerSource) LinkLog(slug, other string) ([]*link.Message, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return link.Log(p, other)
}

// sentNote is what the footer says of a message that went, or waits
// over the link's rate.
func sentNote(m *link.Message) string {
	if !m.Due.IsZero() {
		return "queued " + m.ID + " for " + m.Peer + ": over the link's rate, it goes in order " + link.DueWords(m)
	}
	return "sent " + m.ID + " to " + m.Peer
}

func (s *ServerSource) LinkAct(slug, verb, arg, text string) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	switch verb {
	case "approve":
		m, err := link.Approve(s.Caller, p, arg, false)
		if err != nil {
			return "", err
		}
		return sentNote(m), nil
	case "drop":
		m, err := link.Drop(s.Caller, p, arg, false)
		if err != nil {
			return "", err
		}
		return "dropped " + m.ID, nil
	case "resume":
		return "resumed the link with " + arg, link.Resume(s.Caller, p, arg, false)
	case "send":
		m, err := link.Send(s.Caller, p, arg, link.Opts{Text: text})
		if err != nil {
			return "", err
		}
		return sentNote(m), nil
	case "request":
		r, err := link.Ask(s.Caller, p, arg, text)
		if err != nil {
			return "", err
		}
		return RequestWords(r), nil
	case "accept-file":
		t, err := link.AcceptFile(s.Caller, p, arg)
		if err != nil {
			return "", err
		}
		if t.State == link.FileReceived {
			return "accepted " + t.ID + ": " + t.Name + " is in place", nil
		}
		return "accepted " + t.ID + ": " + t.Name + " comes over the link", nil
	case "decline-file":
		t, err := link.DeclineFile(s.Caller, p, arg)
		if err != nil {
			return "", err
		}
		return "declined " + t.ID + " (" + t.Name + ")", nil
	case "cancel-file":
		t, err := link.CancelFile(s.Caller, p, arg)
		if err != nil {
			return "", err
		}
		return "cancelled " + t.ID + " (" + t.Name + ")", nil
	case "accept", "decline":
		allow, ask, _ := strings.Cut(text, " ")
		r, err := link.Answer(s.Caller, p, arg, verb == "accept", allow, ask == "ask")
		if err != nil {
			return "", err
		}
		return AnswerWords(r), nil
	}
	return "", fmt.Errorf("unknown link action %q", verb)
}

// RequestWords is what sending link request r says.
func RequestWords(r *link.Request) string {
	alias, _, _ := config.Remote(r.Peer)
	return fmt.Sprintf("asked %s to link back (%s, %s on this side); %s's user accepts or declines it there", r.Peer, r.ID, linkAllowWords(config.Link{Allow: r.Allow}), alias)
}

// AnswerWords is what answering link request r says.
func AnswerWords(r *link.Request) string {
	if r.State == link.ReqAccepted {
		return fmt.Sprintf("accepted %s: %s → %s %s on this side; %s hears of it", r.ID, r.Project, r.Peer, linkAllowWords(config.Link{Allow: r.Answer, Ask: r.Ask}), r.Peer)
	}
	return fmt.Sprintf("declined %s from %s; %s hears of it, nothing here changed", r.ID, r.Peer, r.Peer)
}
