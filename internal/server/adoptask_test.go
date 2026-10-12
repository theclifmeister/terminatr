package server

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAdoptAskAllows (T315): the user's go-ahead to adopt holds for its
// project and for what ran in the session when they gave it, not for
// another agent or folder started in that shell since, and for
// AdoptAskTTL (T335).
func TestAdoptAskAllows(t *testing.T) {
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	a := adoptAsk{project: "demo", agent: "claude", cwd: "/src/app", agentSID: "abc", at: at}
	same := proto.SessionInfo{Agent: "claude", Cwd: "/src/app", AgentSID: "abc"}
	for _, c := range []struct {
		name    string
		ask     adoptAsk
		project string
		now     proto.SessionInfo
		t       time.Time
		want    bool
	}{
		{"as asked", a, "demo", same, at.Add(time.Minute), true},
		{"sid not known yet", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/src/app"}, at.Add(time.Minute), true},
		{"another project", a, "other", same, at.Add(time.Minute), false},
		{"no go-ahead", adoptAsk{}, "demo", same, at.Add(time.Minute), false},
		{"another agent", a, "demo", proto.SessionInfo{Agent: "codex", Cwd: "/src/app", AgentSID: "abc"}, at.Add(time.Minute), false},
		{"another folder", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/", AgentSID: "abc"}, at.Add(time.Minute), false},
		{"another conversation", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/src/app", AgentSID: "xyz"}, at.Add(time.Minute), false},
		{"just before it lapses", a, "demo", same, at.Add(AdoptAskTTL), true},
		{"lapsed", a, "demo", same, at.Add(AdoptAskTTL + time.Second), false},
	} {
		if got := c.ask.allows(c.project, c.now, c.t); got != c.want {
			t.Errorf("%s: allows = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAdoptAskWithdraw (T335): the user takes their go-ahead back for
// its project; one for another project stays.
func TestAdoptAskWithdraw(t *testing.T) {
	s := &Server{adoptAsks: map[string]adoptAsk{"s-1": {project: "demo", agent: "claude", at: time.Now()}}}
	if _, err := s.adoptAsk(proto.SessionAdoptAskParams{ID: "s-1", Project: "other", Withdraw: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.adoptAsks["s-1"]; !ok {
		t.Fatal("withdrawing for another project dropped the go-ahead")
	}
	res, err := s.adoptAsk(proto.SessionAdoptAskParams{ID: "s-1", Project: "demo", Withdraw: true})
	if err != nil || res.(proto.SessionAdoptAskResult).Asked {
		t.Fatalf("withdraw: %v %+v", err, res)
	}
	if _, ok := s.adoptAsks["s-1"]; ok {
		t.Fatal("the go-ahead stayed")
	}
}
