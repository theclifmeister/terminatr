package tui

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ProgramSource is what the program dashboard and the program needs
// dialog read and do (the programs design §11.2, §7.2), and what the
// PROJECTS header's "new program" makes (§11.1).
type ProgramSource interface {
	// ProgramNeeds is program slug's rolled-up Needs you as tm's server
	// ranks it (program.needs).
	ProgramNeeds(slug string) (proto.ProgramNeedsResult, error)
	// AnswerProgram records the user's answer to program slug's open
	// question ref (choices, or other in their own words), which goes
	// to every member question it covers (program.Answer). Refused
	// inside an agent: only the user answers.
	AnswerProgram(slug, ref string, choices []string, other string) (program.AnswerResult, error)
	// NewProgram makes program slug, with its goal, as tm program new
	// does (program.New and its clash checks), and lists it in
	// config.toml with no members yet, so the sidebar shows it; it
	// returns the slug. Refused inside an agent and while programs are
	// off.
	NewProgram(slug, goal string) (string, error)
	// AcceptUmbrella marks umbrella task id of program slug's board done,
	// the user's accept once every part is in (the programs design
	// §5.3). Refused inside an agent and while programs are off.
	AcceptUmbrella(slug string, id int) error
	// SendBackUmbrella reopens umbrella task id of program slug's board
	// with the user's note (§5.3). Refused as AcceptUmbrella is.
	SendBackUmbrella(slug string, id int, note string) error
}

func (s *ServerSource) ProgramNeeds(slug string) (proto.ProgramNeedsResult, error) {
	var res proto.ProgramNeedsResult
	err := s.call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: slug}, &res)
	return res, err
}

func (s *ServerSource) AnswerProgram(slug, ref string, choices []string, other string) (program.AnswerResult, error) {
	if s.Caller.IsAgent() {
		return program.AnswerResult{}, errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil {
		return program.AnswerResult{}, err
	}
	if !features.Programs(cfg) {
		return program.AnswerResult{}, errProgramsOff
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return program.AnswerResult{}, err
	}
	res, err := g.Answer(s.Caller, cfg, ref, choices, other)
	s.touch()
	return res, err
}

func (s *ServerSource) NewProgram(slug, goal string) (string, error) {
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if !features.Programs(cfg) {
		return "", errProgramsOff
	}
	g, err := program.New(program.Options{Slug: slug, Goal: goal, Taken: peerNameTaken})
	if err != nil {
		return "", err
	}
	if _, listed := cfg.Program(g.Slug); !listed {
		if err := config.SetProgram(g.Slug, "projects", []string{}); err != nil {
			return g.Slug, err
		}
	}
	s.touch()
	return g.Slug, nil
}

func (s *ServerSource) AcceptUmbrella(slug string, id int) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !features.Programs(cfg) {
		return errProgramsOff
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return err
	}
	_, err = g.AcceptUmbrella(s.Caller, id, "accepted on the program dashboard")
	s.touch()
	return err
}

func (s *ServerSource) SendBackUmbrella(slug string, id int, note string) error {
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !features.Programs(cfg) {
		return errProgramsOff
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return err
	}
	_, err = g.SendBackUmbrella(s.Caller, id, note)
	s.touch()
	return err
}

// peerNameTaken says why slug can't name a program for the paired
// servers (the one namespace, §3.4), as tm program new checks it: this
// server's own name, or a paired server's alias, name or the name it
// gives itself. "" when none has it.
func peerNameTaken(slug string) string {
	if slug == peer.SelfName() {
		return "it is this server's own name"
	}
	dir, err := peer.Dir()
	if err != nil {
		return ""
	}
	peers, _ := peer.LoadPeers(dir)
	for _, p := range peers {
		if p.Alias == slug || p.Name == slug || p.Told == slug {
			return "a paired server is called that"
		}
	}
	return ""
}
