package program

// program.start (the programs design §4.4, §6.3, §10): a director starts
// a thread for a member's task. This half is the program's checks; the
// start itself is tm thread start's (cli), with the caps and the
// project's own start rules.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// StartRequest is a program start: the member task ("acme-api/T12"),
// whether the user said to start it in chat (--approved-by-user), and
// whether --over-cap was passed (refused for a director).
type StartRequest struct {
	Ref      string
	Approved bool
	OverCap  bool
}

// StartCheck runs the program's checks of a start by c (§10): c acts
// for p (its director, or the human), the program is writable, the ref
// names a local member's task, --over-cap is the user's alone, and for a
// director: under authority = "propose" only with the user's word
// (needs-approval), and, without it, nothing the task comes after still
// open or unknown (waiting-on). merged says whether a thread's PR merged
// (the ticker's last look), nil for none known. It returns the member
// and the task's id.
func (p *Program) StartCheck(c caller.Caller, cfg *config.Config, req StartRequest, merged func(slug, id string) bool) (*project.Project, int, error) {
	if err := p.mayDirect(c, "starting threads in member projects"); err != nil {
		return nil, 0, err
	}
	if err := p.writable(cfg, "starting threads"); err != nil {
		return nil, 0, err
	}
	alias, slug, id, ok := tasks.CrossRef(req.Ref)
	if !ok {
		return nil, 0, refuse("invalid-ref", "%q is not a member's task: name it <project>/T<n>", req.Ref)
	}
	if alias != "" {
		slug = alias + "/" + slug
	}
	child, err := p.Member(cfg, slug)
	if err != nil {
		return nil, 0, err
	}
	if c.Kind != caller.Director {
		return child, id, nil
	}
	if req.OverCap {
		return nil, 0, refuse("human-only", "--over-cap is the user's word alone: a director never starts a thread over a cap; it waits for a slot (a slot-free item says when one frees), or the user starts it in %s's console", slug)
	}
	t, _ := cfg.Program(p.Slug)
	if t.Authority != config.AuthorityDelegate && !req.Approved {
		return nil, 0, refuse("needs-approval", "program %s's authority is propose: propose the start to the user, and once they agree start it with tm program start %s --approved-by-user", p.Slug, req.Ref)
	}
	task, err := child.Tasks().Get(id)
	if err != nil {
		return nil, 0, err
	}
	if req.Approved {
		return child, id, nil
	}
	if open := Waiting(cfg, p.Slug, slug, task, merged); len(open) > 0 {
		return nil, 0, refuse("waiting-on", "%s/%s comes after %s, not met yet: start it once they are (a dep-ready item says when), or on the user's word with --approved-by-user", slug, task.Ref(), strings.Join(open, ", "))
	}
	return child, id, nil
}

// Waiting are the dependencies of task t (of member slug of program
// prog) still open or unknown (§6.3), each with its state's word, as
// "acme-api/T12 (review)": its own board's refs and other members' alike.
// A gone one doesn't hold it: the user decides about it (dep-gone).
func Waiting(cfg *config.Config, prog, slug string, t *tasks.Task, merged func(slug, id string) bool) []string {
	deps := NewDeps(cfg, prog, merged)
	var out []string
	check := func(ref string) {
		if d := deps.State(ref); d.State == DepOpen || d.State == DepUnknown {
			out = append(out, fmt.Sprintf("%s (%s)", ref, d.Word()))
		}
	}
	for _, n := range t.After {
		check(slug + "/T" + strconv.Itoa(n))
	}
	for _, ref := range t.AfterX {
		check(ref)
	}
	return out
}

// JournalStart journals a director's start in the program (the child's
// journal has its thread.start line): "program.start acme-api/T12 t-0042".
func (p *Program) JournalStart(c caller.Caller, ref, threadID, detail string) error {
	return p.Journal(c, "program.start", ref, strings.TrimSpace(threadID+" "+detail))
}
