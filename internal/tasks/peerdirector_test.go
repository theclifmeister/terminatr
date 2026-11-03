package tasks

import (
	"errors"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// TestPeerDirector: a paired server's director (the programs design
// §9.3) has a director's program rights on a member whose program keys
// name it, and nothing else: a paired server without a program, or of
// another program, is refused.
func TestPeerDirector(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Slug: "acme-ios", Directs: func(p string) bool { return p == "studio/acme" }}
	if err := Scaffold(s.Dir); err != nil {
		t.Fatal(err)
	}
	pd := caller.ForPeerDirector("studio", "acme")
	out, err := s.ProgramAdd(pd, "studio/acme", "filed by the director of studio/acme", []ProgramTask{{NewTask: NewTask{Title: "Login"}, After: []string{"studio/acme-api/T3"}}})
	if err != nil || out[0].Result != "created" {
		t.Fatalf("add: %+v %v", out, err)
	}
	task, _ := s.Get(1)
	if task.Owner != "director:studio/acme" || len(task.AfterX) != 1 || task.AfterX[0] != "studio/acme-api/T3" {
		t.Fatalf("filed %+v", task)
	}
	title := "Login screen"
	if _, err := s.ProgramEdit(pd, "studio/acme", 1, ProgramChange{Title: &title}); err != nil {
		t.Fatalf("its own filing: %v", err)
	}
	refused := func(c caller.Caller, want string) {
		t.Helper()
		_, err := s.ProgramEdit(c, "studio/acme", 1, ProgramChange{Note: "x"})
		var e *Error
		if !errors.As(err, &e) || e.Code != want {
			t.Fatalf("%v: got %v, want %s", c, err, want)
		}
	}
	refused(caller.ForPeer("studio"), "coordinator-only")
	refused(caller.ForPeerDirector("studio", "beta"), "not-member")
	refused(caller.ForPeerDirector("lab", "acme"), "not-member")
	if _, err := s.SetStatus(pd, 1, Started, ""); err == nil {
		t.Fatal("a paired director set a status")
	}
	if _, err := s.SetApproved(pd, 1, Done, ""); err == nil {
		t.Fatal("a paired director accepted a task")
	}
}
