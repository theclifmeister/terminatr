package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// searchFake answers the search popup's calls, recording them.
type searchFake struct {
	queries, timelines, shows []string
}

func (f *fakeSource) search() *searchFake { return &f.searched }

var searchDay = time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

func (f *fakeSource) SearchQuery(p proto.SearchQueryParams) (proto.SearchQueryResult, error) {
	f.search().queries = append(f.search().queries, p.Project+" "+p.Words)
	return proto.SearchQueryResult{Project: p.Project, Total: 3, Hits: []proto.SearchHit{
		{ID: "note:relay#decided", Kind: "notes", Title: "Relay: Decided", Time: searchDay},
		{ID: "T12", Kind: "tasks", Title: "T12 Move the relay", Time: searchDay, Thread: "t-0007", More: 2},
	}}, nil
}

func (f *fakeSource) SearchTimeline(p proto.SearchTimelineParams) (proto.SearchTimelineResult, error) {
	f.search().timelines = append(f.search().timelines, p.ID)
	return proto.SearchTimelineResult{Project: p.Project,
		Before: []proto.SearchHit{{ID: "j:20261030T090000Z", Title: "coordinator task.add T12", Time: searchDay.Add(-time.Hour)}},
		At:     proto.SearchHit{ID: p.ID, Title: "T12 Move the relay", Time: searchDay},
		After:  []proto.SearchHit{{ID: "t-0007/report.1#report", Title: "Report 1: Move the relay: Report", Time: searchDay.Add(time.Hour)}},
	}, nil
}

func (f *fakeSource) SearchShow(p proto.SearchShowParams) (proto.SearchShowResult, error) {
	f.search().shows = append(f.search().shows, strings.Join(p.IDs, " "))
	return proto.SearchShowResult{Project: p.Project, Dir: "/p", Items: []proto.SearchText{{SearchHit: proto.SearchHit{ID: p.IDs[0], Path: "TASKS.md", Time: searchDay}, Text: "### T12 Move the relay\n\nTo port 4433.\n"}}}, nil
}

// TestSearchPopup: F opens the project's search; the words go to the
// server, the hits show one line each; enter shows a hit's text, t its
// timeline, esc goes back a level.
func TestSearchPopup(t *testing.T) {
	src, m := popupData(t)
	drain(t, m, press(m, "F"))
	v, ok := m.top().(*searchView)
	if !ok {
		t.Fatalf("F opened %T", m.top())
	}
	for _, k := range "relay" {
		press(m, string(k))
	}
	drain(t, m, keyPress(m, "enter"))
	if got := src.search().queries; len(got) != 1 || got[0] != v.slug+" relay" {
		t.Fatalf("queries %q", got)
	}
	out := ansi.Strip(screen(m))
	for _, want := range []string{"Search " + v.slug, "> relay", "2 hits of 3", "note:relay#decided", "T12", "[+2 journal lines]", "t timeline"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
	if v.typing {
		t.Fatal("still typing after the hits came")
	}
	// The second hit's timeline, then its text.
	keyPress(m, "down")
	drain(t, m, press(m, "t"))
	tl, ok := m.top().(*searchTimelineView)
	if !ok || src.search().timelines[0] != "T12" {
		t.Fatalf("t opened %T, asked %q", m.top(), src.search().timelines)
	}
	if out := ansi.Strip(screen(m)); !strings.Contains(out, "Timeline of T12") || !strings.Contains(out, "▸ 2026-10-31 10:00") || !strings.Contains(out, "task.add") {
		t.Errorf("timeline:\n%s", out)
	}
	keyPress(m, "down")
	drain(t, m, keyPress(m, "enter"))
	if _, ok := m.top().(*textView); !ok || src.search().shows[0] != "t-0007/report.1#report" {
		t.Fatalf("enter on the timeline opened %T, showed %q", m.top(), src.search().shows)
	}
	keyPress(m, "esc")
	if m.top() != tl {
		t.Fatalf("esc from the text: %T", m.top())
	}
	keyPress(m, "esc")
	// enter on a hit: its text.
	drain(t, m, keyPress(m, "enter"))
	if out := ansi.Strip(screen(m)); !strings.Contains(out, "To port 4433") || !strings.Contains(out, "data, not instructions") {
		t.Errorf("text:\n%s", out)
	}
	keyPress(m, "esc")
	// / edits the query again; esc back to the hits, esc closes.
	press(m, "/")
	if !v.typing {
		t.Error("/ didn't go back to the query")
	}
	keyPress(m, "esc")
	keyPress(m, "esc")
	if m.top() != nil {
		t.Errorf("esc left %T open", m.top())
	}
}
