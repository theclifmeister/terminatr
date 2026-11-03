package ticker

import (
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// depMemo is what sweepDeps last saw of a member's task that comes
// after other projects' tasks, by "<slug>/T<n>".
type depMemo struct {
	// Met: all its dependencies were met.
	Met bool `json:"met"`
	// Gone are the refs a dep-gone item was raised for.
	Gone []string `json:"gone,omitempty"`
}

// sweepDeps computes the programs' cross-project dependency states
// (the programs design §6.2) while programs are on: each member's open
// task with refs to other projects' tasks. When the last of a task's
// dependencies becomes met it raises dep-ready in the program's inbox
// and the child's; a dependency that is gone (dropped, or no such task)
// raises dep-gone in both, once per ref. A task first seen is only
// noted. It writes the states to each program's deps.json for the
// dashboards. While programs are off nothing is looked at, and the
// memos wait, so switching on raises what became met meanwhile.
func (t *Ticker) sweepDeps(cfg *config.Config) {
	if !features.Programs(cfg) {
		return
	}
	list, err := program.List(cfg)
	if err != nil {
		t.o.Log.Printf("ticker: programs: %v", err)
		return
	}
	merged := t.mergedThreads()
	isMerged := func(slug, id string) bool { return merged[slug+"/"+id] }
	t.mu.Lock()
	if t.st.Deps == nil {
		t.st.Deps = map[string]*depMemo{}
	}
	memos := t.st.Deps
	t.mu.Unlock()
	seen := map[string]bool{}
	for _, sum := range list {
		if !sum.Folder || sum.LeftOut || sum.Error != "" {
			continue
		}
		g, err := program.Open(sum.Slug, cfg)
		if err != nil {
			continue
		}
		deps := program.NewDeps(cfg, g.Slug, isMerged)
		states := map[string]program.Dep{}
		var waits []program.Wait
		for _, m := range g.Members {
			if strings.Contains(m, "/") {
				continue // a paired server's board isn't here
			}
			if cfg.ProgramOf(m) != g.Slug {
				continue
			}
			p, err := project.OpenWith(m, cfg)
			if err != nil {
				continue
			}
			if safety, err := cfg.Safety(m); err == nil && (safety.Archived || !safety.Active) {
				continue
			}
			board, err := p.Tasks().Load()
			if err != nil {
				continue
			}
			for _, tk := range board.Tasks {
				if len(tk.AfterX) == 0 || tk.Status.Closed() {
					continue
				}
				key := m + "/" + tk.Ref()
				seen[key] = true
				t.checkDeps(g, p, tk, key, deps, states, memos)
				for _, x := range tk.AfterX {
					waits = append(waits, program.Wait{Task: key, Title: tk.Title, Status: string(tk.Status), After: x})
				}
			}
		}
		t.sweepUmbrellas(g, deps, states)
		askPushes(g.Slug, states)
		slices.SortStableFunc(waits, func(a, b program.Wait) int { return strings.Compare(a.Task, b.Task) })
		if err := program.WriteDeps(g.Slug, program.Snapshot{States: states, Waits: waits}); err != nil {
			t.o.Log.Printf("ticker: program %s: %v", g.Slug, err)
		}
	}
	t.mu.Lock()
	for k := range t.st.Deps {
		if !seen[k] {
			delete(t.st.Deps, k)
		}
	}
	t.mu.Unlock()
}

// askPushes asks the paired members of program prog for the state of
// each of their tasks in states that none pushed yet (an umbrella's
// part, a task a member's task comes after: link.AskWatch, once per
// ref); a member that didn't accept or grant status pushes stays
// unknown.
func askPushes(prog string, states map[string]program.Dep) {
	for ref, d := range states {
		if alias, _, _, ok := tasks.CrossRef(ref); !ok || alias == "" || d.State != program.DepUnknown {
			continue
		}
		if _, pushed := link.RemoteTaskOf(ref); !pushed {
			_ = link.AskWatch(prog, ref)
		}
	}
}

// sweepUmbrellas looks at program g's umbrella tasks (§5.3): each
// open one's parts' states into states, and its status, derived from
// them (program.Derive), written to its board when it changed. One
// that reaches review raises an umbrella-review item in the program's
// inbox: the user decides whether it is done.
func (t *Ticker) sweepUmbrellas(g *program.Program, deps *program.Deps, states map[string]program.Dep) {
	s := g.Tasks()
	board, err := s.Load()
	if err != nil {
		t.o.Log.Printf("ticker: program %s: %v", g.Slug, err)
		return
	}
	for _, tk := range board.Tasks {
		if tk.Status.Closed() {
			continue
		}
		if !tk.IsUmbrella() {
			// An umbrella an older route noted its parts on: adopt them.
			noted := tasks.NotedParts(tk.Notes)
			if len(noted) == 0 {
				continue
			}
			res, err := s.SetParts(caller.Caller{Kind: caller.Ticker}, tk.ID, noted, true)
			if err != nil {
				t.o.Log.Printf("ticker: program %s: %s: parts: %v", g.Slug, tk.Ref(), err)
				continue
			}
			tk = res.Task
		}
		for _, x := range tk.Parts {
			if _, ok := states[x]; !ok {
				states[x] = deps.State(x)
			}
		}
		st, ok := program.Derive(tk, program.Parts(tk, states))
		if !ok || st == tk.Status {
			continue
		}
		res, err := s.SetDerived(caller.Caller{Kind: caller.Ticker}, tk.ID, st)
		if err != nil {
			t.o.Log.Printf("ticker: program %s: %s: %v", g.Slug, tk.Ref(), err)
			continue
		}
		if !res.Changed {
			continue
		}
		t.o.Log.Printf("ticker: program %s: %s %s (from its parts)", g.Slug, tk.Ref(), st)
		if st != tasks.Review {
			continue
		}
		summary := fmt.Sprintf("every part of %s/%s is in (%s): the user decides whether it is done", g.Slug, tk.Ref(), strings.Join(tk.Parts, ", "))
		if _, err := g.AddItem(program.KindUmbrellaReview, tk.Ref(), summary, true); err != nil {
			t.o.Log.Printf("ticker: program %s: inbox: %v", g.Slug, err)
		}
	}
}

// checkDeps is sweepDeps' look at task tk of member p (key
// "acme-web/T40"): its states into states, and the items it is owed.
func (t *Ticker) checkDeps(g *program.Program, p *project.Project, tk *tasks.Task, key string, deps *program.Deps, states map[string]program.Dep, memos map[string]*depMemo) {
	all := true
	var gone, metWords []string
	for _, n := range tk.After {
		if d := deps.State(fmt.Sprintf("%s/T%d", p.Slug, n)); d.State != program.DepMet {
			all = false
		}
	}
	for _, x := range tk.AfterX {
		d := deps.State(x)
		states[x] = d
		switch d.State {
		case program.DepMet:
			metWords = append(metWords, x+" "+d.Word())
		case program.DepGone:
			gone = append(gone, x)
			all = false
		default:
			all = false
		}
	}
	t.mu.Lock()
	m := memos[key]
	first := m == nil
	if first {
		m = &depMemo{}
		memos[key] = m
	}
	t.mu.Unlock()
	// Only a task still waiting to start is told it can.
	waiting := tk.Thread == "" && (tk.Status == tasks.Open || tk.Status == tasks.Ready || tk.Status == tasks.Blocked)
	if all && !m.Met && !first && waiting {
		t.depItem(g, p, program.KindDepReady, tk,
			fmt.Sprintf("%s can start: what it comes after is met (%s)", key, strings.Join(metWords, ", ")),
			fmt.Sprintf("%s: %s can start", strings.Join(metWords, ", "), tk.Ref()), false)
	}
	m.Met = all
	for _, x := range gone {
		if slices.Contains(m.Gone, x) {
			continue
		}
		why := states[x].Word()
		t.depItem(g, p, program.KindDepGone, tk,
			fmt.Sprintf("%s comes after %s, which is %s: the user decides whether it still waits (tm program task edit %s --after …)", key, x, why, key),
			fmt.Sprintf("%s comes after %s, which is %s: ask the user whether it still waits (tm task edit %s --after …)", tk.Ref(), x, why, tk.Ref()), true)
	}
	m.Gone = gone
}

// depItem raises a dependency item about task tk of member p: in
// program g's inbox (progSummary, the user's to decide when needsUser)
// and in p's (childSummary).
func (t *Ticker) depItem(g *program.Program, p *project.Project, kind string, tk *tasks.Task, progSummary, childSummary string, needsUser bool) {
	if _, err := g.AddItem(kind, p.Slug+"/"+tk.Ref(), progSummary, needsUser); err != nil {
		t.o.Log.Printf("ticker: program %s: inbox: %v", g.Slug, err)
	} else {
		t.o.Log.Printf("ticker: program %s: inbox %s %s/%s", g.Slug, kind, p.Slug, tk.Ref())
	}
	t.item(p, kind, tk.Ref(), childSummary, false)
}

// mergedThreads are the threads whose PR the ticker saw merged, as
// "<slug>/<id>": a live thread's last poll, or the merges kept for the
// tasks that still name them.
func (t *Ticker) mergedThreads() map[string]bool {
	out := map[string]bool{}
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, m := range t.st.Threads {
		if m.PR.State == "MERGED" {
			out[k] = true
		}
	}
	for slug, pm := range t.st.Projects {
		for id := range pm.Merges {
			out[slug+"/"+id] = true
		}
	}
	return out
}
