package server

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/rename"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// renameProgram is program.rename (the programs design §3.4): the
// program's director is stopped, the program renamed between two ticker
// sweeps (the director's memo key is its vault folder, _programs/<slug>,
// which carries over), and the director started again under the new
// name.
func (s *Server) renameProgram(p proto.ProgramRenameParams, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "the user renames programs")
	}
	cfg, _ := config.Load()
	if !features.Programs(cfg) {
		return nil, proto.Errorf(features.OffCode, "%s", features.OffMsg)
	}
	o := rename.ProgramOptions{From: p.From, To: p.To, Caller: c, Taken: peer.ProgramNameTaken, MoveAgentDir: s.moveAgentDir, TellPaired: s.tellPairedRename()}
	if err := rename.CheckProgram(o); err != nil {
		return nil, codeErr(err)
	}
	var director *session.Session
	var rec SessionRecord
	s.mu.Lock()
	for id, sess := range s.sessions {
		if r := s.records[id]; r.Role == proto.RoleDirector && r.Project == p.From && sess.ExitStatus() == "" {
			director, rec = sess, r
		}
	}
	s.mu.Unlock()
	if director != nil {
		s.log.Printf("program.rename %s → %s: stopping director %s", p.From, p.To, director.ID())
		director.Stop(StopGrace)
	}
	var res *rename.ProgramResult
	var after error
	do := func() error {
		var err error
		res, err = rename.Program(o)
		if res == nil {
			return err
		}
		after = err
		return nil
	}
	var err error
	if s.tick != nil {
		err = s.tick.RenameProject(vaultlink.ProgramFolder(p.From), vaultlink.ProgramFolder(p.To), do)
	} else {
		err = do()
	}
	if err != nil {
		if director != nil {
			s.restartDirector(rec, p.From)
		}
		return nil, codeErr(err)
	}
	oldDir, newDir := programFolder(p.From), res.Dir
	if newDir != "" {
		s.renameDormant(p.From, p.To, oldDir, newDir, "", "")
	}
	s.views.renameProgram(p.From, p.To)
	if s.tick != nil {
		s.tick.Kick()
	}
	s.watch.wake()
	out := proto.ProgramRenameResult{Dir: res.Dir, Notes: res.Notes}
	if after != nil {
		out.Notes = append(out.Notes, after.Error())
	}
	if director != nil {
		id, err := s.restartDirector(rec, p.To)
		if err != nil {
			out.Notes = append(out.Notes, "director not started again: "+err.Error()+" (tm program open "+p.To+")")
		}
		out.Director = id
	}
	s.log.Printf("program.rename %s → %s: %d notes", p.From, p.To, len(out.Notes))
	return out, nil
}

// programFolder is program slug's folder, "" for an invalid slug.
func programFolder(slug string) string {
	dir, err := program.Dir(slug)
	if err != nil {
		return ""
	}
	return dir
}

// restartDirector starts the director rec described again, for program
// slug, as tm program open would: fresh, with the program's remote
// control setting, in its folder (directorStart).
func (s *Server) restartDirector(rec SessionRecord, slug string) (string, error) {
	cfg, _ := config.Load()
	safety := cfg.DirectorSafety(slug)
	res, perr := s.startSession(proto.SessionStartParams{
		Agent: rec.Agent, Role: proto.RoleDirector, Project: slug, Model: rec.Model,
		Cols: rec.Cols, Rows: rec.Rows, Kickoff: rec.Kickoff, RemoteControl: safety.CoordinatorRemoteControl,
	})
	if perr != nil {
		return "", perr
	}
	started, ok := res.(proto.SessionStartResult)
	if !ok {
		return "", fmt.Errorf("unexpected result %T", res)
	}
	return started.Session.ID, nil
}

// tellPairedRename is the hook that tells a renamed program's paired
// members' servers its new name (rename.ProgramOptions.TellPaired): the
// program-rename peer kind (the programs design §9.2), which the paired
// children's build returns here. While it returns nil, a program with
// paired members is refused (paired-members), so no paired side is left
// naming a program that is gone.
func (s *Server) tellPairedRename() func(from, to string, paired []string) []string {
	return nil
}
