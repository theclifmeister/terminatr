package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// A program for the consoles that read it over the socket, the apps (the
// programs design §11.2, §12): its dashboard (program.get) and the
// human's acts on it, which the TUI does in-process and tm program does
// from the CLI. Each is the human's and refused programs-off while
// programs are off, but program.delete.

// ProgramDetail is program.get's result: the program as its dashboard
// shows it, but its Needs you (program.needs).
type ProgramDetail struct {
	SideProgram
	// Members are its members in the program's order.
	Members []ProgramMember `json:"members"`
	// Director is its director's session id, "" while none runs.
	Director string `json:"director,omitempty"`
	// Umbrellas are its board's open umbrella and sweep tasks, in board
	// order, each with its parts' states.
	Umbrellas []ProgramUmbrella `json:"umbrellas,omitempty"`
	// Deps are the states of the tasks the members' tasks come after and
	// of the umbrellas' parts, by ref, as the ticker last wrote them
	// (deps.json); Waits the members' open tasks that come after other
	// members' tasks: the dashboard's DEPENDENCIES.
	Deps  map[string]program.Dep `json:"deps,omitempty"`
	Waits []program.Wait         `json:"waits,omitempty"`
	// Tasks are the local members' tasks the parts and the waits (both
	// sides) name,
	// by ref ("acme-api/T12"), for the task details.
	Tasks map[string]SideTask `json:"tasks,omitempty"`
	// Working counts the members' threads that run now, against the
	// program's thread cap (ParallelThreads, 0 for none).
	Working int `json:"working,omitempty"`
}

