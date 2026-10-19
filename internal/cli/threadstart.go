package cli

// tm thread start: the policy checks, the thread's record, worktree and
// files, and its agent's launch.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// startOpts are tm thread start's and tm task delegate's options.
type startOpts struct {
	title, task, agent, model string
	repo, base                string
	approved, overCap         bool
}

func (e *Env) threadStart(p *project.Project, o startOpts, asJSON bool) error {
	if err := e.coordinatorOnly(p, "starting threads"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return err
	}
	// --agent overrides the user's thread_agent setting (§11.2); unset,
	// the one installed agent.
	agentName, err := threadAgent(o.agent, safety)
	if err != nil {
		return err
	}
	if err := checkModel(cfg, agentName, o.model, safety.Models); err != nil {
		return err
	}
	if err := parkedErr(p, safety); err != nil {
		return err
	}
	// --over-cap is the user's word on the cap, not on the thread: under
	// propose it still needs --approved-by-user.
	if safety.StartThreads == config.StartPropose && e.Caller.IsAgent() && !o.approved {
		return &tasks.Error{Code: "needs-approval", Msg: "start_threads = propose: propose the thread to the user, and once they agree start it with --approved-by-user"}
	}
	if !o.overCap {
		if err := e.underCap(p, safety.ParallelThreads, safety.GlobalParallelThreads, cmp.Or(o.task, o.title)); err != nil {
			return err
		}
	}
	task, err := threadTask(p, o.task)
	if err != nil {
		return err
	}
	if task != nil && o.title == "" {
		o.title = task.Title
	}
	o.title = strings.Join(strings.Fields(o.title), " ")
	if o.title == "" {
		return usagef("a thread needs a title, or --task T12")
	}
	repo, err := e.threadRepo(p, o.repo, task)
	if err != nil {
		return err
	}
	if o.base != "" && repo == "" {
		return usagef("--base needs a repo")
	}
	if o.base != "" && !worktree.ValidBase(o.base) {
		return usagef("--base %q is not a branch, tag or commit name", o.base)
	}
	if err := e.serverAgent(agentName); err != nil {
		return err
	}
	now := time.Now().UTC()
	rec := thread.Record{Title: o.title, Agent: agentName, Model: o.model, Repo: repo, State: thread.Running, Created: now, LastPrompt: now}
	if task != nil {
		rec.Task = task.Ref()
	}
	r, err := thread.Create(p, rec)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		thread.Update(p, r.ID, func(x *thread.Record) error { x.State = thread.Stopped; return nil })
		return fmt.Errorf("thread %s: %w", r.ID, err)
	}
	wt, err := thread.WorktreeDir(p.Slug, r.ID, o.title)
	if err != nil {
		return fail(err)
	}
	branch, base := "", ""
	if repo != "" {
		if branch, err = thread.BranchName(safety.BranchName, p.Slug, r.ID, r.Task, o.title); err != nil {
			return fail(err)
		}
		if base, err = worktree.Create(repo, wt, branch, o.base); err != nil {
			return fail(err)
		}
	} else if err := os.MkdirAll(wt, 0o755); err != nil {
		return fail(err)
	}
	if r, err = thread.Update(p, r.ID, func(x *thread.Record) error {
		x.Worktree, x.Branch, x.Base = wt, branch, base
		return nil
	}); err != nil {
		return fail(err)
	}
	brief, err := e.threadFiles(p, r, task)
	if err != nil {
		return fail(err)
	}
	detail := strings.TrimSpace(r.Task + " " + r.Title)
	if r.Model != "" {
		detail += " (model " + r.Model + ")"
	}
	switch {
	case o.overCap:
		detail += " (over the parallel threads cap, approved by the user)"
	case o.approved:
		detail += " (approved by the user)"
	}
	if err := p.Journal(e.Caller, "thread.start", r.ID, detail); err != nil {
		return fail(err)
	}
	info, err := e.launchThread(p, r, brief, "")
	if err != nil {
		return fail(err)
	}
	if asJSON {
		return e.printJSON(map[string]any{"id": r.ID, "worktree": r.Worktree, "branch": r.Branch, "base": r.Base, "session": info.ID, "task": r.Task, "model": r.Model})
	}
	fmt.Fprintf(e.Stdout, "started %s in %s", r.ID, r.Worktree)
	if r.Branch != "" {
		fmt.Fprintf(e.Stdout, " on %s from %s", r.Branch, r.Base)
	}
	if r.Model != "" {
		fmt.Fprintf(e.Stdout, " with %s", r.Model)
	}
	fmt.Fprintf(e.Stdout, " (session %s)\n", info.ID)
	return nil
}

