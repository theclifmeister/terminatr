package program

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestStartCheck (B12; the programs design §6.3, §10): a director's
// start of a member's task. Under propose only with the user's word;
// under delegate on its own, unless a dependency is open or unknown
// (waiting-on), which the user's word overrides; a merged PR meets one
// (dependency_met = merged). Non-members and paired members are refused,
// and --over-cap is never a director's.
func TestStartCheck(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web := openChild(t, "acme-api").Tasks(), openChild(t, "acme-web").Tasks()
	if _, err := api.Add(human, []tasks.NewTask{{Title: "API"}, {Title: "Schema"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.Add(human, []tasks.NewTask{{Title: "Web"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.EditTask(human, cfg, "acme-web/T1", tasks.ProgramChange{After: []string{"acme-api/T1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.EditTask(human, cfg, "acme-api/T2", tasks.ProgramChange{After: []string{"T1"}}); err != nil {
		t.Fatal(err)
	}
	check := func(req StartRequest, merged func(string, string) bool) error {
		_, _, err := g.StartCheck(director, cfg, req, merged)
		return err
	}
	if err := check(StartRequest{Ref: "acme-api/T1"}, nil); code(err) != "needs-approval" {
		t.Fatalf("propose: %v", err)
	}
	if err := check(StartRequest{Ref: "acme-api/T1", Approved: true}, nil); err != nil {
		t.Fatalf("propose with the user's word: %v", err)
	}
	for ref, want := range map[string]string{"loner/T1": "not-member", "other/T1": "not-member", "mini/acme-ios/T1": "remote-member", "T1": "invalid-ref"} {
		if err := check(StartRequest{Ref: ref, Approved: true}, nil); code(err) != want {
			t.Errorf("%s: %v, want %s", ref, err, want)
		}
	}
	if err := check(StartRequest{Ref: "acme-api/T1", Approved: true, OverCap: true}, nil); code(err) != "human-only" {
		t.Fatalf("--over-cap: %v", err)
	}

	conf := strings.Replace(routeConfig, "[programs.beta]", "authority = \"delegate\"\n[programs.beta]", 1)
	writeConf(t, os.Getenv(home.Env), conf)
	cfg, _ = config.Load()
	if err := check(StartRequest{Ref: "acme-api/T1"}, nil); err != nil {
		t.Fatalf("delegate: %v", err)
	}
	for _, ref := range []string{"acme-web/T1", "acme-api/T2"} {
		err := check(StartRequest{Ref: ref}, nil)
		if code(err) != "waiting-on" || !strings.Contains(err.Error(), "acme-api/T1 (open)") {
			t.Errorf("%s: %v", ref, err)
		}
		if err := check(StartRequest{Ref: ref, Approved: true}, nil); err != nil {
			t.Errorf("%s with the user's word: %v", ref, err)
		}
	}
	if err := check(StartRequest{Ref: "acme-api/T2"}, func(slug, id string) bool { return false }); code(err) != "waiting-on" {
		t.Fatalf("unmerged: %v", err)
	}
	// T1's thread's PR merged: met.
	if _, err := api.SetThread(human, 1, "t-0001"); err != nil {
		t.Fatal(err)
	}
	merged := func(slug, id string) bool { return slug == "acme-api" && id == "t-0001" }
	if err := check(StartRequest{Ref: "acme-web/T1"}, merged); err != nil {
		t.Fatalf("merged dependency: %v", err)
	}
	// Paused, nothing starts.
	writeConf(t, os.Getenv(home.Env), strings.Replace(conf, "authority = \"delegate\"", "authority = \"delegate\"\npaused = true", 1))
	cfg, _ = config.Load()
	if err := check(StartRequest{Ref: "acme-api/T1"}, nil); code(err) != "program-paused" {
		t.Fatalf("paused: %v", err)
	}
}

// TestProgramStarted: the task store lets a member's program's director
// link a thread it started and move the task to started, and nothing
// else; another program's director is refused.
func TestProgramStarted(t *testing.T) {
	routeFixture(t, routeConfig)
	s := openChild(t, "acme-api").Tasks()
	if _, err := s.Add(human, []tasks.NewTask{{Title: "API", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.ProgramStarted(director, 1, "t-0004")
	if err != nil || res.Task.Thread != "t-0004" || res.Task.Status != tasks.Started {
		t.Fatalf("started: %+v %v", res.Task, err)
	}
	j := journal(t, &openChild(t, "acme-api").Folder)
	for _, w := range []string{"director:acme task.thread T1 t-0004", "director:acme task.status T1 started"} {
		if !strings.Contains(j, w) {
			t.Errorf("journal lacks %q:\n%s", w, j)
		}
	}
	beta := director
	beta.Project = "beta"
	if _, err := s.ProgramStarted(beta, 1, "t-0005"); code(err) != "not-member" {
		t.Fatalf("another program's director: %v", err)
	}
}
