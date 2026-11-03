package server

// A program's rolled-up Needs you (the programs design §7.1): each local
// member's needs as needsOf builds them for a paired server (menus,
// permission dialogs, the coordinator's questions, NEEDS YOU tasks and
// their PRs), plus its link needs and a failing PR host from its
// snapshot, ranked across the members into one queue; a paired member
// by the count its server's mirror has (its needs in full come over
// peer.needs, B16).

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// programNeeds is program.needs': the human's for any program, a
// director's for its own; programs-off while programs are off.
func (s *Server) programNeeds(p proto.ProgramNeedsParams, cc callCtx) (any, *proto.Error) {
	c := cc.who()
	if c.IsAgent() {
		switch {
		case c.Kind != caller.Director:
			return nil, proto.Errorf("director-only", "program.needs is a program's director's (and the human's)")
		case p.Program != "" && p.Program != c.Project:
			return nil, proto.Errorf("other-program", "a director reads only its own program's needs")
		}
		p.Program = c.Project
	}
	if p.Program == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "program.needs needs the program")
	}
	res, perr := s.rolledUpNeeds(p.Program)
	if perr != nil {
		return nil, perr
	}
	return res, nil
}

// rolledUpNeeds is program slug's Needs you now, refused as
// directorReady refuses (programs off, no such program, one left out).
func (s *Server) rolledUpNeeds(slug string) (proto.ProgramNeedsResult, *proto.Error) {
	cfg, _ := config.Load()
	if _, perr := directorReady(cfg, slug); perr != nil {
		return proto.ProgramNeedsResult{}, perr
	}
	t, _ := cfg.Program(slug)
	var asks []project.Question
	if g, err := program.Open(slug, cfg); err == nil {
		asks, _ = g.Questions()
	}
	sessions := s.list().Sessions
	var mirror []proto.PeerMirror
	members := make([]memberNeeds, 0, len(t.Projects))
	for _, m := range t.Projects {
		alias, member, ok := config.SplitMember(m)
		if !ok {
			continue
		}
		if alias != "" {
			if mirror == nil {
				mirror = s.mirror.List().Peers
			}
			members = append(members, pairedMember(alias, member, mirror))
			continue
		}
		members = append(members, s.localMember(member, sessions))
	}
	return rollUp(slug, members, asks), nil
}

// directorNeeds is rolledUpNeeds for a director's context: nil when it
// doesn't read.
func (s *Server) directorNeeds(slug string) *proto.ProgramNeedsResult {
	res, perr := s.rolledUpNeeds(slug)
	if perr != nil {
		return nil
	}
	return &res
}

// memberNeeds is what one member gives the queue: a local member's
// needs (needsOf), its tasks' priorities by ref, its link needs and its
// PR host's failed polls; a paired member's mirror count.
type memberNeeds struct {
	project, server string
	needs           []proto.PeerNeed
	priority        map[string]tasks.Priority
	links           []link.Need
	// questions are its coordinator's open questions by id, which the
	// similarity hints and the program's merged questions read.
	questions map[string]project.Question
	hostFails int
	count     int
	synced    time.Time
	err       string
}

// localMember reads local member slug's needs from its snapshot and the
// server's sessions.
func (s *Server) localMember(slug string, sessions []proto.SessionInfo) memberNeeds {
	m := memberNeeds{project: slug}
	sn, err := s.projects.get(slug, ticker.StatePath(s.opts.Paths.Sessions), s.watch.generation())
	if err != nil {
		m.err = err.Error()
		return m
	}
	m.needs = needsOf(sn.p, sn.watch(sessions), sessions, s.peerApproval)
	if qs, err := sn.p.Questions(); err == nil && len(qs) > 0 {
		m.questions = map[string]project.Question{}
		for _, q := range qs {
			m.questions[q.ID] = q
		}
	}
	if sn.board != nil {
		m.priority = map[string]tasks.Priority{}
		for _, t := range sn.board.Tasks {
			if t.Priority != tasks.NoPriority {
				m.priority[t.Ref()] = t.Priority
			}
		}
	}
	m.links = sn.linkNeeds
	if !sn.noPRHost {
		m.hostFails = sn.timing.GHFails
	}
	return m
}

// pairedMember is paired member alias/slug as the mirror has it: what
// waits there, counted.
func pairedMember(alias, slug string, peers []proto.PeerMirror) memberNeeds {
	m := memberNeeds{project: slug, server: alias}
	i := slices.IndexFunc(peers, func(p proto.PeerMirror) bool { return p.Alias == alias })
	if i < 0 {
		m.err = "no paired server " + alias
		return m
	}
	j := slices.IndexFunc(peers[i].Projects, func(p proto.PeerMirrorProject) bool { return p.Project == slug })
	if j < 0 {
		m.err = alias + " doesn't share " + slug + " with this server"
		return m
	}
	mp := peers[i].Projects[j]
	m.count, m.synced = max(mp.NeedsYou, len(mp.Needs)), mp.Synced
	return m
}