// threadAgent is the agent a thread runs: --agent, else the project's
// thread_agent, else the one installed agent (docs/SPEC.md §8.2,
// §11.2). One that isn't installed, none installed, or several with
// none chosen are refused before anything is created.
func threadAgent(flag string, safety config.Safety) (string, error) {
	reg := models.Registry()
	if reg == nil {
		return "", errors.New("no home directory for the agent manifests")
	}
	installed := models.Installed(reg, nil)
	if flag == "" {
		name, _, err := models.Resolve(reg, installed, safety.ThreadAgent, "thread agent")
		return name, refusal(err)
	}
	if _, ok := reg.Get(flag); !ok {
		return "", &tasks.Error{Code: models.CodeUnknownAgent, Msg: fmt.Sprintf("no agent %q (tm agent list)", flag)}
	}
	if !slices.Contains(installed, flag) {
		return "", refusal(models.NotInstalled(reg, flag, installed))
	}
	return flag, nil
}

// serverAgent refuses an agent the server doesn't know before a thread
// is created for it. This process and the server each read the
// manifests, the server only at its start and on tm agent reload: a
// manifest added since is read again first.
func (e *Env) serverAgent(name string) error {
	for _, m := range []string{proto.MethodAgentList, proto.MethodAgentReload} {
		agents, err := serverAgents(e, m)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(agents, func(a proto.AgentInfo) bool { return a.Name == name }) {
			return nil
		}
	}
	return &tasks.Error{Code: models.CodeUnknownAgent, Msg: fmt.Sprintf("the server has no agent %q (tm agent list)", name)}
}

// serverAgents are the server's agents, from agent.list or agent.reload
// (a variable for tests).
var serverAgents = func(e *Env, method string) ([]proto.AgentInfo, error) {
	var res proto.AgentListResult
	err := e.call(method, nil, &res)
	return res.Agents, err
}

// checkModel refuses a model the project may not start a thread with
// (models.Catalog.Check): the agent's models are unknown, it doesn't
// offer it, the user's account refused it, or the project's scope
// leaves it out. "" is the agent's own default. An agent never asked
// is asked first.
func checkModel(cfg *config.Config, agentName, model string, scope []string) error {
	if model == "" {
		return nil
	}
	reg := models.Registry()
	if reg == nil {
		return errors.New("no home directory for the agent manifests")
	}
	installed := models.Installed(reg, nil)
	models.Ensure(context.Background(), reg, agentName, nil)
	c, ok := models.CatalogOf(reg, cfg, agentName, installed)
	if !ok {
		return &tasks.Error{Code: models.CodeUnknownAgent, Msg: fmt.Sprintf("no agent %q (tm agent list)", agentName)}
	}
	return refusal(c.Check(model, scope))
}

// refusal is a models.Error as the CLI's refusal.
func refusal(err error) error {
	var me *models.Error
	if errors.As(err, &me) {
		return &tasks.Error{Code: me.Code, Msg: me.Msg}
	}
	return err
}

// parkedErr refuses to start, restart or adopt a thread of an inactive
// or a paused project (docs/SPEC.md §5.1, §11.2).
func parkedErr(p *project.Project, safety config.Safety) error {
	if !safety.Active {
		return &tasks.Error{Code: "project-inactive", Msg: fmt.Sprintf("%s is inactive: no thread starts until the user activates it (tm project activate %s, or space on it in the sidebar)", p.Slug, p.Slug)}
	}
	if !safety.Paused {
		return nil
	}
	return &tasks.Error{Code: "project-paused", Msg: fmt.Sprintf("%s is paused: no thread starts until the user resumes it (tm project resume %s, or Paused in the project popup)", p.Slug, p.Slug)}
}

