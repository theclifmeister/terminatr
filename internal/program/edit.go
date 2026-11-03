package program

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// ChildRef reads "<project>/T<n>", a member's task as tm program task
// takes it.
func ChildRef(ref string) (slug string, id int, err error) {
	alias, slug, id, ok := tasks.CrossRef(strings.TrimSpace(ref))
	if !ok {
		return "", 0, refuse("invalid-ref", "%q is not <project>/T12", ref)
	}
	if alias != "" {
		return "", 0, refuse("remote-member", "%s is on a paired server: program writes there come with paired members", ref)
	}
	return slug, id, nil
}

// EditTask applies ch to member task ref ("acme-api/T12") as c, the
// program's director or the human (§5.2): only priority, after, design
// and a note line; title, repo and ready or open only on tasks it
// filed, before they start. The child's journal and inbox say so, and
// the program's journal.
func (p *Program) EditTask(c caller.Caller, cfg *config.Config, ref string, ch tasks.ProgramChange) (tasks.Result, error) {
	if err := p.mayDirect(c, "changing a child's task"); err != nil {
		return tasks.Result{}, err
	}
	if err := p.writable(cfg, "changing children's tasks"); err != nil {
		return tasks.Result{}, err
	}
	if ch.Empty() {
		return tasks.Result{}, refuse("invalid-edit", "nothing to change: pass --priority, --after, --design, --note, --title, --repo or --status")
	}
	slug, id, err := ChildRef(ref)
	if err != nil {
		return tasks.Result{}, err
	}
	child, err := p.Member(cfg, slug)
	if err != nil {
		return tasks.Result{}, err
	}
	for _, a := range ch.After {
		for _, f := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' }) {
			if strings.Contains(f, "/") {
				if err := p.CheckCrossRef(cfg, f); err != nil {
					return tasks.Result{}, err
				}
			}
		}
	}
	res, err := child.Tasks().ProgramEdit(c, p.Slug, id, ch)
	if err != nil || !res.Changed {
		return res, err
	}
	t := res.Task
	words := fmt.Sprintf("the director of %s changed %s (%s): %s", p.Slug, t.Ref(), oneLine(t.Title, 60), changeWords(ch))
	if _, err := child.AddItem(KindTask, t.Ref(), words, false); err != nil {
		return res, err
	}
	return res, p.Journal(c, "program.task.edit", child.Slug+"/"+t.Ref(), changeWords(ch))
}

// changeWords names what ch changes, in fixed words.
func changeWords(ch tasks.ProgramChange) string {
	var w []string
	if ch.Title != nil {
		w = append(w, "title")
	}
	if ch.Repo != nil {
		w = append(w, "repo")
	}
	if ch.Status != nil {
		w = append(w, "status "+string(*ch.Status))
	}
	if ch.Priority != nil {
		w = append(w, "priority "+strings.TrimSpace(*ch.Priority))
	}
	if ch.Design != nil {
		w = append(w, "design")
	}
	if ch.After != nil {
		w = append(w, "after")
	}
	if strings.TrimSpace(ch.Note) != "" {
		w = append(w, "a note")
	}
	return strings.Join(w, ", ")
}

// Send sends text on the program channel (§8) as c:
//   - the director, or the human for it (from "" or the program's
//     slug): from the program to member to;
//   - a member's coordinator, or the human for one (from its slug):
//     from that project to the program (to is the program's slug).
func (p *Program) Send(c caller.Caller, cfg *config.Config, from, to string, o link.Opts) (*link.Message, error) {
	var fromF, toF *project.Folder
	switch {
	case c.Kind == caller.Director || !c.IsAgent() && (from == "" || from == p.Slug):
		if err := p.mayDirect(c, "the program channel"); err != nil {
			return nil, err
		}
		child, err := p.Member(cfg, to)
		if err != nil {
			return nil, err
		}
		fromF, toF = &p.Folder, &child.Folder
	case c.Kind == caller.Coordinator || !c.IsAgent():
		slug := c.Project
		if !c.IsAgent() {
			slug = from
		}
		if to != p.Slug {
			return nil, refuse("not-member", "a member writes only to its own program")
		}
		child, err := p.Member(cfg, slug)
		if err != nil {
			return nil, err
		}
		fromF, toF = &child.Folder, &p.Folder
	default:
		return nil, refuse("coordinator-only", "the program channel is the director's and the member coordinators'")
	}
	if p.LeftOut {
		return nil, refuse("program-left-out", "a project is called %s too: nothing goes through the program until one is renamed (tm doctor)", p.Slug)
	}
	return link.SendProgram(c, fromF, toF, o)
}
