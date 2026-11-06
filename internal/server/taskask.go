package server

import (
	"errors"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// taskAsk is task.ask's: the dashboard's D, A and X (docs/SPEC.md §4,
// Asking the coordinator) for a console that doesn't write the project
// itself, the Mac app's. The task is checked against the live board
// (modaction.NotAskable), and the item and its journal line are the
// caller's.
func (s *Server) taskAsk(p proto.TaskAskParams, cc callCtx) (any, *proto.Error) {
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task", p.Task)
	}
	switch p.Kind {
	case project.KindDelegate, project.KindAccept, project.KindSendBack:
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task ask: delegate, accept or send-back", p.Kind)
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	t, err := pr.Tasks().Get(id)
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	if why := modaction.NotAskable(t, p.Kind); why != "" {
		return nil, proto.Errorf(proto.ErrRefused, "%s", why)
	}
	var asked bool
	switch p.Kind {
	case project.KindAccept:
		asked, err = pr.AskAccept(cc.who(), t.Ref())
	case project.KindSendBack:
		if _, nerr := project.SendBackNote(p.Note); nerr != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "%v", nerr)
		}
		asked, err = pr.AskSendBack(cc.who(), t.Ref(), p.Note)
	default:
		asked, err = pr.AskDelegate(cc.who(), t.Ref())
	}
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	s.watch.wake()
	return proto.TaskAskResult{Asked: asked}, nil
}

// taskAction is task.accept's, task.send-back's and task.delegate's:
// the ask of kind with the checks tm task accept and send-back apply
// (project.AskTask), each refusal with its code.
func (s *Server) taskAction(kind string, p proto.TaskActionParams, cc callCtx) (any, *proto.Error) {
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task", p.Task)
	}
	if kind != project.KindSendBack && p.Note != "" {
		return nil, proto.Errorf(proto.ErrBadParams, "%s takes no note; task.send-back sends a task back with one", kind)
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	t, err := pr.Tasks().Get(id)
	if err != nil {
		return nil, taskError(err)
	}
	asked, err := pr.AskTask(cc.who(), t, kind, p.Note)
	if err != nil {
		return nil, taskError(err)
	}
	s.watch.wake()
	return proto.TaskAskResult{Asked: asked}, nil
}

// taskGet is task.get's: one task of the board as it is now, notes and
// steps included.
func (s *Server) taskGet(p proto.TaskGetParams) (any, *proto.Error) {
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task", p.Task)
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	b, err := pr.Tasks().Load()
	if err != nil {
		return nil, taskError(err)
	}
	t := b.Find(id)
	if t == nil {
		// An archived task (its Archived says so), or the refusal
		// (unknown-task).
		if t, err = pr.Tasks().Get(id); err != nil {
			return nil, taskError(err)
		}
	}
	cfg, _ := config.Load()
	out := sideTask(t)
	out.After = taskAfter(cfg, pr.Slug, b, t)
	return out, nil
}

// TaskDone is task.done's result: done tasks, newest first
// (tasks.NewestDoneFirst), without their notes and steps. With a groups
// call, Groups are Done's headings, and Total how many tasks they hold;
// otherwise Tasks is the page, Total how many the range holds, and Next
// the offset to read on from (0 at the end). Mark is the line at the
// human's last action, with groups, when work has finished since.
type TaskDone struct {
	Tasks  []SideTask      `json:"tasks"`
	Groups []SideDoneGroup `json:"groups,omitempty"`
	Total  int             `json:"total,omitempty"`
	Next   int             `json:"next,omitempty"`
	Mark   *SideDoneMark   `json:"mark,omitempty"`
}

// SideDoneMark is the line in Done on the wire (tasks.DoneMark).
type SideDoneMark struct {
	At      string `json:"at"`
	Title   string `json:"title"`
	Group   int    `json:"group"`
	Rows    int    `json:"rows,omitempty"`
	Between bool   `json:"between,omitempty"`
}

// markDone places the line at the human's last action in project p's
// groups, made of done as of now. Like them it reads no git, so a plain
// folder gets the same line as a repository.
func markDone(p *project.Project, groups []tasks.DoneGroup, done []*tasks.Task, now time.Time) *tasks.DoneMark {
	at, where, ok := p.LastHumanAct()
	if !ok {
		return nil
	}
	return tasks.MarkDone(groups, done, at, where, now)
}

// SideDoneGroup is a heading of Done on the wire (tasks.DoneGroup).
type SideDoneGroup struct {
	Kind     string     `json:"kind"`
	Title    string     `json:"title"`
	From     string     `json:"from"`
	To       string     `json:"to"`
	Count    int        `json:"count"`
	Archived int        `json:"archived,omitempty"`
	Open     bool       `json:"open,omitempty"`
	Tasks    []SideTask `json:"tasks,omitempty"`
	Next     int        `json:"next,omitempty"`
}

// doneRow is done task t as task.done lists it.
func doneRow(t *tasks.Task) SideTask {
	st := sideTask(t)
	st.Notes, st.Steps = "", nil
	return st
}

