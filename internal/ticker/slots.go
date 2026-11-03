package ticker

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// slotFree tells the coordinators a cap refused (thread.Waiters) that a
// slot freed (docs/SPEC.md §9): oldest first, one coordinator per free
// slot, one slot-free item each; the slot is then held for that waiter
// until it starts a thread or thread.SlotHold passes. A
// waiter is due when its project is under its own cap, its program (if
// any) under the program's, and, with a cap across projects, all
// projects are under it. A paused project's waiter keeps waiting; an
// archived or inactive one's, or one older than a day, is forgotten.
//
// Programs (on; the programs design §1, decision 12): the waiters inside
// one program are ranked among themselves by their task's priority,
// then whether its dependencies are met, then age; everyone else stays
// oldest first. In a delegate program with its director running and
// not paused, a slot that frees for the program is held for the
// director instead: one slot-free item in the program's inbox names the
// ranked candidates, and once the hold passes unused the candidates are
// told themselves. Off, no hold is kept (thread.ReleaseDirectors).
func (t *Ticker) slotFree(cfg *config.Config, sessions []proto.SessionInfo, now time.Time) {
	on := features.Programs(cfg)
	if !on {
		if names, err := thread.ReleaseDirectors(); err != nil || len(names) > 0 {
			t.o.Log.Printf("ticker: programs off: released director slots %v (%v)", names, err)
		}
	}
	ws := thread.Waiters()
	if len(ws) == 0 {
		return
	}
	by := thread.WorkingAll(sessions)
	total := thread.Total(by)
	all, _ := cfg.AllProjects()
	global := all.GlobalParallelThreads
	progs := slotPrograms{cfg: cfg, of: map[string]config.Program{}, in: map[string]bool{}}
	progBy := map[string]int{} // threads working or held in each program
	inProg := func(slug string) (config.Program, bool) {
		g, ok := progs.get(slug)
		if ok {
			if _, seen := progBy[g.Slug]; !seen {
				progBy[g.Slug] = thread.InProgram(by, g)
			}
		}
		return g, ok
	}
	// The directors' live holds count as working in their program.
	held := map[string]bool{}
	dropDir := map[string]bool{}
	var waiters []thread.Waiter
	for _, w := range ws {
		if w.Director == "" {
			waiters = append(waiters, w)
			continue
		}
		if !on || now.Sub(w.Told) > thread.SlotHold {
			dropDir[w.Director] = true
			continue
		}
		held[w.Director] = true
		if g, ok := cfg.Program(w.Director); ok {
			if _, seen := progBy[g.Slug]; !seen {
				progBy[g.Slug] = thread.InProgram(by, g)
			}
			progBy[g.Slug]++
		}
		total++
	}
	waiters = t.rankWaiters(cfg, waiters, progs.get)
	told := map[string]bool{}
	drop := map[string]bool{}
	offered := map[string]bool{}
	var holds []thread.Waiter
	for i, w := range waiters {
		p, err := project.Open(w.Project)
		if err != nil || now.Sub(w.Last) > thread.WaiterTTL || (!w.Told.IsZero() && now.Sub(w.Told) > thread.SlotHold) {
			drop[w.Project] = true
			continue
		}
		g, member := inProg(p.Slug)
		if !w.Told.IsZero() {
			// Told already: the slot is held for it.
			by[p.Slug]++
			total++
			if member {
				progBy[g.Slug]++
			}
			continue
		}
		if offered[w.Project] {
			continue // named to its director this sweep
		}
		safety, err := cfg.Safety(p.Slug)
		if err != nil {
			continue
		}
		if safety.Archived || !safety.Active {
			drop[w.Project] = true
			continue
		}
		if safety.Paused {
			continue
		}
		free := safety.ParallelThreads - by[p.Slug]
		if free <= 0 || (global > 0 && total >= global) {
			continue
		}
		if member && g.ParallelThreads > 0 && progBy[g.Slug] >= g.ParallelThreads {
			continue
		}
		if member && directorDecides(g, sessions) {
			if held[g.Slug] {
				continue // its director holds the program's slot
			}
			if w.Offered.IsZero() {
				names := t.offer(cfg, g, waiters[i:], by, progBy[g.Slug], total, global, now)
				if len(names) > 0 {
					for _, n := range names {
						offered[n] = true
					}
					held[g.Slug] = true
					holds = append(holds, thread.Waiter{Director: g.Slug, Code: "slot-free", At: now, Last: now, Told: now, What: strings.Join(names, ", ")})
					progBy[g.Slug]++
					total++
					continue
				}
			}
			// Named to the director before, whose hold passed unused: it
			// is told itself.
		}
		words := slotWords(p.Slug, w, by[p.Slug], safety.ParallelThreads, total, global)
		if member && g.ParallelThreads > 0 {
			words = slotProgramWords(p.Slug, w, by[p.Slug], safety.ParallelThreads, g, progBy[g.Slug], total, global)
		}
		t.item(p, KindSlotFree, "threads", words, false)
		told[w.Project] = true
		// The slot is this coordinator's to take: the next waiter needs
		// another.
		by[p.Slug]++
		total++
		if member {
			progBy[g.Slug]++
		}
	}
	if len(told) == 0 && len(drop) == 0 && len(offered) == 0 && len(dropDir) == 0 && len(holds) == 0 {
		return
	}
	thread.UpdateWaiters(func(cur []thread.Waiter) []thread.Waiter {
		var keep []thread.Waiter
		for _, w := range cur {
			if w.Director != "" {
				if on && now.Sub(w.Told) <= thread.SlotHold {
					keep = append(keep, w)
				}
				continue
			}
			if drop[w.Project] {
				continue
			}
			if told[w.Project] && w.Told.IsZero() {
				w.Told = now
			}
			if offered[w.Project] && w.Offered.IsZero() {
				w.Offered = now
			}
			keep = append(keep, w)
		}
		return append(keep, holds...)
	})
}