// threadTask is the task a new thread is for (ref "" for none): it
// must exist, not be done, and have no live thread.
func threadTask(p *project.Project, taskRef string) (*tasks.Task, error) {
	if taskRef == "" {
		return nil, nil
	}
	id, err := ref(taskRef)
	if err != nil {
		return nil, err
	}
	task, err := p.Tasks().Get(id)
	if err != nil {
		return nil, err
	}
	if task.Thread != "" {
		if prev, err := thread.Load(p, task.Thread); err == nil && prev.State != thread.Resolved {
			return nil, &tasks.Error{Code: "task-has-thread", Msg: fmt.Sprintf("%s already has thread %s; resolve it first", task.Ref(), prev.ID)}
		}
	}
	if task.Status == tasks.Done {
		return nil, &tasks.Error{Code: "task-done", Msg: task.Ref() + " is done"}
	}
	return task, nil
}

// threadFiles links a new thread's task (started, if it was open or
// ready) and writes its task.md, brief.md and STATUS.md. It returns the
// brief's path. The journal counts the memory notes the brief carries
// (thread.memory), so how often a thread gets a matched one shows.
func (e *Env) threadFiles(p *project.Project, r *thread.Record, task *tasks.Task) (string, error) {
	if task != nil {
		s := p.Tasks()
		if _, err := s.SetThread(e.Caller, task.ID, r.ID); err != nil {
			return "", err
		}
		if task.Status == tasks.Open || task.Status == tasks.Ready {
			if _, err := s.SetStatus(e.Caller, task.ID, tasks.Started, ""); err != nil {
				return "", err
			}
		}
		task, _ = s.Get(task.ID)
	}
	if err := thread.WriteTaskText(p, r.ID, thread.TaskText(r, task)); err != nil {
		return "", err
	}
	brief, mem, err := thread.WriteBrief(p, r, false)
	if err != nil {
		return "", err
	}
	if err := p.Journal(e.Caller, "thread.memory", r.ID, memoryDetail(mem)); err != nil {
		return "", err
	}
	if _, err := thread.UpdateStatus(p, r.ID, nil); err != nil {
		return "", err
	}
	return brief, nil
}

// memoryDetail is a thread.memory journal line's detail:
// "2 matched (memory/a.md, memory/b.md), 1 standing".
func memoryDetail(m thread.Memory) string {
	standing, matched := m.Counts()
	var paths []string
	for _, n := range m.Notes {
		if !n.Standing {
			paths = append(paths, n.Note.Path)
		}
	}
	d := fmt.Sprintf("%d matched", matched)
	if len(paths) > 0 {
		d += " (" + strings.Join(paths, ", ") + ")"
	}
	return fmt.Sprintf("%s, %d standing", d, standing)
}

