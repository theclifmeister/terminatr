package project

import (
	"fmt"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDesignStatus: designing with no build, planned before a task
// moves, building once one has (or is done), done once all are.
func TestDesignStatus(t *testing.T) {
	build := func(ss ...tasks.Status) []*tasks.Task {
		var out []*tasks.Task
		for i, s := range ss {
			out = append(out, &tasks.Task{ID: i + 1, Status: s})
		}
		return out
	}
	for _, c := range []struct {
		build []*tasks.Task
		want  string
	}{
		{nil, DesignDesigning},
		{build(tasks.Open, tasks.Ready), DesignPlanned},
		{build(tasks.Open, tasks.Started), DesignBuilding},
		{build(tasks.Open, tasks.Review), DesignBuilding},
		{build(tasks.Open, tasks.Blocked), DesignBuilding},
		{build(tasks.Open, tasks.Done), DesignBuilding},
		{build(tasks.Done, tasks.Done), DesignDone},
	} {
		if got := DesignStatus(c.build); got != c.want {
			t.Errorf("%v: %s, want %s", c.build, got, c.want)
		}
	}
}

// TestDesignBuilds: a recorded design and one only a task names, each
// with its design task, its build by id from the board and the archive,
// and the design task's open questions alone.
func TestDesignBuilds(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	if _, err := s.Add(human, []tasks.NewTask{
		{Title: "Design", Status: "review"}, {Title: "B", Status: "started"}, {Title: "A", Status: "done"}, {Title: "Other"},
	}); err != nil {
		t.Fatal(err)
	}
	net, other := "net-view", "other"
	for id, d := range map[int]*string{1: &net, 2: &net, 3: &net, 4: &other} {
		if _, err := s.SetPlan(human, id, d, nil); err != nil {
			t.Fatal(err)
		}
	}
	task := "T1"
	if _, _, err := p.SetDesign(net, DesignEdit{Task: &task}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddQuestion(Question{Task: "T1", Question: "Red?", Options: []QuestionOption{{Label: "a"}, {Label: "b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddQuestion(Question{Task: "T4", Question: "Not net-view's", Options: []QuestionOption{{Label: "a"}, {Label: "b"}}}); err != nil {
		t.Fatal(err)
	}
	board, _ := s.Load()
	ds, err := p.DesignBuilds(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Name != net || ds[1].Name != other {
		t.Fatalf("designs %+v", ds)
	}
	d := ds[0]
	var build []string
	for _, t := range d.Build {
		build = append(build, t.Ref())
	}
	if d.DesignTask == nil || d.DesignTask.ID != 1 || fmt.Sprint(build) != "[T2 T3]" || d.Status() != DesignBuilding || d.Done() != 1 ||
		len(d.Questions) != 1 || d.Questions[0].Question != "Red?" {
		t.Fatalf("net-view %+v, build %v", d, build)
	}
	if o := ds[1]; o.DesignTask != nil || len(o.Build) != 1 || o.Status() != DesignPlanned || o.Questions != nil {
		t.Fatalf("other %+v", o)
	}
}

// TestBuildDepths: a cycle in after is cut where it closes, and Ordered
// lists the build by depth, then id.
func TestBuildDepths(t *testing.T) {
	build := []*tasks.Task{{ID: 1, After: []int{2}}, {ID: 2, After: []int{1}}, {ID: 3, After: []int{2, 9}}, {ID: 4}}
	d := BuildDepths(build)
	if d[1] != 1 || d[2] != 0 || d[3] != 1 || d[4] != 0 {
		t.Errorf("depths: %v", d)
	}
	var ids []int
	for _, t := range (DesignBuild{Build: build}).Ordered() {
		ids = append(ids, t.ID)
	}
	if !slices.Equal(ids, []int{2, 4, 1, 3}) {
		t.Errorf("ordered: %v", ids)
	}
}
