package tasks

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
)

// TestPartsLine: an umbrella's parts line (programs, §5.3) is read
// after the metadata and priority lines, plain or as vault links, and
// written back as it was; a line that isn't refs only stays the notes'.
func TestPartsLine(t *testing.T) {
	yamlOn(t)
	const file = "+++\nnext_id = 5\n+++\n# Tasks\n\n## In motion\n\n" +
		"### T4 SSO with Okta\nstatus: started\npriority: must\nparts: acme-infra/T21, acme-api/T12, mini/acme-ios/T7\n\nWhy we do it.\n"
	b, err := Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	tk := b.Find(4)
	if !slices.Equal(tk.Parts, []string{"acme-infra/T21", "acme-api/T12", "mini/acme-ios/T7"}) || tk.Priority != Must || tk.Notes != "Why we do it." {
		t.Fatalf("parts %v, priority %q, notes %q", tk.Parts, tk.Priority, tk.Notes)
	}
	plain := string(b.Render())
	if !strings.Contains(plain, "priority: must\nparts: acme-infra/T21, acme-api/T12, mini/acme-ios/T7\n") {
		t.Fatalf("plain render:\n%s", plain)
	}
	b.Slug = "_programs/acme"
	linked := string(b.Render())
	if !strings.Contains(linked, "parts: [[acme-infra/tasks/T21|acme-infra/T21]], [[acme-api/tasks/T12|acme-api/T12]], mini/acme-ios/T7\n") {
		t.Fatalf("linked render:\n%s", linked)
	}
	again, err := Parse([]byte(linked))
	if err != nil || !slices.Equal(again.Find(4).Parts, tk.Parts) {
		t.Fatalf("read back: %v %v", err, again)
	}
	// No priority: the parts line straight after the metadata line.
	b.Find(4).Priority = NoPriority
	b.Slug = ""
	again, err = Parse(b.Render())
	if err != nil || !slices.Equal(again.Find(4).Parts, tk.Parts) || again.Find(4).Notes != "Why we do it." {
		t.Fatalf("no priority: %v\n%s", err, b.Render())
	}
	// Text that only starts like one is the notes'.
	for _, notes := range []string{"parts: the left one, the right one", "parts: acme/T1 and more"} {
		in := strings.Replace(file, "parts: acme-infra/T21, acme-api/T12, mini/acme-ios/T7", notes, 1)
		b, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if tk := b.Find(4); tk.Parts != nil || !strings.HasPrefix(tk.Notes, notes) {
			t.Fatalf("%q: parts %v notes %q", notes, tk.Parts, tk.Notes)
		}
	}
	if _, err := checkNotes("parts: acme-api/T1\nmore"); err == nil {
		t.Fatal("notes that read as parts were taken")
	}
}

// TestUmbrellaParts: parts are set on a program's board only; an
// umbrella with parts takes started, blocked and review from the
// ticker only (SetDerived), never by hand.
func TestUmbrellaParts(t *testing.T) {
	s, ev := newStore(t)
	if _, err := s.SetParts(human, 1, []string{"acme-api/T1"}, false); code(err) != "not-a-program" {
		t.Fatalf("project board: %v", err)
	}
	s.Program = true
	if _, err := s.Add(human, []NewTask{{Title: "SSO"}, {Title: "Plain"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetParts(human, 1, []string{"acme-api/T12, acme-web/T40"}, false); err != nil {
		t.Fatal(err)
	}
	res, err := s.SetParts(human, 1, []string{"acme-web/T40", "mini/acme-ios/T7"}, true)
	if err != nil || !slices.Equal(res.Task.Parts, []string{"acme-api/T12", "acme-web/T40", "mini/acme-ios/T7"}) {
		t.Fatalf("add: %+v %v", res.Task, err)
	}
	if res, _ := s.SetParts(human, 1, []string{"acme-web/T40"}, true); res.Changed {
		t.Fatal("an added part that was there changed it")
	}
	if !slices.ContainsFunc(ev.journal, func(l string) bool {
		return strings.Contains(l, "task.parts T1 acme-api/T12, acme-web/T40, mini/acme-ios/T7")
	}) {
		t.Fatalf("journal %q", ev.journal)
	}
	for _, bad := range []string{"T12", "acme-api", "Acme/T1"} {
		if _, err := s.SetParts(human, 1, []string{bad}, false); code(err) != "invalid-parts" {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	for _, st := range []Status{Started, Blocked, Review} {
		if _, err := s.SetStatus(human, 1, st, ""); code(err) != "derived-status" {
			t.Fatalf("%s by hand: %v", st, err)
		}
	}
	if _, err := s.SetStatus(human, 2, Started, ""); err != nil {
		t.Fatalf("a task without parts: %v", err)
	}
	if _, err := s.SetStatus(human, 1, Ready, ""); err != nil {
		t.Fatal(err)
	}
	ticker := caller.Caller{Kind: caller.Ticker}
	res, err = s.SetDerived(ticker, 1, Started)
	if err != nil || !res.Changed || res.Task.Status != Started {
		t.Fatalf("derived: %+v %v", res, err)
	}
	if _, err := s.SetDerived(caller.Caller{Kind: caller.Director, Project: "acme"}, 1, Review); code(err) != "ticker-only" {
		t.Fatalf("director: %v", err)
	}
	if _, err := s.SetDerived(ticker, 1, Done); code(err) != "human-only" {
		t.Fatalf("done: %v", err)
	}
	if res, _ := s.SetDerived(ticker, 2, Review); res.Changed {
		t.Fatal("a task without parts took a derived status")
	}
	if _, err := s.SetStatus(human, 1, Done, ""); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.SetDerived(ticker, 1, Started); res.Changed {
		t.Fatal("a done umbrella moved")
	}
	if res, err := s.SetParts(human, 1, nil, false); err != nil || res.Task.Parts != nil {
		t.Fatalf("clear: %+v %v", res.Task, err)
	}
}

// TestNotedParts: an older route's note lines name an umbrella's parts.
func TestNotedParts(t *testing.T) {
	notes := "Why.\ndirector:acme (2026-11-02): routed acme-api/T1, mini/acme-ios (pending M-3), acme-web/T4\ndirector:acme (2026-11-03): routed acme-api/T1, acme-api/T7\nrouted x/T9"
	if got := NotedParts(notes); !slices.Equal(got, []string{"acme-api/T1", "acme-web/T4", "acme-api/T7"}) {
		t.Fatalf("NotedParts = %v", got)
	}
}
