package link

// Program membership across servers (the programs design §9.1): the
// user of the director's server adds a paired server's project to a
// program, and that server's user decides. The invite travels in the
// program stream of the outbox (programpeer.go) as program-invite;
// there it is a program-request item that needs the user, who accepts it
// (choosing what the director may do there, and whether to share the
// project and its needs with that server) or declines it. The answer
// comes back as program-answer. Either side ends the membership with
// program-leave. Each server writes only its own config.toml: the
// director's lists the member, the child's has its program keys. Invites
// are the human's, rate-limited with link requests, and expire as they
// do; while programs are off nothing is sent, taken, or shown.

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Inbox kinds of program membership.
const (
	// KindProgramRequest is a paired server's program inviting a project
	// here: the user's to accept or decline.
	KindProgramRequest = "program-request"
	// KindProgramAnswer tells a program here its invite was answered.
	KindProgramAnswer = "program-answer"
	// KindProgramLeft tells either side the membership ended.
	KindProgramLeft = "program-left"
	// KindProgramUndelivered tells a program here its invite didn't
	// reach the paired server.
	KindProgramUndelivered = "program-undelivered"
)

// InviteLeft is the state of an invite whose membership ended; the
// others are a request's (ReqQueued … ReqUndelivered).
const InviteLeft = "left"

// Invite is a program invite, kept in the store: the newest per folder,
// other side and direction. Out: this server's program invited Peer, a
// paired server's project ("mini/acme-ios"), and Project is the
// program's slug; else Peer is the inviting program ("studio/acme") and
// Project the project here.
type Invite struct {
	ID      string `json:"id"`
	Out     bool   `json:"out"`
	Project string `json:"project"`
	Peer    string `json:"peer"`
	// Features are the ones asked, Granted the ones the answer gave.
	Features []string  `json:"features,omitempty"`
	Granted  []string  `json:"granted,omitempty"`
	Created  time.Time `json:"created"`
	State    string    `json:"state"`
	Answered time.Time `json:"answered,omitzero"`
	Left     time.Time `json:"left,omitzero"`
	// Code is an undelivered invite's refusal; By says who ended a left
	// membership: "here" or "there".
	Code string `json:"code,omitempty"`
	By   string `json:"by,omitempty"`
}

// Pending reports an invite that waits for its answer.
func (r *Invite) Pending() bool { return r.State == ReqQueued || r.State == ReqWaiting }

// Words is the invite as its row says it: "invited, no answer yet",
// "member: tasks, messages, status", "declined", "left (ended there)".
func (r *Invite) Words() string {
	switch r.State {
	case ReqQueued:
		return "invited, not sent yet"
	case ReqWaiting:
		return "invited, no answer yet"
	case ReqAccepted:
		if len(r.Granted) == 0 {
			return "member"
		}
		return "member: " + joinWords(r.Granted)
	case ReqDeclined:
		return "declined"
	case ReqExpired:
		return "invite expired"
	case InviteLeft:
		if r.By == "there" {
			return "left (ended on the other server)"
		}
		return "left (ended here)"
	}
	if w, ok := InviteRefusalWords[r.Code]; ok && r.Code != "refused" {
		return "invite not delivered: " + replaceWords(w, Shown(r.Peer), r.Project)
	}
	return "invite not delivered"
}

func joinWords(ws []string) string {
	out := ""
	for i, w := range ws {
		if i > 0 {
			out += ", "
		}
		out += w
	}
	return out
}

// inviteIDRE is an invite's id: P-<the inviting server's fingerprint>-<n>.
var inviteIDRE = regexp.MustCompile(`^P-([0-9A-F]{4})-[0-9]{1,12}$`)

// ValidInviteID reports whether s is a program invite's id.
func ValidInviteID(s string) bool { return inviteIDRE.MatchString(s) }

// InviteRefusalWords are the codes an invite's delivery fails with, as
// the program's program-undelivered item says them.
var InviteRefusalWords = map[string]string{
	"not-taken":        "{to}'s server doesn't take the invite: it has no active project of that name, or the project is in a program already",
	"rate-limited":     "{to}'s server takes at most 5 link requests and invites an hour from this one; invite again later",
	"expired":          "{to}'s server didn't take it within 72 hours",
	config.ProgramsOff: "{to}'s server has programs off",
	"bad-message":      "{to}'s server refused it as malformed",
	"refused":          "{to}'s server refused it",
}

