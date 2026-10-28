package server

import "testing"

func TestRestartSummary(t *testing.T) {
	outs := []restartOutcome{
		{rec: SessionRecord{ID: "s-9", Role: "shell"}, how: "lost"},
		{rec: SessionRecord{ID: "s-1", Role: "coordinator", Project: "p"}, how: "resumed"},
		{rec: SessionRecord{ID: "s-2", Role: "thread", Project: "p", Thread: "t-0003"}, how: "resumed", reprompted: true},
		{rec: SessionRecord{ID: "s-3", Role: "shell", Agent: "claude"}, how: "fresh"},
		{rec: SessionRecord{ID: "s-4", Role: "thread", Project: "p", Thread: "t-0005"}, how: "lost"},
	}
	for _, c := range []struct{ shut, want string }{
		{"crash", "server restarted after crash; resumed coordinator, t-0003; re-prompted t-0003; started fresh claude s-3; lost shell s-9, t-0005"},
		{"clean", "server restarted; resumed coordinator, t-0003; re-prompted t-0003; started fresh claude s-3; lost shell s-9, t-0005"},
	} {
		if got := restartSummary(c.shut, outs); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.shut, got, c.want)
		}
	}
}