// slotPrograms caches thread.ProgramOf per project slug for one sweep.
type slotPrograms struct {
	cfg *config.Config
	of  map[string]config.Program
	in  map[string]bool
}

func (s slotPrograms) get(slug string) (config.Program, bool) {
	if ok, seen := s.in[slug]; seen {
		return s.of[slug], ok
	}
	g, ok := thread.ProgramOf(s.cfg, slug)
	s.of[slug], s.in[slug] = g, ok
	return g, ok
}

// directorDecides says whether a slot freeing in program g is held for
// its director: delegate authority, the program not paused, and its
// director's session running.
func directorDecides(g config.Program, sessions []proto.SessionInfo) bool {
	if g.Authority != config.AuthorityDelegate || g.Paused {
		return false
	}
	return slices.ContainsFunc(sessions, func(s proto.SessionInfo) bool {
		return s.Role == proto.RoleDirector && s.Project == g.Slug && s.State != "exited" && s.State != "stopped"
	})
}

// offer holds a slot for program g's director: one slot-free item in
// the program's inbox naming the candidates, the program's waiters from
// rest (ranked) that are not yet told or named and whose project has
// room. It returns their projects, none when it raised no item.
func (t *Ticker) offer(cfg *config.Config, g config.Program, rest []thread.Waiter, by map[string]int, inProgram, total, global int, now time.Time) []string {
	var names, lines []string
	for _, w := range rest {
		if !w.Told.IsZero() || !w.Offered.IsZero() || now.Sub(w.Last) > thread.WaiterTTL || cfg.ProgramOf(w.Project) != g.Slug {
			continue
		}
		safety, err := cfg.Safety(w.Project)
		if err != nil || safety.Paused || safety.Archived || !safety.Active || safety.ParallelThreads-by[w.Project] <= 0 {
			continue
		}
		names = append(names, w.Project)
		lines = append(lines, candidateWords(w, t.waiterTask(w)))
	}
	if len(names) == 0 {
		return nil
	}
	gp, err := program.Open(g.Slug, cfg)
	if err != nil {
		t.o.Log.Printf("ticker: program %s: %v", g.Slug, err)
		return nil
	}
	s := fmt.Sprintf("a thread slot is free in program %s: %d", g.Slug, inProgram)
	if g.ParallelThreads > 0 {
		s += fmt.Sprintf(" of %d", g.ParallelThreads)
	}
	s += " working"
	if global > 0 {
		s += fmt.Sprintf(", %d of %d across projects", total, global)
	}
	s += fmt.Sprintf("; it is held for you for %d minutes. Candidates, best first: %s. Start the best within the caps if it should go now; if none starts, the coordinators are told themselves when the hold ends",
		int(thread.SlotHold/time.Minute), strings.Join(lines, "; "))
	if _, err := gp.AddItem(KindSlotFree, "threads", s, false); err != nil {
		t.o.Log.Printf("ticker: program %s: inbox: %v", g.Slug, err)
		return nil
	}
	t.o.Log.Printf("ticker: program %s: inbox %s held for the director: %v", g.Slug, KindSlotFree, names)
	return names
}

// waiterRank is what ranks a waiter within its program.
type waiterRank struct {
	task  *tasks.Task // nil: it named no task, or one not on the board
	ready bool        // its dependencies are met (none counts as met)
}