// ProgramMember is one member of a program: a local project (Server
// "") or one on paired server Server, which the peer mirror lists under
// that alias (peer.list), never copied here. State is "" for a local
// member, else its invite's: invited (no answer yet), member,
// declined, left, undelivered; Words say it as its row does. Dormant: the
// paired server has programs off. Error says why a local member doesn't
// open.
type ProgramMember struct {
	Project string `json:"project"`
	Server  string `json:"server,omitempty"`
	State   string `json:"state,omitempty"`
	Words   string `json:"words,omitempty"`
	Dormant bool   `json:"dormant,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ProgramUmbrella is an umbrella task of the program's board and its
// parts, with their states (program.Parts).
type ProgramUmbrella struct {
	Task  SideTask            `json:"task"`
	Parts []program.PartState `json:"parts,omitempty"`
}

// The members' states as ProgramMember says them.
const (
	MemberInvited  = "invited"
	MemberAccepted = "member"
)

// programGet is program.get's.
func (s *Server) programGet(p proto.ProgramParams) (any, *proto.Error) {
	cfg, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	var sp SideProgram
	for _, x := range SidePrograms(cfg) {
		if x.Slug == g.Slug {
			sp = x
		}
	}
	snap, _ := program.ReadSnapshot(g.Slug)
	out := ProgramDetail{SideProgram: sp, Members: []ProgramMember{}, Deps: snap.States, Waits: snap.Waits}
	invites := map[string]link.Invite{}
	for _, r := range link.Invites(g.Slug) {
		if r.Out {
			invites[r.Peer] = r
		}
	}
	var mirror []proto.PeerMirror
	t, _ := cfg.Program(g.Slug)
	local := map[string]bool{}
	for _, m := range t.Projects {
		alias, member, ok := config.SplitMember(m)
		if !ok {
			continue
		}
		pm := ProgramMember{Project: member, Server: alias}
		if alias == "" {
			local[member] = true
			if _, err := project.OpenWith(member, cfg); err != nil {
				pm.Error = err.Error()
			}
			out.Members = append(out.Members, pm)
			continue
		}
		if inv, ok := invites[m]; ok {
			pm.Words = inv.Words()
			switch {
			case inv.Pending():
				pm.State = MemberInvited
			case inv.State == link.ReqAccepted:
				pm.State = MemberAccepted
			default:
				pm.State = inv.State
			}
		}
		if mirror == nil {
			mirror = s.mirror.List().Peers
		}
		for _, pr := range mirror {
			if pr.Alias == alias {
				pm.Dormant = !pr.Program
			}
		}
		out.Members = append(out.Members, pm)
	}
	for _, si := range s.list().Sessions {
		switch {
		case si.Role == proto.RoleDirector && si.Project == g.Slug:
			out.Director = si.ID
		case si.Role == proto.RoleThread && local[si.Project]:
			out.Working++
		}
	}
	b, err := g.Tasks().Load()
	if err != nil {
		return out, nil
	}
	var refs []string
	for _, ut := range b.Tasks {
		if ut.Status.Closed() || ut.Archived {
			continue
		}
		out.Umbrellas = append(out.Umbrellas, ProgramUmbrella{Task: sideTask(ut), Parts: program.Parts(ut, snap.States)})
		refs = append(refs, ut.Parts...)
	}
	for _, w := range snap.Waits {
		refs = append(refs, w.Task, w.After)
	}
	out.Tasks = memberTasks(cfg, refs)
	return out, nil
}

// memberTasks are the local members' tasks refs name, each board read
// once.
func memberTasks(cfg *config.Config, refs []string) map[string]SideTask {
	boards := map[string]*tasks.Board{}
	var out map[string]SideTask
	for _, ref := range refs {
		alias, member, n, ok := tasks.CrossRef(ref)
		if !ok || alias != "" {
			continue
		}
		if _, done := out[ref]; done {
			continue
		}
		mb, seen := boards[member]
		if !seen {
			if pr, err := project.OpenWith(member, cfg); err == nil {
				mb, _ = pr.Tasks().Load()
			}
			boards[member] = mb
		}
		if mb == nil {
			continue
		}
		if t := mb.Find(n); t != nil {
			if out == nil {
				out = map[string]SideTask{}
			}
			out[ref] = sideTask(t)
		}
	}
	return out
}

// programFor opens program slug for a human's act: refused as
// directorReady refuses (programs off, no such program, one left out).
func programFor(slug string) (*config.Config, *program.Program, *proto.Error) {
	if slug == "" {
		return nil, nil, proto.Errorf(proto.ErrBadParams, "name the program")
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, proto.Errorf(proto.ErrInternal, "config: %v", err)
	}
	if _, perr := directorReady(cfg, slug); perr != nil {
		return nil, nil, perr
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return nil, nil, openError(err)
	}
	return cfg, g, nil
}

// ProgramRouteParams are program.route's: tm program route's JSON for
// Program.
type ProgramRouteParams struct {
	Program string               `json:"program"`
	Route   program.RouteRequest `json:"route"`
}

// programRoute is program.route's: the tasks are filed as the human
// for the program; a refusal after some were filed (a paired member's)
// still answers what was filed.
func (s *Server) programRoute(p ProgramRouteParams, cc callCtx) (any, *proto.Error) {
	cfg, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	g.Paired = serverPaired{s}
	res, err := g.Route(cc.who(), cfg, p.Route)
	if err != nil && (res == nil || len(res.Tasks) == 0) {
		return nil, openError(err)
	}
	s.watch.wake()
	if err != nil {
		s.log.Printf("program.route %s: %v (after %d filed)", g.Slug, err, len(res.Tasks))
	}
	return res, nil
}

// programTaskEdit is program.task.edit's.
func (s *Server) programTaskEdit(p proto.ProgramTaskEditParams, cc callCtx) (any, *proto.Error) {
	cfg, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	if !strings.Contains(p.Ref, "/") {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is not a member's task (acme-api/T12)", p.Ref)
	}
	var ch tasks.ProgramChange
	set := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	ch.Priority, ch.Design, ch.Title, ch.Repo = set(p.Priority), set(p.Design), set(p.Title), set(p.Repo)
	if len(p.After) > 0 {
		ch.After = append([]string(nil), p.After...)
	}
	if p.Status != "" {
		st, ok := tasks.ParseStatus(p.Status)
		if !ok {
			return nil, proto.Errorf("invalid-status", "%q is not a status", p.Status)
		}
		ch.Status = &st
	}
	ch.Note = p.Note
	if ch.Empty() {
		return nil, proto.Errorf(proto.ErrBadParams, "nothing to change")
	}
	g.Paired = serverPaired{s}
	if program.IsRemoteRef(p.Ref) {
		sent, err := g.EditRemote(cc.who(), cfg, p.Ref, ch)
		if err != nil {
			return nil, openError(err)
		}
		s.watch.wake()
		return proto.ProgramTaskEditResult{Sent: sent.ID}, nil
	}
	res, err := g.EditTask(cc.who(), cfg, p.Ref, ch)
	if err != nil {
		return nil, openError(err)
	}
	s.watch.wake()
	out := proto.ProgramTaskEditResult{Result: "unchanged"}
	if res.Changed {
		out.Result = "edited"
	}
	return out, nil
}

// programSend is program.send's: from the program to a member, or from
// a member to the program, as the human for the sending side.
func (s *Server) programSend(p proto.ProgramSendParams, cc callCtx) (any, *proto.Error) {
	cfg, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	if strings.TrimSpace(p.Text) == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "empty message")
	}
	g.Paired = serverPaired{s}
	m, err := g.Send(cc.who(), cfg, p.From, p.To, link.Opts{Text: p.Text, Re: p.Re})
	if err != nil {
		return nil, openError(err)
	}
	s.watch.wake()
	return proto.ProgramSendResult{ID: m.ID, State: m.State, Bytes: m.Bytes}, nil
}

// programRead is program.read's: a message of the program's channel,
// framed as data.
func (s *Server) programRead(p proto.ProgramReadParams) (any, *proto.Error) {
	_, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	m, err := link.Read(&g.Folder, p.ID)
	if err != nil {
		return nil, openError(err)
	}
	return proto.ProgramReadResult{ID: m.ID, Text: link.Frame(g.Slug, m)}, nil
}

// programAnswer is program.answer's: the user's answer to the
// program's open question, fanned out to the member questions it
// covers. program.Answer refuses an agent. The user's own paired phone
// answers too, at write level (Q208), its act signed with its device
// key and journaled as the device ("human (iphone: <name> › program
// acme)"); a device below write (which its app API refuses first) and
// a paired server's user, who has only the answer acts of peer.act, are
// refused here.
func (s *Server) programAnswer(p proto.ProgramAnswerParams, cc callCtx) (any, *proto.Error) {
	cfg, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	c := cc.who()
	if c.IsAgent() || c.Peer != "" {
		return nil, proto.Errorf("human-only", "only the user answers a program's question: at this computer, in the Mac app or on their own paired iPhone")
	}
	if c.Device != "" {
		if d, ok := s.deviceNamed(c.Device); !ok || d.Effective() != device.LevelWrite {
			return nil, proto.Errorf("human-only", "device %s answers a program's question only at write level (tm device access %q write)", c.Device, c.Device)
		}
	}
	g.Paired = serverPaired{s}
	res, err := g.Answer(c, cfg, p.Ref, p.Choices, p.Other)
	if err != nil {
		return nil, openError(err)
	}
	s.watch.wake()
	return res, nil
}

// programUmbrella is program.umbrella.accept's (accept) and
// program.umbrella.send-back's.
func (s *Server) programUmbrella(p proto.ProgramUmbrellaParams, cc callCtx, accept bool) (any, *proto.Error) {
	_, g, perr := programFor(p.Program)
	if perr != nil {
		return nil, perr
	}
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is not an umbrella task of %s (T4)", p.Task, g.Slug)
	}
	var res tasks.Result
	var err error
	if accept {
		note := p.Note
		if note == "" {
			note = "accepted in the app"
		}
		res, err = g.AcceptUmbrella(cc.who(), id, note)
	} else {
		res, err = g.SendBackUmbrella(cc.who(), id, p.Note)
	}
	if err != nil {
		return nil, openError(err)
	}
	s.watch.wake()
	out := proto.ProgramUmbrellaResult{}
	if res.Task != nil {
		out.Result = string(res.Task.Status)
	}
	return out, nil
}

// programDelete is program.delete's, the human's whatever [features]
// programs says (a dormant program is cleared so): refused while its
// director runs, and without the user's confirm.
func (s *Server) programDelete(p proto.ProgramDeleteParams, cc callCtx) (any, *proto.Error) {
	c := cc.who()
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "only the user deletes programs")
	}
	if _, err := program.Dir(p.Program); err != nil {
		return nil, openError(err)
	}
	if p.Confirm != p.Program {
		return nil, proto.Errorf("not-confirmed", "confirm deleting program %s with its name: nothing deleted", p.Program)
	}
	for _, si := range s.list().Sessions {
		if si.Role == proto.RoleDirector && si.Project == p.Program {
			return nil, proto.Errorf("director-running", "program %s's director is running (%s): stop it first", p.Program, si.ID)
		}
	}
	dst, err := program.Delete(c, p.Program, time.Now())
	if err != nil {
		return nil, openError(err)
	}
	s.watch.wake()
	return proto.ProgramDeleteResult{Trash: dst}, nil
}

// taskAfter are task t of project slug's after refs with their states:
// another member's from its program's deps.json (the ticker's), while
// programs are on and slug is in a program; this board's as it is.
func taskAfter(cfg *config.Config, slug string, b *tasks.Board, t *tasks.Task) []SideAfter {
	if len(t.After) == 0 && len(t.AfterX) == 0 {
		return nil
	}
	var deps map[string]program.Dep
	if cfg != nil && features.Programs(cfg) {
		if prog := cfg.ProgramOf(slug); prog != "" {
			deps, _ = program.ReadDeps(prog)
		}
	}
	out := make([]SideAfter, 0, len(t.After)+len(t.AfterX))
	for _, n := range t.After {
		a := SideAfter{Ref: fmt.Sprintf("T%d", n)}
		if b != nil {
			if x := b.Find(n); x != nil {
				a.Status, a.Title = string(x.Status), x.Title
			}
		}
		out = append(out, a)
	}
	for _, x := range t.AfterX {
		a := SideAfter{Ref: x}
		if d, ok := deps[x]; ok {
			a.State, a.Status, a.Title, a.Why, a.Merged, a.Word = d.State, d.Status, d.Title, d.Why, d.Merged, d.Word()
		}
		out = append(out, a)
	}
	return out
}

// SideAfter is a task a task comes after (task.get): Ref is this
// board's ("T10") or another project's ("acme-api/T12"). Another
// member's carries its dependency State (met, open, gone, unknown),
// with Word as the dashboards say it, while programs are on; Status and
// Title are the task's as last read.
type SideAfter struct {
	Ref    string `json:"ref"`
	State  string `json:"state,omitempty"`
	Word   string `json:"word,omitempty"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	Merged bool   `json:"merged,omitempty"`
	Why    string `json:"why,omitempty"`
}

