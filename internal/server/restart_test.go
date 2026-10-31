package server

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

func TestRestartSummary(t *testing.T) {
	outs := []restartOutcome{
		{rec: SessionRecord{ID: "s-9", Role: "shell"}, how: "lost"},
		{rec: SessionRecord{ID: "s-1", Role: "coordinator", Project: "p"}, how: "resumed"},
		{rec: SessionRecord{ID: "s-2", Role: "thread", Project: "p", Thread: "t-0003"}, how: "resumed", reprompted: true, queued: 2},
		{rec: SessionRecord{ID: "s-3", Role: "shell", Agent: "claude"}, how: "fresh"},
		{rec: SessionRecord{ID: "s-4", Role: "thread", Project: "p", Thread: "t-0005"}, how: "lost"},
	}
	for _, c := range []struct{ shut, want string }{
		{"crash", "server restarted after crash; resumed coordinator, t-0003; re-prompted t-0003; kept queued prompts for t-0003 (2); started fresh claude s-3; lost shell s-9, t-0005"},
		{"clean", "server restarted; resumed coordinator, t-0003; re-prompted t-0003; kept queued prompts for t-0003 (2); started fresh claude s-3; lost shell s-9, t-0005"},
	} {
		if got := restartSummary(c.shut, outs); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.shut, got, c.want)
		}
	}
}

// TestQueuedPromptsKeptAcrossRestart: a clean stop saves the prompts a
// client queued for a working agent (never the server's own) in its
// record, and the next server queues them again once the resumed
// agent is idle, behind the re-prompt.
func TestQueuedPromptsKeptAcrossRestart(t *testing.T) {
	s, _, release := slowLaunchServer(t)
	close(release)
	res, perr := s.startSession(proto.SessionStartParams{Agent: "claude", Cwd: t.TempDir()})
	if perr != nil {
		t.Fatal(perr)
	}
	id := res.(proto.SessionStartResult).Session.ID
	sess, _ := s.session(id)
	hook := func(s *Server, id, ev string, extra map[string]any) {
		t.Helper()
		p := map[string]any{"hook_event_name": ev}
		for k, v := range extra {
			p[k] = v
		}
		s.hookEvent(proto.HookEventParams{Session: id, Agent: "claude", Event: ev, Payload: p})
	}
	hook(s, id, "UserPromptSubmit", map[string]any{"prompt": "go"})
	testwait.Eventually(t, "working", func() bool {
		st, _ := sess.AgentState()
		return st.State == agent.StateWorking
	})
	for _, text := range []string{"kept-one", "kept-two"} {
		if _, perr := s.prompt(proto.SessionPromptParams{ID: id, Text: text}); perr != nil {
			t.Fatal(perr)
		}
		if _, perr := s.promptWith(proto.SessionPromptParams{ID: id, Text: "nudge-" + text}, session.PromptOptions{Channel: true}); perr != nil {
			t.Fatal(perr)
		}
	}
	s.shutdown()
	st, err := loadState(s.opts.Paths.Sessions)
	if err != nil || st == nil || len(st.Sessions) != 1 {
		t.Fatalf("sessions.json: %+v %v", st, err)
	}
	rec := st.Sessions[0]
	if !rec.Interrupted || len(rec.Queued) != 2 || rec.Queued[0].Text != "kept-one" || rec.Queued[1].Text != "kept-two" {
		t.Fatalf("record: interrupted %v, queued %+v", rec.Interrupted, rec.Queued)
	}

	s2, _, release2 := slowLaunchServer(t)
	close(release2)
	rec.Prompted = true // resumed, not started fresh
	outs := s2.resume([]SessionRecord{rec})
	if len(outs) != 1 || outs[0].how != "resumed" || !outs[0].reprompted || outs[0].queued != 2 {
		t.Fatalf("outcomes %+v", outs)
	}
	sess2, perr := s2.session(id)
	if perr != nil {
		t.Fatal(perr)
	}
	if k := sess2.KeptPrompts(); len(k) != 0 {
		t.Fatalf("queued before the agent was idle: %+v", k)
	}
	hook(s2, id, "SessionStart", map[string]any{"source": "resume"})
	testwait.Eventually(t, "the re-prompt and both kept prompts queued", func() bool {
		st, _ := sess2.AgentState()
		return st.Queued == 3
	})
	k := sess2.KeptPrompts()
	if len(k) != 2 || k[0].Text != "kept-one" || k[1].Text != "kept-two" || !k[0].Queued.After(rec.Queued[0].Queued) {
		t.Fatalf("kept after the restart %+v", k)
	}
	s2.mu.Lock()
	r2 := s2.records[id]
	s2.mu.Unlock()
	if r2.Interrupted || len(r2.Queued) != 0 {
		t.Fatalf("the live record still carries the old marks: %+v", r2)
	}
}
