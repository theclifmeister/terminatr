package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestProgramStartChecks (B12; the programs design §10): tm program
// start's refusals before anything starts. The director's alone: a
// coordinator is refused, and a non-member, a paired member or a bad ref
// too. Under propose it needs --approved-by-user; under delegate it
// still meets the member's own start_threads, and a task whose
// dependency is open is refused waiting-on (the user's word overrides
// it). --over-cap is never the director's. Off, programs-off.
func TestProgramStartChecks(t *testing.T) {
	h, _ := programHarness(t)
	h.env[caller.EnvProject] = "acme"
	h.ok(human, "task", "add", "Infra first", "--project", "demo")
	h.ok(human, "task", "add", "Web after", "--project", "web")
	h.ok(human, "task", "edit", "T1", "--after", "demo/T1", "--project", "web")
	h.ok(human, "task", "add", "Lone", "--project", "loner")
	coordWeb := caller.Caller{Kind: caller.Coordinator, Project: "web"}

	h.expect(1, "needs-approval", directorAcme, "program", "start", "demo/T1")
	if _, _, errs := h.run(directorAcme, "", "program", "start", "demo/T1"); !strings.Contains(errs, "--approved-by-user") {
		t.Errorf("needs-approval doesn't say how: %s", errs)
	}
	h.expect(1, "director-only", coordWeb, "program", "start", "demo/T1")
	h.expect(1, "not-member", directorAcme, "program", "start", "loner/T1")
	h.expect(1, "not-member", directorAcme, "program", "start", "mini/demo/T1") // not listed: paired members go over the link
	h.expect(1, "invalid-ref", directorAcme, "program", "start", "T1")
	h.expect(1, "human-only", directorAcme, "program", "start", "demo/T1", "--over-cap", "--approved-by-user")
	h.expect(1, "other-program", directorAcme, "program", "start", "demo/T1", "--program", "other")

	// Delegate: the member's start_threads = propose still holds.
	writeTestConfig(t, h, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"web\"]\nauthority = \"delegate\"\n")
	h.expect(1, "needs-approval", directorAcme, "program", "start", "demo/T1")
	if _, _, errs := h.run(directorAcme, "", "program", "start", "demo/T1"); !strings.Contains(errs, "demo's start_threads = propose") {
		t.Errorf("the member's setting isn't named: %s", errs)
	}
	writeTestConfig(t, h, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"web\"]\nauthority = \"delegate\"\n[defaults]\nstart_threads = \"auto\"\n")
	h.expect(1, "waiting-on", directorAcme, "program", "start", "web/T1")
	if _, _, errs := h.run(directorAcme, "", "program", "start", "web/T1"); !strings.Contains(errs, "demo/T1 (open)") {
		t.Errorf("waiting-on doesn't name the open ref: %s", errs)
	}
	// On the user's word the dependency doesn't hold it: the start goes on
	// past the program's checks (here to the agent, which the harness
	// lacks).
	if code, _, errs := h.run(directorAcme, "", "program", "start", "web/T1", "--approved-by-user"); code == 0 || strings.Contains(errs, "waiting-on") || strings.Contains(errs, "needs-approval") {
		t.Errorf("approved start: exit %d %s", code, errs)
	}
	// A dropped dependency is the user's call (dep-gone), not a wait.
	h.ok(human, "task", "status", "T1", "dropped", "--project", "demo")
	if _, _, errs := h.run(directorAcme, "", "program", "start", "web/T1"); strings.Contains(errs, "waiting-on") {
		t.Errorf("a gone dependency held the start: %s", errs)
	}
	h.expect(1, "human-only", directorAcme, "program", "start", "web/T1", "--over-cap")
	// A director's plain tm thread start in a child stays refused.
	h.expect(1, "other-project", directorAcme, "thread", "start", "--task", "T1", "--project", "web")

	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\nauthority = \"delegate\"\n")
	h.expect(1, "programs-off", directorAcme, "program", "start", "web/T1")
}

// TestStartForPeer: a paired server's director's start (link's
// ProgramStarter) meets the member's own start_threads: under propose it
// is refused needs-approval, since that user's word can't come over the
// link (Q207 keeps it so for local directors under delegate too). Only a
// paired director calls it.
func TestStartForPeer(t *testing.T) {
	h, _ := programHarness(t)
	h.ok(human, "task", "add", "Login", "--project", "demo")
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartForPeer(caller.Caller{Kind: caller.Coordinator, Project: "demo"}, p, 1, "", ""); err == nil {
		t.Fatal("a coordinator went through the peer start")
	}
	_, err = StartForPeer(caller.ForPeerDirector("studio", "acme"), p, 1, "", "")
	var e *tasks.Error
	if !errors.As(err, &e) || e.Code != "needs-approval" {
		t.Fatalf("propose: %v", err)
	}
	if task, _ := p.Tasks().Get(1); task.Thread != "" {
		t.Fatalf("linked a thread: %+v", task)
	}
}
