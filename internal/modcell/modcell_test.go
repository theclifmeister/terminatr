package modcell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

var (
	t12  = proto.CellTarget{Project: "demo", Task: "T12"}
	thr  = proto.CellTarget{Project: "demo", Thread: "t-0003"}
	sess = proto.CellTarget{Session: "s-4"}
)

func TestTargets(t *testing.T) {
	for _, tc := range []struct {
		t  proto.CellTarget
		ok bool
	}{
		{proto.CellTarget{Project: "demo"}, true},
		{t12, true},
		{thr, true},
		{sess, true},
		{proto.CellTarget{}, false},
		{proto.CellTarget{Task: "T1"}, false},
		{proto.CellTarget{Project: "demo", Task: "T1", Thread: "t-0001"}, false},
		{proto.CellTarget{Project: "demo", Session: "s-1"}, false},
		{proto.CellTarget{Project: "de mo"}, false},
		{proto.CellTarget{Project: "demo\x1b"}, false},
	} {
		if err := CheckTarget(tc.t); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc.t, err)
		}
	}
}

func TestCheck(t *testing.T) {
	ss := []widget.Span{{Text: "3d\x1b[31m", Role: "warn"}, {Icon: "review"}}
	if err := Check(ss); err != nil || ss[0].Text != "3d[31m" {
		t.Errorf("%v %q", err, ss[0].Text)
	}
	for _, bad := range [][]widget.Span{
		{{Text: "x", Role: "blink"}},
		{{Icon: "rocket"}},
		{{Text: "🚀"}},
		{{Text: strings.Repeat("x", MaxText+1)}},
	} {
		if Check(bad) == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

func TestSetClearKeep(t *testing.T) {
	s := Open("")
	age := []widget.Span{{Text: "3d", Role: "warn"}}
	if ok, err := s.Set("review-age", "age", t12, age); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, _ := s.Set("review-age", "age", t12, age); ok {
		t.Error("the same cell again changed something")
	}
	s.Set("review-age", "age", sess, age)
	s.Set("pr-ci", "ci", thr, []widget.Span{{Icon: "ci.pass"}})
	s.SetStatus("pr-ci", []widget.Span{{Text: "2 red"}})
	if got := s.Cells(nil, func(t proto.CellTarget) bool { return t.Session == "" }); len(got) != 2 || got[0].Mod != "pr-ci" {
		t.Errorf("cells %+v", got)
	}
	if got := s.Cells(func(m string) bool { return m == "review-age" }, nil); len(got) != 2 {
		t.Errorf("review-age's %+v", got)
	}
	if ok, _ := s.Set("review-age", "age", t12, nil); !ok {
		t.Error("clearing changed nothing")
	}
	s.Keep(func(m string) bool { return m != "pr-ci" }, func(_, c string) bool { return c == "age" })
	if got := s.Cells(nil, nil); len(got) != 1 || got[0].Target != sess {
		t.Errorf("after keep %+v", got)
	}
	if len(s.Status(nil)) != 0 {
		t.Error("a dropped mod's status stayed")
	}
}

func TestMaxCells(t *testing.T) {
	s := Open("")
	span := []widget.Span{{Text: "x"}}
	for i := range MaxCells {
		if _, err := s.Set("m", "c", proto.CellTarget{Project: "p", Task: "T" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)}, span); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := s.Set("m", "c", proto.CellTarget{Project: "q"}, span); err == nil {
		t.Error("one past the most")
	}
	if _, err := s.Set("other", "c", proto.CellTarget{Project: "q"}, span); err != nil {
		t.Error("another mod's cell refused:", err)
	}
	if _, err := s.Set("m", "c", proto.CellTarget{Project: "p", Task: "Ta"}, nil); err != nil {
		t.Error(err)
	}
	if _, err := s.Set("m", "c", proto.CellTarget{Project: "q"}, span); err != nil {
		t.Error("after clearing one:", err)
	}
}

// TestRestart: the last cells come back from the file, and what doesn't
// check is left out.
func TestRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cells.json")
	s := Open(path)
	s.Set("review-age", "age", t12, []widget.Span{{Text: "3d", Role: "warn"}})
	s.SetStatus("review-age", []widget.Span{{Text: "2 waiting"}})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	r := Open(path)
	if c := r.Cells(nil, nil); len(c) != 1 || c[0].Spans[0].Text != "3d" || c[0].Target != t12 {
		t.Errorf("reopened %+v", c)
	}
	if st := r.Status(nil); len(st) != 1 || st[0].Spans[0].Text != "2 waiting" {
		t.Errorf("status %+v", st)
	}
	bad := `{"version":1,"cells":[{"mod":"m","column":"c","target":{"project":"p"},"spans":[{"text":"x","role":"blink"}]},` +
		`{"mod":"m","column":"c","target":{},"spans":[{"text":"x"}]},` +
		`{"mod":"m","column":"c","target":{"project":"p","task":"T1"},"spans":[{"text":"ok\u001b"}]}]}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := Open(path).Cells(nil, nil); len(c) != 1 || c[0].Spans[0].Text != "ok" {
		t.Errorf("checked on load %+v", c)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := Open(path).Cells(nil, nil); len(c) != 0 {
		t.Errorf("a broken file %+v", c)
	}
}
