//go:build !windows

package agent

import (
	"os"
	"strconv"
	"testing"
)

// TestAgentTemp: an agent's temp folder is its manifest's first [guard]
// temp entry that resolves: Claude's $CLAUDE_CODE_TMPDIR/claude-<uid>,
// else /tmp/claude-<uid>; Codex's inherited $TMPDIR, else /tmp.
func TestAgentTemp(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	for _, c := range []struct {
		agent string
		env   map[string]string
		want  string
	}{
		{"claude", nil, "/tmp/claude-" + uid},
		{"claude", map[string]string{"CLAUDE_CODE_TMPDIR": "/var/ct/"}, "/var/ct/claude-" + uid},
		{"codex", map[string]string{"TMPDIR": "/var/folders/x/T/"}, "/var/folders/x/T"},
		{"codex", nil, "/tmp"},
		{"nosuch", nil, ""},
	} {
		if got := AgentTemp(nil, c.agent, env(c.env)); got != c.want {
			t.Errorf("AgentTemp(%s, %v) = %q, want %q", c.agent, c.env, got, c.want)
		}
	}
	if got := GuardTemp([]string{"relative/tmp", "$UNSET/x"}, env(nil)); got != "" {
		t.Errorf("GuardTemp of entries that don't resolve = %q", got)
	}
}
