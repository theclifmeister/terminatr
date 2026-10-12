package server

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAdoptAskAllows (T315): the user's go-ahead to adopt holds for its
// project and for what ran in the session when they gave it, not for
// another agent or folder started in that shell since.
func TestAdoptAskAllows(t *testing.T) {
	a := adoptAsk{project: "demo", agent: "claude", cwd: "/src/app", agentSID: "abc"}
	same := proto.SessionInfo{Agent: "claude", Cwd: "/src/app", AgentSID: "abc"}
	for _, c := range []struct {
		name    string
		ask     adoptAsk
		project string
		now     proto.SessionInfo
		want    bool
	}{
		{"as asked", a, "demo", same, true},
		{"sid not known yet", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/src/app"}, true},
		{"another project", a, "other", same, false},
		{"no go-ahead", adoptAsk{}, "demo", same, false},
		{"another agent", a, "demo", proto.SessionInfo{Agent: "codex", Cwd: "/src/app", AgentSID: "abc"}, false},
		{"another folder", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/", AgentSID: "abc"}, false},
		{"another conversation", a, "demo", proto.SessionInfo{Agent: "claude", Cwd: "/src/app", AgentSID: "xyz"}, false},
	} {
		if got := c.ask.allows(c.project, c.now); got != c.want {
			t.Errorf("%s: allows = %v, want %v", c.name, got, c.want)
		}
	}
}
