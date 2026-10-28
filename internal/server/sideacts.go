package server

// The sidebar's and the sessions' acts beside the TUI's keys (proto
// CapSideActs, docs/SPEC.md §3.8): pin a project, make one, ask a
// coordinator to adopt an outside agent session, write on a link. Each
// is the human's (gateHuman) and makes the checks the TUI's make.

import (
	"strings"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// projectPin pins or unpins p.Project (P in the sidebar).
func (s *Server) projectPin(p proto.ProjectPinParams, c callCtx) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, codeErr(err)
	}
	changed, err := pr.SetFlag(c.who(), "pinned", p.Pinned)
	if err != nil {
		return nil, codeErr(err)
	}
	s.kick()
	return proto.ProjectPinResult{Changed: changed}, nil
}

// projectNew makes project p.Project (+ on the dashboard, tm project new).
func (s *Server) projectNew(p proto.ProjectNewParams, c callCtx) (any, *proto.Error) {
	pr, err := project.New(project.Options{Slug: p.Project, Repos: p.Repos})
	if err != nil {
		return nil, codeErr(err)
	}
	if err := pr.Journal(c.who(), "project.new", pr.Slug, strings.Join(pr.Repos, " ")); err != nil {
		s.log.Printf("journal of %s: %v", pr.Slug, err)
	}
	s.kick()
	res := proto.ProjectNewResult{Project: pr.Slug}
	if reg := models.Registry(); reg != nil {
		res.NoAgent = len(models.Installed(reg, nil)) == 0
	}
	return res, nil
}

// adoptRequest asks p.Project's coordinator to adopt session p.ID (O on
// the dashboard), or takes the go-ahead back: the server's go-ahead,
// which the coordinator's tm thread adopt needs, and the inbox item
// that tells it, which an agent could write too.
func (s *Server) adoptRequest(p proto.SessionAdoptRequestParams, c callCtx) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, codeErr(err)
	}
	if p.Withdraw {
		if _, perr := s.adoptAsk(proto.SessionAdoptAskParams{ID: p.ID, Project: pr.Slug, Withdraw: true}); perr != nil {
			return nil, perr
		}
		if err := pr.DoneItems(project.KindAdopt, p.ID); err != nil {
			return nil, codeErr(err)
		}
		if err := pr.Journal(c.who(), "thread.adopt.withdraw", p.ID, ""); err != nil {
			return nil, codeErr(err)
		}
		s.kick()
		return proto.SessionAdoptAskResult{}, nil
	}
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	info := sess.Info()
	switch {
	case info.Project != "" || info.Role != proto.RoleShell:
		return nil, proto.Errorf(proto.ErrRefused, "%s is a project's session already", p.ID)
	case info.Agent == "" || info.State == "exited":
		return nil, proto.Errorf(proto.ErrRefused, "no agent runs in %s; only an agent session becomes a thread", p.ID)
	}
	if _, perr := s.adoptAsk(proto.SessionAdoptAskParams{ID: p.ID, Project: pr.Slug}); perr != nil {
		return nil, perr
	}
	asked, err := pr.AskAdopt(c.who(), p.ID, AdoptWhere(info, info.Cwd))
	if err != nil {
		return nil, codeErr(err)
	}
	s.kick()
	return proto.SessionAdoptAskResult{Asked: asked}, nil
}

// AdoptWhere says what runs in session s and where, for an adopt ask:
// "claude in /src/app on fix-login"; dir is its folder as shown.
func AdoptWhere(s proto.SessionInfo, dir string) string {
	out := s.Agent + " in " + dir
	if pl, ok := worktree.Locate(s.Cwd); ok && pl.Branch != "" {
		out += " on " + pl.Branch
	}
	return out
}

// linkSend writes the user's message from p.Project to p.Peer (M on a
// link), as tm link send does.
func (s *Server) linkSend(p proto.LinkSendParams, c callCtx) (any, *proto.Error) {
	if strings.TrimSpace(p.Text) == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "link.send: the message is empty")
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, codeErr(err)
	}
	m, err := link.Send(c.who(), pr, p.Peer, link.Opts{Text: p.Text})
	if err != nil {
		return nil, codeErr(err)
	}
	s.kick()
	return proto.LinkActResult{Words: sentWords(m)}, nil
}

// sentWords says where message m went: sent, or queued over the link's
// rate.
func sentWords(m *link.Message) string {
	if !m.Due.IsZero() {
		return "queued " + m.ID + " for " + link.Shown(m.Peer) + ": over the link's rate, it goes in order " + link.DueWords(m)
	}
	return "sent " + m.ID + " to " + link.Shown(m.Peer)
}
