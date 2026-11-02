package proto

import (
	"encoding/json"
	"time"
)

// The apps' crash and hang reports (T957, internal/appreport): MetricKit
// diagnostics the iPhone app sends over its device link and the Mac app
// through its bridge, kept by the server per source. appreport.add is
// the apps' only (a device's app API, at any level, or the Mac app's
// bridge); appreport.list and appreport.show answer the human and any
// agent (a thread reads them to fix what crashed). The server's hello
// lists CapAppReports.
const (
	// MethodAppReportAdd keeps a report (AppReportAddParams): an
	// AppReportAddResult. A report refused for what it is answers
	// ErrBadReport.
	MethodAppReportAdd = "appreport.add"
	// MethodAppReportList lists the reports, newest first: an
	// AppReportListResult of their AppReportMeta.
	MethodAppReportList = "appreport.list"
	// MethodAppReportShow is one report with its diagnostic
	// (AppReportShowParams, an id or a unique prefix of one): an
	// AppReport. An id nobody has answers ErrNoReport.
	MethodAppReportShow = "appreport.show"
)

// CapAppReports says the server answers appreport.*.
const CapAppReports = "app-reports"

// Errors of appreport.*.
const (
	ErrBadReport = "bad-report"
	ErrNoReport  = "no-report"
)

// AppReportAddParams are a MetricKit diagnostic and what the app knows
// of it. Kind is crash, hang, cpu-exception, disk-write-exception or
// app-launch; Time when it happened; AppVersion, Build and BinaryUUID
// the app's then (what matches a dSYM); OS and Model MetricKit's
// osVersion and deviceType; Summary one line (a crash's exception, a
// hang's duration); Payload MetricKit's JSON for the diagnostic
// (jsonRepresentation), an object of at most appreport.MaxPayload
// bytes. The server sets the source and the app from the caller.
type AppReportAddParams struct {
	Kind       string          `json:"kind"`
	Time       time.Time       `json:"time,omitzero"`
	AppVersion string          `json:"app_version"`
	Build      string          `json:"build"`
	BinaryUUID string          `json:"binary_uuid,omitempty"`
	OS         string          `json:"os,omitempty"`
	Model      string          `json:"model,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

// AppReportAddResult is the report's ID; Added false when the server
// had it already (the same diagnostic sent again).
type AppReportAddResult struct {
	ID    string `json:"id"`
	Added bool   `json:"added"`
}

// AppReportMeta is a report without its diagnostic: appreport.Meta.
type AppReportMeta struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Device     string    `json:"device,omitempty"`
	App        string    `json:"app"`
	Kind       string    `json:"kind"`
	Time       time.Time `json:"time"`
	Received   time.Time `json:"received"`
	AppVersion string    `json:"app_version"`
	Build      string    `json:"build"`
	BinaryUUID string    `json:"binary_uuid,omitempty"`
	OS         string    `json:"os,omitempty"`
	Model      string    `json:"model,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Bytes      int       `json:"bytes"`
}

// AppReportListResult are the reports, newest received first.
type AppReportListResult struct {
	Reports []AppReportMeta `json:"reports"`
}

// AppReportShowParams name a report by its ID or a unique prefix.
type AppReportShowParams struct {
	ID string `json:"id"`
}

// AppReport is a report with its diagnostic, MetricKit's JSON.
type AppReport struct {
	AppReportMeta
	Payload json.RawMessage `json:"payload"`
}