// AnswerOpts is the user's answer to an invite: what the director may
// do here (the program keys) and the existing switches that make its
// rolled-up needs work; the TUI pre-ticks Share and NeedsView, never
// NeedsAnswer (decision 14).
type AnswerOpts struct {
	Accept bool
	// RaiseNeeds raises the needs switch for the paired server alias through
	// the server, as peer.switch; a raise written only to peers.json doesn't
	// count (docs/SPEC.md §16.8). Nil writes the file (no server to tell).
	RaiseNeeds func(alias, level string) error
	// Tasks is program_tasks (config.ProgramTasksFile or …Propose),
	// Starts program_starts, Status program_status.
	Tasks       string
	Starts      bool
	Status      bool
	Share       bool
	NeedsView   bool
	NeedsAnswer bool
}

// programsOn reads the switch now: the TUI and the CLI run outside the
// server, so they read the file, not SetPrograms.
func programsOnNow() bool {
	cfg, _ := config.Load()
	return features.Programs(cfg)
}

func refuseOff() error { return refuse(config.ProgramsOff, features.OffMsg) }

// inviteFind is invite id of folder slug, out or in.
func (s *store) inviteFind(slug, id string, out bool) *Invite {
	for _, r := range s.Invites {
		if r.ID == id && r.Project == slug && r.Out == out {
			return r
		}
	}
	return nil
}

// inviteOf is folder slug's newest invite with peer, out or in.
func (s *store) inviteOf(slug, peer string, out bool) *Invite {
	for _, r := range slices.Backward(s.Invites) {
		if r.Project == slug && r.Peer == peer && r.Out == out {
			return r
		}
	}
	return nil
}

// inviteKeep puts r in place of the older invite of its folder, peer
// and direction.
func (s *store) inviteKeep(r *Invite) {
	s.Invites = slices.DeleteFunc(s.Invites, func(x *Invite) bool {
		return x.Project == r.Project && x.Peer == r.Peer && x.Out == r.Out
	})
	s.Invites = append(s.Invites, r)
}

// Invites are folder slug's invites both ways, oldest first: a program's
// to its paired members, a project's from paired programs. None while
// programs are off.
func Invites(slug string) []Invite {
	if !programsOnNow() {
		return nil
	}
	var out []Invite
	readStore(func(s *store) {
		for _, r := range s.Invites {
			if r.Project == slug {
				out = append(out, *r)
			}
		}
	})
	return out
}

// askFeatures are the features an invite from program asks: tasks,
// messages and status, and starts when the program delegates.
func askFeatures(cfg *config.Config, program string) []string {
	fs := []string{proto.ProgramFeatureTasks, proto.ProgramFeatureMessages, proto.ProgramFeatureStatus}
	if t, ok := cfg.Program(program); ok && t.Authority == config.AuthorityDelegate {
		fs = append(fs, proto.ProgramFeatureStarts)
	}
	return fs
}

// InviteMember invites member, a paired server's project
// ("mini/acme-ios") that program prog lists, as the human: the program's
// table lists it already (the TUI wrote it). One invite waits per
// member; a member that is already in needs none.
func InviteMember(c caller.Caller, prog *project.Folder, member string) (*Invite, error) {
	if c.IsAgent() {
		return nil, refuse("human-only", "program members are the user's (the program's settings)")
	}
	if !programsOnNow() {
		return nil, refuseOff()
	}
	alias, slug, ok := config.Remote(member)
	if !ok {
		return nil, refuse("not-remote", "an invite goes to a paired server's project (<server>/<project>)")
	}
	d, err := remoteTo(alias, slug)
	if err != nil {
		return nil, err
	}
	fp := d.(remote).fp
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	asked := askFeatures(cfg, prog.Slug)
	t := now()
	var r *Invite
	err = withStore(func(s *store) error {
		if old := s.inviteOf(prog.Slug, member, true); old != nil && (old.Pending() || old.State == ReqAccepted) {
			r = old
			return errUnchanged
		}
		if !spend(s.ReqOut, alias, t) {
			return refuse("rate-limited", "at most %d link requests and invites an hour go to %s; invite again later", RequestsPerHour, alias)
		}
		s.Next++
		r = &Invite{ID: fmt.Sprintf("P-%s-%d", fp, s.Next), Out: true, Project: prog.Slug, Peer: member, Features: asked,
			Created: t.UTC().Truncate(time.Second), State: ReqQueued}
		s.inviteKeep(r)
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerProgramInvite, From: prog.Slug, To: slug, ID: r.ID,
			Created: r.Created, By: "human", Features: asked})
	})
	if err == errUnchanged {
		cp := *r
		return &cp, nil
	}
	if err != nil {
		return nil, err
	}
	prog.Journal(c, "program.invite", member, fmt.Sprintf("%s (%s)", r.ID, joinWords(asked)))
	Kick()
	return r, nil
}

