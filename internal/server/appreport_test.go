package server

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appreport"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAppReports: a device's report is kept under its device id and
// read back by the human; the same diagnostic again is the one kept; a
// plain control connection (no app) can't add one.
func TestAppReports(t *testing.T) {
	p := testPaths(t)
	got := make(chan *Server, 1)
	onRun = func(s *Server) { got <- s }
	t.Cleanup(func() { onRun = nil })
	startServer(t, p)
	s := <-got
	k, _, _ := ed25519.GenerateKey(nil)
	d, err := device.Add(device.DirIn(p.Home), k, nil, "Sam's iPhone", "wss://relay.invalid")
	if err != nil {
		t.Fatal(err)
	}
	dc, err := s.deviceConn("Sam's iPhone")
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	add := proto.AppReportAddParams{Kind: appreport.KindCrash, Time: time.Now().Add(-time.Hour), AppVersion: "0.73.1", Build: "212",
		BinaryUUID: "6F1C2D3E-0000-4000-8000-000000000001", OS: "iPhone OS 26.1", Model: "iPhone17,1", Summary: "EXC_BAD_ACCESS (SIGSEGV)",
		Payload: json.RawMessage(`{"callStackTree":{"callStacks":[]},"diagnosticMetaData":{"appBuildVersion":"212"}}`)}
	var res proto.AppReportAddResult
	if err := dc.Call(proto.MethodAppReportAdd, add, &res); err != nil || !res.Added || res.ID == "" {
		t.Fatalf("add: %+v %v", res, err)
	}
	var again proto.AppReportAddResult
	if err := dc.Call(proto.MethodAppReportAdd, add, &again); err != nil || again.Added || again.ID != res.ID {
		t.Fatalf("again: %+v %v", again, err)
	}
	bad := add
	bad.Kind = "boom"
	var perr *proto.Error
	if err := dc.Call(proto.MethodAppReportAdd, bad, &again); !errors.As(err, &perr) || perr.Code != proto.ErrBadReport {
		t.Fatalf("bad kind: %v", err)
	}

	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(proto.MethodAppReportAdd, add, &again); !errors.As(err, &perr) || perr.Code != "human-only" {
		t.Fatalf("add without an app: %v", err)
	}
	var list proto.AppReportListResult
	if err := c.Call(proto.MethodAppReportList, nil, &list); err != nil || len(list.Reports) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	m := list.Reports[0]
	if m.Source != appreport.DeviceSource(d.ID().String()) || m.Device != "Sam's iPhone" || m.App != "ios" || m.Build != "212" || m.BinaryUUID != add.BinaryUUID {
		t.Fatalf("meta %+v", m)
	}
	var show proto.AppReport
	if err := c.Call(proto.MethodAppReportShow, proto.AppReportShowParams{ID: m.ID}, &show); err != nil || string(show.Payload) != string(add.Payload) {
		t.Fatalf("show: %s %v", show.Payload, err)
	}
	if err := c.Call(proto.MethodAppReportShow, proto.AppReportShowParams{ID: "zzz"}, &show); !errors.As(err, &perr) || perr.Code != proto.ErrNoReport {
		t.Fatalf("show unknown: %v", err)
	}
}