// doneRows are done tasks as task.done lists them.
func doneRows(ts []*tasks.Task) []SideTask {
	out := make([]SideTask, 0, len(ts))
	for _, t := range ts {
		out = append(out, doneRow(t))
	}
	return out
}

// zoneOr is IANA zone name's location, the server's own when it has none
// or it isn't known here.
func zoneOr(name string) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

// taskDone is task.done's: the done tasks as they are now. No git and no
// thread records: a plain folder gets the same answer as a repository.
// The archive is read only for what can hold it: the groups, whose counts
// need its dates, and a range the caller says holds archived tasks.
func (s *Server) taskDone(p proto.TaskDoneParams) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	done, err := pr.Tasks().Done(p.Groups || p.Archived)
	if err != nil {
		return nil, taskError(err)
	}
	loc := zoneOr(p.TZ)
	out := TaskDone{Tasks: []SideTask{}}
	if p.Groups {
		out.Total = len(done)
		now := time.Now().In(loc)
		groups := tasks.GroupDone(done, now, 0)
		if m := markDone(pr, groups, done, now); m != nil {
			out.Mark = &SideDoneMark{At: m.At.UTC().Format(time.RFC3339), Title: m.Title, Group: m.Group, Rows: m.Rows, Between: m.Between}
		}
		for _, g := range groups {
			out.Groups = append(out.Groups, SideDoneGroup{Kind: g.Kind, Title: g.Title, From: g.From, To: g.To,
				Count: g.Count, Archived: g.Archived, Open: g.Open, Tasks: doneRows(g.Tasks), Next: g.Next})
		}
		return out, nil
	}
	page, total, next := tasks.PageDone(done, loc, p.From, p.To, p.Offset, p.Limit)
	out.Tasks, out.Total, out.Next = doneRows(page), total, next
	return out, nil
}

// templates are the task templates of the enabled mods that run in
// project slug, as tm task add --template finds them; none when the
// mods don't read.
func (s *Server) templates(slug string) []mod.Template {
	reg, _, _ := s.installedMods(modRegistryAge)
	if reg == nil {
		return nil
	}
	ts, _ := reg.Templates(slug) // tm doctor says which don't read
	return ts
}

// taskTemplates is task.templates': the project's task templates, by
// mod and name.
func (s *Server) taskTemplates(p proto.TaskDoneParams) (any, *proto.Error) {
	if _, err := project.Open(p.Project); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	out := proto.TaskTemplatesResult{Templates: []proto.TaskTemplate{}}
	for _, t := range s.templates(p.Project) {
		out.Templates = append(out.Templates, proto.TaskTemplate{Ref: t.Ref(), Mod: t.Mod, Name: t.Name, Title: t.Title,
			Notes: t.Notes, Steps: t.Steps, Builtin: t.Builtin})
	}
	return out, nil
}

// taskAdd is task.add's: tm task add's one task, as the caller, each
// refusal with the task store's code.
func (s *Server) taskAdd(p proto.TaskAddParams, cc callCtx) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	it := tasks.NewTask{Title: p.Title, Notes: p.Notes}
	if p.Template != "" {
		tpl, err := mod.FindTemplate(s.templates(p.Project), p.Template)
		if err != nil {
			return nil, proto.Errorf("unknown-template", "%v", err)
		}
		var notes string
		it.Title, notes, it.Steps = tpl.Apply(p.Title)
		if it.Notes == "" {
			it.Notes = notes
		}
	}
	res, err := pr.Tasks().Add(cc.who(), []tasks.NewTask{it})
	if err != nil {
		return nil, taskError(err)
	}
	if len(res) != 1 {
		return nil, proto.Errorf(proto.ErrInternal, "task.add: %d results", len(res))
	}
	r := res[0]
	if r.Result == "failed" {
		code := r.Code
		if code == "" {
			code = proto.ErrBadParams
		}
		return nil, proto.Errorf(code, "%s", r.Error)
	}
	s.watch.wake()
	return proto.TaskAddResult{Task: r.ID, Existing: r.Result == "existing"}, nil
}

// taskPriority sets a task's priority as the caller (task.priority).
func (s *Server) taskPriority(p proto.TaskPriorityParams, cc callCtx) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is not a task id (T12)", p.Task)
	}
	res, err := pr.Tasks().SetPriority(cc.who(), id, p.Priority)
	if err != nil {
		return nil, taskError(err)
	}
	if res.Changed {
		s.watch.wake()
	}
	return proto.TaskPriorityResult{Changed: res.Changed}, nil
}

// taskError is a task store's refusal with its code (unknown-task,
// not-accept, invalid-note, already-asked, …), anything else internal.
func taskError(err error) *proto.Error {
	var terr *tasks.Error
	if errors.As(err, &terr) {
		return proto.Errorf(terr.Code, "%s", terr.Msg)
	}
	return proto.Errorf(proto.ErrInternal, "%v", err)
}
