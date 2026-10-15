package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// esc is text an agent might write: a clipboard write, a line erase and
// a C1 control sequence introducer.
const esc = "a\x1b]52;c;aGk=\x07b\x1b[2Kc\u009b1Ad"

// plain fails t when out holds a control character other than a newline
// or a tab.
func plain(t *testing.T, what, out string) {
	t.Helper()
	for _, r := range out {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0)) {
			t.Fatalf("%s prints control %U:\n%q", what, r, out)
		}
	}
}

// TestPrintsPlainText: what agents wrote (a task's title, notes and
// steps, an inbox item, a library file's name, a report, a todo, a
// question) reaches the terminal without its control characters, and
// the text around them stays.
func TestPrintsPlainText(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")

	h.ok(coord, "task", "add", "title "+esc, "--notes", "notes "+esc+"\nline two", "--step", "step "+esc, "--project", "demo")
	for _, args := range [][]string{{"task", "list"}, {"task", "show", "T1"}, {"context"}} {
		out := h.ok(coord, append(args, "--project", "demo")...)
		plain(t, strings.Join(args, " "), out)
		if !strings.Contains(out, "title a]52;c;aGk=b[2Kc1Ad") {
			t.Errorf("%v lost the text:\n%s", args, out)
		}
	}
	if out := h.ok(coord, "task", "show", "T1", "--project", "demo"); !strings.Contains(out, "\nline two\n") {
		t.Errorf("notes lost their lines:\n%s", out)
	}

	if _, err := p.AddItem("report", "t-0001", "summary "+esc, false); err != nil {
		t.Fatal(err)
	}
	plain(t, "inbox list", h.ok(coord, "inbox", "list", "--project", "demo"))

	r, _ := thread.Create(p, thread.Record{Title: "Research", Task: "T1", State: thread.Running, Reports: 1})
	os.MkdirAll(thread.Path(p, r.ID, "library"), 0o755)
	if err := os.WriteFile(thread.Path(p, r.ID, "library", "x\x1b[2Ky.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain(t, "library list", h.ok(coord, "library", "list", "--project", "demo"))

	os.WriteFile(thread.Path(p, r.ID, "REPORT.md"), []byte("## Report\n"+esc+"\n\n## Next\n- next "+esc+"\n"), 0o644)
	os.WriteFile(thread.Path(p, r.ID, "STATUS.md"), []byte("activity: "+esc+"\n"), 0o644)
	for _, args := range [][]string{{"thread", "show", r.ID}, {"thread", "list"}, {"report", "--show", "--thread", r.ID}} {
		out := h.ok(coord, append(args, "--project", "demo")...)
		plain(t, strings.Join(args, " "), out)
	}
	out := h.ok(coord, "thread", "show", r.ID, "--project", "demo")
	if !strings.Contains(out, "----- end of report -----") || !strings.Contains(out, "## Next") {
		t.Errorf("report lost its shape:\n%s", out)
	}

	// The lines built from the server's state.
	plain(t, "watch", watchLine(proto.Watch{
		Session: proto.WatchSession{ID: "s-1", Role: "thread", State: "working", NeedsYou: esc},
		Task:    &proto.WatchTask{ID: "T1", Status: "started", Current: esc}, PR: esc,
	}))
	plain(t, "session state", stateLine(proto.SessionInfo{State: "working", Current: esc}))
	plain(t, "question", strings.Join(questionLines(&proto.Question{Questions: []proto.QuestionItem{{
		Header: esc, Question: esc, Options: []proto.QuestionOption{{Label: esc, Description: esc}},
	}}}), "\n"))
}
