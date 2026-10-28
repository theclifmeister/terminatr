package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// TestShipped: a task's PR is merged when the repo's history has its
// merge; without it, the ticker's last look decides.
func TestShipped(t *testing.T) {
	repo := t.TempDir()
	git := gittest.In(t, repo)
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, "a"), []byte("a"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "a")
	git("checkout", "-q", "-b", "pr")
	os.WriteFile(filepath.Join(repo, "b"), []byte("b"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "b")
	git("checkout", "-q", "main")
	git("merge", "-q", "--no-ff", "-m", "Merge pull request #61 from o/pr", "pr")

	check := func(n int, pr ticker.PR, ship string) {
		t.Helper()
		if s := shipped(repo, n, pr); s != ship {
			t.Errorf("shipped(#%d, %+v) = %q, want %q", n, pr, s, ship)
		}
	}
	check(61, ticker.PR{}, ShipMerged)
	check(61, ticker.PR{State: "OPEN"}, ShipMerged) // the merge is in the history
	check(62, ticker.PR{State: "OPEN"}, ShipOpen)
	check(62, ticker.PR{State: "CLOSED"}, ShipClosed)
	check(62, ticker.PR{State: "MERGED"}, ShipMerged) // not fetched
	check(62, ticker.PR{}, "")
	check(0, ticker.PR{State: "MERGED"}, "")
	if s := shipped("", 61, ticker.PR{State: "MERGED"}); s != ShipMerged {
		t.Errorf("no repo: %q", s)
	}
	for url, want := range map[string]int{"https://github.com/o/r/pull/61": 61, "": 0, "https://github.com/o/r/pull/x": 0,
		"https://dev.azure.com/o/p/_git/r/pullrequest/12": 12, "https://org.visualstudio.com/p/_git/r/pullrequest/5": 5, "https://dev.azure.com/o/p/_git/r/pullrequest/x": 0} {
		if got := ticker.PRNumber(url); got != want {
			t.Errorf("PRNumber(%q) = %d", url, got)
		}
	}
}

// sourceStub is a Source whose every method panics (its Source is nil):
// a fake embeds it and implements only the concerns its test needs.
type sourceStub struct{ Source }

// The real source implements every concern.
var (
	_ PollSource     = (*ServerSource)(nil)
	_ ProjectSource  = (*ServerSource)(nil)
	_ SettingsSource = (*ServerSource)(nil)
	_ LinkSource     = (*ServerSource)(nil)
	_ PeerSource     = (*ServerSource)(nil)
	_ DeviceSource   = (*ServerSource)(nil)
	_ ModSource      = (*ServerSource)(nil)
	_ ModViewSource  = (*ServerSource)(nil)
	_ SessionSource  = (*ServerSource)(nil)
)

// TestDashboardPollsOnlyReads: a dashboard that only polls and draws
// needs nothing of the Source but its reads and the mods' views; a
// fake of those two concerns is enough, any other call panics.
func TestDashboardPollsOnlyReads(t *testing.T) {
	f := &fakeSource{data: testData()}
	src := struct {
		sourceStub
		PollSource
		ModViewSource
	}{PollSource: f, ModViewSource: f}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	for _, cmd := range []tea.Cmd{m.load(), m.pollModViews()} {
		if cmd != nil {
			m.Update(cmd())
		}
	}
	if out := screen(m); !strings.Contains(out, "NEEDS YOU") || !strings.Contains(out, "server ok") {
		t.Fatalf("the poll didn't draw the data:\n%s", out)
	}
}
