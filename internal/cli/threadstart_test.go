package cli

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestThreadStartServerAgent: an agent this process knows but the server
// doesn't, even after a reload, is refused before a thread is created.
func TestThreadStartServerAgent(t *testing.T) {
	h := newHarness(t) // claude installed
	var asked []string
	old := serverAgents
	serverAgents = func(_ *Env, method string) ([]proto.AgentInfo, error) {
		asked = append(asked, method)
		return []proto.AgentInfo{{Name: "codex"}}, nil
	}
	t.Cleanup(func() { serverAgents = old })
	h.ok(human, "project", "new", "demo")
	h.expect(1, `the server has no agent "claude"`, coord, "thread", "start", "Fix it", "--approved-by-user", "--project", "demo")
	if got := strings.Join(asked, " "); got != "agent.list agent.reload" {
		t.Errorf("asked the server %q", got)
	}
	if out := h.ok(coord, "thread", "list", "--project", "demo"); strings.Contains(out, "t-0001") {
		t.Fatalf("a refused start left a thread:\n%s", out)
	}
}
