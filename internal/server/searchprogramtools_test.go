package server

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

func programSearchFixture(t *testing.T, extra string) *Server {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	for slug, files := range map[string]map[string]string{
		"web": {
			"memory/relay.md":                         "## Decided\n- The wombat relay runs on port 4433.\n",
			"memory/coord.md":                         "---\naudience: coordinator\n---\n## Decided\n- Wombat reviews on Fridays.\n",
			"inbox/20261031T100000Z-report-t-0001.md": "The wombat report.\n",
		},
		"api":   {"memory/auth.md": "## Decided\n- Wombat tokens expire in an hour.\n"},
		"other": {"memory/x.md": "## Decided\n- Wombat outside the program.\n"},
	} {
		p, err := project.New(project.Options{Slug: slug})
		if err != nil {
			t.Fatal(err)
		}
		for name, text := range files {
			path := p.Path(filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"api\", \"mini/ios\"]\n"+extra)
	s := &Server{log: log.New(&testWriter{t: t}, "server: ", 0), sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}, modEv: newModEvents()}
	t.Cleanup(func() { s.searchIdx.CloseIdle(0) })
	return s
}

// TestProgramSearchTools: the director's search tools cover its program
// and its local members at once, a member's ids prefixed and read as a
// thread reads them; a non-member, a paired member and the coordinator's
// chunks stay out; search_children and the switch gate it.
func TestProgramSearchTools(t *testing.T) {
	s := programSearchFixture(t, "")
	dir := SessionRecord{ID: "s-1", Role: proto.RoleDirector, Project: "acme"}
	if got := toolNames(s.sessionTools(dir)); got != "search,search_timeline,search_show" {
		t.Fatalf("director's tools: %s", got)
	}
	run := func(name, input string) (int, ModToolResult) { return s.runTool(dir, name, []byte(input)) }
	code, res := run("search", `{"words":"wombat"}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "web:note:relay#decided") || !strings.Contains(res.Text, "api:note:auth#decided") {
		t.Fatalf("search: %d %+v", code, res)
	}
	for _, bad := range []string{"note:coord", "inbox:", "outside the program", "x#decided"} {
		if strings.Contains(res.Text, bad) {
			t.Errorf("search shows %q: %s", bad, res.Text)
		}
	}
	code, res = run("search", `{"words":"wombat","project":"api"}`)
	if code != http.StatusOK || strings.Contains(res.Text, "web:") || !strings.Contains(res.Text, "api:note:auth#decided") {
		t.Errorf("one member: %d %+v", code, res)
	}
	if code, res = run("search", `{"words":"wombat","project":"other"}`); code == http.StatusOK {
		t.Errorf("a non-member: %+v", res)
	}
	if code, res = run("search", `{"words":"wombat","in":["inbox"]}`); code != http.StatusOK || strings.Contains(res.Text, "web:") {
		t.Errorf("inbox: %d %+v", code, res)
	}
	code, res = run("search_show", `{"ids":["web:note:relay#decided","api:note:auth#decided","web:note:coord#decided"]}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "port 4433") || !strings.Contains(res.Text, "expire in an hour") || strings.Contains(res.Text, "Fridays") ||
		!strings.Contains(res.Text, "==> web:note:relay#decided") {
		t.Errorf("show: %d %+v", code, res)
	}
	code, res = run("search_timeline", `{"id":"web:note:relay#decided"}`)
	if code != http.StatusOK || !strings.Contains(res.Text, "> ") || !strings.Contains(res.Text, "web:note:relay#decided") {
		t.Errorf("timeline: %d %+v", code, res)
	}

	// A thread can't name a member.
	thr := SessionRecord{ID: "s-2", Role: proto.RoleThread, Project: "web", Thread: "t-0001"}
	if code, _ := s.runTool(thr, "search", []byte(`{"words":"wombat","project":"api"}`)); code != http.StatusBadRequest {
		t.Errorf("thread with project: %d", code)
	}

	// search_children off: the program's own index only.
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"api\"]\nsearch_children = false\n")
	_, res = run("search", `{"words":"wombat"}`)
	if strings.Contains(res.Text, "web:") || strings.Contains(res.Text, "api:") {
		t.Errorf("search_children off: %s", res.Text)
	}
	// Programs off: refused.
	writeConfig(t, "[programs.acme]\nprojects = [\"web\"]\n")
	if code, res = run("search", `{"words":"wombat"}`); code == http.StatusOK || !strings.Contains(res.Error, "programs") {
		t.Errorf("off: %d %+v", code, res)
	}
}

// TestProgramSearchScope: program.search is the human's or the program's
// own director's.
func TestProgramSearchScope(t *testing.T) {
	s := programSearchFixture(t, "")
	cfg, _ := config.Load()
	r, perr := s.searchIdx.ProgramQuery(cfg, proto.ProgramSearchParams{Program: "acme", Words: "wombat"}, false)
	if perr != nil || len(r.Hits) != 2 {
		t.Fatalf("human's search: %+v %v", r, perr)
	}
	if _, perr := s.searchIdx.ProgramQuery(cfg, proto.ProgramSearchParams{Program: "nope", Words: "wombat"}, false); perr == nil {
		t.Error("no such program")
	}
	m := methods[proto.MethodProgramSearch]
	req := proto.Request{Method: proto.MethodProgramSearch, Params: []byte(`{"program":"acme","words":"wombat"}`)}
	for _, c := range []struct {
		name  string
		who   caller.Caller
		human bool
		ok    bool
	}{
		{"human", caller.Caller{Kind: caller.Human}, true, true},
		{"own director", caller.Caller{Kind: caller.Director, Project: "acme"}, false, true},
		{"other director", caller.Caller{Kind: caller.Director, Project: "beta"}, false, false},
		{"coordinator of a member", caller.Caller{Kind: caller.Coordinator, Project: "web"}, false, false},
		{"thread of a member", caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}, false, false},
	} {
		_, perr := s.mayAddress(req, m, c.human, func() caller.Caller { return c.who })
		if (perr == nil) != c.ok {
			t.Errorf("%s: %v", c.name, perr)
		}
	}
}