// errUnchanged ends a withStore that has nothing to write.
var errUnchanged = fmt.Errorf("unchanged")

// AnswerInvite answers project p's invite id from a paired server's
// program, as the human: accepting writes p's program keys and the
// switches o ticks; declining sets nothing. Either way the answer goes
// back, and the item is done. peersChanged says the needs switch for
// that server changed in peers.json (the server reads it again).
func AnswerInvite(c caller.Caller, p *project.Project, id string, o AnswerOpts) (r *Invite, peersChanged bool, err error) {
	if c.Kind != caller.Human {
		return nil, false, refuse("human-only", "program invites are the user's to answer (the Messages tab, or tm program accept|decline)")
	}
	if !programsOnNow() {
		return nil, false, refuseOff()
	}
	if o.Accept && o.Tasks != config.ProgramTasksFile && o.Tasks != config.ProgramTasksPropose {
		return nil, false, refuse("bad-value", "program_tasks is %s or %s", config.ProgramTasksFile, config.ProgramTasksPropose)
	}
	var cur Invite
	readStore(func(s *store) {
		if x := s.inviteFind(p.Slug, id, false); x != nil {
			cur = *x
		}
	})
	if cur.ID == "" {
		return nil, false, refuse("unknown-invite", "%s has no program invite %s", p.Slug, id)
	}
	if !cur.Pending() {
		return nil, false, refuse("not-pending", "%s was %s", id, cur.State)
	}
	alias, prog, _ := config.Remote(cur.Peer)
	cfg, err := config.Load()
	if err != nil {
		return nil, false, err
	}
	if o.Accept {
		if l, in := cfg.ProgramLink(p.Slug); in && l.Program != cur.Peer {
			return nil, false, refuse("in-program", "%s is in %s already: a project is in one program at most; it leaves that one first (its settings)", p.Slug, Shown(l.Program))
		}
		if g := cfg.ProgramOf(p.Slug); g != "" {
			return nil, false, refuse("in-program", "%s is in program %s here: a project is in one program at most", p.Slug, g)
		}
		if err := config.SetProgramLink(p.Slug, config.ProgramLink{Program: cur.Peer, Tasks: o.Tasks, Starts: o.Starts, Status: o.Status}); err != nil {
			return nil, false, err
		}
		p.Journal(c, "settings.program", p.Slug, cur.Peer)
		if o.Share {
			if sf, err := cfg.Safety(p.Slug); err == nil && !sf.Share {
				if err := config.SetProject(p.Slug, "share", true); err != nil {
					return nil, false, err
				}
				p.Journal(c, "settings.share", p.Slug, "true")
			}
		}
		if want := needsWord(o); want != "" {
			dir := peer.DirIn(homeDir())
			if peers, err := peer.LoadPeers(dir); err == nil {
				for _, q := range peers {
					if q.Alias == alias && needsRank(q.NeedsLevel()) < needsRank(want) {
						if o.RaiseNeeds != nil {
							err = o.RaiseNeeds(alias, want)
						} else {
							_, err = peer.SetNeeds(dir, alias, want)
						}
						if err != nil {
							return nil, false, err
						}
						peersChanged = true
					}
				}
			}
		}
	}
	granted := grants(cur.Features, o)
	err = withStore(func(s *store) error {
		x := s.inviteFind(p.Slug, id, false)
		if x == nil || !x.Pending() {
			return refuse("not-pending", "%s was answered meanwhile", id)
		}
		x.State, x.Answered = ReqDeclined, now().UTC().Truncate(time.Second)
		allow := proto.ProgramDeclined
		if o.Accept {
			x.State, x.Granted, allow = ReqAccepted, granted, proto.ProgramAccepted
		}
		cp := *x
		r = &cp
		if _, err := remoteTo(alias, ""); err != nil {
			return nil // a server unpaired since keeps the answer here only
		}
		m := proto.PeerMessage{Kind: proto.PeerProgramAnswer, From: p.Slug, To: prog, ID: id, By: "human", Allow: allow}
		if o.Accept {
			m.Features = granted
		}
		return s.queue(alias, m)
	})
	if err != nil {
		return nil, peersChanged, err
	}
	doneInvite(&p.Folder, id)
	if o.Accept {
		p.Journal(c, "program.accept", cur.Peer, fmt.Sprintf("%s (%s)", id, joinWords(granted)))
	} else {
		p.Journal(c, "program.decline", cur.Peer, id)
	}
	Kick()
	return r, peersChanged, nil
}

