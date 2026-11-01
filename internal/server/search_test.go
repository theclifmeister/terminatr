package server

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
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
