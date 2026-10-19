package tasks

import (
	"strings"
	"testing"
)

// TestFindRepo: a task names a project repo by path or folder name, or
// none; anything else is refused, as is a name two repos share.
func TestFindRepo(t *testing.T) {
	repos := []string{"/src/web", "/src/api", "/a/app", "/b/app"}
	for _, tc := range []struct{ name, want, code string }{
		{"none", "", ""},
		{"web", "/src/web", ""},
		{"/src/api", "/src/api", ""},
		{"/src/api/", "/src/api", ""},
		{"/b/app", "/b/app", ""},
		{"app", "", "ambiguous-repo"},
		{"shop", "", "unknown-repo"},
	} {
		got, err := FindRepo(repos, tc.name)
		if code(err) != tc.code || got != tc.want {
			t.Errorf("FindRepo(%q) = %q, %v; want %q, %s", tc.name, got, err, tc.want, tc.code)
		}
	}
	if _, err := FindRepo(nil, "web"); code(err) != "unknown-repo" || !strings.Contains(err.Error(), "has none") {
		t.Errorf("no repos: %v", err)
	}
}

// TestTaskRepo: a task's repo is checked against the store's repos when
// it is set, and kept in the metadata line.
func TestTaskRepo(t *testing.T) {
	s, _ := newStore(t)
	s.Repos = []string{"/src/web", "/src/api"}
	mustAdd(t, s, coord, NewTask{Title: "A", Repo: "web"})
	if res, _ := s.Add(coord, []NewTask{{Title: "B", Repo: "shop"}}); res[0].Code != "unknown-repo" {
		t.Fatalf("bad repo added: %+v", res)
	}
	none, bad, sep := "none", "shop", "a · b"
	if _, err := s.Edit(coord, 1, nil, nil, nil, &bad); code(err) != "unknown-repo" {
		t.Fatalf("edit to a bad repo: %v", err)
	}
	if _, err := s.Edit(coord, 1, nil, nil, nil, &sep); code(err) != "invalid-repo" {
		t.Fatalf("edit to a repo with the separator: %v", err)
	}
	if res, err := s.Edit(coord, 1, nil, nil, nil, &none); err != nil || !res.Changed {
		t.Fatalf("edit to none: %v", err)
	}
	b, err := s.Load()
	if err != nil || b.Tasks[0].Repo != "none" {
		t.Fatalf("loaded %+v %v", b.Tasks, err)
	}
	if out := string(b.Render()); !strings.Contains(out, "status: open · repo: none · created:") {
		t.Fatalf("rendered:\n%s", out)
	}
}
