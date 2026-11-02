package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/appreport"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Variables for tests: the reports as the server answers them, ok false
// when no server runs (then they are read from their folder).
var (
	listAppReports = serverListAppReports
	showAppReport  = serverShowAppReport
)

// deviceReports is tm device reports and tm device report ID: the
// apps' crash and hang reports (T957), as the running server keeps
// them, or read from their folder when none runs. The human's and any
// agent's: a thread reads them to fix what crashed.
func (e *Env) deviceReports(args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	if want := map[string]int{"reports": 0, "report": 1}[args[0]]; len(pos) != want {
		return usagef("%s", deviceUsage)
	}
	if args[0] == "reports" {
		list, err := listAppReports()
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(list)
		}
		return e.printAppReports(list.Reports, time.Now())
	}
	r, err := showAppReport(pos[0])
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(r)
	}
	return e.printAppReport(r)
}

func serverListAppReports() (proto.AppReportListResult, error) {
	var res proto.AppReportListResult
	err := callAppReports(proto.MethodAppReportList, nil, &res)
	if !errors.Is(err, client.ErrNotRunning) {
		return res, err
	}
	dir, err := appreport.Dir()
	if err != nil {
		return res, err
	}
	ms, err := appreport.List(dir)
	res.Reports = make([]proto.AppReportMeta, 0, len(ms))
	for _, m := range ms {
		res.Reports = append(res.Reports, proto.AppReportMeta(m))
	}
	return res, err
}

func serverShowAppReport(id string) (proto.AppReport, error) {
	var res proto.AppReport
	err := callAppReports(proto.MethodAppReportShow, proto.AppReportShowParams{ID: id}, &res)
	if !errors.Is(err, client.ErrNotRunning) {
		return res, err
	}
	dir, err := appreport.Dir()
	if err != nil {
		return res, err
	}
	r, err := appreport.Get(dir, id)
	var nf *appreport.NotFoundError
	if errors.As(err, &nf) {
		return res, &project.Error{Code: proto.ErrNoReport, Msg: nf.Error()}
	}
	if err != nil {
		return res, err
	}
	return proto.AppReport{AppReportMeta: proto.AppReportMeta(r.Meta), Payload: r.Payload}, nil
}

// callAppReports calls a running server's appreport method.
func callAppReports(method string, params, res any) error {
	c, _, err := connect(false)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, res)
}

// appReportFrom is where a report came from, as the user reads it.
func appReportFrom(m proto.AppReportMeta) string {
	if m.Device != "" {
		return m.Device
	}
	if m.App == "mac" {
		return "Mac app"
	}
	return m.Source
}

func (e *Env) printAppReports(ms []proto.AppReportMeta, now time.Time) error {
	if len(ms) == 0 {
		fmt.Fprintln(e.Stdout, "no crash or hang reports from the apps")
		return nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, m := range ms {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s (%s)\t%s\t%s\n", m.ID, m.Kind, appReportFrom(m), m.AppVersion, m.Build, agoWords(m.Time, now), m.Summary)
	}
	return tw.Flush()
}

func (e *Env) printAppReport(r proto.AppReport) error {
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(tw, "%s:\t%s\n", k, v)
		}
	}
	line("Report", r.ID)
	line("Kind", r.Kind)
	line("From", appReportFrom(r.AppReportMeta))
	line("Happened", r.Time.Local().Format(time.DateTime))
	line("Received", r.Received.Local().Format(time.DateTime))
	line("App", fmt.Sprintf("%s (build %s)", r.AppVersion, r.Build))
	line("Binary UUID", r.BinaryUUID)
	line("OS", r.OS)
	line("Model", r.Model)
	line("Summary", r.Summary)
	if err := tw.Flush(); err != nil {
		return err
	}
	var b bytes.Buffer
	if json.Indent(&b, r.Payload, "", "  ") != nil {
		b.Reset()
		b.Write(r.Payload)
	}
	fmt.Fprintf(e.Stdout, "\nDiagnostic (MetricKit, %d bytes):\n%s\n", r.Bytes, textsafe.Text(b.String()))
	return nil
}
