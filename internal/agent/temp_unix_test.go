//go:build !windows

package agent

import (
	"os"
	"slices"
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
		if got, _ := AgentTemp(nil, c.agent, "/home/u", env(c.env)); got != c.want {
			t.Errorf("AgentTemp(%s, %v) = %q, want %q", c.agent, c.env, got, c.want)
		}
	}
	if got := GuardTemp([]string{"relative/tmp", "$UNSET/x", "$ROOT", "$HOMEISH", "/home"}, "/home/u", env(map[string]string{"ROOT": "/", "HOMEISH": "/home/u"})); got != "" {
		t.Errorf("GuardTemp of entries that don't resolve, the root or ones holding home = %q", got)
	}
	if _, private := AgentTemp(nil, "claude", "/home/u", env(nil)); len(private) != 1 || private[0] != "/tmp/claude-"+uid+"/bash-edit-diff" {
		t.Errorf("claude's private temp folders: %q", private)
	}
}

// TestAgentTemps: Claude's temp folders are both of its manifest's
// per-user entries; Codex's /tmp fallback is no extra root.
func TestAgentTemps(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	dirs, _, scratch := AgentTemps(nil, "claude", "/home/u", func(k string) string {
		if k == "CLAUDE_CODE_TMPDIR" {
			return "/var/ct"
		}
		return ""
	})
	if want := []string{"/var/ct/claude-" + uid, "/tmp/claude-" + uid}; !slices.Equal(dirs, want) {
		t.Errorf("claude temps = %v, want %v", dirs, want)
	}
	if !scratch {
		t.Error("claude's temp folder isn't scratch-scoped")
	}
	if dirs, _, sc := AgentTemps(nil, "codex", "/home/u", func(k string) string { return map[string]string{"TMPDIR": "/var/x"}[k] }); !slices.Equal(dirs, []string{"/var/x"}) || sc {
		t.Errorf("codex temps = %v", dirs)
	}
}
