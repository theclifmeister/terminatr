package tasks

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestAfterCrossRefs: refs to other projects' tasks in "after"
// (programs, §6.1) are read, kept and written back as they were, plain or
// as vault links; a command still writes only this board's refs, and
// keeps the others.
func TestAfterCrossRefs(t *testing.T) {
	yamlOn(t)
	const file = "+++\nnext_id = 41\n+++\n# Tasks\n\n## Ready\n\n" +
		"### T40 Wire OIDC\nstatus: ready · after: T10, mini/acme-ios/T7, acme-api/T12, T3\n"
	b, err := Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	tk := b.Find(40)
	if !slices.Equal(tk.After, []int{3, 10}) || !slices.Equal(tk.AfterX, []string{"acme-api/T12", "mini/acme-ios/T7"}) {
		t.Fatalf("after %v, cross %v", tk.After, tk.AfterX)
	}
	if got := strings.Join(tk.AfterRefs(), ", "); got != "T3, T10, acme-api/T12, mini/acme-ios/T7" {
		t.Fatalf("AfterRefs = %s", got)
	}
	plain := string(b.Render())
	if !strings.Contains(plain, "after: T3, T10, acme-api/T12, mini/acme-ios/T7\n") {
		t.Fatalf("plain render:\n%s", plain)
	}
	b.Slug = "acme-web"
	linked := string(b.Render())
	if !strings.Contains(linked, "after: [[acme-web/tasks/T3|T3]], [[acme-web/tasks/T10|T10]], [[acme-api/tasks/T12|acme-api/T12]], mini/acme-ios/T7\n") {
		t.Fatalf("linked render:\n%s", linked)
	}
	// Round trip, and the form a later tm writes, a vault link with the
	// ref as its alias.
	for _, in := range []string{plain, linked, strings.Replace(plain, "acme-api/T12", "[[acme-api/tasks/T12|acme-api/T12]]", 1)} {
		again, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%v\n%s", err, in)
		}
		if a := again.Find(40); !slices.Equal(a.After, tk.After) || !slices.Equal(a.AfterX, tk.AfterX) {
			t.Fatalf("read back %v %v from\n%s", a.After, a.AfterX, in)
		}
		again.Slug = ""
		if string(again.Render()) != plain {
			t.Fatalf("not round-tripped:\n%s", again.Render())
		}
	}
	for _, bad := range []string{"Acme/T1", "acme/T0", "acme/12", "/T1", "a/b/c/T1", "-x/T1", "acme//T1", "acme/T1/x"} {
		in := strings.Replace(file, "acme-api/T12", bad, 1)
		if _, err := Parse([]byte(in)); err == nil || !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "not a task ref") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// TestSetPlanKeepsCrossRefs: without CrossRefs (programs off, or no
// program) a plan edit writes no cross-project refs and keeps the ones
// the file has.
func TestSetPlanKeepsCrossRefs(t *testing.T) {
	s, _ := newStore(t)
	mustAdd(t, s, coord, NewTask{Title: "A"}, NewTask{Title: "B"})
	data, _ := os.ReadFile(s.TasksPath())
	data = []byte(strings.Replace(string(data), "### T2 B\nstatus: open", "### T2 B\nstatus: open · after: acme/T4", 1))
	if !strings.Contains(string(data), "acme/T4") {
		t.Fatalf("fixture not edited:\n%s", data)
	}
	if err := os.WriteFile(s.TasksPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPlan(coord, 2, nil, []string{"acme/T5"}); code(err) != "invalid-after" {
		t.Fatalf("cross write: %v", err)
	}
	if _, err := s.SetPlan(coord, 2, nil, []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	tk, _ := s.Get(2)
	if !slices.Equal(tk.After, []int{1}) || !slices.Equal(tk.AfterX, []string{"acme/T4"}) {
		t.Fatalf("%v %v", tk.After, tk.AfterX)
	}
}

// TestSetPlanCrossRefs: with CrossRefs a plan edit writes the whole
// list, this board's refs and other projects', each cross ref checked
// by the hook; a ref by this project's own slug is this board's.
func TestSetPlanCrossRefs(t *testing.T) {
	s, _ := newStore(t)
	mustAdd(t, s, coord, NewTask{Title: "A"}, NewTask{Title: "B"})
	var asked []string
	s.CrossRefs = func(refs []string) error {
		asked = append(asked, refs...)
		for _, r := range refs {
			if strings.HasPrefix(r, "other/") {
				return &Error{Code: "not-member", Msg: r + " is not in the program"}
			}
		}
		return nil
	}
	if _, err := s.SetPlan(coord, 2, nil, []string{"T1, acme-api/T12 " + s.Slug + "/T1"}); err != nil {
		t.Fatal(err)
	}
	tk, _ := s.Get(2)
	if !slices.Equal(tk.After, []int{1}) || !slices.Equal(tk.AfterX, []string{"acme-api/T12"}) || !slices.Equal(asked, []string{"acme-api/T12"}) {
		t.Fatalf("%v %v asked %v", tk.After, tk.AfterX, asked)
	}
	if _, err := s.SetPlan(coord, 2, nil, []string{"other/T3"}); code(err) != "not-member" {
		t.Fatalf("non-member: %v", err)
	}
	if _, err := s.SetPlan(coord, 2, nil, []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	if tk, _ = s.Get(2); len(tk.AfterX) != 0 {
		t.Fatalf("the list replaces the cross refs too: %v", tk.AfterX)
	}
}

func TestCrossRef(t *testing.T) {
	for _, c := range []struct {
		in, alias, slug string
		id              int
		ok              bool
	}{
		{"acme-api/T12", "", "acme-api", 12, true},
		{"mini/acme-ios/T7", "mini", "acme-ios", 7, true},
		{"T7", "", "", 0, false},
		{"acme/t7", "", "", 0, false},
		{"acme/T07", "", "", 0, false},
		{strings.Repeat("a", 33) + "/x/T1", "", "", 0, false},
		{strings.Repeat("a", 64) + "/T1", "", strings.Repeat("a", 64), 1, true},
	} {
		a, s, id, ok := CrossRef(c.in)
		if a != c.alias || s != c.slug || id != c.id || ok != c.ok {
			t.Errorf("CrossRef(%q) = %q %q %d %v", c.in, a, s, id, ok)
		}
	}
}
