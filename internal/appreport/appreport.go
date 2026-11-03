// Package appreport keeps the crash and hang reports the apps send
// their tm server (docs/SPEC.md §16.15, T957): MetricKit's diagnostics
// (a crash, a hang, a CPU or disk-write exception, a slow launch),
// which iOS and macOS hand an app on its next launch. The iPhone app
// sends each over its device link, the Mac app through its bridge; the
// server keeps them per source under <home>/state/appreports, bounded
// and pruned, so the user and their threads read them with tm device
// reports instead of digging through Settings > Analytics Data. They
// never go anywhere else.
//
// A source's folder holds two files per report: <id>.json, the Meta,
// and <id>.mx.json, MetricKit's own JSON for the diagnostic as the app
// sent it. The server is the only writer (Store.mu); readers take what
// is there.
package appreport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The kinds of report: MetricKit's diagnostics, and the apps' own
// guards' (KindScroll).
const (
	KindCrash     = "crash"                // MXCrashDiagnostic
	KindHang      = "hang"                 // MXHangDiagnostic
	KindCPU       = "cpu-exception"        // MXCPUExceptionDiagnostic
	KindDiskWrite = "disk-write-exception" // MXDiskWriteExceptionDiagnostic
	KindLaunch    = "app-launch"           // MXAppLaunchDiagnostic
	// KindScroll is a conversation's scroll guard (T976): it came to
	// rest past its content's end, and the app scrolled it back. Its
	// payload is the scroll geometry, not MetricKit's.
	KindScroll = "scroll"
)

// Kinds are the kinds a report may have.
var Kinds = []string{KindCrash, KindHang, KindCPU, KindDiskWrite, KindLaunch, KindScroll}

// SourceMac is the Mac app's source; a device's is DeviceSource's.
const SourceMac = "mac"

// Bounds: a source keeps at most MaxPerSource reports, none older than
// MaxAge; a diagnostic's JSON is at most MaxPayload bytes (under a
// device link's line, appapi.MaxLine).
const (
	MaxPerSource = 50
	MaxAge       = 90 * 24 * time.Hour
	MaxPayload   = 512 << 10
)

// Meta is what a report says about itself, besides the diagnostic.
type Meta struct {
	// ID names the report: its source, when it happened, and a hash of
	// its diagnostic, so the same diagnostic sent again is the same one.
	ID string `json:"id"`
	// Source is the folder it lives in: "mac", or "device-<device id>".
	Source string `json:"source"`
	// Device is the device's name when it arrived (a device's only).
	Device string `json:"device,omitempty"`
	// App is the app that sent it: "ios" or "mac".
	App  string `json:"app"`
	Kind string `json:"kind"`
	// Time is when the diagnostic happened (MetricKit's payload's end, or
	// the diagnostic's own time); Received when the server took it.
	Time     time.Time `json:"time"`
	Received time.Time `json:"received"`
	// AppVersion and Build are the app's (CFBundleShortVersionString,
	// CFBundleVersion) when it happened, BinaryUUID its executable's
	// LC_UUID then: what matches the report to a build's dSYM.
	AppVersion string `json:"app_version"`
	Build      string `json:"build"`
	BinaryUUID string `json:"binary_uuid,omitempty"`
	// OS and Model are MetricKit's osVersion and deviceType.
	OS    string `json:"os,omitempty"`
	Model string `json:"model,omitempty"`
	// Summary is one line from the app: the exception and signal of a
	// crash, a hang's duration.
	Summary string `json:"summary,omitempty"`
	// Bytes is the diagnostic's size.
	Bytes int `json:"bytes"`
}

// Report is a Meta with its diagnostic.
type Report struct {
	Meta
	Payload json.RawMessage `json:"payload"`
}

// DeviceSource is the source of the device with id (peer.ID's hex).
func DeviceSource(id string) string { return "device-" + id }

// Dir is the reports' directory, <home>/state/appreports.
func Dir() (string, error) {
	h, err := home.Dir()
	if err != nil {
		return "", err
	}
	return DirIn(h), nil
}

// DirIn is the reports' directory of the home h.
func DirIn(h string) string { return filepath.Join(h, "state", "appreports") }

