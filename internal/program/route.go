package program

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Routing a request to the child projects (the programs design §5):
// tm program route files one task per child, written into the child's
// TASKS.md through its task store as the director (op opProgram), with
// a program-task item in the child's inbox and a journal line in both.
// The child coordinator then owns the task as it owns any other.

// KindTask is the item raised in a member project's inbox when the
// director filed or changed one of its tasks: fixed words.
const KindTask = "program-task"

// MaxRoute is the most tasks one route files.
const MaxRoute = 20

// RouteRequest is tm program route's JSON.
type RouteRequest struct {
	// Umbrella is the program's umbrella task the work is part of: its
	// ref ("T4"), or a title to file one on the program's board.
	Umbrella string      `json:"umbrella,omitempty"`
	Tasks    []RouteTask `json:"tasks"`
}

// RouteTask is one task for one member project.
type RouteTask struct {
	// Key names it within the request, for a later task's after.
	Key      string   `json:"key,omitempty"`
	Project  string   `json:"project"`
	Title    string   `json:"title"`
	Notes    string   `json:"notes,omitempty"`
	Steps    []string `json:"steps,omitempty"`
	Priority string   `json:"priority,omitempty"`
	// Status is ready (the default) or open.
	Status string `json:"status,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Design string `json:"design,omitempty"`
	// After are the tasks it comes after: the key of an earlier task in
	// this request, a task of the same project ("T12"), or a member
	// project's ("acme-web/T40").
	After []string `json:"after,omitempty"`
}

// Routed is one filed task.
type Routed struct {
	Key     string `json:"key,omitempty"`
	Project string `json:"project"`
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	// Result is created, or existing when an open task of that title was
	// there already (a retried route files nothing twice); pending for a
	// paired member's, until its server answers.
	Result string `json:"result"`
	// Message is a paired member's task's program-task id (M-…): the
	// program-task-filed item names it, and the task's ref there.
	Message string `json:"message,omitempty"`
}

// RouteResult is what a route filed.
type RouteResult struct {
	Umbrella string   `json:"umbrella,omitempty"`
	Tasks    []Routed `json:"tasks"`
}

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// mayDirect checks that c acts for program p: its director, or the
// human. A coordinator, a thread or another program's director never
// does.
func (p *Program) mayDirect(c caller.Caller, what string) error {
	switch {
	case c.Kind == caller.Director && c.Project == p.Slug:
		return nil
	case c.Kind == caller.Director:
		return refuse("other-program", "the director of %s can't act for program %s", c.Project, p.Slug)
	case c.IsAgent():
		return refuse("director-only", "%s is the program's director's", what)
	}
	return nil
}

// Member opens member project slug of p for a program write: a local
// member, not archived. A paired server's member (remote-member) gets
// its work over the link instead (remote.go): the callers that take one
// check IsRemote first.
func (p *Program) Member(cfg *config.Config, slug string) (*project.Project, error) {
	if _, _, remote := config.Remote(slug); remote {
		return nil, refuse("remote-member", "%s is on a paired server: this works on local members only", slug)
	}
	if !project.ValidSlug(slug) || cfg.ProgramOf(slug) != p.Slug {
		return nil, refuse("not-member", "%s is not a member of program %s (tm program show %s)", slug, p.Slug, p.Slug)
	}
	child, err := project.OpenWith(slug, cfg)
	if err != nil {
		return nil, err
	}
	if s, err := cfg.Safety(slug); err == nil && s.Archived {
		return nil, refuse("archived", "%s is archived", slug)
	}
	return child, nil
}

// writable refuses a write through p while it is left out or paused.
func (p *Program) writable(cfg *config.Config, what string) error {
	if p.LeftOut {
		return refuse("program-left-out", "a project is called %s too: nothing routes through the program until one is renamed (tm doctor)", p.Slug)
	}
	if t, _ := cfg.Program(p.Slug); t.Paused {
		return refuse("program-paused", "program %s is paused: %s waits until the user resumes it", p.Slug, what)
	}
	return nil
}

// Route files req's tasks in the member projects as c (§5.1): all of
// them or, when a check fails, none. cfg is config.Load's.
func (p *Program) Route(c caller.Caller, cfg *config.Config, req RouteRequest) (*RouteResult, error) {
	if err := p.mayDirect(c, "routing work"); err != nil {
		return nil, err
	}
	if err := p.writable(cfg, "routing"); err != nil {
		return nil, err
	}
	if len(req.Tasks) == 0 {
		return nil, refuse("invalid-route", "the route has no tasks")
	}
	if len(req.Tasks) > MaxRoute {
		return nil, refuse("invalid-route", "%d tasks; a route files at most %d", len(req.Tasks), MaxRoute)
	}
	children := map[string]*project.Project{}
	keys := map[string]bool{}
	// remoteKeys are the keys of tasks that go to paired members: they
	// have no number here until filed there.
	remoteKeys := map[string]bool{}
	for i, rt := range req.Tasks {
		what := fmt.Sprintf("task %d (%s)", i+1, oneLine(rt.Title, 40))
		if rt.Key != "" && (keys[rt.Key] || remoteKeys[rt.Key]) {
			return nil, refuse("invalid-route", "%s: two tasks have key %s", what, rt.Key)
		}
		if IsRemote(rt.Project) {
			if err := p.checkRemoteRoute(cfg, rt, keys, remoteKeys); err != nil {
				return nil, prefixed(what, err)
			}
			if rt.Key != "" {
				remoteKeys[rt.Key] = true
			}
			continue
		}
		child, ok := children[rt.Project]
		if !ok {
			var err error
			if child, err = p.Member(cfg, rt.Project); err != nil {
				return nil, prefixed(what, err)
			}
			children[rt.Project] = child
		}
		if rt.Key != "" {
			if !keyRE.MatchString(rt.Key) {
				return nil, refuse("invalid-route", "%s: key %q must start with a letter, then lower case letters, digits, - and _", what, rt.Key)
			}
			if _, isRef := tasks.ParseRef(rt.Key); isRef {
				return nil, refuse("invalid-route", "%s: key %q reads as a task ref; name it by what it is", what, rt.Key)
			}
			if keys[rt.Key] {
				return nil, refuse("invalid-route", "%s: two tasks have key %s", what, rt.Key)
			}
		}
		if err := child.Tasks().CheckProgramTask(programTask(rt, nil)); err != nil {
			return nil, prefixed(what, err)
		}
		for _, a := range rt.After {
			if remoteKeys[strings.TrimSpace(a)] {
				return nil, prefixed(what, refuse("pending-ref", "after %s: it goes to a paired server, and has no number until it is filed there; set the after with tm program task edit once a program-task-filed item names it", a))
			}
			if err := p.checkRouteAfter(cfg, child, keys, a); err != nil {
				return nil, prefixed(what, err)
			}
		}
		if rt.Key != "" {
			keys[rt.Key] = true
		}
	}
	umbrella, err := p.umbrella(c, req.Umbrella)
	if err != nil {
		return nil, err
	}
	res := &RouteResult{Umbrella: umbrella, Tasks: []Routed{}}
	filed := map[string]Routed{}
	for _, rt := range req.Tasks {
		after := make([]string, 0, len(rt.After))
		for _, a := range rt.After {
			a = strings.TrimSpace(a)
			if r, ok := filed[a]; ok {
				a = r.Project + "/" + r.Ref // read as its own board's on the same project
			}
			after = append(after, a)
		}
		var r Routed
		var err error
		if IsRemote(rt.Project) {
			r, err = p.fileRemote(c, cfg, rt.Project, umbrella, rt, filed)
		} else {
			r, err = p.file(c, children[rt.Project], umbrella, rt, after)
		}
		if err != nil {
			return res, err
		}
		if rt.Key != "" {
			filed[rt.Key] = r
		}
		res.Tasks = append(res.Tasks, r)
	}
	if umbrella != "" {
		// The filed tasks are the umbrella's parts (§5.3); a paired
		// member's joins once its server answers program-task-filed
		// (link.AddFiledPart), and until then a note line says it waits.
		var filedRefs, pending []string
		for _, r := range res.Tasks {
			if r.Ref == "" {
				pending = append(pending, r.Project+" (pending "+r.Message+")")
				continue
			}
			filedRefs = append(filedRefs, r.Project+"/"+r.Ref)
		}
		if len(filedRefs) > 0 {
			if _, err := p.Tasks().SetParts(c, refID(umbrella), filedRefs, true); err != nil {
				return res, err
			}
		}
		if len(pending) > 0 {
			if _, err := p.Tasks().ProgramEdit(c, p.Slug, refID(umbrella), tasks.ProgramChange{Note: "routed " + strings.Join(pending, ", ")}); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// checkRemoteRoute checks a routed task for paired member rt.Project:
// an accepted member that granted tasks, the fields, and its after refs
// (remoteAfter), keys of earlier tasks of the route included.
func (p *Program) checkRemoteRoute(cfg *config.Config, rt RouteTask, keys, remoteKeys map[string]bool) error {
	if err := p.remoteMember(cfg, rt.Project, proto.ProgramFeatureTasks); err != nil {
		return err
	}
	if rt.Key != "" {
		if !keyRE.MatchString(rt.Key) {
			return refuse("invalid-route", "key %q must start with a letter, then lower case letters, digits, - and _", rt.Key)
		}
		if _, isRef := tasks.ParseRef(rt.Key); isRef {
			return refuse("invalid-route", "key %q reads as a task ref; name it by what it is", rt.Key)
		}
	}
	if err := checkRemoteTask(rt); err != nil {
		return err
	}
	for _, a := range rt.After {
		if remoteKeys[strings.TrimSpace(a)] {
			return refuse("pending-ref", "after %s: it goes to a paired server, and has no number until it is filed there; set the after with tm program task edit once a program-task-filed item names it", a)
		}
		if _, err := p.remoteAfter(cfg, rt.Project, a, keys, nil); err != nil {
			return err
		}
	}
	return nil
}

// checkRouteAfter checks one after ref of a task for child: the key of
// an earlier task, child's own T<n>, or a member's <slug>/T<n>.
func (p *Program) checkRouteAfter(cfg *config.Config, child *project.Project, keys map[string]bool, ref string) error {
	ref = strings.TrimSpace(ref)
	if keys[ref] {
		return nil
	}
	if !strings.Contains(ref, "/") {
		if n, ok := tasks.ParseRef(ref); ok && strings.HasPrefix(strings.ToUpper(ref), "T") {
			if _, err := child.Tasks().Get(n); err != nil {
				return refuse("invalid-after", "after %s: %s has no T%d", ref, child.Slug, n)
			}
			return nil
		}
		return refuse("invalid-after", "after %q: not the key of an earlier task in the route, T12 or <project>/T12", ref)
	}
	return p.CheckCrossRef(cfg, ref)
}

// CheckCrossRef checks a ref to a member project's task (§6.1): a local
// member's task that exists, or a paired member's that accepted the
// invite (its board is that server's: its state comes with its pushes).
func (p *Program) CheckCrossRef(cfg *config.Config, ref string) error {
	alias, slug, n, ok := tasks.CrossRef(strings.TrimSpace(ref))
	if !ok {
		return refuse("invalid-after", "after %q: not T12 or <project>/T12", ref)
	}
	key := slug
	if alias != "" {
		key = alias + "/" + slug
	}
	if cfg.ProgramOf(key) != p.Slug {
		return refuse("not-member", "after %s: %s is not a member of program %s", ref, key, p.Slug)
	}
	if alias != "" {
		if _, ok := link.MemberGrants(p.Slug, key); !ok {
			return refuse("not-accepted", "after %s: %s hasn't accepted program %s's invite (%s)", ref, key, p.Slug, link.MemberState(p.Slug, key))
		}
		return nil
	}
	child, err := project.OpenWith(slug, cfg)
	if err != nil {
		return err
	}
	if _, err := child.Tasks().Get(n); err != nil {
		return refuse("invalid-after", "after %s: %s has no T%d", ref, slug, n)
	}
	return nil
}

// file files one task in child, with its item and journal lines.
func (p *Program) file(c caller.Caller, child *project.Project, umbrella string, rt RouteTask, after []string) (Routed, error) {
	note := "filed by the director of " + p.Slug
	if umbrella != "" {
		note += " (" + p.Slug + "/" + umbrella + ")"
	}
	out, err := child.Tasks().ProgramAdd(c, p.Slug, note, []tasks.ProgramTask{programTask(rt, after)})
	if err != nil {
		return Routed{}, prefixed(child.Slug, err)
	}
	r := out[0]
	if r.Result == "failed" {
		return Routed{}, &Error{Code: r.Code, Msg: child.Slug + ": " + r.Error}
	}
	routed := Routed{Key: rt.Key, Project: child.Slug, Ref: r.ID, Title: r.Title, Result: r.Result}
	if r.Result != "created" {
		return routed, nil
	}
	words := fmt.Sprintf("the director of %s filed %s (%s)", p.Slug, r.ID, oneLine(r.Title, 60))
	if umbrella != "" {
		words += ", part of " + p.Slug + "/" + umbrella
	}
	if _, err := child.AddItem(KindTask, r.ID, words, false); err != nil {
		return routed, err
	}
	return routed, p.Journal(c, "program.route", child.Slug+"/"+r.ID, r.Title)
}

// umbrella resolves a route's umbrella: a task of the program's board
// ("T4"), or one filed with that title. "" for none.
func (p *Program) umbrella(c caller.Caller, u string) (string, error) {
	u = strings.TrimSpace(u)
	if u == "" {
		return "", nil
	}
	if tasks.ValidRef(strings.ToUpper(u)) {
		t, err := p.Tasks().Get(refID(u))
		if err != nil {
			return "", err
		}
		if t.Archived {
			return "", refuse("archived", "umbrella %s is archived", t.Ref())
		}
		return t.Ref(), nil
	}
	out, err := p.Tasks().Add(c, []tasks.NewTask{{Title: u}})
	if err != nil {
		return "", err
	}
	if out[0].Result == "failed" {
		return "", &Error{Code: out[0].Code, Msg: "umbrella: " + out[0].Error}
	}
	return out[0].ID, nil
}

func refID(ref string) int {
	n, _ := tasks.ParseRef(ref)
	return n
}

func programTask(rt RouteTask, after []string) tasks.ProgramTask {
	return tasks.ProgramTask{
		NewTask: tasks.NewTask{Title: rt.Title, Notes: rt.Notes, Steps: rt.Steps, Status: rt.Status, Priority: rt.Priority, Repo: rt.Repo},
		After:   after,
		Design:  rt.Design,
	}
}

// prefixed is err with what in front of a refusal's message.
func prefixed(what string, err error) error {
	if e, ok := err.(*Error); ok {
		return &Error{Code: e.Code, Msg: what + ": " + e.Msg}
	}
	return err
}

// oneLine is s on one line, cut to n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
