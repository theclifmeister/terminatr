package program

// Umbrella tasks (the programs design §5.3): a task of the program's
// board whose parts are member tasks ("acme-api/T12"). A route adds
// the tasks it files to its umbrella's parts; the director adds or
// sets them with tm program task edit T<n> --parts. The status is
// derived from the parts' states (Derive), which the ticker computes
// with the dependencies' (deps.json) and writes to the board; by hand
// an umbrella takes open and ready, and the user's done and dropped.
// A sweep ("bump axios everywhere") is an umbrella with one part per
// member; the dashboard draws every umbrella's parts as a checklist.

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// KindUmbrellaReview is the program's item when every part of an
// umbrella is done or met: the user decides whether it is done.
const KindUmbrellaReview = "umbrella-review"

// SetParts sets umbrella task ref ("T4") of p's board to parts (add:
// adds those it hasn't), as c, the director or the human. Each part is
// a member's task (CheckCrossRef): a local member's that exists, or a
// paired member's that accepted the invite.
func (p *Program) SetParts(c caller.Caller, cfg *config.Config, ref string, parts []string, add bool) (tasks.Result, error) {
	if err := p.mayDirect(c, "changing umbrella tasks"); err != nil {
		return tasks.Result{}, err
	}
	if err := p.writable(cfg, "changing umbrella tasks"); err != nil {
		return tasks.Result{}, err
	}
	id, ok := tasks.ParseRef(ref)
	if !ok || strings.Contains(ref, "/") {
		return tasks.Result{}, refuse("invalid-ref", "%q is not an umbrella task of %s (T4)", ref, p.Slug)
	}
	refs, err := tasks.ReadParts(parts)
	if err != nil {
		return tasks.Result{}, err
	}
	for _, x := range refs {
		if err := p.CheckCrossRef(cfg, x); err != nil {
			return tasks.Result{}, partErr(err)
		}
	}
	return p.Tasks().SetParts(c, id, refs, add)
}

// partErr words a CheckCrossRef refusal for a part rather than an
// after ref.
func partErr(err error) error {
	e, ok := err.(*Error)
	if !ok {
		return err
	}
	msg := strings.TrimPrefix(e.Msg, "after ")
	code := e.Code
	if code == "invalid-after" {
		code = "invalid-parts"
	}
	return &Error{Code: code, Msg: "part " + msg}
}

// PartState is one part of an umbrella with its state.
type PartState struct {
	Ref string `json:"ref"`
	Dep
}

// Checked says the part counts as finished on the checklist: met.
func (s PartState) Checked() bool { return s.State == DepMet }

// Parts are umbrella t's parts with their states from deps (by ref, as
// the ticker last wrote them); a part it hasn't seen is unknown.
func Parts(t *tasks.Task, deps map[string]Dep) []PartState {
	out := make([]PartState, len(t.Parts))
	for i, x := range t.Parts {
		d, ok := deps[x]
		if !ok {
			d = Dep{State: DepUnknown, Why: "not known yet"}
		}
		out[i] = PartState{Ref: x, Dep: d}
	}
	return out
}

// Derive is the status umbrella task t takes from its parts' states
// (§5.3); ok is false when it takes none, and keeps its own:
//   - done and dropped are the user's, and stay;
//   - parts that are gone (dropped, no such task) don't count; with no
//     other part it keeps its own;
//   - blocked while any part is blocked;
//   - review when every part is met (done, or merged as the program's
//     dependency_met says);
//   - started while any part is under way (started, review, blocked or
//     met);
//   - else ready, or open if it was open (a part that went back).
func Derive(t *tasks.Task, parts []PartState) (tasks.Status, bool) {
	if t.Status.Closed() || len(parts) == 0 {
		return "", false
	}
	live, met, moving, blocked := 0, 0, 0, false
	for _, ps := range parts {
		if ps.State == DepGone {
			continue
		}
		live++
		switch {
		case ps.State == DepMet:
			met++
			moving++
		case ps.Status == string(tasks.Blocked):
			blocked = true
			moving++
		case ps.Status == string(tasks.Started) || ps.Status == string(tasks.Review):
			moving++
		}
	}
	switch {
	case live == 0:
		return "", false
	case blocked:
		return tasks.Blocked, true
	case met == live:
		return tasks.Review, true
	case moving > 0:
		return tasks.Started, true
	case t.Status == tasks.Open:
		return tasks.Open, true
	}
	return tasks.Ready, true
}

// Progress is an umbrella's checklist in short: "2/4", the parts met of
// those that count (gone ones left out), and the bar ("▮▮▯▯").
func Progress(parts []PartState) (met, of int) {
	for _, ps := range parts {
		if ps.State == DepGone {
			continue
		}
		of++
		if ps.Checked() {
			met++
		}
	}
	return met, of
}

// PartsText is an umbrella's parts as the director's context and tm
// program task list show them: "2/4: acme-infra/T21 merged, acme-api/T12
// review, …".
func PartsText(parts []PartState) string {
	met, of := Progress(parts)
	words := make([]string, len(parts))
	for i, ps := range parts {
		words[i] = ps.Ref + " " + ps.Word()
	}
	return fmt.Sprintf("%d/%d: %s", met, of, strings.Join(words, ", "))
}
