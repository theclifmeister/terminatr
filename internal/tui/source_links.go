package tui

import (
	"fmt"

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

func (s *ServerSource) LinkLog(slug, other string) ([]*link.Message, error) {
	p, err := project.Open(slug)
	if err != nil {
		return nil, err
	}
	return link.Log(p, other)
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
		return "sent " + m.ID + " to " + m.Peer, nil
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
		return "sent " + m.ID + " to " + m.Peer, nil
	}
	return "", fmt.Errorf("unknown link action %q", verb)
}
