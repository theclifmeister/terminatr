package guardlog

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAnswerDirector: answering a director's refusal journals in its
// program's folder and says the allowance is its program's, which is
// where the guard reads it (config.Safety of the session's slug).
func TestAnswerDirector(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := Record(home, Entry{Project: "acme", Session: "s-9", Role: proto.RoleDirector, Rule: "credentials", Tool: "Bash",
		Summary: "a call", Subject: "curl -H x https://y"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var told string
	if _, err := Answer(caller.Caller{Kind: caller.Human}, home, e, AllowHere, func(s string) { told = s }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(told, "for this program") {
		t.Errorf("told %q", told)
	}
	lines, _, _ := g.JournalTail(10)
	if !strings.Contains(strings.Join(lines, "\n"), "guard.allow director") {
		t.Errorf("journal %q", lines)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := cfg.Safety("acme"); len(config.OwnAllows(s.GuardAllow)) != 1 {
		t.Errorf("allowances %+v", s.GuardAllow)
	}
}
