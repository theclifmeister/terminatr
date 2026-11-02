package server

// The apps' crash and hang reports (T957, internal/appreport): the
// server keeps what the iPhone app sends over its device link and the
// Mac app through its bridge, per source, and answers them to the human
// and to agents (proto.MethodAppReportList, Show).

import (
	"errors"

	"github.com/theclifmeister/terminatr/internal/appreport"
	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// addAppReport is appreport.add: only an app's (a device's app API, or
// the Mac app's bridge), its source the caller.
func (s *Server) addAppReport(p proto.AppReportAddParams, c callCtx) (any, *proto.Error) {
	r := appreport.Report{Meta: appreport.Meta{Kind: p.Kind, Time: p.Time, AppVersion: p.AppVersion, Build: p.Build, BinaryUUID: p.BinaryUUID,
		OS: p.OS, Model: p.Model, Summary: p.Summary}, Payload: p.Payload}
	switch {
	case c.device != "":
		d, ok := s.deviceNamed(c.device)
		if !ok {
			return nil, proto.Errorf(proto.ErrRefused, "%s: device %s isn't paired", c.method, c.device)
		}
		r.Source, r.Device, r.App = appreport.DeviceSource(d.ID().String()), d.Name, "ios"
	case c.app:
		r.Source, r.App = appreport.SourceMac, "mac"
	default:
		return nil, proto.Errorf("human-only", "%s is the apps': the iPhone app over its device link, the Mac app through its bridge", c.method)
	}
	m, added, err := s.appReports.Add(r)
	var bad *appreport.Error
	if errors.As(err, &bad) {
		return nil, proto.Errorf(proto.ErrBadReport, "%s", bad.Msg)
	}
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%s: %v", c.method, err)
	}
	if added {
		from := "the Mac app"
		if m.Device != "" {
			from = "device " + m.Device
		}
		s.log.Printf("%s: %s report %s (app %s build %s)", from, m.Kind, m.ID, m.AppVersion, m.Build)
	}
	return proto.AppReportAddResult{ID: m.ID, Added: added}, nil
}

// deviceNamed is the paired device called name.
func (s *Server) deviceNamed(name string) (device.Device, bool) {
	ds, err := device.Load(device.DirIn(s.home))
	if err != nil {
		return device.Device{}, false
	}
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return device.Device{}, false
}

// listAppReports is appreport.list.
func (s *Server) listAppReports(_ callCtx) (any, *proto.Error) {
	ms, err := appreport.List(s.appReports.Dir)
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "appreport.list: %v", err)
	}
	res := proto.AppReportListResult{Reports: make([]proto.AppReportMeta, 0, len(ms))}
	for _, m := range ms {
		res.Reports = append(res.Reports, proto.AppReportMeta(m))
	}
	return res, nil
}

// showAppReport is appreport.show.
func (s *Server) showAppReport(p proto.AppReportShowParams, _ callCtx) (any, *proto.Error) {
	r, err := appreport.Get(s.appReports.Dir, p.ID)
	var nf *appreport.NotFoundError
	var bad *appreport.Error
	switch {
	case errors.As(err, &nf):
		return nil, proto.Errorf(proto.ErrNoReport, "%s", nf.Error())
	case errors.As(err, &bad):
		return nil, proto.Errorf(proto.ErrBadParams, "%s", bad.Msg)
	case err != nil:
		return nil, proto.Errorf(proto.ErrInternal, "appreport.show: %v", err)
	}
	return proto.AppReport{AppReportMeta: proto.AppReportMeta(r.Meta), Payload: r.Payload}, nil
}
