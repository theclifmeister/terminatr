package guardlog

import (
	"os"
	"testing"
	"time"
)

// TestRecordDecide: the same call refused again is one entry counted;
// kept refusing it is no longer listed, allowed and refused again it is.
func TestRecordDecide(t *testing.T) {
	home := t.TempDir()
	now := time.Unix(1000, 0)
	e := Entry{Project: "demo", Thread: "t-0001", Rule: "force-push", Tool: "Bash", Summary: "git push with force", Subject: "git push -f origin x"}
	got, listed, err := Record(home, e, now)
	if err != nil || !listed || got.Count != 1 || got.ID == "" {
		t.Fatalf("%+v %t %v", got, listed, err)
	}
	again, listed, _ := Record(home, e, now.Add(time.Minute))
	if listed || again.ID != got.ID || again.Count != 2 || !again.Last.Equal(now.Add(time.Minute)) {
		t.Fatalf("again %+v %t", again, listed)
	}
	other := e
	other.Subject, other.Project = "git push -f origin y", "web"
	Record(home, other, now.Add(2*time.Minute))
	if l, _ := List(home, "demo"); len(l) != 1 || l[0].ID != got.ID {
		t.Fatalf("demo %+v", l)
	}
	if l, _ := List(home, ""); len(l) != 2 || l[0].Project != "web" {
		t.Fatalf("all %+v", l)
	}
	if _, err := Decide(home, got.ID, Kept); err != nil {
		t.Fatal(err)
	}
	if _, listed, _ := Record(home, e, now.Add(3*time.Minute)); listed {
		t.Error("kept refusing, listed again")
	}
	if l, _ := List(home, "demo"); len(l) != 0 {
		t.Fatalf("kept %+v", l)
	}
	Decide(home, got.ID, Allowed)
	if _, listed, _ := Record(home, e, now.Add(4*time.Minute)); !listed {
		t.Error("allowed and refused again, not listed")
	}
	if _, err := Decide(home, "nope", Kept); err != ErrGone {
		t.Errorf("gone: %v", err)
	}
	fi, err := os.Stat(Path(home))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v %v", fi, err)
	}
}

// TestPrune: past maxEntries the oldest decided entry goes first.
func TestPrune(t *testing.T) {
	var list []Entry
	for i := range maxEntries + 1 {
		list = append(list, Entry{ID: string(rune('a' + i%26)), Last: time.Unix(int64(i), 0)})
	}
	list[5].Decision = Kept
	list = prune(list)
	if len(list) != maxEntries || list[5].Last.Unix() != 6 {
		t.Errorf("%d %v", len(list), list[5].Last.Unix())
	}
	if list = prune(append(list, Entry{Last: time.Unix(9999, 0)})); list[0].Last.Unix() != 1 {
		t.Errorf("oldest open went? first %v", list[0].Last.Unix())
	}
}

// TestActionable: a refusal is the user's choice only while an allowance
// can lift it; the floor's, tm's files' and a write folder's aren't.
func TestActionable(t *testing.T) {
	for _, c := range []struct {
		e    Entry
		want bool
	}{
		{Entry{Rule: "force-push", Subject: "git push -f origin x"}, true},
		{Entry{Rule: "credentials", Subject: "cat ~/.netrc"}, true},
		{Entry{Rule: "mod:lint.no-rm", Subject: "rm -r x"}, true},
		{Entry{Rule: "worktree-only", Subject: "cp a /opt/b"}, true},
		{Entry{Rule: "worktree-only", Subject: "/opt/b", Path: true}, false},
		{Entry{Rule: "merge", Subject: "gh pr merge 1", Floor: true}, false},
		{Entry{Rule: "merge", Subject: "gh pr merge 1"}, false},
		{Entry{Rule: "tm-files", Subject: "tm config set x y", Floor: true}, false},
		{Entry{Rule: "tm-files", Subject: "echo x > ~/.terminatr/config.toml"}, false},
		{Entry{Rule: "temp-folders", Subject: "/tmp/other", Path: true, Floor: true}, false},
		{Entry{Rule: "unchecked", Subject: "ls", Floor: true}, false},
		{Entry{Rule: "credentials", Subject: "cat ~/.ssh/*"}, false},
	} {
		if got := c.e.Actionable(); got != c.want {
			t.Errorf("%s %q: actionable %t, want %t", c.e.Rule, c.e.Subject, got, c.want)
		}
	}
}
