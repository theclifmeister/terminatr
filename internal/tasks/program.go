package tasks

import (
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// Program writes (the programs design §5.2): what a program's director
// may do to a member project's tasks, and only through these. Status
// changes go through SetStatus, which refuses a director outright
// (coordinator-only), and accept and drop (SetApproved, refuseClosed)
// are the user's (human-only).

// DirectorOwner is the owner of a task a program's director filed:
// "director:acme".
func DirectorOwner(program string) string { return "director:" + program }

// allowDirector applies the caller rules to a director (§10).
func (s *Store) allowDirector(c caller.Caller, o op) error {
	if s.Program && c.Project != "" && c.Project == s.Slug {
		return nil // its own program's umbrella board
	}
	if o != opProgram {
		if s.Program {
			return refuse("other-program", fmt.Sprintf("the director of %s can't change program %s", c.DirectorOf(), s.Slug))
		}
		return refuse("coordinator-only", fmt.Sprintf("a director changes %s's tasks only through tm program route and tm program task edit; the rest is %s's coordinator's", s.Slug, s.Slug))
	}
	prog := c.DirectorOf()
	if s.Directs == nil || prog == "" || !s.Directs(prog) {
		return refuse("not-member", fmt.Sprintf("%s is not a member of program %s", s.Slug, prog))
	}
	return nil
}

// ProgramTask is one task a director files in a member project.
type ProgramTask struct {
	NewTask
	// After are the tasks it comes after: this board's ("T12") and other
	// projects' ("acme-web/T40"); the program layer checks the latter
	// are members' (§6.1).
	After  []string
	Design string
}

// ProgramAdd files tasks for program's director (c, or the human on
// its behalf): status ready or open only, the owner DirectorOwner, a
// note line saying who filed it (filed, e.g. "filed by the director of
// acme (acme/T4)"). As Add, a title that matches an open task is
// reported as existing, and valid items are saved when others fail.
func (s *Store) ProgramAdd(c caller.Caller, program, filed string, items []ProgramTask) ([]AddResult, error) {
	if err := s.allow(c, nil, opProgram); err != nil {
		return nil, err
	}
	plain := make([]NewTask, len(items))
	var pre []AddResult
	for i, it := range items {
		nt := it.NewTask
		switch st, ok := ParseStatus(nt.Status); {
		case nt.Status == "":
			nt.Status = string(Ready)
		case ok && st.Closed():
			pre = append(pre, AddResult{Title: nt.Title, Result: "failed", Code: "human-only", Error: "only the user sets " + string(st)})
			continue
		case ok && st != Open && st != Ready:
			pre = append(pre, AddResult{Title: nt.Title, Result: "failed", Code: "coordinator-only", Error: "a director files tasks as ready or open; " + string(st) + " is the child coordinator's"})
			continue
		}
		nt.Owner = DirectorOwner(program)
		if filed != "" {
			nt.Notes = joinNotes(nt.Notes, filed)
		}
		plain[i] = nt
	}
	if len(pre) > 0 {
		return pre, refuse(pre[0].Code, pre[0].Title+": "+pre[0].Error)
	}
	var results []AddResult
	var created []*Task
	archive, err := s.LoadArchive()
	if err != nil {
		return nil, err
	}
	err = s.mutate(func(b *Board) error {
		results, created = nil, nil
		for _, a := range archive.Tasks {
			if a.ID >= b.NextID {
				b.NextID = a.ID + 1
			}
		}
		for i, nt := range plain {
			r, t := s.addOne(c, b, nt)
			if t != nil {
				if err := s.planNew(b, t, items[i]); err != nil {
					// Take it back: the plan is part of the filing.
					b.Tasks = b.Tasks[:len(b.Tasks)-1]
					b.NextID--
					r, t = AddResult{Title: r.Title, Result: "failed"}, nil
					if e, ok := err.(*Error); ok {
						r.Code, r.Error = e.Code, e.Msg
					} else {
						r.Error = err.Error()
					}
				}
			}
			results = append(results, r)
			if t != nil {
				created = append(created, t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, t := range created {
		if err := s.journal(c, "task.add", t, t.Title); err != nil {
			return results, err
		}
	}
	return results, nil
}

// planNew sets a just-filed task's design and after refs.
func (s *Store) planNew(b *Board, t *Task, it ProgramTask) error {
	if it.Design != "" {
		d, err := checkDesign(it.Design)
		if err != nil {
			return err
		}
		t.Design = d
	}
	if len(it.After) > 0 {
		local, cross, err := s.readProgramAfter(t.ID, it.After)
		if err != nil {
			return err
		}
		for _, n := range local {
			if b.Find(n) == nil {
				return refuse("invalid-after", fmt.Sprintf("%s has no T%d", s.Slug, n))
			}
		}
		t.After, t.AfterX = local, cross
	}
	return nil
}

// readProgramAfter is readAfter, a ref to this project's own task by
// its slug ("acme-api/T12" on acme-api's board) read as this board's.
func (s *Store) readProgramAfter(id int, refs []string) ([]int, []string, error) {
	local, cross, err := readAfter(id, refs)
	if err != nil {
		return nil, nil, err
	}
	keep := cross[:0]
	for _, x := range cross {
		if alias, slug, n, _ := CrossRef(x); alias == "" && slug == s.Slug {
			if n == id {
				return nil, nil, refuse("invalid-after", fmt.Sprintf("T%d can't come after itself", id))
			}
			if !slices.Contains(local, n) {
				local = append(local, n)
			}
			continue
		}
		keep = append(keep, x)
	}
	slices.Sort(local)
	return local, keep, nil
}

// ProgramChange is what tm program task edit changes on a member's
// task; nil (and "") leave a field alone.
type ProgramChange struct {
	Priority *string
	Design   *string
	// After replaces the refs it comes after, this board's and other
	// projects'; an empty, non-nil list clears them.
	After []string
	// Note is appended to the notes, prefixed with the director and the
	// day; never a rewrite.
	Note string
	// Title and Repo only on a task the director filed, before it starts.
	Title, Repo *string
	// Status is open or ready only, on a task the director filed.
	Status *Status
}

// Empty reports whether ch changes nothing.
func (ch ProgramChange) Empty() bool {
	return ch.Priority == nil && ch.Design == nil && ch.After == nil && ch.Note == "" && ch.Title == nil && ch.Repo == nil && ch.Status == nil
}

// ProgramEdit applies ch to task id for program's director (§5.2),
// journaled "task.edit T12 <what changed>".
func (s *Store) ProgramEdit(c caller.Caller, program string, id int, ch ProgramChange) (Result, error) {
	// Membership first: a non-member's tasks aren't told apart.
	if err := s.allow(c, nil, opProgram); err != nil {
		return Result{}, err
	}
	var prio Priority
	var design, title, repo, note string
	var err error
	if ch.Status != nil {
		switch st := *ch.Status; {
		case st.Closed():
			return Result{}, refuse("human-only", "only the user accepts or drops work")
		case st != Open && st != Ready:
			return Result{}, refuse("coordinator-only", fmt.Sprintf("%s is the child coordinator's to set; a director sets only ready or open, on tasks it filed", st))
		}
	}
	if ch.Priority != nil {
		if prio, err = checkPriority(*ch.Priority); err != nil {
			return Result{}, err
		}
	}
	if ch.Design != nil {
		if design, err = checkDesign(*ch.Design); err != nil {
			return Result{}, err
		}
	}
	if ch.Title != nil {
		if title, err = checkTitle(*ch.Title); err != nil {
			return Result{}, err
		}
	}
	if ch.Repo != nil {
		if repo, err = s.checkRepo(*ch.Repo); err != nil {
			return Result{}, err
		}
	}
	if n := strings.TrimSpace(ch.Note); n != "" {
		if strings.ContainsAny(n, "\r\n") {
			return Result{}, refuse("invalid-notes", "a director's note is one line")
		}
		note = fmt.Sprintf("%s (%s): %s", DirectorOwner(program), s.today(), n)
	}
	var what []string
	res, err := s.edit(c, id, opProgram, func(t *Task) (bool, error) {
		own := c.DirectorOf() == "" || t.Owner == DirectorOwner(program)
		unstarted := t.Thread == "" && (t.Status == Open || t.Status == Ready)
		if (ch.Title != nil || ch.Repo != nil || ch.Status != nil) && !(own && unstarted) {
			return false, refuse("not-its-filing", fmt.Sprintf("a director changes the title, repo and status only of tasks it filed, before they start; %s is %s's coordinator's now", t.Ref(), s.Slug))
		}
		var local []int
		var cross []string
		if ch.After != nil {
			var err error
			if local, cross, err = s.readProgramAfter(t.ID, ch.After); err != nil {
				return false, err
			}
		}
		set := func(name string, changed bool) {
			if changed {
				what = append(what, name)
			}
		}
		if ch.Title != nil {
			set("title", t.Title != title)
			t.Title = title
		}
		if ch.Repo != nil {
			set("repo", t.Repo != repo)
			t.Repo = repo
		}
		if ch.Status != nil {
			set("status "+string(*ch.Status), t.Status != *ch.Status)
			t.Status = *ch.Status
		}
		if ch.Priority != nil {
			set("priority "+prio.String(), t.Priority != prio)
			t.Priority = prio
		}
		if ch.Design != nil {
			set("design", t.Design != design)
			t.Design = design
		}
		if ch.After != nil {
			changed := !slices.Equal(t.After, local) || !slices.Equal(t.AfterX, cross)
			t.After, t.AfterX = local, cross
			if changed {
				what = append(what, "after "+orNone(strings.Join(t.AfterRefs(), ", ")))
			}
		}
		if note != "" {
			n, err := checkNotes(joinNotes(t.Notes, note))
			if err != nil {
				return false, err
			}
			t.Notes = n
			what = append(what, "note")
		}
		return len(what) > 0, nil
	})
	if err != nil || !res.Changed {
		return res, err
	}
	return res, s.journal(c, "task.edit", res.Task, strings.Join(what, ", "))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// CheckProgramTask checks what ProgramAdd would refuse of it, before
// anything is written, so a route files all of its tasks or none: the
// title, notes, steps, priority, status (ready or open), repo and
// design. The after refs are the program layer's.
func (s *Store) CheckProgramTask(it ProgramTask) error {
	if _, err := checkTitle(it.Title); err != nil {
		return err
	}
	if _, err := checkNotes(it.Notes); err != nil {
		return err
	}
	for _, st := range it.Steps {
		if _, err := checkStep(st); err != nil {
			return err
		}
	}
	if _, err := checkPriority(it.Priority); err != nil {
		return err
	}
	if it.Status != "" {
		st, ok := ParseStatus(it.Status)
		switch {
		case !ok:
			return refuse("invalid-status", fmt.Sprintf("unknown status %q", it.Status))
		case st.Closed():
			return refuse("human-only", "only the user sets "+string(st))
		case st != Open && st != Ready:
			return refuse("coordinator-only", "a director files tasks as ready or open; "+string(st)+" is the child coordinator's")
		}
	}
	if _, err := s.checkRepo(it.Repo); err != nil {
		return err
	}
	_, err := checkDesign(it.Design)
	return err
}

// ProgramStarted links thread to task id of a member project for
// program's director (c), whose tm program start launched it (§10), and
// moves the task to started if it was open or ready: what tm thread
// start does to the task, through opProgram. Any other status change
// stays SetStatus's, which refuses a director.
func (s *Store) ProgramStarted(c caller.Caller, id int, thread string) (Result, error) {
	th, err := checkLabel("thread", thread)
	if err != nil {
		return Result{}, err
	}
	moved := false
	res, err := s.edit(c, id, opProgram, func(t *Task) (bool, error) {
		changed := t.Thread != th
		t.Thread = th
		if t.Status == Open || t.Status == Ready {
			t.Status, moved, changed = Started, true, true
		}
		return changed, nil
	})
	if err != nil || !res.Changed {
		return res, err
	}
	if err := s.journal(c, "task.thread", res.Task, th); err != nil {
		return res, err
	}
	if moved {
		return res, s.journal(c, "task.status", res.Task, string(Started))
	}
	return res, nil
}