// rollUp is program slug's Needs you from its members, in the program's
// order: each member's needs as rows, the same need seen twice folded
// into one (foldNeeds), the program's own open questions (asks) as rows
// that stand for the member questions they cover (programAsks), alike
// questions flagged (flagSimilar), then ranked (rankNeeds).
func rollUp(slug string, members []memberNeeds, asks []project.Question) proto.ProgramNeedsResult {
	res := proto.ProgramNeedsResult{Program: slug, Needs: []proto.ProgramNeed{}, Members: []proto.NeedMember{}}
	order := map[string]int{"/" + slug: -1}
	covered := map[string]bool{} // member questions an open program question covers, as acme-api/Q3
	for _, q := range asks {
		for _, cv := range q.Covers {
			covered[cv.Ref()] = true
		}
	}
	res.Needs = append(res.Needs, programAsks(slug, asks)...)
	for i, m := range members {
		order[m.server+"/"+m.project] = i
		rows := foldNeeds(m)
		rows = slices.DeleteFunc(rows, func(n proto.ProgramNeed) bool {
			return n.Server == "" && n.Kind == proto.NeedAsk && covered[n.Project+"/"+strings.TrimPrefix(n.ID, "ask:")]
		})
		nm := proto.NeedMember{Project: m.project, Server: m.server, Needs: len(rows), Count: m.count, Synced: m.synced, Error: m.err}
		if nm.Needs == 0 && nm.Count == 0 && nm.Error == "" {
			res.Quiet++
		}
		res.Members = append(res.Members, nm)
		res.Needs = append(res.Needs, rows...)
	}
	flagSimilar(res.Needs, members)
	rankNeeds(res.Needs, func(n proto.ProgramNeed) int { return order[n.Server+"/"+n.Project] })
	return res
}

// programAsks are program slug's open questions as rows: the user
// answers each once, for the member questions it covers (Merged).
func programAsks(slug string, asks []project.Question) []proto.ProgramNeed {
	var out []proto.ProgramNeed
	for _, q := range asks {
		n := proto.ProgramNeed{Project: slug, Program: true, Acts: []string{proto.ActAnswer}}
		n.ID, n.Kind, n.Role, n.Since = "ask:"+q.ID, proto.NeedAsk, proto.RoleDirector, q.Asked
		n.Question = &proto.Question{Questions: []proto.QuestionItem{questionItem(q)}}
		for _, cv := range q.Covers {
			n.Merged = append(n.Merged, cv.Ref())
		}
		out = append(out, n)
	}
	return out
}

// questionItem is stored question q as a menu's question.
func questionItem(q project.Question) proto.QuestionItem {
	item := proto.QuestionItem{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
	for _, o := range q.Options {
		item.Options = append(item.Options, proto.QuestionOption{Label: o.Label, Description: o.Description})
	}
	return item
}

// flagSimilar sets Similar on each local member's coordinator question
// row that asks what another member's does (program.Similar): the
// server suggests, the director merges.
func flagSimilar(ns []proto.ProgramNeed, members []memberNeeds) {
	qs := map[string]map[string]project.Question{}
	for _, m := range members {
		if m.server == "" {
			qs[m.project] = m.questions
		}
	}
	question := func(n proto.ProgramNeed) (project.Question, bool) {
		if n.Program || n.Server != "" || n.Kind != proto.NeedAsk {
			return project.Question{}, false
		}
		q, ok := qs[n.Project][strings.TrimPrefix(n.ID, "ask:")]
		return q, ok && q.CoveredBy == ""
	}
	for i := range ns {
		a, ok := question(ns[i])
		if !ok {
			continue
		}
		for j := range ns {
			if j == i || ns[j].Project == ns[i].Project {
				continue
			}
			if b, ok := question(ns[j]); ok && program.Similar(a, b) {
				ns[i].Similar = append(ns[i].Similar, b.ID+"@"+ns[j].Project)
			}
		}
	}
}

// foldNeeds are member m's rows: its needs, a task's coordinator
// questions and thread question folded into the task's own row (review,
// blocked, a red PR) as one need with both acts, then its link needs
// and a failing PR host. The row of a folded group is its best-ranked
// need, Since the group's oldest. An escape or a permission dialog is
// never folded: each is its own stop.
func foldNeeds(m memberNeeds) []proto.ProgramNeed {
	taskRow := map[string]bool{} // tasks with a row of their own
	for _, n := range m.needs {
		if n.Task != "" && (n.Kind == proto.NeedReview || n.Kind == proto.NeedBlocked || n.Kind == proto.NeedCI) {
			taskRow[n.Task] = true
		}
	}
	var out []proto.ProgramNeed
	group := map[string]int{} // task → its row in out
	for _, n := range m.needs {
		foldable := n.Task != "" && taskRow[n.Task] && !n.Escape && n.Kind != proto.NeedApproval && n.Kind != proto.NeedQueue
		if !foldable {
			out = append(out, programNeed(m, n))
			continue
		}
		i, ok := group[n.Task]
		if !ok {
			group[n.Task] = len(out)
			out = append(out, programNeed(m, n))
			continue
		}
		row := &out[i]
		if kindWeight(n.Kind, false) < kindWeight(row.Kind, false) {
			// The better-ranked need leads the row; the one before
			// it folds in.
			n, row.PeerNeed = row.PeerNeed, n
		}
		if !n.Since.IsZero() && (row.Since.IsZero() || n.Since.Before(row.Since)) {
			row.Since = n.Since
		}
		row.Also = append(row.Also, n)
	}
	for i := range out {
		out[i].Acts = rowActs(out[i])
	}
	for _, l := range m.links {
		out = append(out, linkNeed(m, l))
	}
	if m.hostFails > 0 {
		out = append(out, proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "host:" + m.project, Kind: proto.NeedHost}, Project: m.project})
	}
	return out
}

