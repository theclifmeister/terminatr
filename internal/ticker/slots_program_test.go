package ticker

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

const programsOnCfg = "[features]\nprograms = true\n\n"

// newProjects makes the projects slugs beside the rig's demo.
func newProjects(t *testing.T, slugs ...string) {
	t.Helper()
	for _, s := range slugs {
		if _, err := project.New(project.Options{Slug: s}); err != nil {
			t.Fatal(err)
		}
	}
}

// addTask files a task in slug with priority, after the refs given,
// and returns its ref.
func addTask(t *testing.T, slug, title, priority string, after ...string) string {
	t.Helper()
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	res, err := p.Tasks().Add(human, []tasks.NewTask{{Title: title, Priority: priority, Status: "ready"}})
	if err != nil || len(res) != 1 || res[0].ID == "" {
		t.Fatalf("add %s: %+v %v", title, res, err)
	}
	if len(after) > 0 {
		id, _ := tasks.ParseRef(res[0].ID)
		if _, err := p.Tasks().SetPlan(human, id, nil, after); err != nil {
			t.Fatal(err)
		}
	}
	return res[0].ID
}

func programItems(t *testing.T, slug string) []project.Item {
	t.Helper()
	g, err := program.Open(slug, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := g.Inbox()
	var out []project.Item
	for _, it := range items {
		if it.Kind == KindSlotFree {
			out = append(out, it)
		}
	}
	return out
}

// TestSlotFreeProgramCap: a program's cap sits between the project's
// and the global cap; a member refused at it is told when the program
// has room, in words that name the program; another program's and a
// standalone project's waiters are untouched by it. Off, the cap is
// not applied.
func TestSlotFreeProgramCap(t *testing.T) {
	r := newRig(t)
	newProjects(t, "web", "solo")
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	table := "[programs.acme]\nprojects = [\"demo\", \"web\", \"mini/ios\"]\nparallel_threads = 1\n\n[defaults]\nglobal_parallel_threads = 3\n"
	cfg := writeConfig(t, programsOnCfg+table)
	thread.Refused("web", "over-program-cap", "Fix the build", r.now.Add(-time.Minute))
	thread.Refused("solo", "over-cap", "", r.now)
	// demo's thread works: the program is full, solo isn't in it.
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	if got := slotItems(t, "web"); len(got) != 0 {
		t.Fatalf("web told while its program is full: %+v", got)
	}
	if got := slotItems(t, "solo"); len(got) != 1 || strings.Contains(got[0].Summary, "in program") {
		t.Fatalf("solo: %+v", got)
	}
	r.host.set("s-2", "idle", "")
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	got := slotItems(t, "web")
	if len(got) != 1 || !strings.Contains(got[0].Summary, "0 of 10 working in web, 0 of 1 in program acme, 0 of 3 across projects") ||
		!strings.Contains(got[0].Summary, "refused (over-program-cap)") {
		t.Fatalf("web: %+v", got)
	}

	// Off: the same waiter is told with the program full.
	r2 := newRig(t)
	newProjects(t, "web")
	cfg = writeConfig(t, table)
	thread.Refused("web", "over-program-cap", "", r2.now)
	r2.tk.slotFree(cfg, r2.host.sessions, r2.now)
	if got := slotItems(t, "web"); len(got) != 1 || strings.Contains(got[0].Summary, "in program") {
		t.Fatalf("off: %+v", got)
	}
}

// TestSlotRankWithinProgram: inside a program the waiters trade places
// by priority, then met dependencies, then age (decision 12); a
// standalone project keeps its oldest-first place, and so does
// everyone with programs off.
func TestSlotRankWithinProgram(t *testing.T) {
	r := newRig(t)
	newProjects(t, "web", "api", "solo")
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	r.host.set("s-2", "idle", "")
	could := addTask(t, "web", "Polish", "could")
	dep := addTask(t, "api", "Base", "should")
	waits := addTask(t, "api", "Needs base", "must", dep)
	must := addTask(t, "demo", "Ship", "must")
	table := "[programs.acme]\nprojects = [\"demo\", \"web\", \"api\"]\n\n[defaults]\nglobal_parallel_threads = 2\n"
	at := r.now.Add(-time.Hour)
	thread.Refused("solo", "over-global-cap", "", at)
	thread.Refused("web", "over-global-cap", could+" Polish", at.Add(time.Minute))
	thread.Refused("api", "over-global-cap", waits+" Needs base", at.Add(2*time.Minute))
	thread.Refused("demo", "over-global-cap", must+" Ship", at.Add(3*time.Minute))

	// Off: oldest first, solo and web.
	cfg := writeConfig(t, table)
	ranked := r.tk.rankWaiters(thread.Waiters(), slotPrograms{cfg: cfg, of: map[string]config.Program{}, in: map[string]bool{}}.get)
	if got := projectsOf(ranked); got != "solo web api demo" {
		t.Fatalf("off: %s", got)
	}
	cfg = writeConfig(t, programsOnCfg+table)
	ranked = r.tk.rankWaiters(thread.Waiters(), slotPrograms{cfg: cfg, of: map[string]config.Program{}, in: map[string]bool{}}.get)
	// demo's must is ready; api's must waits on an open task; web's could.
	if got := projectsOf(ranked); got != "solo demo api web" {
		t.Fatalf("on: %s", got)
	}
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	if len(slotItems(t, "solo")) != 1 || len(slotItems(t, "demo")) != 1 || len(slotItems(t, "web")) != 0 || len(slotItems(t, "api")) != 0 {
		t.Fatal("the two slots didn't go to solo and demo")
	}

	// Its dependency done, api's must is ready, and outranks nothing
	// older of the same rank: with demo's gone it goes before web.
	p, _ := project.Open("api")
	id, _ := tasks.ParseRef(dep)
	if _, err := p.Tasks().SetStatus(caller.Caller{Kind: caller.Human}, id, tasks.Done, ""); err != nil {
		t.Fatal(err)
	}
	thread.Started("demo", "acme")
	ranked = r.tk.rankWaiters(thread.Waiters(), slotPrograms{cfg: cfg, of: map[string]config.Program{}, in: map[string]bool{}}.get)
	if got := projectsOf(ranked); got != "solo api web" {
		t.Fatalf("after the dependency: %s", got)
	}
}

// TestDepsReady: a dependency is met when done, or under merged when a
// thread of it has its PR merged; one on a paired server never is
// here, nor a dropped one.
func TestDepsReady(t *testing.T) {
	r := newRig(t)
	newProjects(t, "api")
	base := addTask(t, "api", "Base", "")
	d := depReader{boards: map[string]*tasks.Board{}, archives: map[string]*tasks.Board{}, recs: map[string][]*thread.Record{}}
	merged := config.Program{DependencyMet: "merged"}
	if d.ready("demo", &tasks.Task{AfterX: []string{"api/" + base}}, merged) {
		t.Fatal("an open dependency is met")
	}
	if d.ready("demo", &tasks.Task{AfterX: []string{"mini/api/T1"}}, merged) {
		t.Fatal("a paired server's dependency is met")
	}
	// demo's thread t-0001 works on T1; its PR merged.
	addTask(t, "demo", "One", "")
	st := state{Threads: map[string]*threadMemo{"demo/t-0001": {PR: PR{Number: 4, State: "MERGED"}}}}
	d.st = State{st: st}
	if !d.ready("api", &tasks.Task{AfterX: []string{"demo/T1"}}, merged) {
		t.Fatal("a merged PR's task isn't met under merged")
	}
	if d.ready("api", &tasks.Task{AfterX: []string{"demo/T1"}}, config.Program{DependencyMet: "done"}) {
		t.Fatal("a merged PR's task is met under done")
	}
	_ = r
}

// TestSlotDirectorHold: in a delegate program with its director
// running, a slot that frees for the program is held for the director:
// one item in the program's inbox names the ranked candidates and no
// child is told; once the hold passes unused the best is told itself.
// A member's start ends the hold; programs off releases it.
func TestSlotDirectorHold(t *testing.T) {
	r := newRig(t)
	newProjects(t, "web", "api")
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	r.host.sessions = append(r.host.sessions, proto.SessionInfo{ID: "s-9", Role: proto.RoleDirector, Project: "acme", State: "idle"})
	low := addTask(t, "web", "Polish", "could")
	high := addTask(t, "api", "Ship", "must")
	table := "[programs.acme]\nprojects = [\"demo\", \"web\", \"api\"]\nauthority = \"delegate\"\nparallel_threads = 2\n"
	cfg := writeConfig(t, programsOnCfg+table)
	thread.Refused("web", "over-program-cap", low+" Polish", r.now.Add(-2*time.Minute))
	thread.Refused("api", "over-program-cap", high+" Ship", r.now.Add(-time.Minute))
	r.tk.slotFree(cfg, r.host.sessions, r.now)
	items := programItems(t, "acme")
	if len(items) != 1 || !strings.Contains(items[0].Summary, "a thread slot is free in program acme: 1 of 2 working") ||
		!strings.Contains(items[0].Summary, "best first: api "+high+" Ship (must)") ||
		strings.Index(items[0].Summary, "api ") > strings.Index(items[0].Summary, "web ") {
		t.Fatalf("director's item %+v", items)
	}
	if len(slotItems(t, "web"))+len(slotItems(t, "api")) != 0 {
		t.Fatal("a child was told during the director's hold")
	}
	ws := thread.Waiters()
	if len(ws) != 3 || ws[2].Director != "acme" || ws[0].Offered.IsZero() || ws[1].Offered.IsZero() {
		t.Fatalf("waiters %+v", ws)
	}
	// During the hold nothing more.
	r.tk.slotFree(cfg, r.host.sessions, r.now.Add(time.Minute))
	if len(programItems(t, "acme")) != 1 || len(slotItems(t, "api")) != 0 {
		t.Fatal("told again during the hold")
	}
	// The hold passes unused: the best candidate is told itself.
	later := r.now.Add(thread.SlotHold + time.Minute)
	r.tk.slotFree(cfg, r.host.sessions, later)
	if len(slotItems(t, "api")) != 1 || len(slotItems(t, "web")) != 0 || len(programItems(t, "acme")) != 1 {
		t.Fatalf("after the hold: api %+v web %+v", slotItems(t, "api"), slotItems(t, "web"))
	}
	for _, w := range thread.Waiters() {
		if w.Director != "" {
			t.Fatalf("hold kept after it passed: %+v", w)
		}
	}

	// A new waiter: a new hold, which a member's start ends (web, named
	// before, is told itself).
	r.host.set("s-2", "idle", "")
	thread.Started("api", "acme")
	thread.Refused("demo", "over-program-cap", "", later)
	r.tk.slotFree(cfg, r.host.sessions, later)
	if len(programItems(t, "acme")) != 2 {
		t.Fatalf("no new hold: %+v", thread.Waiters())
	}
	thread.Started("demo", "acme")
	for _, w := range thread.Waiters() {
		if w.Director != "" || w.Project == "demo" {
			t.Fatalf("after demo's start: %+v", w)
		}
	}

	// The director not running: the child is told.
	thread.Refused("demo", "over-program-cap", "", later)
	r.tk.slotFree(cfg, r.host.sessions[:2], later)
	if len(slotItems(t, "demo")) != 1 {
		t.Fatalf("demo not told without a director: %+v", thread.Waiters())
	}

	// Off: a hold is released and the waiters named to it go back.
	thread.UpdateWaiters(func(ws []thread.Waiter) []thread.Waiter {
		for i := range ws {
			ws[i].Offered = later
		}
		return append(ws, thread.Waiter{Director: "acme", At: later, Last: later, Told: later})
	})
	cfg = writeConfig(t, table)
	r.tk.slotFree(cfg, r.host.sessions, later)
	for _, w := range thread.Waiters() {
		if w.Director != "" || !w.Offered.IsZero() {
			t.Fatalf("off kept %+v", w)
		}
	}
}

func projectsOf(ws []thread.Waiter) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Project)
	}
	return strings.Join(out, " ")
}
