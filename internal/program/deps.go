package program

// Cross-project task dependencies (the programs design §6): a task's
// after refs to other members' tasks ("acme-api/T12"), checked when
// written, and their state (depState, §6.2) as the ticker computes it
// for the dep-ready and dep-gone items and for the dashboards.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Inbox item kinds of dependencies (§6.2, §6.3).
const (
	// KindDepReady: the last dependency of a member's task is met, so it
	// can start; in the program's inbox and the child's.
	KindDepReady = "dep-ready"
	// KindDepGone: a task a member's task comes after was dropped or is
	// gone; the user decides what to do, and nothing removes the ref.
	KindDepGone = "dep-gone"
)

// States of a dependency (depState, §6.2).
const (
	DepMet     = "met"
	DepOpen    = "open"
	DepUnknown = "unknown"
	DepGone    = "gone"
)

// Dep is the state of one dependency.
type Dep struct {
	State string `json:"state"`
	// Status is the task's status when it was read ("review").
	Status string `json:"status,omitempty"`
	// Merged: its thread's PR merged.
	Merged bool `json:"merged,omitempty"`
	// Why says why it is unknown or gone, in tm's words.
	Why string `json:"why,omitempty"`
	// Title is the task's title when it was read: its board's words (a
	// paired member's capped as it pushed it).
	Title string `json:"title,omitempty"`
}

// Word is how a dashboard shows d after its ref: "review", "merged",
// "done", "dropped", "not in the program".
func (d Dep) Word() string {
	switch {
	case d.Why != "":
		return d.Why
	case d.State == DepMet && d.Merged && d.Status != string(tasks.Done):
		return "merged"
	case d.Status != "":
		return d.Status
	}
	return d.State
}

// CrossRefs is the check tm task edit --after makes of refs to other
// projects' tasks on project slug's board (tasks.Store.CrossRefs): nil
// while programs are off or slug is in no program, so none is written;
// else each ref must be a local member's task of slug's program.
func CrossRefs(cfg *config.Config, slug string) func(refs []string) error {
	if !features.Programs(cfg) {
		return nil
	}
	name := cfg.ProgramOf(slug)
	if name == "" {
		return nil
	}
	return func(refs []string) error {
		p, err := Open(name, cfg)
		if err != nil {
			return err
		}
		if p.LeftOut {
			return refuse("program-left-out", "a project is called %s too: nothing routes through the program until one is renamed (tm doctor)", p.Slug)
		}
		for _, r := range refs {
			if err := p.CheckCrossRef(cfg, r); err != nil {
				return err
			}
		}
		return nil
	}
}

// NoCrossRefs is why a ref to another project's task can't be written
// on project slug's board, for a tm task edit that has no CrossRefs.
func NoCrossRefs(cfg *config.Config, slug string) error {
	if !features.Programs(cfg) {
		return refuse(features.OffCode, "%s", features.OffMsg)
	}
	return refuse("not-member", "%s is in no program: its tasks come after its own tasks only (T12)", slug)
}

// Deps looks up dependency states for program prog (§6.2), reading
// each project's board once.
type Deps struct {
	cfg  *config.Config
	prog string
	// met is the program's dependency_met.
	met string
	// merged says whether thread id of project slug has a merged PR (the
	// ticker knows); nil knows none.
	merged func(slug, id string) bool
	boards map[string]*boardRead
}

type boardRead struct {
	tasks map[int]*tasks.Task
	err   error
}

// NewDeps looks up dependencies for program prog with cfg (config.Load's).
func NewDeps(cfg *config.Config, prog string, merged func(slug, id string) bool) *Deps {
	met := config.DependencyMerged
	if t, ok := cfg.Program(prog); ok && t.DependencyMet != "" {
		met = t.DependencyMet
	}
	return &Deps{cfg: cfg, prog: prog, met: met, merged: merged, boards: map[string]*boardRead{}}
}

