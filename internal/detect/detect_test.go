package detect

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

func claudeEngine(t *testing.T) *Engine {
	t.Helper()
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := reg.Get("claude")
	if !ok {
		t.Fatal("no claude manifest")
	}
	e, err := New(a.Rules())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func screen(title, body string) Screen {
	return Screen{Title: title, Rows: strings.Split(body, "\n")}
}

// Fixtures: the 2.1.289 screens recorded by the Claude spike
// (docs/research/claude.md §2, "Screen strings").
const (
	idleBox = `╭───────────────────────────────────────────╮
│ ✻ Welcome to Claude Code                  │
╰───────────────────────────────────────────╯

> say hi

⏺ Hi!

────────────────────────────────────────────
❯
────────────────────────────────────────────
  ? for shortcuts`

	working = `> write a poem

✢ Churning… (6s · ↓ 201 tokens)

────────────────────────────────────────────
❯
────────────────────────────────────────────`

	permission = `⏺ Write(x.txt)

────────────────────────────────────────────
 Create file
 x.txt
 Do you want to create x.txt?
 ❯ 1. Yes
   2. Yes, and switch to accept edits (shift+tab)
   3. No

 Esc to cancel · Tab to amend`

	question = `☐ Color

Which color do you want?

❯ 1. Red
  2. Blue
  3. Type something.

Enter to select · ↑/↓ to navigate · Esc to cancel`

	trust = ` Do you trust the files in this folder?

 /Users/me/src/x

 ❯ 1. No, exit
   2. Yes, I trust this folder

 Enter to confirm · Esc to exit`

	bypass = ` WARNING: Claude Code running in Bypass Permissions mode

 ❯ 1. No, exit
   2. Yes, I accept`

	transcript = `⏺ Bash(ls)
  ⎿  a b c

  Showing detailed transcript · ctrl+r to toggle`
)

func TestClaudeRules(t *testing.T) {
	e := claudeEngine(t)
	cases := []struct {
		name, title, body string
		rule              string
		state             agent.State
		reason            string
	}{
		{"idle prompt box", "✳ Claude Code", idleBox, "idle-title", agent.StateIdle, ""},
		{"idle box without title", "", idleBox, "idle-prompt-box", agent.StateIdle, ""},
		{"working by title spinner", "◐ Claude Code", idleBox, "working-title-spinner", agent.StateWorking, ""},
		{"working by spinner line", "", working, "working-spinner-line", agent.StateWorking, ""},
		{"permission beats the idle title", "✳ Claude Code", permission, "blocked-permission-dialog", agent.StateBlocked, "permission"},
		{"question", "✳ Claude Code", question, "blocked-question", agent.StateBlocked, "question"},
		{"trust", "", trust, "trust-folder", agent.StateBlocked, "trust"},
		{"bypass warning", "", bypass, "trust-bypass-warning", agent.StateBlocked, "trust"},
		{"transcript view abstains", "✳ Claude Code", transcript, "transcript-view", agent.StateUnknown, "transcript view"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, all := e.Eval(screen(c.title, c.body))
			if m == nil {
				t.Fatalf("no rule matched")
			}
			if m.Rule != c.rule || m.State != c.state || m.Reason != c.reason {
				t.Fatalf("got %+v (all %+v), want %s %s/%s", *m, all, c.rule, c.state, c.reason)
			}
		})
	}
}

