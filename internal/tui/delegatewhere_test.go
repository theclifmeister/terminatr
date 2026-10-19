package tui

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDelegateWhere: D's question says where the thread works: the
// task's repo, a folder without git, or, with several repos and none
// named, that the coordinator asks which.
func TestDelegateWhere(t *testing.T) {
	two := []string{"/src/web", "/src/api"}
	for _, tc := range []struct {
		repo  string
		repos []string
		want  string
	}{
		{"web", two, " in web"},
		{"none", two, " in a folder without git"},
		{"", two, "; it names none of the project's repos, so the coordinator asks you which"},
		{"", two[:1], ""},
		{"", nil, ""},
	} {
		if got := delegateWhere(&tasks.Task{ID: 1, Repo: tc.repo}, tc.repos); got != tc.want {
			t.Errorf("repo %q of %v: %q, want %q", tc.repo, tc.repos, got, tc.want)
		}
	}
}