// programOpen is program.open's: program p.Program's director, started
// if none runs (OpenDirector), as tm program open and the sidebar's
// director row do; a start is journaled in the program (director.start).
// Refused, as a director's start is, while programs are off.
func (s *Server) programOpen(p proto.ProgramOpenParams, cc callCtx) (any, *proto.Error) {
	if p.Program == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "name the program")
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	list := func() ([]proto.SessionInfo, error) { return s.list().Sessions, nil }
	start := func(sp proto.SessionStartParams) (string, error) {
		res, perr := s.startSession(sp)
		if perr != nil {
			return "", perr
		}
		started, ok := res.(proto.SessionStartResult)
		if !ok {
			return "", fmt.Errorf("unexpected result %T", res)
		}
		return started.Session.ID, nil
	}
	id, started, err := OpenDirector(list, start, p.Program, "", p.Cols, p.Rows)
	if err != nil {
		return nil, openError(err)
	}
	if started {
		if g, gerr := program.Open(p.Program, nil); gerr == nil {
			if err := g.Journal(cc.who(), "director.start", id, ""); err != nil {
				s.log.Printf("program.open %s: journal: %v", p.Program, err)
			}
		}
		s.watch.wake()
	}
	return proto.ProgramOpenResult{Session: id, Started: started}, nil
}
