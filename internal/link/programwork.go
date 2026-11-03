package link

// A program's work with its members on paired servers (the programs
// design §9.2, §9.3), once a membership is accepted (programinvite.go):
//
//   - the director files and changes the member's tasks (program-task),
//     and hears back what became of each (program-task-filed);
//   - the program channel crosses as program-message, both ways;
//   - the member pushes the state of the tasks the director filed or
//     referenced (program-status), while its program_status is on;
//   - the director starts threads there (program-start, answered by
//     program-start-result), only while the member's program_starts is
//     on, and within that server's caps and start rules;
//   - a renamed program tells its members (program-rename).
//
// All of them travel in the program stream of the outbox
// (programpeer.go). The member's server enforces everything for its
// project: the director there is caller.ForPeerDirector, journaled
// "peer:<alias> (director <program>)", with no right beyond a local
// director's on a member. Every string that arrives is cleaned and
// capped before it is stored or drawn; ids, refs and slugs match their
// patterns or the message is refused bad-message.

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Inbox kinds of the program's work with paired members.
const (
	// KindProgramTask is the member's item when the director filed,
	// changed or proposes one of its tasks (as program.KindTask).
	KindProgramTask = "program-task"
	// KindProgramTaskFiled tells a program here what became of a task
	// it sent to a paired member.
	KindProgramTaskFiled = "program-task-filed"
	// KindProgramStartResult tells a program here what became of a
	// start it sent to a paired member.
	KindProgramStartResult = "program-start-result"
)

// Limits of what crosses (§9.2).
const (
	maxProgramTitle  = 200
	maxProgramNotes  = 4 << 10
	maxProgramSteps  = 50
	maxProgramStep   = 200
	maxProgramAfter  = 20
	maxStatusTitle   = 120
	maxProgramWord   = 64
	maxProgramSent   = 200
	maxRemoteTasks   = 2000
	statusPushPeriod = 15 * time.Second
)

// The states of a ProgramSent besides the answers' words
// (proto.ProgramFiled …).
const (
	SentPending     = "pending"
	SentUndelivered = "undelivered"
)

// ProgramSent is a task, change or start a program here sent to a paired
// member, kept until its answer (the newest maxProgramSent).
type ProgramSent struct {
	ID string `json:"id"`
	// Kind is "task", "edit" or "start".
	Kind    string `json:"kind"`
	Program string `json:"program"`
	// Member is the paired project, "mini/acme-ios"; Ref its task once
	// known ("T7"), Key the route's key, Title the task's.
	Member   string    `json:"member"`
	Ref      string    `json:"ref,omitempty"`
	Key      string    `json:"key,omitempty"`
	Title    string    `json:"title,omitempty"`
	Umbrella string    `json:"umbrella,omitempty"`
	Created  time.Time `json:"created"`
	// State is SentPending until the answer: its Allow word
	// (proto.ProgramFiled …), or SentUndelivered with Code.
	State    string    `json:"state"`
	Code     string    `json:"code,omitempty"`
	Thread   string    `json:"thread,omitempty"`
	Answered time.Time `json:"answered,omitzero"`
}

// RemoteTask is a paired member's task as it last pushed it
// (program-status): what the program's dependencies and dashboards
// read.
type RemoteTask struct {
	Status  string    `json:"status"`
	Title   string    `json:"title,omitempty"`
	PR      int       `json:"pr,omitempty"`
	URL     string    `json:"url,omitempty"`
	Merged  bool      `json:"merged,omitempty"`
	Changed time.Time `json:"changed,omitzero"`
	Seen    time.Time `json:"seen"`
}

// ProgramStarter starts a thread in project p here for task id, on a
// paired server's director's start (c is caller.ForPeerDirector): tm
// thread start's checks and launch, the caps and p's start rules
// included. It returns the thread's id. The server sets it (package
// cli); without it a start is refused.
var ProgramStarter func(c caller.Caller, p *project.Project, id int, agent, model string) (string, error)

// ThreadPR says what the ticker last saw of thread id of project slug's
// PR: its number, URL and whether it merged. The server sets it; nil
// knows none.
var ThreadPR func(slug, thread string) (number int, url string, merged bool)

// --- the director's side ---

// memberGrants are the features paired member ("mini/acme-ios") of
// program prog granted, from its accepted invite; ok is false while it
// isn't an accepted member.
func (s *store) memberGrants(prog, member string) ([]string, bool) {
	r := s.inviteOf(prog, member, true)
	if r == nil || r.State != ReqAccepted {
		return nil, false
	}
	return r.Granted, true
}

// MemberGrants are the features paired member of program prog granted;
// ok is false while it isn't an accepted member (invited, declined,
// left).
func MemberGrants(prog, member string) (granted []string, ok bool) {
	readStore(func(s *store) { granted, ok = s.memberGrants(prog, member) })
	return granted, ok
}

// MemberState is a paired member's invite in words ("invited, no answer
// yet"), "not invited" when it has none.
func MemberState(prog, member string) string {
	w := "not invited"
	readStore(func(s *store) {
		if r := s.inviteOf(prog, member, true); r != nil {
			w = r.Words()
		}
	})
	return w
}

// checkMember refuses a program write to paired member unless it
// accepted, with feature granted; the store is locked.
func (s *store) checkMember(prog, member, feature string) error {
	granted, ok := s.memberGrants(prog, member)
	if !ok {
		w := "not invited"
		if r := s.inviteOf(prog, member, true); r != nil {
			w = r.Words()
		}
		return refuse("not-accepted", "%s isn't a member of %s yet (%s): program work goes there once its user accepts the invite", Shown(member), prog, w)
	}
	if !slices.Contains(granted, feature) {
		return refuse("not-granted", "%s's user didn't grant %s: %s's program settings there decide", Shown(member), featureWords(feature), Shown(member))
	}
	return nil
}

func featureWords(f string) string {
	switch f {
	case proto.ProgramFeatureTasks:
		return "filing tasks"
	case proto.ProgramFeatureStarts:
		return "starting threads"
	case proto.ProgramFeatureStatus:
		return "status pushes"
	}
	return f
}

// directorBy is who a program's work is sent as.
func directorBy(c caller.Caller) string {
	if c.IsAgent() {
		return "director"
	}
	return "human"
}

