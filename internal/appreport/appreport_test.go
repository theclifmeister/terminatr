package appreport

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home/hometest"
)

func report(src, kind string, at time.Time, payload string) Report {
	return Report{Meta: Meta{Source: src, App: "ios", Kind: kind, Time: at, AppVersion: "1.2", Build: "345", BinaryUUID: "ABCD-1234"},
		Payload: json.RawMessage(payload)}
}

func TestAddListGet(t *testing.T) {
	dir := hometest.PrivateDir(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s := &Store{Dir: dir, Now: func() time.Time { return now }}
	src := DeviceSource("ab12")
	m, added, err := s.Add(report(src, KindCrash, now.Add(-time.Hour), `{ "callStackTree": {"x": 1} }`))
	if err != nil || !added {
		t.Fatalf("Add: %v %v", added, err)
	}
	if m.Bytes != len(`{"callStackTree":{"x":1}}`) || !strings.Contains(m.ID, "-crash-") {
		t.Fatalf("meta %+v", m)
	}
	// the same diagnostic again is the one kept
	again, added, err := s.Add(report(src, KindCrash, now.Add(-time.Hour), `{"callStackTree":{"x":1}}`))
	if err != nil || added || again.ID != m.ID {
		t.Fatalf("again: %+v %v %v", again, added, err)
	}
	now = now.Add(time.Minute)
	if _, _, err := s.Add(report(SourceMac, KindHang, now, `{"hangDuration":"2 sec"}`)); err != nil {
		t.Fatal(err)
	}
	ms, err := List(dir)
	if err != nil || len(ms) != 2 || ms[0].Kind != KindHang || ms[1].ID != m.ID || ms[1].Source != src {
		t.Fatalf("List: %+v %v", ms, err)
	}
	r, err := Get(dir, m.ID[:20])
	if err != nil || string(r.Payload) != `{"callStackTree":{"x":1}}` || r.BinaryUUID != "ABCD-1234" {
		t.Fatalf("Get: %+v %v", r, err)
	}
	var nf *NotFoundError
	if _, err := Get(dir, "nope"); !errors.As(err, &nf) {
		t.Fatalf("Get nope: %v", err)
	}
	rec, _ := Recent(dir, now)
	if len(rec) != 1 || rec[0].Kind != KindHang {
		t.Fatalf("Recent: %+v", rec)
	}
}

func TestAddRefuses(t *testing.T) {
	s := &Store{Dir: hometest.PrivateDir(t)}
	for name, r := range map[string]Report{
		"source":   report("../x", KindCrash, time.Time{}, `{}`),
		"kind":     report(SourceMac, "boom", time.Time{}, `{}`),
		"array":    report(SourceMac, KindCrash, time.Time{}, `[1]`),
		"empty":    report(SourceMac, KindCrash, time.Time{}, ``),
		"too big":  report(SourceMac, KindCrash, time.Time{}, `{"a":"`+strings.Repeat("x", MaxPayload)+`"}`),
		"not json": report(SourceMac, KindCrash, time.Time{}, `{nope`),
	} {
		var e *Error
		if _, _, err := s.Add(r); !errors.As(err, &e) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPrune(t *testing.T) {
	dir := hometest.PrivateDir(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &Store{Dir: dir, Now: func() time.Time { return now }}
	for i := range MaxPerSource + 3 {
		now = now.Add(time.Minute)
		if _, _, err := s.Add(report(SourceMac, KindHang, now, `{"n":`+string(rune('0'+i%10))+`,"i":"`+strings.Repeat("y", i)+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	ms, _ := List(dir)
	if len(ms) != MaxPerSource {
		t.Fatalf("kept %d", len(ms))
	}
	files, _ := os.ReadDir(filepath.Join(dir, SourceMac))
	if len(files) != 2*MaxPerSource {
		t.Fatalf("%d files", len(files))
	}
	now = now.Add(MaxAge + time.Hour)
	if _, _, err := s.Add(report(SourceMac, KindCrash, now, `{"late":true}`)); err != nil {
		t.Fatal(err)
	}
	if ms, _ := List(dir); len(ms) != 1 {
		t.Fatalf("after MaxAge kept %d", len(ms))
	}
}

func TestListNoDir(t *testing.T) {
	ms, err := List(filepath.Join(t.TempDir(), "none"))
	if err != nil || ms == nil || len(ms) != 0 {
		t.Fatalf("%v %v", ms, err)
	}
}

// A conversation's scroll guard reports as kind scroll (T976), its
// payload the geometry it found.
func TestAddScroll(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := &Store{Dir: hometest.PrivateDir(t), Now: func() time.Time { return now }}
	m, added, err := s.Add(report(DeviceSource("ab12"), KindScroll, now, `{"offset":2000,"content":1000,"visible":500}`))
	if err != nil || !added || !strings.Contains(m.ID, "-scroll-") {
		t.Fatalf("Add scroll: %+v %v %v", m, added, err)
	}
}
