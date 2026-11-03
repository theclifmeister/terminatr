package server

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestProgramAnswerCallers (Q208): the user's own paired phone answers
// a program's question at write level, journaled as the device; the
// phone at view, an agent, a paired server's director and a paired
// server's user are refused human-only, and the question stays open
// until the write-level phone answers it.
func TestProgramAnswerCallers(t *testing.T) {
	p := testPaths(t)
	demo := newWatchProject(t)
	web, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	opts := []project.QuestionOption{{Label: "EU"}, {Label: "US"}}
	qd, err := demo.AddQuestion(project.Question{Question: "Which region do we deploy to?", Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	qw, err := web.AddQuestion(project.Question{Question: "Which region should we deploy to?", Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	g, err := program.Open("acme", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Merge(human, cfg, program.MergeRequest{Questions: []string{qd.ID + "@demo", qw.ID + "@web"}}); err != nil {
		t.Fatal(err)
	}

	got := make(chan *Server, 1)
	onRun = func(s *Server) { got <- s }
	t.Cleanup(func() { onRun = nil })
	startServer(t, p)
	s := <-got
	k, _, _ := ed25519.GenerateKey(nil)
	wk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wkb, _ := wk.PublicKey.Bytes()
	devDir := device.DirIn(p.Home)
	if _, err := device.Add(devDir, k, wkb, "Sam's iPhone", "wss://relay.invalid"); err != nil {
		t.Fatal(err)
	}
	answer := proto.ProgramAnswerParams{Program: "acme", Ref: "Q1", Choices: []string{"EU"}}
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}

	for _, c := range []caller.Caller{
		{Kind: caller.Coordinator, Project: "demo"},
		{Kind: caller.Director, Project: "acme", Session: "s-9"},
		caller.ForPeerDirector("mini", "acme"),
		caller.ForPeerHuman("mini", ""),
		caller.ForPeerHuman("mini", "Ana's iPhone"),
	} {
		if _, perr := s.programAnswer(answer, callCtx{who: func() caller.Caller { return c }}); perr == nil || perr.Code != "human-only" {
			t.Errorf("%s answered: %v", c, perr)
		}
	}

	// At view (a pairing's level), the server refuses the device's
	// answer even past its app API.
	dc, err := s.deviceConn("Sam's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	if err := dc.Call(proto.MethodProgramAnswer, answer, nil); code(err) != "human-only" {
		t.Fatalf("the device at view: %v", err)
	}
	if _, err := device.SetLevel(devDir, "Sam's iPhone", device.LevelAnswer); err != nil {
		t.Fatal(err)
	}
	if err := dc.Call(proto.MethodProgramAnswer, answer, nil); code(err) != "human-only" {
		t.Fatalf("the device at answer: %v", err)
	}
	if q, err := demo.Question(qd.ID); err != nil || q.CoveredBy != "acme/Q1" {
		t.Fatalf("a refused answer reached demo: %+v %v", q, err)
	}

	if _, err := device.SetLevel(devDir, "Sam's iPhone", device.LevelWrite); err != nil {
		t.Fatal(err)
	}
	var res program.AnswerResult
	if err := dc.Call(proto.MethodProgramAnswer, answer, &res); err != nil {
		t.Fatalf("the device at write: %v", err)
	}
	if res.Question != "Q1" || len(res.Fanned) != 2 {
		t.Errorf("answered %+v", res)
	}
	dir, _ := program.Dir("acme")
	j, err := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))
	if err != nil || !strings.Contains(string(j), "human (iphone: Sam's iPhone › program acme) program.answer Q1") {
		t.Errorf("program journal: %v\n%s", err, j)
	}
}
