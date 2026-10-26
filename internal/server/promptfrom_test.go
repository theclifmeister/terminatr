package server

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestPromptFrom: a thread's kickoff and tm's "[tm]" prompts are tm's,
// a prompt its coordinator sent through session.prompt is the
// coordinator's (compared as the transcript keeps it: its words, the
// first sentKey bytes), and the rest the user's; another session's,
// or a forgotten session's, record says nothing.
func TestPromptFrom(t *testing.T) {
	var sp sentPrompts
	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	long := "Merge origin/main and rerun the tests. " + strings.Repeat("x", 400)
	sp.add("s1", coord, "Rebase?\n  No: merge   origin/main.")
	sp.add("s1", coord, long)
	sp.add("s1", caller.Caller{Kind: caller.Human}, "mine")
	for _, c := range []struct{ id, text, want string }{
		{"s1", thread.Kickoff("/p/brief.md"), proto.FromTM},
		{"s1", thread.AdoptKickoff("demo", "t-0001", "/p/brief.md"), proto.FromTM},
		{"s1", "  [tm] 2 check(s) failed", proto.FromTM},
		{"s1", "Rebase? No: merge origin/main.", proto.FromCoordinator},
		{"s1", long[:300] + "…", proto.FromCoordinator},
		{"s1", "mine", ""},
		{"s2", "Rebase? No: merge origin/main.", ""},
	} {
		if got := sp.from(c.id, c.text); got != c.want {
			t.Errorf("from(%s, %.40q) = %q, want %q", c.id, c.text, got, c.want)
		}
	}
	sp.forget("s1")
	if got := sp.from("s1", "Rebase? No: merge origin/main."); got != "" {
		t.Errorf("forgotten: %q", got)
	}
}