// checkOutTask checks the task part of a program-task before it goes:
// the receiver's limits, so a refusal comes now rather than over the
// link.
func checkOutTask(t *proto.ProgramTask) error {
	if len([]rune(t.Title)) > maxProgramTitle {
		return refuse("too-long", "a title to a paired server is at most %d characters", maxProgramTitle)
	}
	if len(t.Notes) > maxProgramNotes {
		return refuse("too-long", "notes to a paired server are at most %s", KB(maxProgramNotes))
	}
	if len(t.Steps) > maxProgramSteps {
		return refuse("too-long", "at most %d steps go to a paired server", maxProgramSteps)
	}
	for _, st := range t.Steps {
		if len([]rune(st)) > maxProgramStep {
			return refuse("too-long", "a step to a paired server is at most %d characters", maxProgramStep)
		}
	}
	if len(t.After) > maxProgramAfter {
		return refuse("too-long", "at most %d after refs go to a paired server", maxProgramAfter)
	}
	for _, w := range []string{t.Priority, t.Status, t.Repo, t.Design, t.Agent, t.Model} {
		if len(w) > maxProgramWord {
			return refuse("too-long", "%q is longer than %d characters", textsafe.Line(w, 20), maxProgramWord)
		}
	}
	if e := t.Edit; e != nil {
		if e.Title != nil && len([]rune(*e.Title)) > maxProgramTitle {
			return refuse("too-long", "a title to a paired server is at most %d characters", maxProgramTitle)
		}
		if len(e.Note) > maxProgramNotes {
			return refuse("too-long", "a note to a paired server is at most %s", KB(maxProgramNotes))
		}
		if e.After != nil && len(*e.After) > maxProgramAfter {
			return refuse("too-long", "at most %d after refs go to a paired server", maxProgramAfter)
		}
	}
	return nil
}

// sendWork queues a program's work kind to paired member of folder prog
// as c, once the member granted feature; it records it to await its
// answer when kind is "task", "edit" or "start".
func sendWork(c caller.Caller, prog *project.Folder, member, feature, kind string, m proto.PeerMessage) (*ProgramSent, error) {
	if c.Kind == caller.Thread {
		return nil, refuse("director-only", "program work is the director's")
	}
	if !programsOnNow() {
		return nil, refuseOff()
	}
	alias, slug, ok := config.Remote(member)
	if !ok {
		return nil, refuse("not-remote", "%s isn't on a paired server", member)
	}
	d, err := remoteTo(alias, slug)
	if err != nil {
		return nil, err
	}
	r := d.(remote)
	t := now()
	var sent *ProgramSent
	err = withStore(func(s *store) error {
		if err := s.checkMember(prog.Slug, member, feature); err != nil {
			return err
		}
		m.ID, m.From, m.To, m.By = r.newID(s), prog.Slug, slug, directorBy(c)
		m.Created = t.UTC().Truncate(time.Second)
		sent = &ProgramSent{ID: m.ID, Kind: kind, Program: prog.Slug, Member: member, Created: m.Created, State: SentPending}
		if m.Task != nil {
			sent.Ref, sent.Key, sent.Title, sent.Umbrella = m.Task.Ref, m.Task.Key, textsafe.Line(m.Task.Title, maxProgramTitle), m.Task.Umbrella
		}
		s.ProgramSent = append(s.ProgramSent, sent)
		if n := len(s.ProgramSent) - maxProgramSent; n > 0 {
			s.ProgramSent = slices.Delete(s.ProgramSent, 0, n)
		}
		return s.queue(alias, m)
	})
	if err != nil {
		return nil, err
	}
	Kick()
	cp := *sent
	return &cp, nil
}

// SendProgramTask files task t in paired member ("mini/acme-ios") of
// program prog, as c (its director, or the human): queued as
// program-task. The member's server files it, or makes it an item its
// coordinator proposes; a program-task-filed item here says which.
func SendProgramTask(c caller.Caller, prog *project.Folder, member string, t proto.ProgramTask) (*ProgramSent, error) {
	t.Ref, t.Edit = "", nil
	if textsafe.Line(t.Title, 0) == "" {
		return nil, refuse("empty-title", "title is empty")
	}
	if err := checkOutTask(&t); err != nil {
		return nil, err
	}
	sent, err := sendWork(c, prog, member, proto.ProgramFeatureTasks, "task", proto.PeerMessage{Kind: proto.PeerProgramTask, Task: &t})
	if err != nil {
		return nil, err
	}
	prog.Journal(c, "program.route", member, fmt.Sprintf("%s %s (queued)", sent.ID, textsafe.Line(t.Title, 80)))
	return sent, nil
}

// SendProgramEdit changes task ref ("T7") of paired member of program
// prog as c: queued as program-task with the change.
func SendProgramEdit(c caller.Caller, prog *project.Folder, member, ref string, e proto.ProgramTaskEdit) (*ProgramSent, error) {
	if !tasks.ValidRef(ref) {
		return nil, refuse("invalid-ref", "%q is not T12", ref)
	}
	t := proto.ProgramTask{Ref: ref, Edit: &e}
	if err := checkOutTask(&t); err != nil {
		return nil, err
	}
	sent, err := sendWork(c, prog, member, proto.ProgramFeatureTasks, "edit", proto.PeerMessage{Kind: proto.PeerProgramTask, Task: &t})
	if err != nil {
		return nil, err
	}
	prog.Journal(c, "program.task.edit", member+"/"+ref, sent.ID+" (queued)")
	return sent, nil
}

// SendProgramStart starts a thread for task ref of paired member of
// program prog, as c: queued as program-start. allow says why the
// director may (proto.ProgramStartDelegate or …Approved); the member's
// program_starts, caps and start rules decide there.
func SendProgramStart(c caller.Caller, prog *project.Folder, member, ref, agent, model, allow string) (*ProgramSent, error) {
	if !tasks.ValidRef(ref) {
		return nil, refuse("invalid-ref", "%q is not T12", ref)
	}
	if allow != proto.ProgramStartDelegate && allow != proto.ProgramStartApproved {
		return nil, refuse("needs-approval", "a start on a paired member needs the program's delegate authority or the user's word")
	}
	t := proto.ProgramTask{Ref: ref, Agent: agent, Model: model}
	if err := checkOutTask(&t); err != nil {
		return nil, err
	}
	sent, err := sendWork(c, prog, member, proto.ProgramFeatureStarts, "start", proto.PeerMessage{Kind: proto.PeerProgramStart, Allow: allow, Task: &t})
	if err != nil {
		return nil, err
	}
	prog.Journal(c, "program.start", member+"/"+ref, sent.ID+" (queued, "+allow+")")
	return sent, nil
}