// programNeed is member m's need n as a row.
func programNeed(m memberNeeds, n proto.PeerNeed) proto.ProgramNeed {
	return proto.ProgramNeed{PeerNeed: n, Project: m.project, Server: m.server, Priority: string(m.priority[n.Task])}
}

// linkNeed is member m's link need l as a row, with link.act's actions
// on it.
func linkNeed(m memberNeeds, l link.Need) proto.ProgramNeed {
	n := proto.ProgramNeed{Project: m.project, Server: m.server, Peer: l.Peer, Link: l.ID}
	n.ID, n.Since, n.Title = "link:"+l.Kind+":"+l.Peer+":"+l.ID, l.Created, l.Title
	switch l.Kind {
	case link.NeedHeld:
		n.Kind, n.Acts = proto.NeedLinkHeld, []string{proto.LinkApprove, proto.LinkDrop}
	case link.NeedFile:
		n.Kind, n.Acts, n.Title = proto.NeedLinkFile, []string{proto.LinkAcceptFile, proto.LinkDeclineFile}, l.File
	case link.NeedRequest:
		n.Kind, n.Acts = proto.NeedLinkRequest, []string{proto.LinkAccept, proto.LinkDecline}
	}
	return n
}

// rowActs are row n's acts: its need's, then each folded need's, once.
func rowActs(n proto.ProgramNeed) []string {
	acts := slices.Clone(proto.ActsOf(n.PeerNeed))
	for _, a := range n.Also {
		for _, act := range proto.ActsOf(a) {
			if !slices.Contains(acts, act) {
				acts = append(acts, act)
			}
		}
	}
	return acts
}

// kindWeight ranks a need's kind in the program's queue (§7.1): a
// stopped thread first (an escape, a permission dialog), then a
// thread's question, a task in review, the coordinator's question, a
// red PR or a failing PR host, a link's need, held prompts, and a
// blocked task last.
func kindWeight(kind string, escape bool) int {
	if escape {
		return 0
	}
	switch kind {
	case proto.NeedApproval:
		return 0
	case proto.NeedQuestion:
		return 1
	case proto.NeedReview:
		return 2
	case proto.NeedAsk:
		return 3
	case proto.NeedCI, proto.NeedHost:
		return 4
	case proto.NeedLinkHeld, proto.NeedLinkFile, proto.NeedLinkRequest:
		return 5
	case proto.NeedQueue:
		return 6
	case proto.NeedBlocked:
		return 7
	}
	return 8
}

// rankNeeds sorts ns in the queue's order: by kind (kindWeight), the
// task's priority (must, should, could, none), age (the oldest first,
// an unknown age after a known one), then the member's place in the
// program (member).
func rankNeeds(ns []proto.ProgramNeed, member func(proto.ProgramNeed) int) {
	slices.SortStableFunc(ns, func(a, b proto.ProgramNeed) int {
		if c := cmp.Compare(kindWeight(a.Kind, a.Escape), kindWeight(b.Kind, b.Escape)); c != 0 {
			return c
		}
		if c := cmp.Compare(tasks.Priority(a.Priority).Rank(), tasks.Priority(b.Priority).Rank()); c != 0 {
			return c
		}
		if c := cmp.Compare(boolRank(a.Since.IsZero()), boolRank(b.Since.IsZero())); c != 0 {
			return c
		}
		if c := a.Since.Compare(b.Since); c != 0 {
			return c
		}
		return cmp.Compare(member(a), member(b))
	})
}
