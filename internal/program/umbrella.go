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
//     dependency_met says), unless the umbrella is open: the user sent
//     it back, and it stays open until a part moves;
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
	case met == live && t.Status == tasks.Open:
		// Sent back by the user (or never in review): it stays open
		// until a part moves, rather than asking again at once.
		return tasks.Open, true
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

// UmbrellaInReview is umbrella task id of p's board when it waits for the
// user's decision: an umbrella (it has parts) in review.
func (p *Program) UmbrellaInReview(id int) (*tasks.Task, error) {
	t, err := p.Tasks().Get(id)
	if err != nil {
		return nil, err
	}
	if !t.IsUmbrella() {
		return nil, refuse("not-umbrella", "%s/%s is not an umbrella task (it has no parts)", p.Slug, t.Ref())
	}
	if t.Status != tasks.Review {
		return nil, refuse("not-in-review", "%s/%s is %s: it waits for the user once every part is in (review)", p.Slug, t.Ref(), t.Status)
	}
	return t, nil
}

// AcceptUmbrella marks umbrella task id of p's board done, the user's
// accept once every part is in (§5.3), and closes its umbrella-review
// item. Refused inside an agent: only the user accepts (human-only).
func (p *Program) AcceptUmbrella(c caller.Caller, id int, note string) (tasks.Result, error) {
	if c.IsAgent() {
		return tasks.Result{}, refuse("human-only", "only the user accepts an umbrella task, by their own word: tell them which key or command does it")
	}
	t, err := p.UmbrellaInReview(id)
	if err != nil {
		return tasks.Result{}, err
	}
	res, err := p.Tasks().SetStatus(c, id, tasks.Done, note)
	if err != nil {
		return res, err
	}
	return res, p.DoneItems(KindUmbrellaReview, t.Ref())
}

// SendBackUmbrella reopens umbrella task id of p's board for the user
// (note says what should change, kept on the task), and closes its
// umbrella-review item. It stays open until a part moves again. Refused
// inside an agent (human-only).
func (p *Program) SendBackUmbrella(c caller.Caller, id int, note string) (tasks.Result, error) {
	if c.IsAgent() {
		return tasks.Result{}, refuse("human-only", "only the user sends an umbrella task back, by their own word")
	}
	t, err := p.UmbrellaInReview(id)
	if err != nil {
		return tasks.Result{}, err
	}
	why := "sent back"
	if n := strings.TrimSpace(note); n != "" {
		why += ": " + n
	}
	res, err := p.Tasks().SetStatus(c, id, tasks.Open, why)
	if err != nil {
		return res, err
	}
	return res, p.DoneItems(KindUmbrellaReview, t.Ref())
}
