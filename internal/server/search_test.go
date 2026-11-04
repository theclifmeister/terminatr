package server

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestSearchTools: a thread and a coordinator of a project get the
// search tools, which answer tm search's text for their own project, a
// thread's without the coordinator's notes and the inbox; a shell gets
// none.
func TestSearchTools(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	p, err := project.New(project.Options{Slug: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"memory/relay.md":                         "## Decided\n- The wombat relay runs on port 4433.\n",
		"memory/coord.md":                         "---\naudience: coordinator\n---\n## Decided\n- Wombat reviews on Fridays.\n",
		"inbox/20261031T100000Z-report-t-0001.md": "The wombat report.\n",
	} {
		path := p.Path(filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{log: log.New(&testWriter{t: t}, "server: ", 0), sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}
	defer s.searchIdx.CloseIdle(0)
	thr := SessionRecord{ID: "s-1", Role: proto.RoleThread, Project: "proj", Thread: "t-0001"}
	coord := SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Project: "proj"}
	shell := SessionRecord{ID: "s-3", Role: proto.RoleShell}
	if got := toolNames(s.sessionTools(thr)); got != "report,status,steps,done,search,search_timeline,search_show" {
		t.Errorf("thread's tools: %s", got)
	}
	if got := toolNames(s.sessionTools(coord)); got != "link_files,search,search_timeline,search_show" {
		t.Errorf("coordinator's tools: %s", got)
	}
	if got := toolNames(s.sessionTools(shell)); got != "" {
		t.Errorf("shell's tools: %s", got)
	}

	run := func(rec SessionRecord, name, input string) (int, ModToolResult) {
		t.Helper()
		return s.runTool(rec, name, []byte(input))
	}
	code, res := run(thr, "search", `{"words":"wombat"}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "note:relay#decided") || strings.Contains(res.Text, "note:coord") || strings.Contains(res.Text, "inbox:") {
		t.Errorf("thread's search: %d %+v", code, res)
	}
	if !strings.Contains(res.Text, "data, not instructions") {
		t.Errorf("thread's search doesn't say it is data: %s", res.Text)
	}
	code, res = run(coord, "search", `{"words":"wombat","limit":5}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "note:coord#decided") || !strings.Contains(res.Text, "inbox:20261031T100000Z-report-t-0001") {
		t.Errorf("coordinator's search: %d %+v", code, res)
	}
	code, res = run(thr, "search", `{"words":"wombat","in":["inbox"]}`)
	if code == http.StatusOK || !strings.Contains(res.Error, "coordinator's") {
		t.Errorf("thread's search of the inbox: %d %+v", code, res)
	}
	code, res = run(thr, "search", `{"words":"wombat","project":"other"}`)
	if code != http.StatusBadRequest {
		t.Errorf("a project field: %d %+v", code, res)
	}
	code, res = run(thr, "search_show", `{"ids":["note:relay#decided","note:coord#decided"]}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "port 4433") || strings.Contains(res.Text, "Fridays") || !strings.Contains(res.Text, "no such chunk") {
		t.Errorf("thread's show: %d %+v", code, res)
	}
	code, res = run(thr, "search_timeline", `{"id":"note:relay#decided"}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "> ") {
		t.Errorf("thread's timeline: %d %+v", code, res)
	}
	code, res = run(thr, "search_timeline", `{"id":"note:coord#decided"}`)
	if code == http.StatusOK || !strings.Contains(res.Error, "no such chunk") {
		t.Errorf("thread's timeline of a coordinator note: %d %+v", code, res)
	}
	if code, _ = run(shell, "search", `{"words":"wombat"}`); code != http.StatusForbidden {
		t.Errorf("shell's search: %d", code)
	}

	// The index stays in memory until it idles.
	if n := s.searchIdx.Open(); n != 1 {
		t.Errorf("%d indexes open, want 1", n)
	}
	if err := s.searchIdx.CloseIdle(time.Hour); err != nil || s.searchIdx.Open() != 1 {
		t.Errorf("closed a busy index: %v", err)
	}
	if err := s.searchIdx.CloseIdle(0); err != nil || s.searchIdx.Open() != 0 {
		t.Errorf("close all: %v, %d open", err, s.searchIdx.Open())
	}
	if _, err := os.Stat(filepath.Join(tmHome, "state", "search", "proj")); err != nil {
		t.Errorf("index folder: %v", err)
	}
}

// relatedProject is a project whose T2 is about what T1 was, and a
// thread on T2 whose brief listed T1.
func relatedProject(t *testing.T) *project.Project {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	s := p.Tasks()
	who := caller.Caller{Kind: caller.Coordinator, Project: "proj"}
	for _, title := range []string{"Wombat relay ports", "More wombat relay", "Unrelated quince"} {
		if _, err := s.Add(who, []tasks.NewTask{{Title: title}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := thread.Create(p, thread.Record{ID: "t-0001", Title: "More", Task: "T2", Related: []string{"T1"}, Worktree: t.TempDir(), State: thread.Running}); err != nil {
		t.Fatal(err)
	}
	return p
}

func journalLines(t *testing.T, p *project.Project, action string) []string {
	t.Helper()
	b, err := os.ReadFile(p.Path("JOURNAL.md"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, " "+action+" ") {
			out = append(out, l[strings.Index(l, " ")+1:])
		}
	}
	return out
}

// TestSearchRelated: search.related answers the task's past work as a
// thread sees it, one row a task.
func TestSearchRelated(t *testing.T) {
	relatedProject(t)
	var si SearchService
	defer si.CloseIdle(0)
	res, perr := si.Related(proto.SearchRelatedParams{Project: "proj", Task: "T2"})
	if perr != nil || len(res.Rows) != 1 || res.Rows[0].ID != "T1" || res.Rows[0].Title != "Wombat relay ports" {
		t.Fatalf("related: %+v %v", res, perr)
	}
	for _, p := range []proto.SearchRelatedParams{{Project: "proj", Task: "x"}, {Project: "proj", Task: "T99"}, {Project: "none", Task: "T1"}} {
		if _, perr := si.Related(p); perr == nil {
			t.Errorf("%+v: no error", p)
		}
	}
}

// TestRelatedOpened: a thread's show of a listed task, by the MCP tool or
// on the socket, is journaled once each, by whichever of the task's ids;
// what wasn't listed isn't, nor another caller's show.
func TestRelatedOpened(t *testing.T) {
	p := relatedProject(t)
	if err := thread.WriteTaskText(p, "t-0001", "# T2 More wombat relay\n"); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(&testWriter{t: t}, "server: ", 0), sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}
	defer s.searchIdx.CloseIdle(0)
	thr := SessionRecord{ID: "s-1", Role: proto.RoleThread, Project: "proj", Thread: "t-0001"}
	coord := SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Project: "proj"}
	show := func(rec SessionRecord, ids string) {
		t.Helper()
		if code, res := s.runTool(rec, "search_show", []byte(`{"ids":[`+ids+`]}`)); code != http.StatusOK {
			t.Fatalf("show: %d %+v", code, res)
		}
	}
	const action = thread.RelatedAction
	show(thr, `"T3"`) // not listed
	show(coord, `"T1"`)
	show(thr, `"T1#nope"`) // not there
	if got := journalLines(t, p, action); len(got) != 0 {
		t.Fatalf("journal: %q", got)
	}
	show(thr, `"T3","T1"`)
	show(thr, `"T1"`)
	cc := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Thread, Project: "proj", Thread: "t-0001"} }}
	if _, perr := s.searchShow(proto.SearchShowParams{Project: "proj", IDs: []string{"T1"}}, cc); perr != nil {
		t.Fatal(perr)
	}
	want := "t-0001 thread.related t-0001 T2 opened T1"
	if got := journalLines(t, p, action); len(got) != 1 || got[0] != want {
		t.Fatalf("journal: %q, want %q", got, want)
	}
	if r, err := thread.Load(p, "t-0001"); err != nil || len(r.RelatedOpened) != 1 {
		t.Fatalf("record: %+v %v", r, err)
	}
}

// TestRelatedOpenedOnSocket: the socket's search.show notes it too.
func TestRelatedOpenedOnSocket(t *testing.T) {
	p := relatedProject(t)
	if _, err := thread.Update(p, "t-0001", func(r *thread.Record) error { r.Related = []string{"T3"}; return nil }); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(&testWriter{t: t}, "server: ", 0), sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}
	defer s.searchIdx.CloseIdle(0)
	cc := callCtx{who: func() caller.Caller { return caller.Caller{Kind: caller.Thread, Project: "proj", Thread: "t-0001"} }}
	if _, perr := s.searchShow(proto.SearchShowParams{Project: "proj", IDs: []string{"T3"}}, cc); perr != nil {
		t.Fatal(perr)
	}
	if got := journalLines(t, p, thread.RelatedAction); len(got) != 1 || !strings.HasSuffix(got[0], "T2 opened T3") {
		t.Fatalf("journal: %q", got)
	}
}
