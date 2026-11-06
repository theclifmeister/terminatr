package appapi

import (
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

// Programs in the app API (app API 44, the programs design §12): the
// programs on projects.watch, a program's dashboard (program.get) and
// rolled-up Needs you (program.needs), its history (program.search),
// and the human's acts on it. All of it only while the server's hello
// lists the capability programs (proto.CapPrograms): without it
// projects.watch sends no programs and every program method answers
// programs-off, so the apps draw no program rows, screens or badges.
// Program settings stay the TUI's, as safety settings do; renaming and
// deleting a program, and starting its director (program.open, app
// API 45), are the Mac app's alone (macOnly); its question is answered
// at the write level too.

// Program is a program on this server as the sidebar shows it: its
// Members in its table's order, a paired one by (Server, Project),
// which peer.list's mirror lists under that alias (never copied
// here); its authority (propose or delegate), thread cap (0 for none)
// and whether it is paused; PROGRAM.md's goal, its open questions and
// unhandled inbox items; Error why its folder doesn't read.
type Program struct {
	Slug            string          `json:"slug"`
	Members         []ProgramMember `json:"members"`
	Authority       string          `json:"authority"`
	ParallelThreads int             `json:"parallel_threads,omitempty"`
	Paused          bool            `json:"paused,omitempty"`
	Goal            string          `json:"goal,omitempty"`
	Questions       int             `json:"questions,omitempty"`
	Unread          int             `json:"unread,omitempty"`
	Error           string          `json:"error,omitempty"`
}

// ProgramMember is one member of a program: a project of this server
// (Server "") or of the paired server Server. On program.get, State is
// a paired member's invite (invited, member, declined, left,
// undelivered) and Words say it as its row does; Dormant: that server
// has programs off; Error why a local member doesn't open.
type ProgramMember struct {
	Project string `json:"project"`
	Server  string `json:"server,omitempty"`
	State   string `json:"state,omitempty"`
	Words   string `json:"words,omitempty"`
	Dormant bool   `json:"dormant,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ProgramDetail is program.get's result: the program's dashboard but
// its Needs you (program.needs). Director is its director's session
// ("" while none runs; session.list has it, role director). Umbrellas
// are its board's open umbrella tasks with their parts; Deps the states
// of the tasks the members' tasks come after and of the parts, by ref;
// Waits the members' tasks that wait on other members' (DEPENDENCIES);
// Tasks the local members' tasks those name, by ref. Working counts the
// members' threads running now, against ParallelThreads.
type ProgramDetail struct {
	Program
	Director  string             `json:"director,omitempty"`
	Umbrellas []ProgramUmbrella  `json:"umbrellas"`
	Deps      map[string]TaskDep `json:"deps,omitempty"`
	Waits     []ProgramWait      `json:"waits"`
	Tasks     map[string]Task    `json:"tasks,omitempty"`
	Working   int                `json:"working,omitempty"`
}

// ProgramUmbrella is an umbrella task of a program's board ("T4"), its
// parts by ref with their states, in the order they were added.
type ProgramUmbrella struct {
	Task  Task      `json:"task"`
	Parts []TaskDep `json:"parts"`
}

// TaskDep is a task another depends on, by Ref ("T10" on its own
// board, "acme-api/T12" another member's, "mini/acme-ios/T7" a paired
// one's). State is met, open, gone or unknown, as tm's ticker last
// computed it (another member's only; none for its own board's); Word
// says it as the dashboards do (review, merged, done, dropped, not in
// the program); Status and Title the task's when last read; Merged its
// thread's PR merged; Why why it is unknown or gone.
type TaskDep struct {
	Ref    string `json:"ref"`
	State  string `json:"state,omitempty"`
	Word   string `json:"word,omitempty"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	Merged bool   `json:"merged,omitempty"`
	Why    string `json:"why,omitempty"`
}

// ProgramWait is a member's task (Task, with its Title and Status)
// that comes after another member's task (After).
type ProgramWait struct {
	Task   string `json:"task"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	After  string `json:"after"`
}

// ProgramNeeds is program.needs' result: the program's Needs you, one
// queue across its members ranked as tm ranks it, each member's count,
// and Quiet, the members with nothing waiting.
type ProgramNeeds struct {
	Program string            `json:"program"`
	Needs   []ProgramNeed     `json:"needs"`
	Members []ProgramNeedsFor `json:"members"`
	Quiet   int               `json:"quiet,omitempty"`
}

// ProgramNeed is one row of a program's Needs you: Need of member
// Project (on paired server Server, "" for this one; its Session is
// "<alias>/<session>" there), with the task's Priority. A link need
// (link-held, link-file, link-request) names the other project (Peer)
// and the id link.act takes (Link); host: the member's PR host fails;
// umbrella-review: an umbrella of the program's board is in review
// (program.umbrella.accept, send-back). Acts are the human acts the row
// takes: on this server's members their own methods (session.answer,
// session.approve, task.accept, task.send-back, questions.open,
// link.act), on a paired member's peer.need-act. Also are the member's
// other needs folded into it, Similar other members' questions that ask
// the same (Q7@acme-web). Program: one of the program's own questions
// (Project the program), answered once with program.answer for the
// member questions it covers (Merged).
type ProgramNeed struct {
	Need     PeerNeed   `json:"need"`
	Project  string     `json:"project"`
	Server   string     `json:"server,omitempty"`
	Priority string     `json:"priority,omitempty"`
	Peer     string     `json:"peer,omitempty"`
	Link     string     `json:"link,omitempty"`
	Acts     []string   `json:"acts,omitempty"`
	Also     []PeerNeed `json:"also,omitempty"`
	Similar  []string   `json:"similar,omitempty"`
	Program  bool       `json:"program,omitempty"`
	Merged   []string   `json:"merged,omitempty"`
	// Recommend is the director's recommended answer to the row's
	// question (app API 48): the user accepts it with
	// program.recommend.accept (Question its RecommendKey) or picks
	// another; never an answer by itself.
	Recommend *Recommendation `json:"recommend,omitempty"`
}

// Recommendation is the director's recommended answer (app API 48):
// Choices, the options' labels it recommends (one, or a
// multi-select's), or Other, an answer in words; Why, its one-line
// reason, with its sources; At, when; Question, the question as
// program.recommend.accept names it (acme-api/Q3, mini/acme-ios/Q3,
// <program>/Q2).
type Recommendation struct {
	Question string    `json:"question"`
	Choices  []string  `json:"choices,omitempty"`
	Other    string    `json:"other,omitempty"`
	Why      string    `json:"why"`
	At       time.Time `json:"at"`
}

// ProgramNeedsFor is one member as a program's Needs you counts it: its
// rows in the queue (Needs); a paired member whose server doesn't show
// its needs here has Count, what that server's mirror says waits there,
// read at Synced. Error says why its needs don't read.
type ProgramNeedsFor struct {
	Project string `json:"project"`
	Server  string `json:"server,omitempty"`
	Needs   int    `json:"needs"`
	Count   int    `json:"count,omitempty"`
	Error   string `json:"error,omitempty"`
}

// The params of the program methods.
type (
	// ProgramParams name a program.
	ProgramParams struct {
		Program string `json:"program"`
	}
	// CoordinatorClearParams are coordinator.clear's: Project's
	// coordinator, or in its place Program's director (app API 44).
	CoordinatorClearParams struct {
		Project string `json:"project,omitempty"`
		Program string `json:"program,omitempty"`
	}
	// ProgramRouteParams are program.route's: tasks for Program's
	// members, each filed as ready (or open) in its project's board,
	// under Umbrella (a ref of the program's board, "T4", or a title to
	// file one).
	ProgramRouteParams struct {
		Program  string      `json:"program"`
		Umbrella string      `json:"umbrella,omitempty"`
		Tasks    []RouteTask `json:"tasks"`
	}
	// RouteTask is one task of a route, for member Project ("acme-api",
	// "mini/acme-ios"). Key names it for a later task's After, which
	// also takes the same project's T<n> or a member's <project>/T<n>.
	RouteTask struct {
		Key      string   `json:"key,omitempty"`
		Project  string   `json:"project"`
		Title    string   `json:"title"`
		Notes    string   `json:"notes,omitempty"`
		Steps    []string `json:"steps,omitempty"`
		Priority string   `json:"priority,omitempty"`
		Status   string   `json:"status,omitempty"`
		Repo     string   `json:"repo,omitempty"`
		Design   string   `json:"design,omitempty"`
		After    []string `json:"after,omitempty"`
	}
	// ProgramTaskEditParams are program.task.edit's: member task Ref
	// ("acme-api/T12") of Program. An empty field stays as it is; After
	// replaces what it comes after; Note adds a note line. Title, Repo
	// and Status (ready, open) only on a task the director filed, before
	// it starts.
	ProgramTaskEditParams struct {
		Program  string   `json:"program"`
		Ref      string   `json:"ref"`
		Priority string   `json:"priority,omitempty"`
		After    []string `json:"after,omitempty"`
		Design   string   `json:"design,omitempty"`
		Title    string   `json:"title,omitempty"`
		Repo     string   `json:"repo,omitempty"`
		Status   string   `json:"status,omitempty"`
		Note     string   `json:"note,omitempty"`
	}
	// ProgramSendParams are program.send's: from the program (From ""
	// or its slug) to member To, or from member From to the program (To
	// its slug), as the user for that side; Re the message it answers.
	ProgramSendParams struct {
		Program string `json:"program"`
		From    string `json:"from,omitempty"`
		To      string `json:"to"`
		Text    string `json:"text"`
		Re      string `json:"re,omitempty"`
	}
	// ProgramReadParams are program.read's: message ID (M-…) of
	// Program's channel.
	ProgramReadParams struct {
		Program string `json:"program"`
		ID      string `json:"id"`
	}
	// ProgramAnswerParams are program.answer's: the program's open
	// question Ref ("Q2"), with Choices (its options' labels) or Other.
	ProgramAnswerParams struct {
		Program string   `json:"program"`
		Ref     string   `json:"ref"`
		Choices []string `json:"choices,omitempty"`
		Other   string   `json:"other,omitempty"`
	}
	// ProgramRecommendAcceptParams are program.recommend.accept's (app
	// API 48): the question, as its row's Recommendation.Question names
	// it.
	ProgramRecommendAcceptParams struct {
		Program  string `json:"program"`
		Question string `json:"question"`
	}
	// ProgramUmbrellaParams are program.umbrella.accept's and
	// .send-back's: umbrella Task ("T4") of Program, with the user's
	// Note (send-back needs one).
	ProgramUmbrellaParams struct {
		Program string `json:"program"`
		Task    string `json:"task"`
		Note    string `json:"note,omitempty"`
	}
	// ProgramRenameParams are program.rename's.
	ProgramRenameParams struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	// ProgramDeleteParams are program.delete's: Confirm repeats the
	// program's name, the user's confirm, asked first in the app.
	ProgramDeleteParams struct {
		Program string `json:"program"`
		Confirm string `json:"confirm"`
	}
	// ProgramOpenParams are program.open's (app API 45): the program,
	// and the size a director it starts gets (80x24 when unset).
	ProgramOpenParams struct {
		Program string `json:"program"`
		Cols    uint16 `json:"cols,omitempty"`
		Rows    uint16 `json:"rows,omitempty"`
	}
	// ProgramSearchParams are program.search's: search.query's over
	// Program and its local members (as a thread reads them), or only
	// Member (a member's slug, or the program's own). Hits of a member
	// carry it (SearchHit.Member), their ids prefixed "<slug>:".
	ProgramSearchParams struct {
		Program string   `json:"program"`
		Member  string   `json:"member,omitempty"`
		Words   string   `json:"words"`
		In      []string `json:"in,omitempty"`
		Since   string   `json:"since,omitempty"`
		Until   string   `json:"until,omitempty"`
		Limit   int      `json:"limit,omitempty"`
	}
	// ProgramTimelineParams are program.search.timeline's, with a
	// prefixed id.
	ProgramTimelineParams struct {
		Program string `json:"program"`
		ID      string `json:"id"`
		Before  int    `json:"before,omitempty"`
		After   int    `json:"after,omitempty"`
		All     bool   `json:"all,omitempty"`
	}
	// ProgramShowParams are program.search.show's, with prefixed ids.
	ProgramShowParams struct {
		Program string   `json:"program"`
		IDs     []string `json:"ids"`
	}
)

// The results of the program methods.
type (
	// ProgramRouted is program.route's: the umbrella the tasks are
	// part of, and each task filed: its member, ref there and Result
	// (created; existing when an open task of that title was there;
	// pending for a paired member's, whose program-task Message's answer
	// names its ref).
	ProgramRouted struct {
		Umbrella string       `json:"umbrella,omitempty"`
		Tasks    []RoutedTask `json:"tasks"`
	}
	RoutedTask struct {
		Key     string `json:"key,omitempty"`
		Project string `json:"project"`
		Ref     string `json:"ref,omitempty"`
		Title   string `json:"title"`
		Result  string `json:"result"`
		Message string `json:"message,omitempty"`
	}
	// ProgramTaskEdited is program.task.edit's: a local member's task
	// Result (edited, unchanged), or Sent, the program-task message to a
	// paired member, whose answer comes as an item.
	ProgramTaskEdited struct {
		Result string `json:"result,omitempty"`
		Sent   string `json:"sent,omitempty"`
	}
	// ProgramSent is program.send's: the message (M-…), its State (sent,
	// or queued for a paired member) and Bytes.
	ProgramSent struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Bytes int    `json:"bytes"`
	}
	// ProgramMessage is program.read's: the message framed as data.
	ProgramMessage struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	// ProgramAnswered is program.answer's: the question answered, the
	// labels picked or the user's own words, and the member questions it
	// went to, each with what it answered there (or why not: Error).
	ProgramAnswered struct {
		Question string          `json:"question"`
		Choices  []string        `json:"choices,omitempty"`
		Other    string          `json:"other,omitempty"`
		Fanned   []ProgramFanned `json:"fanned"`
	}
	// RecommendAccepted is program.recommend.accept's: the question
	// answered with the director's recommendation, the answer in words,
	// and where it went: a member question's Result (answered, or
	// waiting: an earlier answer waits on its coordinator), or a
	// program question's covered members (Fanned).
	RecommendAccepted struct {
		Question string          `json:"question"`
		Answer   string          `json:"answer"`
		Result   string          `json:"result,omitempty"`
		Fanned   []ProgramFanned `json:"fanned"`
	}
	// ProgramFanned is one member question an answer went to:
	// Result is answered, waiting (an earlier answer waits on its
	// coordinator), closed (no longer open there) or failed (Error).
	ProgramFanned struct {
		Project  string `json:"project"`
		Question string `json:"question"`
		Result   string `json:"result"`
		Answer   string `json:"answer,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	// ProgramUmbrellaDone is program.umbrella.accept's and send-back's:
	// the umbrella's status now (done, open).
	ProgramUmbrellaDone struct {
		Status string `json:"status"`
	}
	// ProgramRenamed is program.rename's: the director started again
	// under the new name (its session, "" when none ran) and what didn't
	// go through after the rename stood.
	ProgramRenamed struct {
		Director string   `json:"director,omitempty"`
		Notes    []string `json:"notes,omitempty"`
	}
	// ProgramDeleted is program.delete's: Trashed, the program's folder
	// went to the trash (false: it had only its settings).
	ProgramDeleted struct {
		Trashed bool `json:"trashed,omitempty"`
	}
)

// The copies.

func programOf(sp server.SideProgram) Program {
	out := Program{Slug: sp.Slug, Members: make([]ProgramMember, 0, len(sp.Projects)), Authority: sp.Authority,
		ParallelThreads: sp.ParallelThreads, Paused: sp.Paused, Goal: sp.Goal, Questions: sp.Questions, Unread: sp.Unread, Error: sp.Error}
	for _, m := range sp.Projects {
		alias, slug, ok := config.SplitMember(m)
		if !ok {
			continue
		}
		out.Members = append(out.Members, ProgramMember{Project: slug, Server: alias})
	}
	return out
}

func programDetailOf(d server.ProgramDetail) ProgramDetail {
	out := ProgramDetail{Program: programOf(d.SideProgram), Director: d.Director, Umbrellas: []ProgramUmbrella{},
		Waits: []ProgramWait{}, Working: d.Working}
	out.Members = make([]ProgramMember, 0, len(d.Members))
	for _, m := range d.Members {
		out.Members = append(out.Members, ProgramMember{Project: m.Project, Server: m.Server, State: m.State, Words: m.Words,
			Dormant: m.Dormant, Error: m.Error})
	}
	for _, u := range d.Umbrellas {
		pu := ProgramUmbrella{Task: taskOf(u.Task), Parts: make([]TaskDep, 0, len(u.Parts))}
		for _, p := range u.Parts {
			pu.Parts = append(pu.Parts, taskDepOf(p.Ref, p.Dep))
		}
		out.Umbrellas = append(out.Umbrellas, pu)
	}
	if len(d.Deps) > 0 {
		out.Deps = make(map[string]TaskDep, len(d.Deps))
		for ref, dep := range d.Deps {
			out.Deps[ref] = taskDepOf(ref, dep)
		}
	}
	for _, w := range d.Waits {
		out.Waits = append(out.Waits, ProgramWait{Task: w.Task, Title: w.Title, Status: w.Status, After: w.After})
	}
	if len(d.Tasks) > 0 {
		out.Tasks = make(map[string]Task, len(d.Tasks))
		for ref, t := range d.Tasks {
			out.Tasks[ref] = taskOf(t)
		}
	}
	return out
}

func taskDepOf(ref string, d program.Dep) TaskDep {
	return TaskDep{Ref: ref, State: d.State, Word: d.Word(), Status: d.Status, Title: d.Title, Merged: d.Merged, Why: d.Why}
}

func taskAfterOf(a server.SideAfter) TaskDep {
	return TaskDep{Ref: a.Ref, State: a.State, Word: a.Word, Status: a.Status, Title: a.Title, Merged: a.Merged, Why: a.Why}
}

// programNeedsOf is r as the app sees it. A need's times are on its
// server's clock, as the TUI shows them.
func programNeedsOf(r proto.ProgramNeedsResult) ProgramNeeds {
	out := ProgramNeeds{Program: r.Program, Needs: make([]ProgramNeed, 0, len(r.Needs)), Members: make([]ProgramNeedsFor, 0, len(r.Members)),
		Quiet: r.Quiet}
	for _, n := range r.Needs {
		pn := ProgramNeed{Need: memberNeedOf(n.Server, n.PeerNeed), Project: n.Project, Server: n.Server, Priority: n.Priority,
			Peer: n.Peer, Link: n.Link, Acts: append([]string(nil), n.Acts...), Similar: append([]string(nil), n.Similar...),
			Program: n.Program, Merged: append([]string(nil), n.Merged...)}
		// The row's acts are the server's (ProgramNeed.Acts), not the
		// need's alone.
		pn.Need.Acts = append([]string(nil), n.Acts...)
		if rec := n.Recommend; rec != nil {
			pn.Recommend = &Recommendation{Question: proto.RecommendKey(r.Program, n), Choices: append([]string(nil), rec.Labels...),
				Other: rec.Text, Why: rec.Why, At: rec.At}
		}
		for _, a := range n.Also {
			pn.Also = append(pn.Also, memberNeedOf(n.Server, a))
		}
		out.Needs = append(out.Needs, pn)
	}
	for _, m := range r.Members {
		out.Members = append(out.Members, ProgramNeedsFor{Project: m.Project, Server: m.Server, Needs: m.Needs, Count: m.Count, Error: m.Error})
	}
	return out
}

// memberNeedOf is need n of a program member on server alias ("" for
// this one): a paired one's session as "<alias>/<session>", this
// server's as it is.
func memberNeedOf(alias string, n proto.PeerNeed) PeerNeed {
	out := peerNeedOf(alias, 0, n)
	if alias == "" {
		out.Session = n.Session
	}
	return out
}

func programRoutedOf(r program.RouteResult) ProgramRouted {
	out := ProgramRouted{Umbrella: r.Umbrella, Tasks: make([]RoutedTask, 0, len(r.Tasks))}
	for _, t := range r.Tasks {
		out.Tasks = append(out.Tasks, RoutedTask{Key: t.Key, Project: t.Project, Ref: t.Ref, Title: t.Title, Result: t.Result, Message: t.Message})
	}
	return out
}

func programTaskEditedOf(r proto.ProgramTaskEditResult) ProgramTaskEdited {
	return ProgramTaskEdited{Result: r.Result, Sent: r.Sent}
}

func programSentOf(r proto.ProgramSendResult) ProgramSent {
	return ProgramSent{ID: r.ID, State: r.State, Bytes: r.Bytes}
}

func programMessageOf(r proto.ProgramReadResult) ProgramMessage {
	return ProgramMessage{ID: r.ID, Text: r.Text}
}

func programAnsweredOf(r program.AnswerResult) ProgramAnswered {
	out := ProgramAnswered{Question: r.Question, Choices: append([]string(nil), r.Answer.Labels...), Other: r.Answer.Other,
		Fanned: make([]ProgramFanned, 0, len(r.Fanned))}
	for _, f := range r.Fanned {
		out.Fanned = append(out.Fanned, ProgramFanned{Project: f.Project, Question: f.Question, Result: f.Result, Answer: f.Answer, Error: f.Error})
	}
	return out
}

func recommendAcceptedOf(r program.AcceptResult) RecommendAccepted {
	out := RecommendAccepted{Question: r.Key, Answer: r.Answer, Result: r.Result, Fanned: make([]ProgramFanned, 0, len(r.Fanned))}
	for _, f := range r.Fanned {
		out.Fanned = append(out.Fanned, ProgramFanned{Project: f.Project, Question: f.Question, Result: f.Result, Answer: f.Answer, Error: f.Error})
	}
	return out
}

func programRecommendAcceptParams(p ProgramRecommendAcceptParams) proto.ProgramRecommendAcceptParams {
	return proto.ProgramRecommendAcceptParams{Program: p.Program, Question: p.Question}
}

func programUmbrellaOf(r proto.ProgramUmbrellaResult) ProgramUmbrellaDone {
	return ProgramUmbrellaDone{Status: r.Result}
}

// programRenamedOf is r as the app sees it: the folder's path stays
// home.
func programRenamedOf(r proto.ProgramRenameResult) ProgramRenamed {
	return ProgramRenamed{Director: r.Director, Notes: append([]string(nil), r.Notes...)}
}

// programDeletedOf is r as the app sees it: the trash's path stays
// home.
func programDeletedOf(r proto.ProgramDeleteResult) ProgramDeleted {
	return ProgramDeleted{Trashed: r.Trash != ""}
}

// ProgramOpen is program.open's result (app API 45): the program's
// director session, and Started when this call started it.
type ProgramOpen struct {
	Session string `json:"session"`
	Started bool   `json:"started,omitempty"`
}

func programOpenOf(r proto.ProgramOpenResult) ProgramOpen {
	return ProgramOpen{Session: r.Session, Started: r.Started}
}

// The params' copies.

func coordinatorClearParams(p CoordinatorClearParams) proto.CoordinatorClearParams {
	return proto.CoordinatorClearParams{Project: p.Project, Program: p.Program}
}

func programParams(p ProgramParams) proto.ProgramParams {
	return proto.ProgramParams{Program: p.Program}
}

func programNeedsParams(p ProgramParams) proto.ProgramNeedsParams {
	return proto.ProgramNeedsParams{Program: p.Program}
}

func programRouteParams(p ProgramRouteParams) server.ProgramRouteParams {
	out := server.ProgramRouteParams{Program: p.Program, Route: program.RouteRequest{Umbrella: p.Umbrella, Tasks: make([]program.RouteTask, 0, len(p.Tasks))}}
	for _, t := range p.Tasks {
		out.Route.Tasks = append(out.Route.Tasks, program.RouteTask{Key: t.Key, Project: t.Project, Title: t.Title, Notes: t.Notes,
			Steps: append([]string(nil), t.Steps...), Priority: t.Priority, Status: t.Status, Repo: t.Repo, Design: t.Design,
			After: append([]string(nil), t.After...)})
	}
	return out
}

func programTaskEditParams(p ProgramTaskEditParams) proto.ProgramTaskEditParams {
	return proto.ProgramTaskEditParams{Program: p.Program, Ref: p.Ref, Priority: p.Priority, After: append([]string(nil), p.After...),
		Design: p.Design, Title: p.Title, Repo: p.Repo, Status: p.Status, Note: p.Note}
}

func programSendParams(p ProgramSendParams) proto.ProgramSendParams {
	return proto.ProgramSendParams{Program: p.Program, From: p.From, To: p.To, Text: p.Text, Re: p.Re}
}

func programReadParams(p ProgramReadParams) proto.ProgramReadParams {
	return proto.ProgramReadParams{Program: p.Program, ID: p.ID}
}

func programAnswerParams(p ProgramAnswerParams) proto.ProgramAnswerParams {
	return proto.ProgramAnswerParams{Program: p.Program, Ref: p.Ref, Choices: append([]string(nil), p.Choices...), Other: p.Other}
}

func programUmbrellaParams(p ProgramUmbrellaParams) proto.ProgramUmbrellaParams {
	return proto.ProgramUmbrellaParams{Program: p.Program, Task: p.Task, Note: p.Note}
}

func programRenameParams(p ProgramRenameParams) proto.ProgramRenameParams {
	return proto.ProgramRenameParams{From: p.From, To: p.To}
}

func programOpenParams(p ProgramOpenParams) proto.ProgramOpenParams {
	return proto.ProgramOpenParams{Program: p.Program, Cols: p.Cols, Rows: p.Rows}
}

func programDeleteParams(p ProgramDeleteParams) proto.ProgramDeleteParams {
	return proto.ProgramDeleteParams{Program: p.Program, Confirm: p.Confirm}
}

func programSearchParams(p ProgramSearchParams) proto.ProgramSearchParams {
	return proto.ProgramSearchParams{Program: p.Program, Member: p.Member, Words: p.Words, In: append([]string(nil), p.In...),
		Since: p.Since, Until: p.Until, Limit: p.Limit}
}

func programTimelineParams(p ProgramTimelineParams) proto.ProgramTimelineParams {
	return proto.ProgramTimelineParams{Program: p.Program, ID: p.ID, Before: p.Before, After: p.After, All: p.All}
}

func programShowParams(p ProgramShowParams) proto.ProgramShowParams {
	return proto.ProgramShowParams{Program: p.Program, IDs: append([]string(nil), p.IDs...)}
}

// macOnly are the methods only the Mac app has, at no device level:
// renaming and deleting a program, which the user does at this computer
// (as program settings, which stay the TUI's), and starting its
// director (app API 45). A program's question is answered by the user's
// own paired phone too, at write level (Q208): program.answer is an
// ordinary write. A session's folder (app API 49) is read at this computer
// too, until the iPhone's follow-up looks at what a device may read of it.
var macOnly = []string{proto.MethodProgramRename, proto.MethodProgramDelete, proto.MethodProgramOpen,
	proto.MethodFilesList, proto.MethodFilesRead, proto.MethodFilesStamp, proto.MethodFilesRepos}

// MacOnly says m is the Mac app's alone, at no device level (macOnly).
func (m Method) MacOnly() bool { return slices.Contains(macOnly, m.Name) }

// peerPrograms maps "<alias>/<slug>" of each paired project a program
// on this server has to that program, while programs are on: the
// sidebar's "· acme" tag (PeerProject.Program).
func peerPrograms() map[string]string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out map[string]string
	for _, g := range server.SidePrograms(cfg) {
		for _, m := range g.Projects {
			if !strings.Contains(m, "/") {
				continue
			}
			if out == nil {
				out = map[string]string{}
			}
			if _, taken := out[m]; !taken {
				out[m] = g.Slug
			}
		}
	}
	return out
}