// TestClaudeEmptyBox: the paste injector's empty-box rule, on the bytes
// Claude 2.1.289 really sends: "❯", a no-break space, then the dim
// suggestion.
func TestClaudeEmptyBox(t *testing.T) {
	e := claudeEngine(t)
	rows := []string{"──────", "❯\u00a0Try \"refactor <filepath>\"", "──────"}
	noDim := []string{"──────", "❯\u00a0                       ", "──────"}
	has := func(s Screen) bool {
		_, all := e.Eval(s)
		for _, m := range all {
			if m.Rule == "idle-empty-box" {
				return true
			}
		}
		return false
	}
	if !has(Screen{Rows: rows, NoDim: noDim}) {
		t.Fatal("an empty box with ghost text is not empty")
	}
	if has(Screen{Rows: rows, NoDim: rows}) {
		t.Fatal("typed text counts as an empty box")
	}
	// Mid-turn (2.1.306, T653): the ❯ itself is dim, with or without the
	// no-break space after it.
	rows = []string{"✻ Actualizing… (1m 32s · ↓ 4.7k tokens)", "", "──────", "❯ ", "──────", "  ⏵⏵ auto mode on"}
	for _, box := range []string{"  ", "  ", ""} {
		noDim := slices.Clone(rows)
		noDim[3] = box
		if !has(Screen{Rows: rows, NoDim: noDim}) {
			t.Fatalf("an empty box with a dim ❯ (%q) is not empty", box)
		}
	}
	draft := slices.Clone(rows)
	draft[3] = "❯ half a thought"
	noDim = slices.Clone(draft)
	noDim[3] = "  half a thought"
	if has(Screen{Rows: draft, NoDim: noDim}) {
		t.Fatal("a draft typed mid-turn counts as an empty box")
	}
}

// TestSkipDim: ghost text in the prompt box is not typed input, but the
// prompt rule still sees an idle box; a dim-only spinner is ignored by a
// skip_dim rule.
func TestSkipDim(t *testing.T) {
	e, err := New([]agent.Rule{
		{ID: "empty-box", State: agent.StateIdle, Region: "bottom:3", Regex: `(?m)^❯\s*$`, SkipDim: true},
		{ID: "typed", State: agent.StateWorking, Region: "bottom:3", Regex: `(?m)^❯ \S`},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := Screen{Rows: []string{"x", "❯ ! sleep 25", ""}, NoDim: []string{"x", "❯           ", ""}}
	_, all := e.Eval(s)
	if len(all) != 2 {
		t.Fatalf("both rules should match (one on the dimless text): %+v", all)
	}
	s.NoDim = nil // no attribute layer: dim text counts as typed
	_, all = e.Eval(s)
	if len(all) != 1 || all[0].Rule != "typed" {
		t.Fatalf("without NoDim only the typed rule matches: %+v", all)
	}
}

func TestRegionsAndNot(t *testing.T) {
	e, err := New([]agent.Rule{
		{ID: "bottom", State: agent.StateIdle, Region: "bottom:1", Contains: []string{"last"}},
		{ID: "top-only", State: agent.StateWorking, Region: "bottom:1", Contains: []string{"first"}},
		{ID: "not", State: agent.StateBlocked, Priority: 9, Region: "screen", Contains: []string{"first"}, Not: []string{"last"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, all := e.Eval(screen("", "first\nlast\n\n\n"))
	if m == nil || m.Rule != "bottom" || len(all) != 1 {
		t.Fatalf("got %+v %+v", m, all)
	}
	if _, err := New([]agent.Rule{{ID: "x", Region: "middle"}}); err == nil {
		t.Fatal("bad region accepted")
	}
}

// TestClaudeWindowsGlyphs: Claude on Windows draws ">" for "❯" (the
// figures package's fallback), in the idle box and the permission dialog.
func TestClaudeWindowsGlyphs(t *testing.T) {
	e := claudeEngine(t)
	idle := "──────\n>\u00a0\n──────"
	if m, _ := e.Eval(screen("", idle)); m == nil || m.Rule != "idle-empty-box" && m.Rule != "idle-prompt-box" {
		t.Fatalf("idle: %+v", m)
	}
	dlg := strings.ReplaceAll(permission, "❯ 1. Yes", "> 1. Yes")
	if m, _ := e.Eval(screen("", dlg)); m == nil || m.Rule != "blocked-permission-dialog" {
		t.Fatalf("dialog: %+v", m)
	}
}
