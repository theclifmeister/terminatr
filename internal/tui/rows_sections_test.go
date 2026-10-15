package tui

import (
	"slices"
	"testing"
	"time"

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

// keys are the keys of rows, in order, a note as "note" and a heading as
// "head:" and its text.
func keys(rows []row) []string {
	var out []string
	for _, r := range rows {
		switch {
		case r.head != "":
			out = append(out, "head:"+r.head)
		case r.note != "":
			out = append(out, "note")
		default:
			out = append(out, r.key)
		}
	}
	return out
}

// TestBuildRowsParts: each of buildRows' sections on its own. NEEDS YOU
// is the project's, unlabelled, then the user's own blocked session; with
// all projects every project's, labelled. The project's section is its
// heading, coordinator, threads (the working one first) and task counts.
// OTHER SESSIONS are those outside the projects.
func TestBuildRowsParts(t *testing.T) {
	d := testData()
	beta := d.Projects[1]
	if got, want := keys(needsRows(d, "beta")), []string{"n:s-4"}; !slices.Equal(got, want) {
		t.Errorf("beta's needs %v, want %v", got, want)
	}
	all := needsRows(d, allProjects)
	if got, want := keys(all), []string{"n:s-1", "n:s-4"}; !slices.Equal(got, want) {
		t.Errorf("all needs %v, want %v", got, want)
	}
	if all[0].who != "alpha" {
		t.Errorf("all projects' row unlabelled: %+v", all[0])
	}
	if n := needsRows(d, "alpha"); n[0].who != "" {
		t.Errorf("the project's own row labelled: %+v", n[0])
	}
	byID := map[string]proto.SessionInfo{}
	for _, s := range d.Sessions {
		byID[s.ID] = s
	}
	got := keys(projectListRows(d, beta, byID, time.Now()))
	if want := []string{"head:beta", "p:beta", "th:beta:t-0005", "th:beta:t-0006", "note"}; !slices.Equal(got, want) {
		t.Errorf("beta's section %v, want %v", got, want)
	}
	if got, want := keys(otherRows(d, time.Now())), []string{"s:s-3", "s:s-4"}; !slices.Equal(got, want) {
		t.Errorf("other sessions %v, want %v", got, want)
	}
	blocks := map[string][]row{"needs": {{key: "a"}}, "project": {{key: "b"}}, "other": {{key: "c"}}}
	if got, want := keys(sectionsInOrder([]string{"other", "needs"}, blocks)), []string{"c", "a"}; !slices.Equal(got, want) {
		t.Errorf("sectionsInOrder %v, want %v", got, want)
	}
}
