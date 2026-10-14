package ticker

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// slotFree tells the coordinators a cap refused (thread.Waiters) that a
// slot freed (docs/SPEC.md §9): oldest first, one coordinator per free
// slot, one slot-free item each; the slot is then held for that waiter
// until it starts a thread or thread.SlotHold passes. A
// waiter is due when its project is under its own cap and, with a cap
// across projects, all projects are under it. A paused project's waiter
// keeps waiting; an archived or inactive one's, or one older than a day,
// is forgotten.
func (t *Ticker) slotFree(cfg *config.Config, sessions []proto.SessionInfo, now time.Time) {
	ws := thread.Waiters()
	if len(ws) == 0 {
		return
	}
	by := thread.WorkingAll(sessions)
	total := thread.Total(by)
	all, _ := cfg.AllProjects()
	global := all.GlobalParallelThreads
	told := map[string]bool{}
	drop := map[string]bool{}
	for _, w := range ws {
		p, err := project.Open(w.Project)
		if err != nil || now.Sub(w.Last) > thread.WaiterTTL || (!w.Told.IsZero() && now.Sub(w.Told) > thread.SlotHold) {
			drop[w.Project] = true
			continue
		}
		if !w.Told.IsZero() {
			// Told already: the slot is held for it.
			by[p.Slug]++
			total++
			continue
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
		t.item(p, KindSlotFree, "threads", slotWords(p.Slug, w, by[p.Slug], safety.ParallelThreads, total, global), false)
		told[w.Project] = true
		// The slot is this coordinator's to take: the next waiter needs
		// another.
		by[p.Slug]++
		total++
	}
	if len(told) == 0 && len(drop) == 0 {
		return
	}
	thread.UpdateWaiters(func(cur []thread.Waiter) []thread.Waiter {
		var keep []thread.Waiter
		for _, w := range cur {
			if drop[w.Project] {
				continue
			}
			if told[w.Project] && w.Told.IsZero() {
				w.Told = now
			}
			keep = append(keep, w)
		}
		return keep
	})
}

// slotWords is a slot-free item's summary: fixed words, counts, the
// refusal and what the coordinator wanted to start (a task ref and
// title, or a title the coordinator gave).
func slotWords(slug string, w thread.Waiter, working, capN, total, global int) string {
	s := fmt.Sprintf("a thread slot is free: %d of %d working in %s", working, capN, slug)
	if global > 0 {
		s += fmt.Sprintf(", %d of %d across projects", total, global)
	}
	s += fmt.Sprintf("; you were refused (%s) at %s", w.Code, w.At.Local().Format("15:04"))
	if w.What != "" {
		s += " for " + oneLineClip(w.What, 60)
	}
	return s + ". Start the thread you proposed if the user agreed; else remind them of it"
}

func oneLineClip(s string, n int) string {
	r := []rune(s)
	for i, c := range r {
		if c < ' ' {
			r[i] = ' '
		}
	}
	if len(r) > n {
		r = append(r[:n-1], '…')
	}
	return string(r)
}
