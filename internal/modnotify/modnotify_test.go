package modnotify

import (
	"strings"
	"testing"
	"time"
)

func TestLineStripsControlAndClips(t *testing.T) {
	got := Line("a\x1b]52;c;ZXZpbA==\x07b\n\tc  d", 100)
	if strings.ContainsAny(got, "\x1b\x07\n\t") {
		t.Fatalf("control characters left: %q", got)
	}
	if got := Line(strings.Repeat("x", 100), 10); got != "xxxxxxxxx…" {
		t.Fatalf("clip = %q", got)
	}
	if got := Line("a\u202eb\u200bc", 10); got != "a b c" {
		t.Fatalf("format characters: %q", got)
	}
}

func TestPostChecks(t *testing.T) {
	h := New(nil)
	good := Button{ID: "task.accept", Project: "p", Task: 12}
	n, err := h.Post("m", Request{Title: "T12 in review", Buttons: []Button{good}})
	if err != nil {
		t.Fatal(err)
	}
	if n.Level != "info" || n.Buttons[0].Label != "Accept" {
		t.Fatalf("notice = %+v", n)
	}
	for name, req := range map[string]Request{
		"no title":     {Title: " \x1b "},
		"bad level":    {Title: "x", Level: "loud"},
		"unknown id":   {Title: "x", Buttons: []Button{{ID: "task.delete", Project: "p", Task: 1}}},
		"no target":    {Title: "x", Buttons: []Button{{ID: "task.show"}}},
		"many buttons": {Title: "x", Buttons: []Button{good, good, good, good}},
		"settings id":  {Title: "x", Buttons: []Button{{ID: "settings.open", Project: "p", Task: 1}}},
		"quit":         {Title: "x", Buttons: []Button{{ID: "console.quit", Project: "p", Task: 1}}},
	} {
		if _, err := h.Post("m", req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestButtonLabelIsTheCores(t *testing.T) {
	h := New(nil)
	n, _ := h.Post("m", Request{Title: "x", Buttons: []Button{{ID: "task.accept", Label: "View", Project: "p", Task: 1}}})
	if n.Buttons[0].Label != "Accept" {
		t.Fatalf("a mod worded a button: %q", n.Buttons[0].Label)
	}
}

func TestRateAndKeep(t *testing.T) {
	now := time.Now()
	h := New(func() time.Time { return now })
	for i := 0; i < PerMinute; i++ {
		if _, err := h.Post("m", Request{Title: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.Post("m", Request{Title: "x"}); err != ErrRate {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.Post("other", Request{Title: "x"}); err != nil {
		t.Fatalf("one mod's rate held up another: %v", err)
	}
	if got := len(h.List()); got != MaxKept+1 {
		t.Fatalf("kept %d", got)
	}
	now = now.Add(2 * time.Minute)
	if _, err := h.Post("m", Request{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(TTL)
	if len(h.List()) != 0 {
		t.Fatal("old notifications stay")
	}
}

func TestClaimDismissDrop(t *testing.T) {
	h := New(nil)
	a, _ := h.Post("m", Request{Title: "a"})
	b, _ := h.Post("n", Request{Title: "b"})
	if !h.Claim(a.ID) || h.Claim(a.ID) {
		t.Fatal("claim is for the first console only")
	}
	if !h.Dismiss(a.ID) || h.Dismiss(a.ID) {
		t.Fatal("dismiss")
	}
	h.DropMod("n")
	if h.Claim(b.ID) || len(h.List()) != 0 {
		t.Fatal("a removed mod's notifications stay")
	}
}

func TestTaskNumber(t *testing.T) {
	for in, want := range map[string]int{"T12": 12, "t3": 3, "7": 7, "": 0, "T": 0, "Tx": 0, "T-1": 0} {
		if got := TaskNumber(in); got != want {
			t.Errorf("TaskNumber(%q) = %d", in, got)
		}
	}
}

func TestNilHub(t *testing.T) {
	var h *Hub
	if _, err := h.Post("m", Request{Title: "x"}); err == nil {
		t.Error("a nil hub took a notification")
	}
	if h.List() != nil || h.Claim("n1") || h.Dismiss("n1") {
		t.Error("a nil hub holds something")
	}
	h.DropMod("m")
}
