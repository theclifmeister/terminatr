package tasks

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile"
)

// specExample is §6.2's example, with created dates added.
const specExample = `---
next_id: 13
---
# Tasks

## Needs you

### T7 Pick a licence for the repo
status: review · owner: me · thread: t-0002 · updated: 2026-10-04

MIT or Apache-2.0? See t-0002's report.

## In motion

### T12 Fix login redirect after OAuth
status: started · owner: claude · thread: t-0005 · updated: 2026-10-04

Users land on /home instead of the page they came from.

- [x] Reproduce with a test
- [ ] Fix the redirect
- [ ] Open a PR

## On deck

### T9 Add tm doctor
status: ready · updated: 2026-10-03

## Done

### T3 Bootstrap repo and write the spec
status: done · thread: t-0002 · updated: 2026-10-04
`

// yamlOn has the board written with YAML front matter for the test, as
// after the move (mdfile.WriteYAML).
func yamlOn(t *testing.T) {
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
}

func TestParseSpecExample(t *testing.T) {
	yamlOn(t)
	b, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	if b.NextID != 13 || len(b.Tasks) != 4 {
		t.Fatalf("next_id %d, %d tasks", b.NextID, len(b.Tasks))
	}
	t12 := b.Find(12)
	if t12.Status != Started || t12.Owner != "claude" || t12.Thread != "t-0005" {
		t.Fatalf("T12 = %+v", t12)
	}
	if t12.Notes != "Users land on /home instead of the page they came from." {
		t.Fatalf("notes %q", t12.Notes)
	}
	if len(t12.Steps) != 3 || !t12.Steps[0].Done || t12.Steps[1].Done || t12.Steps[2].N != 3 {
		t.Fatalf("steps %+v", t12.Steps)
	}
	if got := string(b.Render()); got != specExample {
		t.Fatalf("render differs from the spec example:\n%s", got)
	}
}

func TestRoundTrip(t *testing.T) {
	b := &Board{NextID: 5, Tasks: []*Task{
		{ID: 4, Title: "Steps only", Status: Open, Steps: []Step{{N: 1, Text: "a"}, {N: 2, Text: "b", Done: true}}},
		{ID: 2, Title: "Notes with a list", Status: Blocked, Notes: "Intro\n\n- [ ] not a step\n\nend.", Created: "2026-10-01"},
		{ID: 1, Title: "Plain · with dot", Status: Done},
		{ID: 3, Title: "Ready", Status: Ready, Notes: "#### sub\ntext"},
	}}
	first := b.Render()
	b2, err := Parse(first)
	if err != nil {
		t.Fatalf("%v\n%s", err, first)
	}
	if second := b2.Render(); string(second) != string(first) {
		t.Fatalf("not stable:\n%s\n---\n%s", first, second)
	}
	if b2.Find(2).Notes != "Intro\n\n- [ ] not a step\n\nend." || len(b2.Find(2).Steps) != 0 {
		t.Fatalf("T2 = %+v", b2.Find(2))
	}
	order := []int{}
	for _, t := range b2.Tasks {
		order = append(order, t.ID)
	}
	if want := []int{2, 3, 4, 1}; !equal(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct {
		in   string
		line int
	}{
		"no status":      {"# Tasks\n\n### T1 A\nhello\n", 4},
		"bad status":     {"# Tasks\n\n### T1 A\nstatus: maybe\n", 4},
		"unknown key":    {"### T1 A\nstatus: open · colour: red\n", 2},
		"duplicate":      {"### T1 A\nstatus: open\n### T1 B\nstatus: open\n", 3},
		"bad heading":    {"# Tasks\n### Fix it\nstatus: open\n", 2},
		"stray text":     {"# Tasks\nhello\n", 2},
		"front matter":   {"+++\nnext_id = 2\n+++\n# Tasks\n\n### T1 A\nstatus: nope\n", 7},
		"unclosed front": {"+++\nnext_id = 2\n", 1},
		"missing meta":   {"### T1 A", 2},
	}
	for name, c := range cases {
		_, err := Parse([]byte(c.in))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: err = %v, want ParseError", name, err)
			continue
		}
		if pe.Line != c.line {
			t.Errorf("%s: line %d, want %d (%v)", name, pe.Line, c.line, err)
		}
	}
}

func TestParseBumpsNextID(t *testing.T) {
	b, err := Parse([]byte("+++\nnext_id = 2\n+++\n### T9 Hand-added\nstatus: open\n"))
	if err != nil {
		t.Fatal(err)
	}
	if b.NextID != 10 {
		t.Fatalf("next_id %d", b.NextID)
	}
}