// Sent are program prog's tasks, changes and starts sent to paired
// members, oldest first.
func Sent(prog string) []ProgramSent {
	var out []ProgramSent
	readStore(func(s *store) {
		for _, x := range s.ProgramSent {
			if x.Program == prog {
				out = append(out, *x)
			}
		}
	})
	return out
}

// RemoteTaskOf is paired member task ref's last pushed state
// ("mini/acme-ios/T7"), and whether one came.
func RemoteTaskOf(ref string) (RemoteTask, bool) {
	var t RemoteTask
	ok := false
	readStore(func(s *store) {
		if x := s.RemoteTasks[ref]; x != nil {
			t, ok = *x, true
		}
	})
	return t, ok
}

// SendProgramRemote sends text on the program channel between folder f
// here and other on a paired server, as c: a program here writing to its
// paired member ("mini/acme-ios"), or a project here writing to the
// paired program it is in ("studio/acme"). As SendProgram: text only,
// the breaker and the rate (refused over it here; over the other
// server's, the outbox keeps it in order until that side takes it).
func SendProgramRemote(c caller.Caller, f *project.Folder, other string, o Opts) (*Message, error) {
	if c.Kind == caller.Thread {
		return nil, refuse("coordinator-only", "the program channel is the coordinators' and the director's")
	}
	if !programsOnNow() {
		return nil, refuseOff()
	}
	if len(o.Attach) > 0 || o.File != "" {
		return nil, refuse("bad-attachment", "program messages carry text only: cite the file instead, or send it with tm link send --file over a link")
	}
	alias, slug, ok := config.Remote(other)
	if !ok {
		return nil, refuse("not-remote", "%s isn't on a paired server", other)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if !f.IsProgram() {
		if l, in := cfg.ProgramLink(f.Slug); !in || l.Program != other {
			return nil, refuse("not-member", "%s is not in program %s", f.Slug, Shown(other))
		}
	} else if cfg.ProgramOf(other) != f.Slug {
		return nil, refuse("not-member", "%s is not a member of program %s", Shown(other), f.Slug)
	}
	text := Clean(o.Text)
	if text == "" {
		return nil, refuse("empty", "the message has no text")
	}
	l := limitsOf(cfg, f.Slug)
	if len(text) > l.maxText {
		return nil, refuse("too-long", "message is %s, at most %s: shorten it", KB(len(text)), KB(l.maxText))
	}
	if o.Re != "" {
		if _, err := Read(f, o.Re); !ValidID(o.Re) || err != nil {
			return nil, refuse("unknown-message", "no message %s between %s and %s", o.Re, f.Slug, Shown(other))
		}
	}
	d, err := remoteTo(alias, slug)
	if err != nil {
		return nil, err
	}
	r := d.(remote)
	m := &Message{Out: true, Created: now().UTC().Truncate(time.Second), Re: o.Re, Bytes: len(text), By: programBy(c), Peer: other, Text: text}
	err = withStore(func(s *store) error {
		if f.IsProgram() {
			if err := s.checkMember(f.Slug, other, proto.ProgramFeatureMessages); err != nil {
				return err
			}
		}
		if err := s.allow(f.Slug, other); err != nil {
			return err
		}
		t := now()
		if s.over(f.Slug, other, l, t) {
			return refuse("rate-limited", "%s has sent its %d messages an hour to %s: wait, or say it in one message", f.Slug, l.perHour, Shown(other))
		}
		m.ID, m.State = r.newID(s), StateQueued
		if err := write(f, m); err != nil {
			return err
		}
		pm := proto.PeerMessage{Kind: proto.PeerProgramMessage, From: f.Slug, To: slug, ID: m.ID, Created: m.Created, Re: m.Re, By: m.By, Text: m.Text}
		if err := s.queue(alias, pm); err != nil {
			return err
		}
		s.account(c.IsAgent(), f.Slug, other, l, t, side{f, other})
		return nil
	})
	if err != nil {
		return nil, err
	}
	f.Journal(c, "program.send", other, fmt.Sprintf("%s (%s, queued)", m.ID, KB(m.Bytes)))
	Kick()
	return m, nil
}

// remoteProgramSummary is the item of a program message from a paired
// server: fixed words, never the text.
func remoteProgramSummary(key string, fromProgram bool, m *Message) string {
	who := Shown(key) + "'s coordinator"
	how := "tm program read " + m.ID
	if fromProgram {
		who, how = "the director of "+Shown(key), "tm link read "+m.ID
	}
	if m.By == "human" {
		who = "the user, for " + Shown(key)
	}
	extra := KB(m.Bytes)
	if m.Re != "" {
		extra += ", re " + m.Re
	}
	return fmt.Sprintf("%s wrote %s (%s); read it with %s", who, m.ID, extra, how)
}

// ProgramRenamed tells the paired members of program from, renamed to
// to here, its new slug (program-rename), and moves its invites to the
// new name. A tm program rename calls it once the folder moved.
func ProgramRenamed(c caller.Caller, from, to string) error {
	if !config.ValidProgramSlug(from) || !config.ValidProgramSlug(to) {
		return refuse("invalid-program", "a program slug is a-z, 0-9 and -")
	}
	var aliases []string
	err := withStore(func(s *store) error {
		for _, r := range s.Invites {
			if !r.Out || r.Project != from {
				continue
			}
			r.Project = to
			if r.State != ReqAccepted && !r.Pending() {
				continue
			}
			alias, slug, ok := config.Remote(r.Peer)
			if !ok {
				continue
			}
			d, err := remoteTo(alias, slug)
			if err != nil {
				continue
			}
			m := proto.PeerMessage{Kind: proto.PeerProgramRename, From: from, To: slug, ID: d.(remote).newID(s), By: "human", Rename: to}
			if err := s.queue(alias, m); err != nil {
				return err
			}
			aliases = append(aliases, r.Peer)
		}
		for _, x := range s.ProgramSent {
			if x.Program == from {
				x.Program = to
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if g := programFolder(to); g != nil && len(aliases) > 0 {
		g.Journal(c, "program.rename", from, "told "+strings.Join(aliases, ", "))
	}
	Kick()
	return nil
}

// --- what arrives ---

// childOf is the project here that program prog on the server alias
// directs: m.To, a project whose program keys name alias/prog. nil when
// it isn't (gone, archived, left, or never a member).
func childOf(cfg *config.Config, alias, prog, slug string) (*project.Project, config.ProgramLink) {
	to, err := target(cfg, slug)
	if err != nil {
		return nil, config.ProgramLink{}
	}
	l, in := cfg.ProgramLink(to.Slug)
	if !in || l.Program != alias+"/"+prog {
		return nil, config.ProgramLink{}
	}
	return to, l
}

// programRefs reads the after refs a director sent for a task here:
// "T3" (this board's) stays, "acme-api/T12" (a project on the
// director's server) becomes "<alias>/acme-api/T12", kept as text. Any
// other is malformed.
func programRefs(alias string, refs []string) ([]string, bool) {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if tasks.ValidRef(r) {
			out = append(out, r)
			continue
		}
		a, slug, n, ok := tasks.CrossRef(r)
		if !ok || a != "" {
			return nil, false
		}
		out = append(out, fmt.Sprintf("%s/%s/T%d", alias, slug, n))
	}
	return out, true
}

// cleanTask is t's strings as they are kept here: lines cleaned and
// capped, notes cleaned and capped; ok is false when an id or ref
// doesn't match its pattern or a list is over its limit.
func cleanTask(t *proto.ProgramTask) bool {
	if t.Ref != "" && !tasks.ValidRef(t.Ref) || len(t.Steps) > maxProgramSteps || len(t.After) > maxProgramAfter {
		return false
	}
	t.Key = textsafe.Line(t.Key, 32)
	t.Title = textsafe.Line(t.Title, maxProgramTitle)
	t.Notes = textsafe.Cut(Clean(t.Notes), maxProgramNotes)
	for i, st := range t.Steps {
		t.Steps[i] = textsafe.Line(st, maxProgramStep)
	}
	for _, w := range []*string{&t.Priority, &t.Status, &t.Repo, &t.Design, &t.Agent, &t.Model, &t.Thread} {
		*w = textsafe.Line(*w, maxProgramWord)
	}
	if t.Umbrella != "" {
		if _, _, _, ok := tasks.CrossRef(t.Umbrella); !ok {
			t.Umbrella = ""
		}
	}
	if e := t.Edit; e != nil {
		for _, w := range []**string{&e.Priority, &e.Design, &e.Repo, &e.Status} {
			if *w != nil {
				v := textsafe.Line(**w, maxProgramWord)
				*w = &v
			}
		}
		if e.Title != nil {
			v := textsafe.Line(*e.Title, maxProgramTitle)
			e.Title = &v
		}
		e.Note = textsafe.Line(e.Note, maxProgramNotes)
		if e.After != nil && len(*e.After) > maxProgramAfter {
			return false
		}
	}
	return true
}

// answer queues the answer kind to message m from the server alias:
// From and To swapped, Re its id; the store is locked.
func (s *store) answer(alias, fp string, m proto.PeerMessage, kind, allow, code string, t *proto.ProgramTask) {
	if _, err := remoteTo(alias, ""); err != nil {
		return
	}
	s.Next++
	s.queue(alias, proto.PeerMessage{Kind: kind, From: m.To, To: m.From, ID: fmt.Sprintf("M-%s-%d", fp, s.Next), Re: m.ID,
		Created: now().UTC().Truncate(time.Second), By: "human", Allow: allow, Code: code, Task: t})
}

// selfFP is this server's fingerprint, for the ids of answers.
func selfFP() string {
	h, err := home.Dir()
	if err != nil {
		return ""
	}
	id, err := peer.LoadIdentity(peer.DirIn(h))
	if err != nil {
		return ""
	}
	return fingerprint(id.ID)
}

// watch adds task id of project slug to what its program status pushes
// cover; the store is locked.
func (s *store) watch(slug string, id int) {
	if s.Watch == nil {
		s.Watch = map[string][]int{}
	}
	if !slices.Contains(s.Watch[slug], id) {
		s.Watch[slug] = append(s.Watch[slug], id)
		slices.Sort(s.Watch[slug])
	}
}

// overProgram is the rate of the program's work into project slug from
// key: a retry with when, or nil to go on; it spends a token.
func (s *store) overProgram(cfg *config.Config, key, slug string, t time.Time) *proto.PeerMessageResult {
	l := limitsOf(cfg, slug)
	if b := s.refill(key, slug, l, t); b.Tokens < 1 {
		wait := time.Duration((1 - b.Tokens) / float64(l.perHour) * float64(time.Hour))
		return &proto.PeerMessageResult{Status: proto.PeerRetry, Code: "rate-limited", RetryAfter: int(wait.Seconds()) + 1}
	}
	s.spend(key, slug, l, t)
	return nil
}

// receiveProgramTask takes a director's task or change from program
// m.From on the server alias for project m.To here; the store is
// locked. The answer goes back as program-task-filed.
func (s *store) receiveProgramTask(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || m.Task == nil || !config.ValidProgramSlug(m.From) {
		return bad
	}
	if m.By != "director" && m.By != "human" {
		return bad
	}
	t := *m.Task
	if !cleanTask(&t) || t.Edit == nil && (t.Ref != "" || t.Title == "") || t.Edit != nil && t.Ref == "" {
		return bad
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	to, l := childOf(cfg, alias, m.From, m.To)
	if to == nil {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	}
	key := alias + "/" + m.From
	if r := s.overProgram(cfg, key, to.Slug, now()); r != nil {
		return *r
	}
	c := caller.ForPeerDirector(alias, m.From)
	ack := proto.PeerMessageResult{Status: proto.PeerDelivered}
	reply := func(allow, code, ref string) {
		s.answer(alias, selfFP(), m, proto.PeerProgramTaskFiled, allow, code, &proto.ProgramTask{Ref: ref})
	}
	refused := func(err error, ref string) proto.PeerMessageResult {
		code := "refused"
		if e, ok := err.(*tasks.Error); ok {
			code = e.Code
		}
		reply(proto.ProgramRefused, code, ref)
		to.Journal(c, "program.refused", key, strings.TrimSpace(m.ID+" "+ref)+" ("+code+")")
		return ack
	}
	if t.Edit != nil {
		n, _ := tasks.ParseRef(t.Ref)
		ch, err := programChange(alias, t.Edit)
		if err != nil {
			return refused(err, t.Ref)
		}
		res, err := to.Tasks().ProgramEdit(c, key, n, ch)
		if err != nil {
			return refused(err, t.Ref)
		}
		s.watch(to.Slug, n)
		if res.Changed {
			to.AddItem(KindProgramTask, t.Ref, fmt.Sprintf("the director of %s changed %s (%s): %s", Shown(key), t.Ref, textsafe.Line(res.Task.Title, 60), editWords(t.Edit)), false)
		}
		reply(proto.ProgramEdited, "", t.Ref)
		return ack
	}
	after, ok := programRefs(alias, t.After)
	if !ok {
		return refused(refuse("invalid-after", "after refs are T12 or <project>/T12"), "")
	}
	note := "filed by the director of " + Shown(key)
	if t.Umbrella != "" {
		note += " (" + t.Umbrella + ")"
	}
	if l.Tasks == config.ProgramTasksPropose {
		// The coordinator proposes it to the user: the task waits as a
		// message, framed as data, and an item says so.
		text := proposalText(t, after)
		in := &Message{ID: m.ID, Created: now().UTC().Truncate(time.Second), Bytes: len(text), State: StateSent, By: "director", Peer: key, Text: text}
		if err := write(to, in); err != nil {
			return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
		}
		to.AddMessageItem(KindProgramTask, key, fmt.Sprintf("the director of %s proposes a task for %s (%s); read it with tm link read %s, propose it to the user, and file it with tm task add if they agree",
			Shown(key), to.Slug, textsafe.Line(t.Title, 60), m.ID), false, m.ID)
		to.Journal(c, "program.proposed", key, fmt.Sprintf("%s (%s)", m.ID, KB(in.Bytes)))
		reply(proto.ProgramProposed, "", "")
		return ack
	}
	pt := tasks.ProgramTask{NewTask: tasks.NewTask{Title: t.Title, Notes: t.Notes, Steps: t.Steps, Status: t.Status, Priority: t.Priority, Repo: t.Repo}, After: after, Design: t.Design}
	out, err := to.Tasks().ProgramAdd(c, key, note, []tasks.ProgramTask{pt})
	if err != nil {
		return refused(err, "")
	}
	r := out[0]
	if r.Result == "failed" {
		return refused(&tasks.Error{Code: r.Code, Msg: r.Error}, "")
	}
	n, _ := tasks.ParseRef(r.ID)
	s.watch(to.Slug, n)
	allow := proto.ProgramExisting
	if r.Result == "created" {
		allow = proto.ProgramFiled
		words := fmt.Sprintf("the director of %s filed %s (%s)", Shown(key), r.ID, textsafe.Line(r.Title, 60))
		if t.Umbrella != "" {
			words += ", part of " + t.Umbrella
		}
		to.AddItem(KindProgramTask, r.ID, words, false)
	}
	reply(allow, "", r.ID)
	return ack
}

// programChange is a director's change, as the task store takes it.
func programChange(alias string, e *proto.ProgramTaskEdit) (tasks.ProgramChange, error) {
	ch := tasks.ProgramChange{Priority: e.Priority, Design: e.Design, Title: e.Title, Repo: e.Repo, Note: e.Note}
	if e.Status != nil {
		st, ok := tasks.ParseStatus(*e.Status)
		if !ok {
			return ch, refuse("invalid-status", "unknown status %q", *e.Status)
		}
		ch.Status = &st
	}
	if e.After != nil {
		refs, ok := programRefs(alias, *e.After)
		if !ok {
			return ch, refuse("invalid-after", "after refs are T12 or <project>/T12")
		}
		ch.After = []string{strings.Join(refs, ",")}
	}
	if ch.Empty() {
		return ch, refuse("invalid-edit", "nothing to change")
	}
	return ch, nil
}

// editWords names what e changes, in fixed words.
func editWords(e *proto.ProgramTaskEdit) string {
	var w []string
	if e.Title != nil {
		w = append(w, "title")
	}
	if e.Repo != nil {
		w = append(w, "repo")
	}
	if e.Status != nil {
		w = append(w, "status "+*e.Status)
	}
	if e.Priority != nil {
		w = append(w, "priority "+*e.Priority)
	}
	if e.Design != nil {
		w = append(w, "design")
	}
	if e.After != nil {
		w = append(w, "after")
	}
	if e.Note != "" {
		w = append(w, "a note")
	}
	return strings.Join(w, ", ")
}

// proposalText is a proposed task as its message keeps it.
func proposalText(t proto.ProgramTask, after []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Proposed task: %s\n", t.Title)
	for _, kv := range [][2]string{{"Priority", t.Priority}, {"Status", t.Status}, {"Repo", t.Repo}, {"Design", t.Design}, {"Part of", t.Umbrella}, {"After", strings.Join(after, ", ")}} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
		}
	}
	if t.Notes != "" {
		fmt.Fprintf(&b, "\n%s\n", t.Notes)
	}
	for i, st := range t.Steps {
		if i == 0 {
			b.WriteString("\nSteps:\n")
		}
		fmt.Fprintf(&b, "- %s\n", st)
	}
	return strings.TrimSpace(b.String())
}

// receiveProgramMessage takes a program channel message from the server
// alias; the store is locked. To a project here: from the director of
// its program there. To a program here: from its paired member there.
func (s *store) receiveProgramMessage(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp {
		return bad
	}
	if m.By != "director" && m.By != "coordinator" && m.By != "human" {
		return bad
	}
	text := Clean(m.Text)
	if text == "" || len(text) > config.MaxLinkKB<<10 {
		return bad
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	key := alias + "/" + m.From
	notMember := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	var box *project.Folder
	fromProgram := false
	if g := programFolder(m.To); g != nil {
		if m.By == "director" || cfg.ProgramOf(key) != g.Slug {
			return notMember
		}
		if _, ok := s.memberGrants(g.Slug, key); !ok {
			return notMember
		}
		box = g
	} else {
		if m.By == "coordinator" || !config.ValidProgramSlug(m.From) {
			return bad
		}
		to, _ := childOf(cfg, alias, m.From, m.To)
		if to == nil {
			return notMember
		}
		box, fromProgram = &to.Folder, true
	}
	if _, err := Read(box, m.ID); err == nil {
		return proto.PeerMessageResult{Status: proto.PeerDuplicate}
	}
	if pr := s.Pairs[pairKey(key, box.Slug)]; pr != nil && pr.Paused {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "link-paused"}
	}
	t := now()
	l := limitsOf(cfg, box.Slug)
	if len(text) > l.maxText {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "too-long"}
	}
	if b := s.refill(key, box.Slug, l, t); b.Tokens < 1 {
		wait := time.Duration((1 - b.Tokens) / float64(l.perHour) * float64(time.Hour))
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "rate-limited", RetryAfter: int(wait.Seconds()) + 1}
	}
	created := m.Created.UTC().Truncate(time.Second)
	if created.IsZero() || created.After(t.Add(clockSkew)) {
		created = t.UTC().Truncate(time.Second)
	}
	re := ""
	if ValidID(m.Re) {
		if _, err := Read(box, m.Re); err == nil {
			re = m.Re
		}
	}
	in := &Message{ID: m.ID, Created: created, Re: re, Bytes: len(text), State: StateSent, By: m.By, Peer: key, Text: text}
	if err := write(box, in); err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	box.AddMessageItem(KindProgramMessage, key, remoteProgramSummary(key, fromProgram, in), false, in.ID)
	box.Journal(caller.ForPeer(alias), "program.receive", key, fmt.Sprintf("%s (%s)", in.ID, KB(in.Bytes)))
	s.account(m.By != "human", key, box.Slug, l, t, side{box, key})
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// receiveProgramStart takes a director's start of a thread from program
// m.From on the server alias for project m.To here; the store is
// locked. The start itself runs once the store is unlocked (run), and
// its answer goes back as program-start-result.
func (s *store) receiveProgramStart(alias, fp string, m proto.PeerMessage, run *[]func()) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || m.Task == nil || !config.ValidProgramSlug(m.From) {
		return bad
	}
	if m.Allow != proto.ProgramStartDelegate && m.Allow != proto.ProgramStartApproved {
		return bad
	}
	t := *m.Task
	if !cleanTask(&t) || t.Ref == "" {
		return bad
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	to, l := childOf(cfg, alias, m.From, m.To)
	if to == nil {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	}
	key := alias + "/" + m.From
	c := caller.ForPeerDirector(alias, m.From)
	fp = selfFP()
	reply := func(allow, code, thread string) {
		withStore(func(s *store) error {
			s.answer(alias, fp, m, proto.PeerProgramStartResult, allow, code, &proto.ProgramTask{Ref: t.Ref, Thread: thread})
			return nil
		})
		Kick()
	}
	if !l.Starts {
		s.answer(alias, fp, m, proto.PeerProgramStartResult, proto.ProgramRefused, "starts-off", &proto.ProgramTask{Ref: t.Ref})
		to.Journal(c, "program.start-refused", t.Ref, m.ID+" (starts-off)")
		return proto.PeerMessageResult{Status: proto.PeerDelivered}
	}
	n, _ := tasks.ParseRef(t.Ref)
	s.watch(to.Slug, n)
	*run = append(*run, func() {
		if ProgramStarter == nil {
			reply(proto.ProgramRefused, "refused", "")
			return
		}
		thread, err := ProgramStarter(c, to, n, t.Agent, t.Model)
		if err != nil {
			code := "refused"
			if e, ok := err.(*tasks.Error); ok {
				code = e.Code
			}
			to.Journal(c, "program.start-refused", t.Ref, m.ID+" ("+code+")")
			to.AddItem(KindProgramTask, t.Ref, fmt.Sprintf("the director of %s asked to start a thread for %s; it was refused (%s)", Shown(key), t.Ref, code), false)
			reply(proto.ProgramRefused, code, "")
			return
		}
		to.AddItem(KindProgramTask, t.Ref, fmt.Sprintf("the director of %s started %s for %s", Shown(key), thread, t.Ref), false)
		reply(proto.ProgramStarted, "", thread)
	})
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// receiveWorkAnswer takes a paired member's answer to a task, change or
// start that program m.To here sent it (program-task-filed,
// program-start-result); the store is locked. One that matches nothing
// here goes quietly.
func (s *store) receiveWorkAnswer(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	ok := proto.PeerMessageResult{Status: proto.PeerDelivered}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || !ValidID(m.Re) || !project.ValidSlug(m.From) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	t := proto.ProgramTask{}
	if m.Task != nil {
		t = *m.Task
	}
	if !cleanTask(&t) || t.Thread != "" && !threadIDRE.MatchString(t.Thread) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	code := ""
	if m.Allow == proto.ProgramRefused {
		code = textsafe.Line(m.Code, 40)
		if !codeRE.MatchString(code) {
			code = "refused"
		}
	}
	switch m.Allow {
	case proto.ProgramFiled, proto.ProgramExisting, proto.ProgramEdited, proto.ProgramProposed, proto.ProgramRefused, proto.ProgramStarted:
	default:
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	key := alias + "/" + m.From
	var x *ProgramSent
	for _, y := range s.ProgramSent {
		if y.ID == m.Re && y.Member == key && y.Program == m.To {
			x = y
		}
	}
	if x == nil || x.State != SentPending {
		return ok
	}
	x.State, x.Code, x.Answered = m.Allow, code, now().UTC().Truncate(time.Second)
	if t.Ref != "" {
		x.Ref = t.Ref
	}
	x.Thread = t.Thread
	g := programFolder(m.To)
	if g == nil {
		return ok
	}
	ref := key
	if x.Ref != "" {
		ref += "/" + x.Ref
	}
	kind, words := KindProgramTaskFiled, ""
	switch {
	case x.Kind == "start" && x.State == proto.ProgramStarted:
		kind, words = KindProgramStartResult, fmt.Sprintf("%s started %s for %s (%s)", Shown(key), x.Thread, Shown(ref), x.ID)
	case x.Kind == "start":
		kind, words = KindProgramStartResult, fmt.Sprintf("%s refused the start of %s (%s): %s", Shown(key), Shown(ref), x.ID, WorkWords(code))
	case x.State == proto.ProgramRefused:
		words = fmt.Sprintf("%s refused %s (%s): %s", Shown(key), sentWhat(x), x.ID, WorkWords(code))
	case x.State == proto.ProgramProposed:
		words = fmt.Sprintf("%s's coordinator will propose %s to its user (%s): its board takes the director's tasks as proposals", Shown(key), sentWhat(x), x.ID)
	case x.State == proto.ProgramEdited:
		words = fmt.Sprintf("%s changed %s (%s)", Shown(key), Shown(ref), x.ID)
	default:
		words = fmt.Sprintf("%s filed %s (%s), routed as %s", Shown(key), Shown(ref), x.Title, x.ID)
		if x.State == proto.ProgramExisting {
			words = fmt.Sprintf("%s had %s open already (%s), routed as %s", Shown(key), Shown(ref), x.Title, x.ID)
		}
		if x.Umbrella != "" {
			words += ", part of " + x.Umbrella
		}
	}
	g.AddMessageItem(kind, key, words, false, x.ID)
	g.Journal(caller.ForPeer(alias), "program."+x.State, ref, strings.TrimSpace(x.ID+" "+code+" "+x.Thread))
	return ok
}

func sentWhat(x *ProgramSent) string {
	if x.Kind == "edit" {
		return "the change of " + x.Ref
	}
	return "the task " + strconv.Quote(x.Title)
}

// workWords are the refusals of a paired member's work answers, as the
// program's items say them; a task store's own codes read as they are.
var workWords = map[string]string{
	"starts-off":       "its user hasn't let the director start threads there (program_starts)",
	"needs-approval":   "its start_threads = propose: ask its coordinator to propose the start to its user",
	"not-member":       "it isn't in the program any more",
	"not-its-filing":   "the director changes the title, repo and status only of tasks it filed, before they start",
	"coordinator-only": "that is its coordinator's",
	"human-only":       "that is its user's",
	"cap-reached":      "its thread caps are full: it starts once a slot frees",
	"parked":           "it is parked",
	"refused":          "refused",
}

// WorkWords is code as a program's item says it.
func WorkWords(code string) string {
	if w, ok := workWords[code]; ok {
		return w
	}
	return code
}

var (
	codeRE     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	threadIDRE = regexp.MustCompile(`^t-[0-9]{1,9}$`)
	statuses   = func() []string {
		var out []string
		for _, s := range tasks.Statuses {
			out = append(out, string(s))
		}
		return out
	}()
)

// receiveProgramStatus takes the state of task m.Task.Ref that paired
// member m.From pushes to program m.To here; the store is locked. Only
// an accepted member that granted status pushes, only for a task it may
// push (the director filed or referenced it there), and only what is
// clean: the title capped, the URL https on a known code host.
func (s *store) receiveProgramStatus(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	bad := proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || m.Task == nil || !project.ValidSlug(m.From) {
		return bad
	}
	t := *m.Task
	if !cleanTask(&t) || t.Ref == "" || !slices.Contains(statuses, t.Status) || t.PR < 0 || t.PR > 1<<30 {
		return bad
	}
	g := programFolder(m.To)
	key := alias + "/" + m.From
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	if g == nil || cfg.ProgramOf(key) != g.Slug {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	}
	if granted, ok := s.memberGrants(g.Slug, key); !ok || !slices.Contains(granted, proto.ProgramFeatureStatus) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	}
	at := now().UTC().Truncate(time.Second)
	changed := t.Changed.UTC().Truncate(time.Second)
	if changed.After(at.Add(clockSkew)) {
		changed = at
	}
	ref := key + "/" + t.Ref
	old := s.RemoteTasks[ref]
	rt := &RemoteTask{Status: t.Status, Title: textsafe.Line(t.Title, maxStatusTitle), PR: t.PR, URL: prURL(t.URL), Merged: t.Merged, Changed: changed, Seen: at}
	if s.RemoteTasks == nil {
		s.RemoteTasks = map[string]*RemoteTask{}
	}
	if old == nil && len(s.RemoteTasks) >= maxRemoteTasks {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "too-many"}
	}
	s.RemoteTasks[ref] = rt
	if old == nil || old.Status != rt.Status || old.Merged != rt.Merged {
		detail := rt.Status
		if rt.Merged {
			detail += ", merged"
		}
		g.Journal(caller.ForPeer(alias), "program.status", ref, detail)
	}
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// prURL is u when it is an https URL on a known code host (GitHub, or
// Azure DevOps by the code host rules), else "".
func prURL(u string) string {
	if u == "" || len(u) > 512 || !textsafe.Safe(u) {
		return ""
	}
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.User != nil {
		return ""
	}
	switch h := strings.ToLower(p.Hostname()); {
	case h == "github.com", codehost.TrustedAzureURL(u):
		return p.String()
	}
	return ""
}

// receiveProgramRename takes program m.From's new slug m.Rename from
// the server alias; the store is locked. Each project here in it
// follows: its program key is rewritten, with no prompt, since the
// membership stands (§9.2).
func (s *store) receiveProgramRename(alias, fp string, m proto.PeerMessage) proto.PeerMessageResult {
	if g := remoteIDRE.FindStringSubmatch(m.ID); g == nil || g[1] != fp || !config.ValidProgramSlug(m.From) || !config.ValidProgramSlug(m.Rename) {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "bad-message"}
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	old, renamed := alias+"/"+m.From, alias+"/"+m.Rename
	to, l := childOf(cfg, alias, m.From, m.To)
	if to == nil {
		return proto.PeerMessageResult{Status: proto.PeerRefused, Code: "not-member"}
	}
	l.Program = renamed
	if err := config.SetProgramLink(to.Slug, l); err != nil {
		return proto.PeerMessageResult{Status: proto.PeerRetry, Code: "refused"}
	}
	for _, r := range s.Invites {
		if !r.Out && r.Project == to.Slug && r.Peer == old {
			r.Peer = renamed
		}
	}
	to.Journal(caller.ForPeer(alias), "program.rename", old, renamed)
	return proto.PeerMessageResult{Status: proto.PeerDelivered}
}

// workSettled tells a program here that its paired member's server
// refused, or didn't take in time, the work m (a task, change or start):
// the sent record is undelivered and an item says why. A program
// message's sender copy is marked as a link message's is. Answers and
// pushes that aren't taken go quietly.
func workSettled(alias string, m proto.PeerMessage, code string) {
	switch m.Kind {
	case proto.PeerProgramMessage:
		programMessageSettled(alias, m, code)
		return
	case proto.PeerProgramTask, proto.PeerProgramStart:
	default:
		return
	}
	if code == "" {
		return
	}
	words, known := RefusalWords[code]
	if !known {
		code, words = "refused", RefusalWords["refused"]
	}
	if code == "not-member" {
		words = "{to} isn't in the program on its server any more"
	}
	key := alias + "/" + m.To
	var x ProgramSent
	withStore(func(s *store) error {
		for _, y := range s.ProgramSent {
			if y.ID == m.ID && y.Program == m.From && y.State == SentPending {
				y.State, y.Code = SentUndelivered, code
				x = *y
			}
		}
		return nil
	})
	g := programFolder(m.From)
	if x.ID == "" || g == nil {
		return
	}
	kind := KindProgramTaskFiled
	if x.Kind == "start" {
		kind = KindProgramStartResult
	}
	g.AddMessageItem(kind, key, fmt.Sprintf("your %s %s to %s wasn't delivered: %s", x.Kind, x.ID, Shown(key), replaceWords(words, key, g.Slug)), false, x.ID)
	g.Journal(caller.Caller{Kind: caller.Ticker}, "program.undelivered", key, x.ID+" ("+code+")")
}

// programMessageSettled marks the sender's copy of program message m
// sent, or undelivered for code with an item.
func programMessageSettled(alias string, m proto.PeerMessage, code string) {
	var box *project.Folder
	if g := programFolder(m.From); g != nil {
		box = g
	} else if p, err := project.Open(m.From); err == nil {
		box = &p.Folder
	}
	if box == nil || !ValidID(m.ID) {
		return
	}
	key := alias + "/" + m.To
	if code == "" {
		update(box, key, m.ID, func(x *Message) {
			if x.State == StateQueued {
				x.State = StateSent
			}
		})
		return
	}
	words, known := RefusalWords[code]
	if !known {
		code, words = "refused", RefusalWords["refused"]
	}
	if code == "not-member" {
		words = "{to} isn't in the program on its server any more"
	}
	update(box, key, m.ID, func(x *Message) { x.State, x.Refused = StateUndelivered, code })
	box.AddMessageItem(KindUndelivered, key, fmt.Sprintf("your message %s to %s wasn't delivered: %s", m.ID, Shown(key), replaceWords(words, key, box.Slug)), false, m.ID)
	box.Journal(caller.Caller{Kind: caller.Ticker}, "program.undelivered", key, m.ID+" ("+code+")")
}

// --- status pushes from a member here ---

var (
	pushMu   sync.Mutex
	lastPush time.Time
)

// PushProgramStatus queues a program-status to its paired program for
// each task of a member here that changed since the last push: tasks the
// director there filed (their owner) or referenced (changed or started
// them), while the project's program_status is on. The outbox poll runs
// it, at most every statusPushPeriod.
func PushProgramStatus() {
	pushMu.Lock()
	if now().Sub(lastPush) < statusPushPeriod {
		pushMu.Unlock()
		return
	}
	lastPush = now()
	pushMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return
	}
	type push struct {
		alias string
		m     proto.PeerMessage
		key   string
		state string
	}
	var watched map[string][]int
	var pushed map[string]string
	readStore(func(s *store) { watched, pushed = s.Watch, s.Pushed })
	var out []push
	for _, slug := range cfg.ProjectSlugs() {
		l, in := cfg.ProgramLink(slug)
		if !in || !l.Status {
			continue
		}
		alias, prog, ok := config.Remote(l.Program)
		if !ok {
			continue
		}
		p, err := project.OpenWith(slug, cfg)
		if err != nil {
			continue
		}
		b, err := p.Tasks().Load()
		if err != nil {
			continue
		}
		owner := tasks.DirectorOwner(l.Program)
		for _, t := range b.Tasks {
			if t.Owner != owner && !slices.Contains(watched[slug], t.ID) {
				continue
			}
			pt := proto.ProgramTask{Ref: t.Ref(), Status: string(t.Status), Title: textsafe.Line(t.Title, maxStatusTitle)}
			if t.Thread != "" && ThreadPR != nil {
				pt.PR, pt.URL, pt.Merged = ThreadPR(slug, t.Thread)
				pt.URL = prURL(pt.URL)
			}
			state := fmt.Sprintf("%s|%s|%d|%s|%v|%s", pt.Status, pt.Title, pt.PR, pt.URL, pt.Merged, l.Program)
			k := slug + "/" + pt.Ref
			if pushed[k] == state {
				continue
			}
			pt.Changed = now().UTC().Truncate(time.Second)
			out = append(out, push{alias: alias, key: k, state: state,
				m: proto.PeerMessage{Kind: proto.PeerProgramStatus, From: slug, To: prog, By: "human", Task: &pt}})
		}
	}
	if len(out) == 0 {
		return
	}
	fp := selfFP()
	withStore(func(s *store) error {
		if s.Pushed == nil {
			s.Pushed = map[string]string{}
		}
		for _, x := range out {
			if _, err := remoteTo(x.alias, ""); err != nil {
				continue
			}
			s.Next++
			x.m.ID = fmt.Sprintf("M-%s-%d", fp, s.Next)
			x.m.Created = now().UTC().Truncate(time.Second)
			if s.queue(x.alias, x.m) == nil {
				s.Pushed[x.key] = x.state
			}
		}
		return nil
	})
	Kick()
}