// threadRepo is the repo a new thread works in, "" for a plain folder
// (docs/SPEC.md §9): --repo (flag), else its task's repo, either a
// project repo by name or path or tasks.NoRepo; with neither, the
// project's one repo, or none when it has none. A project with several
// repos and no choice is refused, pick-repo: tm never guesses which.
func (e *Env) threadRepo(p *project.Project, flag string, task *tasks.Task) (string, error) {
	switch {
	case flag != "":
		if repo, err := tasks.FindRepo(p.Repos, flag); err == nil {
			return repo, nil
		} else if !strings.ContainsAny(flag, `/\`) && !strings.HasPrefix(flag, ".") {
			return "", err // a name, not a path
		}
		repo := e.abs(flag)
		// An agent names only the project's repos, which the user adds:
		// any other path would give a thread a worktree of whatever
		// repository the agent names. Asked first, so the refusal says
		// nothing about the path.
		if e.Caller.IsAgent() && !listedRepo(p, repo) {
			return "", &tasks.Error{Code: "unlisted-repo", Msg: fmt.Sprintf("--repo %s is not one of the project's repos (tm context lists them); only the user adds one", repo)}
		}
		if fi, err := os.Stat(repo); err != nil || !fi.IsDir() {
			return "", usagef("--repo %s is not a directory", repo)
		}
		return repo, nil
	case task != nil && task.Repo != "":
		repo, err := tasks.FindRepo(p.Repos, task.Repo)
		if err != nil {
			var te *tasks.Error
			if errors.As(err, &te) {
				te.Msg = task.Ref() + "'s repo: " + te.Msg + "; change it with tm task edit " + task.Ref() + " --repo, or pass --repo"
			}
			return "", err
		}
		return repo, nil
	}
	switch len(p.Repos) {
	case 0:
		return "", nil
	case 1:
		return p.Repos[0], nil
	}
	set := "set the task's with tm task edit T12 --repo NAME, or pass --repo NAME"
	if task != nil {
		set = fmt.Sprintf("set %s's with tm task edit %s --repo NAME, or pass --repo NAME", task.Ref(), task.Ref())
	}
	return "", &tasks.Error{Code: "pick-repo", Msg: fmt.Sprintf("the project has %d repos and nothing says which this thread works in: %s (or none, for a folder without git). Its repos: %s", len(p.Repos), set, tasks.RepoList(p.Repos))}
}

// listedRepo reports whether dir is one of p's repos, either spelling
// of a path with links.
func listedRepo(p *project.Project, dir string) bool {
	for _, r := range p.Repos {
		if filepath.Clean(r) == filepath.Clean(dir) || realPath(r) == realPath(dir) {
			return true
		}
	}
	return false
}

// underCap refuses a new thread while capN or more of the project's
// threads work (thread.IsWorking; docs/SPEC.md §9), or global (0 for no
// such cap) or more of every project's together.
//
// A refusal records the project's coordinator as waiting for a slot
// (thread.Refused): the ticker tells it, with one slot-free item, once
// one frees. Passing the caps forgets it (thread.Started).
func (e *Env) underCap(p *project.Project, capN, global int, what string) error {
	recs, err := thread.List(p)
	if err != nil {
		return err
	}
	var res proto.SessionListResult
	if err := e.call(proto.MethodSessionList, nil, &res); err != nil {
		return nil // no server: starting fails on its own
	}
	if n := thread.Working(recs, res.Sessions); n >= capN {
		thread.Refused(p.Slug, "over-cap", what, time.Now())
		return &tasks.Error{Code: "over-cap", Msg: fmt.Sprintf("%d of %d parallel threads are working: propose the thread to the user instead and start it once one finishes (an inbox item says when a slot frees); only if the user says to start it anyway, add --over-cap", n, capN)}
	}
	if global > 0 {
		by := thread.WorkingAll(res.Sessions)
		if n := thread.Total(by); n >= global {
			thread.Refused(p.Slug, "over-global-cap", what, time.Now())
			return &tasks.Error{Code: "over-global-cap", Msg: fmt.Sprintf("%d of %d threads across all projects are working (%s): propose the thread to the user instead and start it once one finishes (an inbox item says when a slot frees); only if the user says to start it anyway, add --over-cap", n, global, thread.ByProjectWords(by))}
		}
	}
	thread.Started(p.Slug)
	return nil
}

// launchThread starts the thread's agent session and records it. resume
// is an agent session id to resume, or "" for a fresh start with the
// kickoff prompt.
func (e *Env) launchThread(p *project.Project, r *thread.Record, brief, resume string) (proto.SessionInfo, error) {
	cfg, err := config.Load()
	if err != nil {
		return proto.SessionInfo{}, err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return proto.SessionInfo{}, err
	}
	params := proto.SessionStartParams{
		Agent: r.Agent, Cwd: r.Worktree, Cols: 120, Rows: 40,
		Role: proto.RoleThread, Project: p.Slug, Thread: r.ID,
		Brief: brief, Kickoff: thread.Kickoff(brief), Yolo: safety.Yolo, NoSandboxEscapes: !safety.SandboxEscapes, Model: r.Model, ResumeSID: resume,
	}
	var res proto.SessionStartResult
	if err := e.call(proto.MethodSessionStart, params, &res); err != nil {
		return proto.SessionInfo{}, err
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(e.Stderr, "warning: %s\n", w)
	}
	_, err = thread.Update(p, r.ID, func(x *thread.Record) error {
		x.Session, x.State = res.Session.ID, thread.Running
		if res.Session.AgentSID != "" {
			x.AgentSID = res.Session.AgentSID
		}
		if resume == "" {
			x.Prompted = false
		}
		return nil
	})
	return res.Session, err
}