func TestCheckNotes(t *testing.T) {
	for _, bad := range []string{"## Done", "x\n### T3 Foo", "# Title", "text\n- [ ] a"} {
		if _, err := checkNotes(bad); err == nil {
			t.Errorf("checkNotes(%q) accepted", bad)
		}
	}
	for _, ok := range []string{"#### fine", "### Not a task", "- [ ] a\n\nend"} {
		if _, err := checkNotes(ok); err != nil {
			t.Errorf("checkNotes(%q): %v", ok, err)
		}
	}
	if !strings.HasPrefix(string((&Board{NextID: 1}).Render()), "+++\nnext_id = 1\n+++\n") {
		t.Fatal("empty board render before the move")
	}
	yamlOn(t)
	if !strings.HasPrefix(string((&Board{NextID: 1}).Render()), "---\nnext_id: 1\n---\n") {
		t.Fatal("empty board render")
	}
}

// TestGroupOfBacklog: open tasks are the backlog, after on deck (T108).
func TestGroupOfBacklog(t *testing.T) {
	if GroupOf(Open) != Backlog || GroupOf(Ready) != OnDeck || GroupOf(Done) != DoneG {
		t.Fatalf("groups %v %v %v", GroupOf(Open), GroupOf(Ready), GroupOf(Done))
	}
	b := &Board{NextID: 3, Tasks: []*Task{{ID: 1, Title: "a", Status: Open}, {ID: 2, Title: "b", Status: Ready}}}
	out := string(b.Render())
	if !strings.Contains(out, "## Backlog") || strings.Index(out, "## On deck") > strings.Index(out, "## Backlog") {
		t.Fatalf("render:\n%s", out)
	}
}

// TestThreadLink (T717): with the project's slug the metadata line links
// the thread (vaultlink); the board reads back the same either way.
func TestThreadLink(t *testing.T) {
	yamlOn(t)
	b, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	b.Slug = "demo"
	linked := string(b.Render())
	const want = "status: started · owner: claude · thread: [[demo/threads/t-0005/t-0005|t-0005]] · updated: 2026-10-04\n"
	if !strings.Contains(linked, want) {
		t.Fatalf("no thread link:\n%s", linked)
	}
	again, err := Parse([]byte(linked))
	if err != nil {
		t.Fatal(err)
	}
	if again.Find(12).Thread != "t-0005" || string(again.Render()) != specExample {
		t.Fatalf("linked board reads back as\n%s", again.Render())
	}
	b.Slug = "demo"
	if got := string(b.RenderArchive()); !strings.Contains(got, "thread: [[demo/threads/t-0002/t-0002|t-0002]]") {
		t.Fatalf("archive has no thread link:\n%s", got)
	}
}

// TestDesignAfter: a task's design and the tasks it comes after are
// linked on its metadata line and read back as the name and ids.
func TestDesignAfter(t *testing.T) {
	yamlOn(t)
	s, ev := newStore(t)
	mustAdd(t, s, coord, NewTask{Title: "A"}, NewTask{Title: "B"}, NewTask{Title: "C"})
	design := "network-view"
	if res, err := s.SetPlan(coord, 3, &design, []string{"T2, t1", "T2"}); err != nil || !res.Changed {
		t.Fatalf("%v %v", res, err)
	}
	if res, _ := s.SetPlan(coord, 3, &design, []string{"T1 T2"}); res.Changed {
		t.Fatal("plan not idempotent")
	}
	data, _ := os.ReadFile(s.TasksPath())
	const want = "design: [[demo/designs/network-view|network-view]] · after: [[demo/tasks/T1|T1]], [[demo/tasks/T2|T2]] · "
	if !strings.Contains(string(data), want) {
		t.Fatalf("no plan links:\n%s", data)
	}
	tk, _ := s.Get(3)
	if tk.Design != "network-view" || !slices.Equal(tk.After, []int{1, 2}) {
		t.Fatalf("%+v", tk)
	}
	for _, c := range []struct {
		design string
		after  []string
		code   string
	}{
		{"../x", nil, "invalid-design"},
		{"a b", nil, "invalid-design"},
		{"", []string{"T3"}, "invalid-after"},
		{"", []string{"T2,x"}, "invalid-after"},
	} {
		if _, err := s.SetPlan(coord, 3, &c.design, c.after); code(err) != c.code {
			t.Errorf("%q %q: %v, want %s", c.design, c.after, err, c.code)
		}
	}
	if _, err := s.SetPlan(thread, 3, nil, []string{""}); code(err) != "coordinator-only" {
		t.Fatalf("thread plan: %v", err)
	}
	empty := ""
	if _, err := s.SetPlan(coord, 3, &empty, []string{""}); err != nil {
		t.Fatal(err)
	}
	if tk, _ := s.Get(3); tk.Design != "" || tk.After != nil {
		t.Fatalf("not cleared: %+v", tk)
	}
	if !strings.Contains(strings.Join(ev.journal, "\n"), "task.edit T3") {
		t.Fatalf("journal %v", ev.journal)
	}
	if _, err := Parse([]byte("# Tasks\n\n### T1 A\nstatus: open · after: T1\n")); err == nil {
		t.Fatal("a task after itself read")
	}
}