// grants are the asked features the answer o gives: tasks and messages
// with membership, status and starts as their keys say.
func grants(asked []string, o AnswerOpts) []string {
	if !o.Accept {
		return nil
	}
	var out []string
	for _, f := range asked {
		switch {
		case f == proto.ProgramFeatureStatus && !o.Status, f == proto.ProgramFeatureStarts && !o.Starts:
		case slices.Contains(proto.ProgramFeatures, f):
			out = append(out, f)
		}
	}
	return out
}

func needsWord(o AnswerOpts) string {
	switch {
	case !o.Accept:
		return ""
	case o.NeedsAnswer:
		return proto.NeedsAnswer
	case o.NeedsView:
		return proto.NeedsView
	}
	return ""
}

// needsRank orders the needs levels: off, view, answer. Accepting only
// raises one.
func needsRank(level string) int {
	switch level {
	case proto.NeedsView:
		return 1
	case proto.NeedsAnswer:
		return 2
	}
	return 0
}

func homeDir() string {
	d, _ := home.Dir()
	return d
}

// Leave ends folder f's membership with other, as the human: a project
// here leaving the paired program other ("studio/acme"), whose program
// keys go; or program f here dropping its paired member other
// ("mini/acme-ios"), which the caller has taken off the program's table.
// The other side hears of it with program-leave. A pending invite ends
// too. Leaving what isn't a membership changes nothing.
func Leave(c caller.Caller, f *project.Folder, other string) (*Invite, error) {
	if c.IsAgent() {
		return nil, refuse("human-only", "program membership is the user's")
	}
	if !programsOnNow() {
		return nil, refuseOff()
	}
	alias, slug, ok := config.Remote(other)
	if !ok {
		return nil, refuse("not-remote", "%s isn't on a paired server", other)
	}
	if !f.IsProgram() {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		if l, in := cfg.ProgramLink(f.Slug); in && l.Program == other {
			if err := config.UnsetProgramLink(f.Slug); err != nil {
				return nil, err
			}
			f.Journal(c, "settings.program", f.Slug, "(none)")
		}
	}
	out := f.IsProgram()
	var r *Invite
	err := withStore(func(s *store) error {
		x := s.inviteOf(f.Slug, other, out)
		id := ""
		if x != nil && (x.Pending() || x.State == ReqAccepted) {
			x.State, x.Left, x.By, id = InviteLeft, now().UTC().Truncate(time.Second), "here", x.ID
			cp := *x
			r = &cp
		}
		if !out && x == nil {
			// Set by hand: a membership without an invite here.
			s.Next++
			if d, err := remoteTo(alias, slug); err == nil {
				id = fmt.Sprintf("P-%s-%d", d.(remote).fp, s.Next)
			}
		}
		if id == "" {
			return errUnchanged
		}
		if _, err := remoteTo(alias, slug); err != nil {
			return nil
		}
		return s.queue(alias, proto.PeerMessage{Kind: proto.PeerProgramLeave, From: f.Slug, To: slug, ID: id, By: "human"})
	})
	if err == errUnchanged {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r != nil {
		doneInvite(f, r.ID)
	}
	f.Journal(c, "program.leave", other, "")
	Kick()
	return r, nil
}

// doneInvite moves the program-request items of invite id to done.
func doneInvite(f *project.Folder, id string) {
	items, _ := f.Inbox()
	for _, it := range items {
		if it.Kind == KindProgramRequest && it.Message == id {
			f.DoneItem(it.ID)
		}
	}
}

// InviteSummary is an incoming invite's item: fixed words.
func InviteSummary(r *Invite) string {
	return fmt.Sprintf("%s invites %s into its program, asking for %s; accept or decline it in the Messages tab (or tm program accept|decline %s)",
		Shown(r.Peer), r.Project, joinWords(r.Features), r.ID)
}

// receiveInvite takes invite m from program m.From on the server alias
// for project m.To here; the store is locked. A project that isn't
// there, is archived, or is in a program already answers alike,
// not-taken.
func (s *store) receiveInvite(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := inviteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || m.By != "human" || !config.ValidProgramSlug(m.From) {
		return bad
	}
	var asked []string
	for _, f := range m.Features {
		if !slices.Contains(proto.ProgramFeatures, f) || slices.Contains(asked, f) {
			return bad
		}
		asked = append(asked, f)
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	notTaken := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-taken"}
	to, err := target(cfg, m.To)
	if err != nil {
		return notTaken
	}
	key := alias + "/" + m.From
	if l, in := cfg.ProgramLink(to.Slug); in && l.Program != key || cfg.ProgramOf(to.Slug) != "" {
		return notTaken
	}
	if s.inviteFind(to.Slug, m.ID, false) != nil {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	t := now()
	if !spend(s.ReqIn, alias, t) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "rate-limited"}
	}
	if old := s.inviteOf(to.Slug, key, false); old != nil && old.Pending() {
		doneInvite(&to.Folder, old.ID)
	}
	r := &Invite{ID: m.ID, Project: to.Slug, Peer: key, Features: asked, Created: t.UTC().Truncate(time.Second), State: ReqWaiting}
	s.inviteKeep(r)
	to.AddMessageItem(KindProgramRequest, key, InviteSummary(r), true, r.ID)
	to.Journal(caller.ForPeer(alias), "program.invited", key, fmt.Sprintf("%s (%s)", r.ID, joinWords(asked)))
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// receiveProgramAnswer takes the answer to invite m.ID, which program
// m.To here sent to project m.From on the server alias; the store is
// locked. An answer to no invite here goes quietly.
func (s *store) receiveProgramAnswer(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if !ValidInviteID(m.ID) || m.Allow != proto.ProgramAccepted && m.Allow != proto.ProgramDeclined {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	key := alias + "/" + m.From
	r := s.inviteFind(m.To, m.ID, true)
	if r == nil || r.Peer != key || !r.Pending() {
		return ok
	}
	r.State, r.Answered = ReqDeclined, now().UTC().Truncate(time.Second)
	if m.Allow == proto.ProgramAccepted {
		r.State, r.Granted = ReqAccepted, nil
		for _, f := range m.Features {
			// Only what was asked: the answer can't widen it.
			if slices.Contains(r.Features, f) && !slices.Contains(r.Granted, f) {
				r.Granted = append(r.Granted, f)
			}
		}
	}
	g := programFolder(m.To)
	if g == nil {
		return ok
	}
	if r.State == ReqAccepted {
		g.AddMessageItem(KindProgramAnswer, key, fmt.Sprintf("%s accepted %s's invite %s: %s", Shown(key), g.Slug, r.ID, r.Words()), false, r.ID)
		g.Journal(caller.ForPeer(alias), "program.accepted", key, fmt.Sprintf("%s (%s)", r.ID, joinWords(r.Granted)))
	} else {
		g.AddMessageItem(KindProgramAnswer, key, fmt.Sprintf("%s declined %s's invite %s; the program still lists it until you take it off", Shown(key), g.Slug, r.ID), false, r.ID)
		g.Journal(caller.ForPeer(alias), "program.declined", key, r.ID)
	}
	return ok
}

// receiveProgramLeave takes the end of a membership from the server
// alias; the store is locked. To a program here (its member m.From
// left) the invite is marked left and the program's table is left as
// the user wrote it; to a project here (program m.From dropped it) its
// program keys go. A leave of nothing here goes quietly.
func (s *store) receiveProgramLeave(alias string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if !ValidInviteID(m.ID) || !config.ValidProgramSlug(m.To) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	key := alias + "/" + m.From
	t := now().UTC().Truncate(time.Second)
	end := func(r *Invite) {
		if r != nil && (r.Pending() || r.State == ReqAccepted) {
			r.State, r.Left, r.By = InviteLeft, t, "there"
		}
	}
	if g := programFolder(m.To); g != nil {
		if r := s.inviteOf(m.To, key, true); r != nil {
			end(r)
			g.AddMessageItem(KindProgramLeft, key, fmt.Sprintf("%s left program %s; take it off the members in the program's settings", Shown(key), g.Slug), false, r.ID)
			g.Journal(caller.ForPeer(alias), "program.left", key, r.ID)
		}
		return ok
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	p, err := project.Open(m.To)
	if err != nil {
		return ok
	}
	r := s.inviteOf(p.Slug, key, false)
	if r != nil && r.Pending() {
		doneInvite(&p.Folder, r.ID)
	}
	end(r)
	if l, in := cfg.ProgramLink(p.Slug); in && l.Program == key {
		if err := config.UnsetProgramLink(p.Slug); err != nil {
			return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
		}
		p.AddMessageItem(KindProgramLeft, key, fmt.Sprintf("%s took %s out of its program; its program settings are gone", Shown(key), p.Slug), false, m.ID)
		p.Journal(caller.ForPeer(alias), "program.left", key, m.ID)
	}
	return ok
}

// inviteDelivered marks the program's invite waiting: that server took
// it.
func inviteDelivered(alias string, m proto.PeerMessage) {
	if m.Kind != proto.PeerProgramInvite {
		return
	}
	withStore(func(s *store) error {
		if r := s.inviteFind(m.From, m.ID, true); r != nil && r.State == ReqQueued {
			r.State = ReqWaiting
		}
		return nil
	})
}

// inviteUndelivered marks the program's invite undelivered and tells its
// inbox, in fixed words for code. An answer or a leave that isn't taken
// goes quietly.
func inviteUndelivered(alias string, m proto.PeerMessage, code string) {
	if m.Kind != proto.PeerProgramInvite {
		return
	}
	words, known := InviteRefusalWords[code]
	if !known {
		code, words = "refused", InviteRefusalWords["refused"]
	}
	key := alias + "/" + m.To
	found := false
	withStore(func(s *store) error {
		if r := s.inviteFind(m.From, m.ID, true); r != nil && r.Pending() {
			r.State, r.Code, found = ReqUndelivered, code, true
		}
		return nil
	})
	g := programFolder(m.From)
	if !found || g == nil {
		return
	}
	g.AddMessageItem(KindProgramUndelivered, key, fmt.Sprintf("your invite %s to %s wasn't delivered: %s", m.ID, Shown(key), replaceWords(words, key, g.Slug)), false, m.ID)
	g.Journal(caller.Caller{Kind: caller.Ticker}, "program.undelivered", key, m.ID+" ("+code+")")
}

// ExpireInvites ends the invites that waited RequestExpiry for their
// answer: an incoming one's item is done. It writes the store only when
// one expired.
func ExpireInvites() {
	t := now()
	old := func(r *Invite) bool { return r.State == ReqWaiting && t.Sub(r.Created) > RequestExpiry }
	some := false
	readStore(func(s *store) { some = slices.ContainsFunc(s.Invites, old) })
	if !some {
		return
	}
	var gone []Invite
	withStore(func(s *store) error {
		for _, r := range s.Invites {
			if old(r) {
				r.State = ReqExpired
				gone = append(gone, *r)
			}
		}
		return nil
	})
	for _, r := range gone {
		var f *project.Folder
		if r.Out {
			f = programFolder(r.Project)
		} else if p, err := project.Open(r.Project); err == nil {
			f = &p.Folder
			doneInvite(f, r.ID)
		}
		if f != nil {
			f.Journal(caller.Caller{Kind: caller.Ticker}, "program.invite-expired", r.Peer, r.ID)
		}
	}
}
