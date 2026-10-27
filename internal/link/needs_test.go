package link

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
)

// TestNeeds: a held message and an offered file are needs by the ids
// that act on them, oldest first, gone once answered.
func TestNeeds(t *testing.T) {
	a, b, clock := setup(t)
	os.WriteFile(filepath.Join(os.Getenv(home.Env), "config.toml"), []byte("[projects.a.links]\nb = { allow = \"both\", ask = true }\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	ca := coord
	ca.Project = "a"
	h1, err := Send(ca, a, "b", Opts{Text: "\n  first line  \nmore"})
	if err != nil || h1.State != StateHeld {
		t.Fatalf("%v %+v", err, h1)
	}
	*clock = clock.Add(time.Minute)
	h2, _ := Send(ca, a, "b", Opts{Text: "second"})
	cb := coord
	cb.Project = "b"
	libFile(t, b, "t-0001", "x.txt", 10)
	f1, err := Send(cb, b, "a", Opts{Text: "x", File: "t-0001/x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Minute)
	f2, _ := Send(cb, b, "a", Opts{Text: "y", File: "t-0001/x.txt"})
	got := Needs(a, Rows(mustConfig(t), a))
	want := []Need{
		{Kind: NeedHeld, Peer: "b", ID: h1.ID, Created: h1.Created, Title: "first line", Bytes: h1.Bytes},
		{Kind: NeedHeld, Peer: "b", ID: h2.ID, Created: h2.Created, Title: "second", Bytes: h2.Bytes},
		{Kind: NeedFile, Peer: "b", ID: f1.Transfer, Created: f1.Created, File: "x.txt", Size: 10, Message: f1.ID},
		{Kind: NeedFile, Peer: "b", ID: f2.Transfer, Created: f2.Created, File: "x.txt", Size: 10, Message: f2.ID},
	}
	if len(got) != len(want) {
		t.Fatalf("needs %+v", got)
	}
	for i := range want {
		if !got[i].Created.Equal(want[i].Created) {
			t.Errorf("need %d created %v, want %v", i, got[i].Created, want[i].Created)
		}
		got[i].Created = want[i].Created
		if got[i] != want[i] {
			t.Errorf("need %d %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := Drop(human, a, h1.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := DeclineFile(human, a, f1.Transfer); err != nil {
		t.Fatal(err)
	}
	got = Needs(a, Rows(mustConfig(t), a))
	if len(got) != 2 || got[0].ID != h2.ID || got[1].ID != f2.Transfer {
		t.Errorf("after answering: %+v", got)
	}
	if n := Needs(b, Rows(mustConfig(t), b)); len(n) != 0 {
		t.Errorf("b waits on nothing: %+v", n)
	}
}
