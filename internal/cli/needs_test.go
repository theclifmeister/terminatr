package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// inbox is project slug's unhandled items.
func inbox(t *testing.T, slug string) []project.Item {
	t.Helper()
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	items, err := p.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// TestTaskAcceptSendBack: tm task accept and send-back are the
// dashboard's A and X, the human's: they drop the same items.
func TestTaskAcceptSendBack(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"
	h.ok(coord, "task", "add", "Ship it")
	h.ok(coord, "task", "add", "Fix it")
	h.ok(coord, "task", "add", "Plan it")
	h.ok(coord, "task", "status", "T1", "review")
	h.ok(coord, "task", "status", "T2", "review")

	h.expect(1, "human-only", coord, "task", "accept", "T1")
	h.expect(1, "human-only", thr, "task", "send-back", "T1", "--note", "x")
	h.expect(2, "usage", human, "task", "accept")
	h.expect(2, "takes no note", human, "task", "accept", "T1", "--note", "x")
	h.expect(1, "not-accept", human, "task", "accept", "T3")
	h.expect(1, "invalid-note", human, "task", "send-back", "T2")
	h.expect(1, "invalid-note", human, "task", "send-back", "T2", "--note", strings.Repeat("x", project.MaxSendBackNote+1))

	if out := h.ok(human, "task", "accept", "T1"); out != "told the coordinator you accept T1; it marks it done\n" {
		t.Fatalf("accept %q", out)
	}
	// Again is already true; a send-back while it waits is refused.
	if out := h.ok(human, "task", "accept", "T1"); !strings.Contains(out, "waits on the coordinator already") {
		t.Fatalf("accept again %q", out)
	}
	h.expect(1, "already-asked", human, "task", "send-back", "T1", "--note", "no")
	if out := h.ok(human, "task", "send-back", "T2", "--note", "the\ntests fail", "--json"); !strings.Contains(out, `"asked": true`) {
		t.Fatalf("send-back %q", out)
	}
	items := inbox(t, "demo")
	if len(items) != 2 || items[0].Kind != project.KindAccept || items[0].Summary != "the user accepts T1" ||
		items[1].Kind != project.KindSendBack || items[1].Summary != "the user sends T2 back: the tests fail" {
		t.Fatalf("items %+v", items)
	}
}

// TestAskAnswer: tm ask answer is the user's answer to an open question,
// an answer item that tm ask done handles with the question.
func TestAskAnswer(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env[caller.EnvProject] = "demo"
	h.ok(coord, "ask", "add", "Which repo?", "--task", "T12", "--option", "Main", "--option", "Site")
	h.ok(coord, "ask", "add", "Which parts?", "--multi", "--option", "A", "--option", "B", "--option", "C")

	h.expect(1, "human-only", coord, "ask", "answer", "Q1", "--choice", "1")
	h.expect(1, "human-only", thr, "ask", "answer", "Q1", "--choice", "1")
	h.expect(2, "go with answer", coord, "ask", "list", "--choice", "1")
	h.expect(1, "unknown-question", human, "ask", "answer", "Q9", "--choice", "1")
	h.expect(1, "invalid-answer", human, "ask", "answer", "Q1")
	h.expect(1, "invalid-answer", human, "ask", "answer", "Q1", "--choice", "3")
	h.expect(1, "invalid-answer", human, "ask", "answer", "Q1", "--choice", "1", "--choice", "2")
	h.expect(1, "invalid-answer", human, "ask", "answer", "Q1", "--choice", "1", "--other", "both")

	if out := h.ok(human, "ask", "answer", "q1", "--choice", "2"); out != "told the coordinator your answer to Q1: \"Site\"\n" {
		t.Fatalf("answer %q", out)
	}
	if out := h.ok(human, "ask", "answer", "Q1", "--choice", "Main"); !strings.Contains(out, "answered already") {
		t.Fatalf("answer again %q", out)
	}
	h.ok(human, "ask", "answer", "Q2", "--choice", "C", "--choice", "1", "--other", "and D")
	items := inbox(t, "demo")
	if len(items) != 2 || items[0].Kind != project.KindAnswer || items[0].Subject != "Q1" ||
		items[1].Summary != `the user answers Q2: "A", "C", Other: "and D"` {
		t.Fatalf("items %+v", items)
	}
	// Closing the question handles its answer.
	h.ok(coord, "ask", "done", "Q1")
	if items := inbox(t, "demo"); len(items) != 1 || items[0].Subject != "Q2" {
		t.Fatalf("after done %+v", items)
	}
}

// TestNeeds: tm needs lists what needs the user across the projects,
// with ids and the actions each takes; an agent is refused.
func TestNeeds(t *testing.T) {
	h := newHarness(t)
	stubPeerMirror(t, nil)
	h.expect(1, "human-only", coord, "needs")
	h.expect(2, "usage: tm needs", human, "needs", "extra")
	if out := h.ok(human, "needs"); !strings.Contains(out, "nothing needs you") {
		t.Fatalf("empty %q", out)
	}

	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "web")
	h.env[caller.EnvProject] = "demo"
	h.ok(coord, "task", "add", "Ship it")
	h.ok(coord, "task", "add", "Fix it")
	h.ok(coord, "task", "status", "T1", "review")
	h.ok(coord, "task", "status", "T2", "blocked", "--note", "key")
	h.ok(coord, "ask", "add", "Which repo?", "--option", "Main", "--option", "Site")
	h.ok(human, "project", "link", "demo", "web", "--ask")
	h.ok(coord, "link", "send", "web", "hello", "--project", "demo")
	h.ok(human, "task", "accept", "T1")

	var l needsList
	if err := json.Unmarshal([]byte(h.ok(human, "needs", "--json")), &l); err != nil {
		t.Fatal(err)
	}
	if l.Schema != NeedsSchema || l.Running {
		t.Fatalf("head %+v", l)
	}
	var kinds []string
	for _, n := range l.Needs {
		kinds = append(kinds, n.Kind+" "+n.Project+" "+n.ID+" "+strings.Join(n.Actions, ","))
	}
	got := strings.Join(kinds, "\n")
	want := "question demo Q1 answer\n" +
		"link-held demo M-1 approve,drop\n" +
		"task demo T1 \n" +
		"task demo T2 delegate"
	if got != want {
		t.Fatalf("needs\n%s\nwant\n%s", got, want)
	}
	if n := l.Needs[2]; n.Asked != project.KindAccept || n.Title != "Ship it" || !strings.Contains(n.Summary, "waits on the coordinator") {
		t.Fatalf("asked task %+v", n)
	}
	if q := l.Needs[0].Question; q == nil || len(q.Options) != 2 {
		t.Fatalf("question %+v", l.Needs[0])
	}

	out := h.ok(human, "needs")
	for _, s := range []string{"⚑ demo  Q1 waits for your answer", "tm ask answer Q1 --choice N --project demo",
		"tm link approve|drop M-1 --project demo", "tm task delegate T2 --project demo", "NEEDS YOU 4", "the server isn't running"} {
		if !strings.Contains(out, s) {
			t.Fatalf("text lacks %q:\n%s", s, out)
		}
	}

	// A paired server's shared project's needs come from the mirror.
	stubPeerMirror(t, &proto.PeerList{Peers: []proto.PeerMirror{{Alias: "mini", Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "api"},
		Watch: &proto.ProjectWatch{Questions: 1, NeedsYou: []proto.WatchNeed{{Why: proto.WhyReview, Task: "T3", Title: "Their task"}}}}}}}})
	if err := json.Unmarshal([]byte(h.ok(human, "needs", "--json")), &l); err != nil {
		t.Fatal(err)
	}
	last := l.Needs[len(l.Needs)-1]
	if !l.Running || last.Kind != needRemote || last.Server != "mini" || last.ID != "T3" || last.Summary != "T3 in review" || last.Title != "Their task" {
		t.Fatalf("remote %+v", l.Needs)
	}
}