// Store is the reports' directory as the server writes it.
type Store struct {
	Dir string
	// Now is the clock (tests); nil is time.Now.
	Now func() time.Time
	mu  sync.Mutex
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Error is a report refused for what it is (bad-report).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func refuse(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

// Add keeps r for its source and prunes that source: the oldest past
// MaxPerSource, and any older than MaxAge. r's Payload must be a JSON
// object of at most MaxPayload bytes; its strings are cleaned to lines.
// The same diagnostic again is the report already kept (added false).
func (s *Store) Add(r Report) (m Meta, added bool, err error) {
	if err := check(&r); err != nil {
		return Meta{}, false, err
	}
	now := s.now().UTC()
	r.Received = now
	if r.Time.IsZero() || r.Time.After(now.Add(time.Hour)) {
		r.Time = now
	}
	r.Time = r.Time.UTC()
	var compact bytes.Buffer
	if err := json.Compact(&compact, r.Payload); err != nil {
		return Meta{}, false, refuse("the diagnostic isn't JSON: %v", err)
	}
	r.Payload = compact.Bytes()
	r.Bytes = len(r.Payload)
	sum := sha256.Sum256(r.Payload)
	r.ID = r.Time.Format("20060102-150405") + "-" + r.Kind + "-" + hex.EncodeToString(sum[:4])
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.Dir, r.Source)
	if err := fsx.EnsurePrivateDir(s.Dir); err != nil {
		return Meta{}, false, err
	}
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return Meta{}, false, err
	}
	if old, err := readMeta(filepath.Join(dir, r.ID+".json")); err == nil {
		return old, false, nil
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, r.ID+".mx.json"), r.Payload, 0o600); err != nil {
		return Meta{}, false, err
	}
	b, err := json.MarshalIndent(r.Meta, "", "  ")
	if err != nil {
		return Meta{}, false, err
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, r.ID+".json"), append(b, '\n'), 0o600); err != nil {
		return Meta{}, false, err
	}
	prune(dir, now)
	return r.Meta, true, nil
}

// check refuses what a report can't be and cleans its strings.
func check(r *Report) error {
	if !validSource(r.Source) {
		return refuse("no such source %q", r.Source)
	}
	if !slices.Contains(Kinds, r.Kind) {
		return refuse("kind %q isn't one of %s", r.Kind, strings.Join(Kinds, ", "))
	}
	if r.App != "ios" && r.App != "mac" {
		return refuse("app %q isn't ios or mac", r.App)
	}
	if len(r.Payload) == 0 || len(r.Payload) > MaxPayload {
		return refuse("the diagnostic is %d bytes: it must be 1 to %d", len(r.Payload), MaxPayload)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r.Payload, &obj); err != nil {
		return refuse("the diagnostic isn't a JSON object")
	}
	r.Device = textsafe.Line(r.Device, 60)
	r.AppVersion = textsafe.Line(r.AppVersion, 40)
	r.Build = textsafe.Line(r.Build, 40)
	r.BinaryUUID = textsafe.Line(r.BinaryUUID, 40)
	r.OS = textsafe.Line(r.OS, 80)
	r.Model = textsafe.Line(r.Model, 40)
	r.Summary = textsafe.Line(r.Summary, 200)
	return nil
}

// validSource says whether s names a source folder: mac, or a device's
// by its hex id.
func validSource(s string) bool {
	if s == SourceMac {
		return true
	}
	id, ok := strings.CutPrefix(s, "device-")
	if !ok || id == "" || len(id) > 128 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// prune removes a source's reports past MaxPerSource (the oldest) and
// past MaxAge.
func prune(dir string, now time.Time) {
	ms, _ := listDir(dir)
	for i, m := range ms {
		if i >= MaxPerSource || now.Sub(m.Received) > MaxAge {
			os.Remove(filepath.Join(dir, m.ID+".json"))
			os.Remove(filepath.Join(dir, m.ID+".mx.json"))
		}
	}
}

func readMeta(path string) (Meta, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Meta{}, err
	}
	return m, nil
}

// listDir is one source's reports, newest first.
func listDir(dir string) ([]Meta, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ms []Meta
	for _, e := range ents {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".mx.json") {
			continue
		}
		m, err := readMeta(filepath.Join(dir, name))
		if err != nil || m.ID+".json" != name {
			continue
		}
		m.Source = filepath.Base(dir)
		ms = append(ms, m)
	}
	sortNewest(ms)
	return ms, nil
}

func sortNewest(ms []Meta) {
	slices.SortFunc(ms, func(a, b Meta) int {
		if c := b.Received.Compare(a.Received); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
}

// List is every source's reports in dir, newest received first; none
// when dir doesn't exist.
func List(dir string) ([]Meta, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Meta{}, nil
	}
	if err != nil {
		return nil, err
	}
	ms := []Meta{}
	for _, e := range ents {
		if !e.IsDir() || !validSource(e.Name()) {
			continue
		}
		got, err := listDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		ms = append(ms, got...)
	}
	sortNewest(ms)
	return ms, nil
}

// NotFoundError is a report id nobody has.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return "no report " + e.ID + ": tm device reports lists them" }

// Get is the report id in dir, with its diagnostic; a unique prefix of
// an id names it too.
func Get(dir, id string) (Report, error) {
	ms, err := List(dir)
	if err != nil {
		return Report{}, err
	}
	var found []Meta
	for _, m := range ms {
		if m.ID == id {
			found = []Meta{m}
			break
		}
		if id != "" && strings.HasPrefix(m.ID, id) {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return Report{}, &NotFoundError{ID: id}
	case 1:
	default:
		return Report{}, refuse("%d reports start with %s: give more of the id", len(found), id)
	}
	m := found[0]
	b, err := os.ReadFile(filepath.Join(dir, m.Source, m.ID+".mx.json"))
	if err != nil {
		return Report{}, err
	}
	return Report{Meta: m, Payload: b}, nil
}

// Recent is the reports received since t, newest first.
func Recent(dir string, since time.Time) ([]Meta, error) {
	ms, err := List(dir)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ms, func(m Meta) bool { return m.Received.Before(since) }), nil
}
