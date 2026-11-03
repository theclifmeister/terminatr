package tui

import (
	"errors"
	"fmt"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/features"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ProgramPeerSource works program membership across paired servers (the
// programs design §9.1): the invites of a program here to paired
// projects, and of paired programs to the projects here.
type ProgramPeerSource interface {
	// ProgramInvites are folder slug's invites both ways (link.Invites):
	// a program's to its paired members, a project's from paired
	// programs, with a membership set by hand as an accepted one. None
	// while programs are off.
	ProgramInvites(slug string) ([]link.Invite, error)
	// ProgramAct answers project slug's invite arg (accept with o, or
	// decline), or leaves the paired program arg (leave), as the user.
	ProgramAct(slug, verb, arg string, o link.AnswerOpts) (string, error)
	// SetProgramLinkKey changes program_tasks, program_starts or
	// program_status of project slug, a paired program's member.
	SetProgramLinkKey(slug, key string, value any) error
}

func (s *ServerSource) ProgramInvites(slug string) ([]link.Invite, error) {
	rs := link.Invites(slug)
	cfg, _ := config.Load()
	if l, in := cfg.ProgramLink(slug); in && programsOnIn(cfg) {
		found := false
		for _, r := range rs {
			found = found || !r.Out && r.Peer == l.Program && r.State == link.ReqAccepted
		}
		if !found {
			rs = append(rs, link.Invite{Project: slug, Peer: l.Program, State: link.ReqAccepted})
		}
	}
	return rs, nil
}

func (s *ServerSource) ProgramAct(slug, verb, arg string, o link.AnswerOpts) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	switch verb {
	case "accept", "decline":
		o.Accept = verb == "accept"
		r, changed, err := link.AnswerInvite(s.Caller, p, arg, o)
		if err != nil {
			return "", err
		}
		if changed {
			s.reloadPeers()
		}
		if r.State == link.ReqAccepted {
			return fmt.Sprintf("accepted %s: %s is in %s (%s); %s hears of it", r.ID, slug, link.Shown(r.Peer), r.Words(), link.Shown(r.Peer)), nil
		}
		return fmt.Sprintf("declined %s from %s; nothing here changed, and %s hears of it", r.ID, link.Shown(r.Peer), link.Shown(r.Peer)), nil
	case "leave":
		if _, err := link.Leave(s.Caller, &p.Folder, arg); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s left %s; its program settings are gone, and %s hears of it", slug, link.Shown(arg), link.Shown(arg)), nil
	}
	return "", fmt.Errorf("unknown program action %q", verb)
}

func (s *ServerSource) SetProgramLinkKey(slug, key string, value any) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	cfg, _ := config.Load()
	if !programsOnIn(cfg) {
		return errProgramsOff
	}
	if err := config.SetProgramLinkKey(slug, key, value); err != nil {
		return err
	}
	if p, err := project.Open(slug); err == nil {
		p.Journal(s.Caller, "settings."+key, slug, fmt.Sprint(value))
	}
	return nil
}

// reloadPeers has the server read peers.json again, as a needs switch
// set here changed it; a server that doesn't run reads it at its start.
func (s *ServerSource) reloadPeers() {
	var st *proto.LinkStatus
	_ = s.call(proto.MethodPeerReload, nil, &st)
}

// programsOnIn is the programs switch in cfg.
func programsOnIn(cfg *config.Config) bool { return features.Programs(cfg) }

var errProgramsOff = errors.New(features.OffMsg)

// inviteMembers invites program slug's paired members that have no
// invite waiting or accepted, and tells the ones taken off (before, not
// in after) they left; the program's table says after already. It says
// what it did, "" for nothing.
func inviteMembers(c caller.Caller, slug string, before, after []string) (string, error) {
	g, err := program.Open(slug, nil)
	if err != nil {
		return "", err
	}
	var invited, gone []string
	var errs []error
	for _, mem := range after {
		if _, _, remote := config.Remote(mem); !remote {
			continue
		}
		r, err := link.InviteMember(c, &g.Folder, mem)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", mem, err))
			continue
		}
		if r.State == link.ReqQueued {
			invited = append(invited, link.Shown(mem))
		}
	}
	for _, mem := range before {
		if _, _, remote := config.Remote(mem); !remote || slices.Contains(after, mem) {
			continue
		}
		if _, err := link.Leave(c, &g.Folder, mem); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", mem, err))
			continue
		}
		gone = append(gone, link.Shown(mem))
	}
	var words string
	if len(invited) > 0 {
		words = "invited " + joinAnd(invited) + "; their users accept there"
	}
	if len(gone) > 0 {
		if words != "" {
			words += "; "
		}
		words += joinAnd(gone) + " hear they left"
	}
	return words, errors.Join(errs...)
}
