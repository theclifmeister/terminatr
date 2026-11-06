package tasks

import "strconv"

// WaitsFor is what the board's rows say of the after links (the board
// design's step 7): a task that is not done or dropped shows the tasks it
// still comes after, and a task shows how many such tasks wait for it.
// A link that is met says nothing: its task is done or dropped, archived
// (not on the board) or gone. Everything comes from the after field.
type WaitsFor struct {
	// After are the unmet refs of each open task, by task id, in after
	// order: "T10", then other projects' ("acme-api/T12").
	After map[int][]string
	// Waiters counts the open tasks that wait for each task, by id,
	// among this board's.
	Waiters map[int]int
}

// Waits reads board b. crossOpen says whether another project's task
// (a ref in AfterX) is still open; nil knows none, so those links say
// nothing.
func (b *Board) Waits(crossOpen func(ref string) bool) WaitsFor {
	w := WaitsFor{After: map[int][]string{}, Waiters: map[int]int{}}
	byID := make(map[int]*Task, len(b.Tasks))
	for _, t := range b.Tasks {
		byID[t.ID] = t
	}
	for _, t := range b.Tasks {
		if t.Status.Closed() {
			continue
		}
		for _, n := range t.After {
			if d := byID[n]; d != nil && !d.Status.Closed() {
				w.After[t.ID] = append(w.After[t.ID], "T"+strconv.Itoa(n))
				w.Waiters[n]++
			}
		}
		if crossOpen != nil {
			for _, x := range t.AfterX {
				if crossOpen(x) {
					w.After[t.ID] = append(w.After[t.ID], x)
				}
			}
		}
	}
	return w
}

// WaitWords is the muted text on a waiting task's row: "after T12", the
// first two refs and "+N" for more. "" for none.
func WaitWords(refs []string) string {
	switch len(refs) {
	case 0:
		return ""
	case 1, 2:
		out := "after " + refs[0]
		if len(refs) == 2 {
			out += ", " + refs[1]
		}
		return out
	}
	return "after " + refs[0] + ", " + refs[1] + " +" + strconv.Itoa(len(refs)-2)
}
