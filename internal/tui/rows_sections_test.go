package tui

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// heads are the section headings of rows, in order ("" for a project's
// own is its slug), with cards as "card".
func heads(rows []row) []string {
	var out []string
	for _, r := range rows {
		switch {
		case r.card:
			out = append(out, "card")
		case r.head != "":
			out = append(out, r.head)
		}
	}
	return out
}

// TestSectionOrder: dashboard.sections orders the list's sections, and
// leaving out other or cards hides them (the layout v2 design §2.2).
func TestSectionOrder(t *testing.T) {
	d := Data{ServerOK: true,
		Sessions: []proto.SessionInfo{{ID: "s-1", Role: proto.RoleCoordinator, Project: "alpha", State: "blocked", Reason: "?"}, {ID: "s-2", State: "idle"}},
		Projects: []ProjectData{{Slug: "alpha", Counts: map[string]int{}}}}
	cards := []row{{key: "c:x", card: true}}
	for _, c := range []struct {
		src  string
		want []string
	}{
		{"", []string{"NEEDS YOU", "alpha", "OTHER SESSIONS", "card"}},
		{"[dashboard]\nsections = [\"cards\", \"other\", \"needs\", \"project\"]\n", []string{"card", "OTHER SESSIONS", "NEEDS YOU", "alpha"}},
		{"[dashboard]\nsections = [\"project\", \"other\"]\n", []string{"NEEDS YOU", "alpha", "OTHER SESSIONS"}},
		{"[dashboard]\nsections = [\"other\", \"project\"]\n", []string{"NEEDS YOU", "OTHER SESSIONS", "alpha"}},
		{"[dashboard]\nsections = [\"needs\", \"project\"]\n", []string{"NEEDS YOU", "alpha"}},
	} {
		l := effectiveLayout(parseSpec(t, c.src), Layout{}, "")
		if got := heads(buildRows(d, "alpha", l.sections, cards)); !slices.Equal(got, c.want) {
			t.Errorf("%q: sections %v, want %v", c.src, got, c.want)
		}
	}
	if l := (Layout{}); !l.shows("cards") {
		t.Error("a zero layout hides cards")
	}
	hidden := effectiveLayout(parseSpec(t, "[dashboard]\nsections = [\"project\"]\n"), Layout{}, "")
	if hidden.shows("cards") || hidden.shows("other") || !hidden.shows("needs") || !hidden.shows("project") {
		t.Errorf("shows with %v", hidden.sections)
	}
}
