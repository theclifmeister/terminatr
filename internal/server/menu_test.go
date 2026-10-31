package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestMenuLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Blue":                 "Blue",
		"[ ] Apple":            "Apple",
		"[✔] Apple":            "Apple",
		"Yes  Proceed with it": "Yes",
		"Type something.":      "Type something.",
	} {
		if got := menuLabel(in); got != want {
			t.Errorf("menuLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// longQuestion is T884's: Claude Code v2.1.314 draws it behind "│ " on
// each wrapped line, as testdata/menu's screens captured it in panes
// of 100x40, 60x20 and 60x14 (long-60x18-clipped is the 60x20 one less
// its top two lines).
const longQuestion = "T882's thread found that the terminal view has no composer: tapping the pane opens the keyboard with its own key bar, and the Keys button (with its 'Type into its terminal' strip) exists only in the conversation's composer. What should happen?"

func TestMenuShows(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct {
		screen, question, why string
	}{
		{read("long-100x40.screen"), longQuestion, ""},
		{read("long-60x20.screen"), longQuestion, ""},
		// Its start scrolled off: the rest, from the top line on.
		{read("long-60x18-clipped.screen"), longQuestion, ""},
		// T887: a ~450-character question with quotes, parentheses,
		// semicolons, a slash list and <angle> words, drawn as
		// Claude draws long-60x20 (behind "│ ", wrapped at words):
		// whole, and with its start scrolled off a short pane.
		{read("t886-100x40.screen"), t886Question, ""},
		{read("t886-60x24.screen"), t886Question, ""},
		{read("t886-60x20-clipped.screen"), t886Question, ""},
		// Nothing of it above the options.
		{read("long-60x14.screen"), longQuestion, "pane is too short to show the question above its options"},
		// Another question, or one ending the same way, isn't it.
		{read("long-100x40.screen"), "Which color should the button be?", `screen doesn't show "Which color should the button be?"`},
		{read("long-60x18-clipped.screen"), "Should the key bar move? (with its 'Type into its terminal' strip) exists only in the conversation's composer. What should happen?", "screen doesn't show"},
		{"│ composer. What should happen?\n\n❯ 1. Yes\n  2. No\n", longQuestion, "screen doesn't show"},
		// A menu drawn without bars, and in a box.
		{"Which color should the button be?\n\n❯ 1. Red\n  2. Blue\n", "Which color should the button be?", ""},
		{"│ Which color should │\n│ the button be?      │\n│ ❯ 1. Red            │\n", "Which color should the button be?", ""},
	} {
		why := menuShows(c.screen, c.question)
		if c.why == "" && why != "" || c.why != "" && !strings.HasPrefix(why, c.why) {
			t.Errorf("menuShows(%.40q…) = %q, want %q", c.question, why, c.why)
		}
	}
}

// TestMenuOptionsLong: the long question's options are read off its
// screen, labels and all, wherever the pane cuts it.
func TestMenuOptionsLong(t *testing.T) {
	keys := []string{"Drop Keys from chat (Recommended)", "Add Keys to terminal", "Type something.", "Chat about this"}
	t886 := []string{"Build with defaults (Recommended)", "Defaults + Designs now", "Let me read it first", "Type something.", "Chat about this"}
	for name, want := range map[string][]string{
		"long-100x40.screen": keys, "long-60x20.screen": keys, "long-60x14.screen": keys,
		"t886-100x40.screen": t886, "t886-60x20-clipped.screen": t886,
	} {
		b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, o := range agent.DialogOptions(string(b)) {
			got = append(got, menuLabel(o))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: options %q, want %q", name, got, want)
		}
	}
}

// t886Question is the coordinator's question of T887's screenshot, as
// its AskUserQuestion call sent it.
const t886Question = "T886 (peer project pages) design is ready. Its defaults: switch called 'project pages' (tm peer pages <alias> <project> on|off, set on the server, not from the phone); Board shares only task title/status/steps (no notes); Overview shares the goal and PROJECT.md body but no repo paths; Library shares thread files but not files that came from a third server; Designs deferred (shows 'Not shared'); Memory never. Build it with these defaults?"

// The multi-select screens of testdata/menu are Claude Code v2.1.316's
// (T900), captured in a PTY against a local stand-in for the API that
// answers with one AskUserQuestion call: Camera (single), Subjects
// (multi-select) and Editing (single). multi-only is Subjects alone
// (its row is Submit, not Next); review-* are the step after the last
// answer.
const subjectsQuestion = "Which subjects do you photograph most often?"

var subjects = proto.QuestionItem{Question: subjectsQuestion, Header: "Subjects", MultiSelect: true, Options: []proto.QuestionOption{
	{Label: "Plants & flowers"}, {Label: "Birds & wildlife"}, {Label: "Landscapes"}, {Label: "People & street"}}}

func readMenu(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "menu", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func claudeAnswer(t *testing.T) *agent.Answer {
	t.Helper()
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := reg.Get("claude")
	how := agent.AnswerOf(a)
	if !ok || !how.Multi() || how.Review == "" {
		t.Fatalf("claude's [answer]: %+v", how)
	}
	return how
}

func TestMultiMenuScreens(t *testing.T) {
	how := claudeAnswer(t)
	labels := []string{"Plants & flowers", "Birds & wildlife", "Landscapes", "People & street", "Type something"}
	for _, c := range []struct {
		name    string
		checked []int // option numbers ticked
		cursor  int   // the option the cursor is on; 0: on Next
		ok      bool
	}{
		{"multi-100x40.screen", nil, 1, true},
		{"multi-13-100x40.screen", []int{1, 3}, 1, true},
		{"multi-13-next-100x40.screen", []int{1, 3}, 0, true},
		{"multi-60x20.screen", nil, 1, true},
		{"multi-2-next-60x20.screen", []int{2}, 0, true},
		{"multi-only-100x40.screen", nil, 1, true},
		// Its options scrolled ("↓ 3."): not all of them show.
		{"multi-60x14.screen", nil, 0, false},
		// Single-selects and the review step aren't multi-selects.
		{"long-100x40.screen", nil, 0, false},
		{"review-100x40.screen", nil, 0, false},
	} {
		boxes, onDone, ok := agent.MultiMenu(readMenu(t, c.name), how.MultiDone)
		if ok != c.ok {
			t.Errorf("%s: ok %v", c.name, ok)
			continue
		}
		if !ok {
			continue
		}
		var got []string
		cursor := 0
		for _, b := range boxes {
			got = append(got, b.Label)
			if b.Checked != slices.Contains(c.checked, b.N) {
				t.Errorf("%s: option %d checked %v", c.name, b.N, b.Checked)
			}
			if b.Cursor {
				cursor = b.N
			}
		}
		if !slices.Equal(got, labels) || cursor != c.cursor || onDone != (c.cursor == 0) {
			t.Errorf("%s: %q, cursor %d, on Next %v", c.name, got, cursor, onDone)
		}
		if why := menuShows(readMenu(t, c.name), subjectsQuestion); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestReviewOption(t *testing.T) {
	how := claudeAnswer(t)
	for name, want := range map[string]int{
		"review-100x40.screen": 1, "review-single-100x30.screen": 1,
		"multi-100x40.screen": 0, "long-100x40.screen": 0,
	} {
		if got := reviewOption(readMenu(t, name), how); got != want {
			t.Errorf("%s: %d, want %d", name, got, want)
		}
	}
	// The title alone, or the option alone, isn't the step.
	if reviewOption("Review your answers\n\n❯ 1. Yes\n  2. No\n", how) != 0 || reviewOption("❯ 1. Submit answers\n  2. Cancel\n", how) != 0 {
		t.Error("a screen that isn't the review step")
	}
}

// fakeMulti is Claude's multi-select as the captured screen draws it:
// keys change its boxes and cursor as Claude's do (a number toggles its
// box, the cursor stays; Down moves it, down to Next; Enter on Next
// confirms), and read redraws the screen from them. stuck lists numbers
// whose key does nothing.
type fakeMulti struct {
	screen  string
	checked map[int]bool
	cursor  int // 1..options; options+1 is Next
	n       int
	stuck   map[string]bool
	sent    []string
	done    bool
}

func newFakeMulti(t *testing.T, name string) *fakeMulti {
	f := &fakeMulti{screen: readMenu(t, name), checked: map[int]bool{}, cursor: 1, stuck: map[string]bool{}}
	boxes, onDone, _ := agent.MultiMenu(f.screen, []string{"Next", "Submit"})
	f.n = len(boxes)
	if onDone {
		f.cursor = f.n + 1
	}
	for _, b := range boxes {
		f.checked[b.N] = b.Checked
		if b.Cursor {
			f.cursor = b.N
		}
	}
	return f
}

func (f *fakeMulti) send(keys ...string) *proto.Error {
	for _, k := range keys {
		f.sent = append(f.sent, k)
		switch {
		case f.stuck[k]:
		case k == "\x1b[B":
			f.cursor = min(f.cursor+1, f.n+1)
		case k == "\r" && f.cursor == f.n+1:
			f.done = true
		case len(k) == 1 && k[0] >= '1' && k[0] <= '9':
			n := int(k[0] - '0')
			f.checked[n] = !f.checked[n]
		}
	}
	return nil
}

func (f *fakeMulti) read() (string, *proto.Error) {
	lines := strings.Split(f.screen, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		for n := 1; n <= f.n; n++ {
			if strings.HasPrefix(strings.TrimLeft(t, "❯ "), fmt.Sprintf("%d. [", n)) {
				box := "[ ]"
				if f.checked[n] {
					box = "[✔]"
				}
				mark := "  "
				if f.cursor == n {
					mark = "❯ "
				}
				_, label, _ := strings.Cut(t, "] ")
				lines[i] = fmt.Sprintf("%s%d. %s %s", mark, n, box, label)
			}
		}
		if row := strings.TrimLeft(t, "❯ "); row == "Next" || row == "Submit" {
			mark := "  "
			if f.cursor == f.n+1 {
				mark = "❯ "
			}
			lines[i] = mark + "   " + row
		}
	}
	return strings.Join(lines, "\n"), nil
}

func TestAnswerMulti(t *testing.T) {
	how := claudeAnswer(t)
	down := "\x1b[B"
	for _, c := range []struct {
		name   string
		screen string
		picks  []int
		stuck  []string
		keys   []string
		code   string
	}{
		// Plants & flowers and Landscapes: 1, 3, down past the five
		// options to Next, Enter.
		{name: "two", screen: "multi-100x40.screen", picks: []int{0, 2}, keys: []string{"1", "3", down, down, down, down, down, "\r"}},
		{name: "narrow", screen: "multi-60x20.screen", picks: []int{3}, keys: []string{"4", down, down, down, down, down, "\r"}},
		// Already ticked: only what differs; the cursor on Next stays.
		{name: "untick", screen: "multi-13-next-100x40.screen", picks: []int{2}, keys: []string{"1", "\r"}},
		{name: "as is", screen: "multi-13-next-100x40.screen", picks: []int{0, 2}, keys: []string{"\r"}},
		{name: "only question", screen: "multi-only-100x40.screen", picks: []int{1}, keys: []string{"2", down, down, down, down, down, "\r"}},
		// A key that doesn't tick stops the answer there.
		{name: "stuck", screen: "multi-100x40.screen", picks: []int{0, 2}, stuck: []string{"3"}, keys: []string{"1", "3"}, code: proto.ErrNoQuestion},
		// A pane too short to show every option: no key at all.
		{name: "clipped", screen: "multi-60x14.screen", picks: []int{0}, code: proto.ErrNoQuestion},
		{name: "single-select", screen: "long-100x40.screen", picks: []int{0}, code: proto.ErrNoQuestion},
	} {
		f := newFakeMulti(t, c.screen)
		for _, k := range c.stuck {
			f.stuck[k] = true
		}
		perr := answerMulti(f, "s-1", subjects, how, c.picks, f.screen)
		switch {
		case c.code == "" && perr != nil:
			t.Errorf("%s: %v", c.name, perr)
		case c.code != "" && (perr == nil || perr.Code != c.code):
			t.Errorf("%s: %v, want %s", c.name, perr, c.code)
		}
		if !slices.Equal(f.sent, c.keys) {
			t.Errorf("%s: keys %q, want %q", c.name, f.sent, c.keys)
		}
		if c.code == "" && !f.done {
			t.Errorf("%s: Next not confirmed", c.name)
		}
	}
	// The stuck answer says what it did.
	f := newFakeMulti(t, "multi-100x40.screen")
	f.stuck["3"] = true
	if perr := answerMulti(f, "s-1", subjects, how, []int{0, 2}, f.screen); perr == nil || !strings.Contains(perr.Message, `didn't tick "Landscapes" (tm ticked "Plants & flowers")`) {
		t.Errorf("stuck: %v", perr)
	}
}

func TestPickedAnswer(t *testing.T) {
	if got, perr := pickedAnswer(subjects, []int{3, 0, 3}); perr != nil || got != "Plants & flowers, People & street" {
		t.Errorf("picked: %q %v", got, perr)
	}
	if _, perr := pickedAnswer(subjects, []int{4}); perr == nil || perr.Code != proto.ErrBadParams {
		t.Errorf("out of range: %v", perr)
	}
	single := proto.QuestionItem{Question: "Which?", Options: []proto.QuestionOption{{Label: "A"}}}
	if _, perr := pickedAnswer(single, []int{0}); perr == nil || perr.Code != proto.ErrBadParams {
		t.Errorf("single-select: %v", perr)
	}
}

// TestActGuardPicks: a paired server's multi-select answer goes by
// index, its labels the guard: other labels mean the need changed.
func TestActGuardPicks(t *testing.T) {
	n := proto.PeerNeed{Question: &proto.Question{Questions: []proto.QuestionItem{subjects}}}
	p := proto.PeerAct{Act: proto.ActAnswer, Question: subjectsQuestion, Answer: "Plants & flowers, Landscapes", Picks: []int{2, 0}}
	got, perr := actGuard(n, p)
	if perr != nil || got.Answer != "Plants & flowers, Landscapes" || !slices.Equal(got.Picks, []int{2, 0}) {
		t.Fatalf("picks: %+v %v", got, perr)
	}
	p.Answer = "Plants & flowers"
	if _, perr := actGuard(n, p); perr == nil || perr.Code != proto.ErrNeedChanged {
		t.Fatalf("other labels: %v", perr)
	}
	p.Picks = []int{9}
	if _, perr := actGuard(n, p); perr == nil || perr.Code != proto.ErrBadParams {
		t.Fatalf("a pick it hasn't: %v", perr)
	}
}

// TestCodexNoMulti: Codex's request_user_input (0.163) takes one option
// and notes, no multi-select (its multi-select schemas are MCP
// elicitation's), and its menu is read off the screen only: its
// [answer] types no picks, so answerMenu refuses them (screen-only).
func TestCodexNoMulti(t *testing.T) {
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := reg.Get("codex")
	if how := agent.AnswerOf(a); !ok || how == nil || how.Multi() || how.Tool != "" {
		t.Fatalf("codex's [answer]: %+v", how)
	}
}