// waiterTask reads the task a waiter wanted to start ("T12" first in
// its words), nil for none.
func (t *Ticker) waiterTask(w thread.Waiter) *tasks.Task {
	f := strings.Fields(w.What)
	if len(f) == 0 || !strings.HasPrefix(f[0], "T") {
		return nil
	}
	id, ok := tasks.ParseRef(f[0])
	if !ok {
		return nil
	}
	p, err := project.Open(w.Project)
	if err != nil {
		return nil
	}
	b, err := p.Tasks().Load()
	if err != nil {
		return nil
	}
	return b.Find(id)
}

// rankWaiters orders the waiters for a sweep: oldest first, except that
// the waiters inside one program trade places among themselves by
// priority, then dependencies met, then age (decision 12). progOf says
// a project's program, if any.
func (t *Ticker) rankWaiters(cfg *config.Config, ws []thread.Waiter, progOf func(string) (config.Program, bool)) []thread.Waiter {
	groups := map[string][]int{} // program slug → positions in ws
	for i, w := range ws {
		if g, ok := progOf(w.Project); ok {
			groups[g.Slug] = append(groups[g.Slug], i)
		}
	}
	if len(groups) == 0 {
		return ws
	}
	out := slices.Clone(ws)
	merged := t.mergedThreads()
	isMerged := func(slug, id string) bool { return merged[slug+"/"+id] }
	for _, pos := range groups {
		if len(pos) < 2 {
			continue
		}
		g, _ := progOf(ws[pos[0]].Project)
		deps := program.NewDeps(cfg, g.Slug, isMerged)
		members := make([]thread.Waiter, len(pos))
		ranks := map[string]waiterRank{}
		for k, i := range pos {
			members[k] = ws[i]
			task := t.waiterTask(ws[i])
			ranks[ws[i].Project] = waiterRank{task: task, ready: task == nil || depsMet(deps, ws[i].Project, task)}
		}
		slices.SortStableFunc(members, func(a, b thread.Waiter) int {
			ra, rb := ranks[a.Project], ranks[b.Project]
			if c := cmp.Compare(priorityRank(ra.task), priorityRank(rb.task)); c != 0 {
				return c
			}
			if ra.ready != rb.ready {
				if ra.ready {
					return -1
				}
				return 1
			}
			return a.At.Compare(b.At)
		})
		for k, i := range pos {
			out[i] = members[k]
		}
	}
	return out
}

func priorityRank(t *tasks.Task) int {
	if t == nil {
		return tasks.NoPriority.Rank()
	}
	return t.Priority.Rank()
}

// depsMet says whether every task t (of project slug) comes after is
// met, by program.Deps (the programs design §6.2): its own board's refs
// and other members' alike; open, unknown and gone are not met.
func depsMet(deps *program.Deps, slug string, t *tasks.Task) bool {
	for _, n := range t.After {
		if deps.State(slug+"/T"+strconv.Itoa(n)).State != program.DepMet {
			return false
		}
	}
	for _, ref := range t.AfterX {
		if deps.State(ref).State != program.DepMet {
			return false
		}
	}
	return true
}

// candidateWords is one candidate in a director's slot-free item: the
// project, what its coordinator wanted to start, the task's priority
// and whether its dependencies are met, as plain words.
func candidateWords(w thread.Waiter, t *tasks.Task) string {
	s := w.Project
	if w.What != "" {
		s += " " + textsafe.Line(w.What, 60)
	}
	if t != nil {
		s += " (" + t.Priority.String() + ")"
	}
	return s + fmt.Sprintf(", refused (%s) at %s", w.Code, w.At.Local().Format("15:04"))
}

// slotWords is a slot-free item's summary: fixed words, counts, the
// refusal and what the coordinator wanted to start (a task ref and
// title, or a title the coordinator gave).
func slotWords(slug string, w thread.Waiter, working, capN, total, global int) string {
	s := fmt.Sprintf("a thread slot is free: %d of %d working in %s", working, capN, slug)
	if global > 0 {
		s += fmt.Sprintf(", %d of %d across projects", total, global)
	}
	return s + slotTail(w)
}

// slotProgramWords is slotWords for a member of a program with a cap.
func slotProgramWords(slug string, w thread.Waiter, working, capN int, g config.Program, inProgram, total, global int) string {
	s := fmt.Sprintf("a thread slot is free: %d of %d working in %s, %d of %d in program %s", working, capN, slug, inProgram, g.ParallelThreads, g.Slug)
	if global > 0 {
		s += fmt.Sprintf(", %d of %d across projects", total, global)
	}
	return s + slotTail(w)
}

func slotTail(w thread.Waiter) string {
	s := fmt.Sprintf("; you were refused (%s) at %s", w.Code, w.At.Local().Format("15:04"))
	if w.What != "" {
		s += " for " + textsafe.Line(w.What, 60)
	}
	return s + ". Start the thread you proposed if the user agreed; else remind them of it"
}