// State is depState(ref) for a ref to a member's task, "acme-api/T12"
// or "mini/acme-ios/T7":
//   - met: done, or (dependency_met merged) its thread's PR merged;
//   - open: neither yet, with its status;
//   - gone: dropped, or no such task on its project's board;
//   - unknown: not a member, a paired server's (until paired members
//     push their state), or its board can't be read.
func (d *Deps) State(ref string) Dep {
	alias, slug, n, ok := tasks.CrossRef(ref)
	if !ok {
		return Dep{State: DepUnknown, Why: "not a task ref"}
	}
	key := slug
	if alias != "" {
		key = alias + "/" + slug
	}
	if d.cfg.ProgramOf(key) != d.prog {
		return Dep{State: DepUnknown, Why: "not in the program"}
	}
	if alias != "" {
		// A paired member's task: as it last pushed it (program-status).
		rt, ok := link.RemoteTaskOf(ref)
		if !ok {
			return Dep{State: DepUnknown, Why: "on " + alias}
		}
		dep := Dep{State: DepOpen, Status: rt.Status, Merged: rt.Merged, Title: rt.Title}
		switch {
		case rt.Status == string(tasks.Dropped):
			dep.State = DepGone
		case rt.Status == string(tasks.Done):
			dep.State = DepMet
		case rt.Merged && d.met == config.DependencyMerged:
			dep.State = DepMet
		}
		return dep
	}
	b := d.board(slug)
	if b.err != nil {
		return Dep{State: DepUnknown, Why: "not readable"}
	}
	t := b.tasks[n]
	if t == nil {
		return Dep{State: DepGone, Why: "no such task"}
	}
	dep := Dep{State: DepOpen, Status: string(t.Status), Title: t.Title}
	if t.Thread != "" && d.merged != nil && d.merged(slug, t.Thread) {
		dep.Merged = true
	}
	switch {
	case t.Status == tasks.Dropped:
		dep.State = DepGone
	case t.Status == tasks.Done:
		dep.State = DepMet
	case dep.Merged && d.met == config.DependencyMerged:
		dep.State = DepMet
	}
	return dep
}

// board reads project slug's tasks, the archived ones too.
func (d *Deps) board(slug string) *boardRead {
	if b := d.boards[slug]; b != nil {
		return b
	}
	b := &boardRead{tasks: map[int]*tasks.Task{}}
	d.boards[slug] = b
	p, err := project.OpenWith(slug, d.cfg)
	if err != nil {
		b.err = err
		return b
	}
	s := p.Tasks()
	board, err := s.Load()
	if err != nil {
		b.err = err
		return b
	}
	if archive, err := s.LoadArchive(); err == nil {
		for _, t := range archive.Tasks {
			b.tasks[t.ID] = t
		}
	}
	for _, t := range board.Tasks {
		b.tasks[t.ID] = t
	}
	return b
}

// DepsFile is the program folder's file of its members' cross-project
// dependency states and its umbrellas' parts' (Snapshot), as the ticker
// last computed them; the dashboards read it and never recompute
// merges. Only tm writes it.
const DepsFile = "deps.json"

// Snapshot is what DepsFile holds.
type Snapshot struct {
	// States are the states of the tasks members' tasks come after and
	// of the umbrellas' parts, by ref ("acme-api/T12").
	States map[string]Dep `json:"states"`
	// Waits are the members' open tasks that come after other members'
	// tasks, one per ref, by task then ref: the dashboard's
	// DEPENDENCIES.
	Waits []Wait `json:"waits,omitempty"`
}

// Wait is one member task that comes after another member's task: Task
// ("acme-web/T40") after After ("acme-api/T12").
type Wait struct {
	Task   string `json:"task"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	After  string `json:"after"`
}

// WriteDeps writes program slug's snapshot, when it changed.
func WriteDeps(slug string, snap Snapshot) error {
	dir, err := Dir(slug)
	if err != nil {
		return err
	}
	if snap.States == nil {
		snap.States = map[string]Dep{}
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := dir + "/" + DepsFile
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return fsx.WriteAtomic(path, data, 0o644)
}

// ReadSnapshot reads program slug's snapshot; empty before the ticker
// wrote one.
func ReadSnapshot(slug string) (Snapshot, error) {
	dir, err := Dir(slug)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := os.ReadFile(dir + "/" + DepsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	if err := json.Unmarshal(data, &out); err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", DepsFile, err)
	}
	return out, nil
}

// ReadDeps reads program slug's dependency states by ref; none before
// the ticker wrote any.
func ReadDeps(slug string) (map[string]Dep, error) {
	snap, err := ReadSnapshot(slug)
	return snap.States, err
}

// AfterText is a task's after refs as a child's dashboard shows them
// (§6.3): "T10, acme-api/T12 (review)", each cross ref with its state
// from deps when known.
func AfterText(t *tasks.Task, deps map[string]Dep) string {
	out := make([]string, 0, len(t.After)+len(t.AfterX))
	for _, n := range t.After {
		out = append(out, "T"+strconv.Itoa(n))
	}
	for _, x := range t.AfterX {
		if d, ok := deps[x]; ok {
			x += " (" + d.Word() + ")"
		}
		out = append(out, x)
	}
	return strings.Join(out, ", ")
}
