package doctor

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appreport"
)

func TestAppReports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if cs := appReports(dir, now); cs != nil {
		t.Fatalf("no reports: %+v", cs)
	}
	at := now.Add(-10 * 24 * time.Hour)
	s := &appreport.Store{Dir: dir, Now: func() time.Time { return at }}
	add := func(kind, payload string) {
		t.Helper()
		if _, _, err := s.Add(appreport.Report{Meta: appreport.Meta{Source: appreport.DeviceSource("ab"), Device: "iPhone", App: "ios", Kind: kind, AppVersion: "1.0", Build: "7"},
			Payload: json.RawMessage(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	add(appreport.KindCrash, `{"old":1}`)
	if cs := appReports(dir, now); cs != nil {
		t.Fatalf("only old reports: %+v", cs)
	}
	at = now.Add(-time.Hour)
	add(appreport.KindHang, `{"a":1}`)
	add(appreport.KindHang, `{"a":2}`)
	at = now.Add(-time.Minute)
	add(appreport.KindCrash, `{"a":3}`)
	cs := appReports(dir, now)
	if len(cs) != 1 || cs[0].Status != Warn || !strings.Contains(cs[0].Detail, "1 crash, 2 hang") || !strings.Contains(cs[0].Detail, "newest a crash from iPhone, app 1.0 build 7") {
		t.Fatalf("%+v", cs)
	}
}
