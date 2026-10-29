package moddev

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// EventLimit is how long an event's handlers may take, as on the event
// bus.
const EventLimit = 5 * time.Second

// Mod is a code mod ready to test: its folder, manifest and script.
type Mod struct {
	Dir      string
	Manifest *mod.Manifest
	Script   string
	// Budget is each loop slice's CPU budget; zero is the host's default.
	Budget time.Duration
}

// Load validates the mod in dir and bundles its main. A data mod, with
// no main, has a nil Script: there is nothing to run.
func Load(dir string) (*Mod, error) {
	man, err := mod.Validate(dir)
	if err != nil {
		return nil, err
	}
	m := &Mod{Dir: dir, Manifest: man}
	if !man.IsCode() {
		return m, nil
	}
	if m.Script, err = modhost.Load(filepath.Join(dir, filepath.FromSlash(man.Provides.Main))); err != nil {
		return nil, err
	}
	return m, nil
}

// Result is one fixture's outcome: OK, or the problems found, with the
// calls the mod made and its log lines.
type Result struct {
	File     string   `json:"file"`
	Name     string   `json:"name"`
	OK       bool     `json:"ok"`
	Problems []string `json:"problems,omitempty"`
	Calls    []Call   `json:"calls"`
	Logs     []string `json:"logs,omitempty"`
}

// Run replays fixture f (from file) against the mod in a fresh host.
func (m *Mod) Run(f *Fixture, file string) (r Result) {
	r = Result{File: file, Name: cmp.Or(f.Name, file), Calls: []Call{}}
	bad := func(format string, a ...any) { r.Problems = append(r.Problems, fmt.Sprintf(format, a...)) }
	defer func() { r.OK = len(r.Problems) == 0 }()
	all, projects, err := m.settings(f)
	if err != nil {
		bad("settings: %v", err)
	}
	m.checkRules(f, all, projects, bad)
	if len(f.Events) == 0 {
		return r
	}
	if m.Script == "" {
		bad("%s is a data mod: it has no code to run", m.Manifest.ID)
		return r
	}
	asked := m.Manifest.Permissions.Requested()
	grants := asked
	if f.Grants != nil {
		grants = []string{}
		for _, g := range *f.Grants {
			if !slices.Contains(asked, g) {
				bad("grants: the mod doesn't ask for %s", g)
				continue
			}
			grants = append(grants, g)
		}
	}
	settings, _ := json.Marshal(all)
	var projectSettings map[string]json.RawMessage
	for slug, s := range projects {
		if projectSettings == nil {
			projectSettings = map[string]json.RawMessage{}
		}
		projectSettings[slug], _ = json.Marshal(s)
	}
	steps := f.Events
	if steps[0].Name != modbus.ModReady {
		steps = append([]Step{{Name: modbus.ModReady, Data: json.RawMessage(`{"projects":[]}`)}}, steps...)
	}
	tools, hosts := 0, 0
	for i, s := range steps {
		if modproto.IsCodeHost(s.Name) {
			hosts++
			if m.Manifest.Provides.CodeHost == "" {
				bad("events[%d]: %s asks a code host, which the mod doesn't provide (provides.code_host)", i, s.Name)
			}
			continue
		}
		if s.Name == ToolStep {
			tools++
			var c modproto.ToolCall
			_ = json.Unmarshal(s.Data, &c)
			switch {
			case !slices.ContainsFunc(m.Manifest.Provides.Tools, func(t mod.Tool) bool { return t.ID == c.Tool }):
				bad("events[%d]: the mod offers no tool %q", i, c.Tool)
			case !slices.Contains(grants, mod.PermTools):
				bad("events[%d]: a tool call needs the tools permission, which the mod isn't granted", i)
			}
			continue
		}
		known := slices.ContainsFunc(modapi.Events, func(e modapi.Event) bool { return e.Name == s.Name })
		switch need := modbus.Needs(s.Name); {
		case !known:
			bad("events[%d]: no event is called %q", i, s.Name)
		case need != "" && !slices.Contains(grants, need):
			bad("events[%d]: %s needs the %s permission, which the mod isn't granted: it would never get it", i, s.Name, need)
		}
	}
	if len(f.Expect.Tools) > tools {
		bad("expect.tools: %d answers for %d tool calls", len(f.Expect.Tools), tools)
	}
	if len(f.Expect.CodeHost) > hosts {
		bad("expect.codehost: %d answers for %d code host calls", len(f.Expect.CodeHost), hosts)
	}
	for _, c := range f.Expect.Calls {
		if !modapi.Has(c.Method) {
			bad("expect.calls: no method is called %q", c.Method)
		}
	}
	if len(r.Problems) > 0 {
		return r
	}

	clock := time.Now
	if f.Now != "" {
		at, err := time.Parse(time.RFC3339, f.Now)
		if err != nil {
			bad("now: %q is not an RFC 3339 time like \"2026-10-13T12:00:00Z\"", f.Now)
			return r
		}
		clock = func() time.Time { return at }
	}

	b := NewBroker(grants, m.Manifest.Permissions, f.Answers, f.Store, f.Files)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	exited := make(chan int, 1)
	go func() {
		exited <- modhost.Run(modhost.Options{Name: m.Manifest.Provides.Main, Script: m.Script, Budget: m.Budget,
			In: inR, Out: outW, Bind: modapi.Bind, Now: clock})
		// Writes to a host that is gone fail, not hang.
		inR.CloseWithError(io.EOF)
		outW.Close()
	}()
	d := newDriver(outR, inW, b)
	defer inW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), EventLimit+modhost.DefaultBudget)
	_, err = d.call(ctx, modproto.MethodHello, modproto.Hello{Protocol: modproto.Protocol, API: mod.APIVersion(),
		TM: version.Version, Mod: m.Manifest.ID, Grants: grants, Settings: settings, Projects: projectSettings})
	cancel()
	if err != nil {
		bad("loading the script: %v", err)
	}
	var answers []string
	var hostAnswers []json.RawMessage
	for i, s := range steps {
		if err != nil {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), EventLimit)
		if modproto.IsCodeHost(s.Name) {
			var raw json.RawMessage
			if raw, err = d.call(ctx, s.Name, s.Data); err != nil {
				// The handler's throw is a failed call of the host, not
				// the run's end.
				raw, _ = json.Marshal(map[string]string{"error": err.Error()})
				err = nil
			}
			hostAnswers = append(hostAnswers, raw)
			cancel()
			continue
		}
		if s.Name == ToolStep {
			var raw json.RawMessage
			if raw, err = d.call(ctx, modproto.MethodToolCall, s.Data); err != nil {
				// The handler's throw is the agent's answer, not the run's end.
				answers, err = append(answers, "error: "+err.Error()), nil
			} else {
				var res modproto.ToolResult
				_ = json.Unmarshal(raw, &res)
				answers = append(answers, res.Text)
			}
			cancel()
			continue
		}
		data := s.Data
		if f.Now != "" && s.Name == modbus.ClockMinute {
			data = withNow(data, clock().Truncate(time.Minute))
		}
		_, err = d.call(ctx, modproto.MethodEvent, modproto.Event{Name: s.Name, Project: s.Project,
			Seq: uint64(i + 1), Time: clock(), Data: data})
		cancel()
		if err != nil {
			bad("events[%d] %s: %v", i, s.Name, err)
			break
		}
		if s.WaitMS > 0 {
			time.Sleep(time.Duration(s.WaitMS) * time.Millisecond)
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = d.call(ctx, modproto.MethodShutdown, nil)
	cancel()
	inW.Close()
	select {
	case code := <-exited:
		switch code {
		case modproto.ExitInterrupted:
			bad("a handler ran over the CPU budget and was interrupted")
		case modproto.ExitLoad:
			bad("the script doesn't compile")
		}
	case <-time.After(5 * time.Second):
		bad("the host didn't exit after shutdown")
	}
	<-d.c.Load().Done()

	r.Calls, r.Logs = b.Calls(), d.lines()
	failed := d.failures()
	if len(failed) != f.Expect.Failures {
		bad("%d handlers failed; expected %d", len(failed), f.Expect.Failures)
		for _, h := range failed {
			bad("  %s %s: %s", h.Where, h.Kind, firstLine(h.Error))
		}
	}
	next := 0
	for _, want := range f.Expect.Calls {
		i := slices.IndexFunc(r.Calls[next:], func(c Call) bool { return c.Method == want.Method && Match(want.Params, c.Params) })
		if i < 0 {
			bad("no call %s after the calls matched before it", want)
			continue
		}
		next += i + 1
	}
	for _, name := range f.Expect.NoCalls {
		if i := slices.IndexFunc(r.Calls, func(c Call) bool { return c.Method == name }); i >= 0 {
			bad("called %s, which it must not", r.Calls[i])
		}
	}
	for _, want := range f.Expect.Logs {
		if !slices.ContainsFunc(r.Logs, func(l string) bool { return strings.Contains(l, want) }) {
			bad("no log line has %q", want)
		}
	}
	for i, want := range f.Expect.Tools {
		if i < len(answers) && !strings.Contains(answers[i], want) {
			bad("tool call %d answered %q; expected it to hold %q", i+1, answers[i], want)
		}
	}
	for i, want := range f.Expect.CodeHost {
		if i >= len(hostAnswers) {
			break
		}
		got := hostAnswers[i]
		var w struct {
			Error *string `json:"error"`
		}
		if json.Unmarshal(want, &w) == nil && w.Error != nil {
			var g struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(got, &g) != nil || g.Error == "" || !strings.Contains(g.Error, *w.Error) {
				bad("code host call %d answered %s; expected an error holding %q", i+1, got, *w.Error)
			}
			continue
		}
		if !Match(want, got) {
			bad("code host call %d answered %s; expected %s", i+1, got, want)
		}
	}
	store := b.Store()
	for _, k := range slices.Sorted(maps.Keys(f.Expect.Store)) {
		want := f.Expect.Store[k]
		got, ok := store[k]
		switch {
		case string(want) == "null" && ok:
			bad("store[%q] is %s; expected none", k, got)
		case string(want) != "null" && !ok:
			bad("store[%q] is missing; expected %s", k, want)
		case ok && !Match(want, got):
			bad("store[%q] is %s; expected %s", k, got, want)
		}
	}
	return r
}

// RunDir loads the mod in dir and replays each of its fixtures. A code
// mod with none is an error: nothing would be tested. A data mod's
// fixtures, when it has some, check its rules text.
func RunDir(dir string) ([]Result, error) {
	m, err := Load(dir)
	if err != nil {
		return nil, err
	}
	paths, err := Fixtures(dir)
	if err != nil {
		return nil, err
	}
	if m.Script == "" && len(paths) == 0 {
		return nil, nil
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s has no fixtures: add %s/<name>.json (tm mod new writes one)", m.Manifest.ID, TestsDir)
	}
	var out []Result
	for _, p := range paths {
		name := filepath.ToSlash(filepath.Join(TestsDir, filepath.Base(p)))
		f, err := ReadFixture(p)
		if err != nil {
			out = append(out, Result{File: name, Name: name, Problems: []string{err.Error()}, Calls: []Call{}})
			continue
		}
		out = append(out, m.Run(f, name))
	}
	return out, nil
}

// settings are the manifest's defaults overlaid with f's settings, as
// hello sends them, and each of f's projects' settings over those.
func (m *Mod) settings(f *Fixture) (all map[string]any, projects map[string]map[string]any, err error) {
	whole := func(set map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range set {
			// JSON's numbers are floats; a setting's are whole.
			if f, ok := v.(float64); ok && f == float64(int64(f)) {
				v = int64(f)
			}
			out[k] = v
		}
		return out
	}
	var errs []error
	all, bad := mod.SettingsOf(m.Manifest.ID, m.Manifest, whole(f.Settings))
	for _, b := range bad {
		errs = append(errs, errors.New(b))
	}
	for _, slug := range slices.Sorted(maps.Keys(f.Projects)) {
		set := whole(f.Settings)
		maps.Copy(set, whole(f.Projects[slug]))
		s, bad := mod.SettingsOf(m.Manifest.ID, m.Manifest, set)
		for _, b := range bad {
			errs = append(errs, fmt.Errorf("projects.%s: %s", slug, b))
		}
		if projects == nil {
			projects = map[string]map[string]any{}
		}
		projects[slug] = s
	}
	return all, projects, errors.Join(errs...)
}

// checkRules checks f's rules expectations against the mod's rules
// files, resolved with the settings for each one's project.
func (m *Mod) checkRules(f *Fixture, all map[string]any, projects map[string]map[string]any, bad func(string, ...any)) {
	md := mod.Mod{ID: m.Manifest.ID, Dir: m.Dir, Manifest: m.Manifest}
	for _, key := range slices.Sorted(maps.Keys(f.Rules)) {
		want := f.Rules[key]
		rel, ok := m.Manifest.Provides.Rules[key]
		if !ok {
			bad("rules.%s: the mod provides no such rules (provides.rules)", key)
			continue
		}
		settings := all
		if s, ok := projects[want.Project]; ok {
			settings = s
		}
		b, err := md.ReadAgentText(rel)
		var text string
		if err == nil {
			text, err = mod.ResolveRules(string(b), m.Manifest, settings)
		}
		if err != nil {
			bad("rules.%s: %v", key, err)
			continue
		}
		where := "for all projects"
		if want.Project != "" {
			where = "for " + want.Project
		}
		for _, c := range want.Contains {
			if !strings.Contains(text, c) {
				bad("rules.%s %s lacks %q", key, where, c)
			}
		}
		for _, l := range want.Lacks {
			if strings.Contains(text, l) {
				bad("rules.%s %s has %q", key, where, l)
			}
		}
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// driver is the server's side of the host: a modproto.Client that
// answers the host's calls with the broker on the read loop, so they are
// recorded in the order the mod made them, and keeps the host's log lines
// and failed handlers.
type driver struct {
	c atomic.Pointer[modproto.Client] // set once the client exists
	b *Broker

	mu     sync.Mutex
	logs   []string
	failed []modproto.HandlerFailed
}

func newDriver(r io.Reader, w io.Writer, b *Broker) *driver {
	d := &driver{b: b}
	d.c.Store(modproto.NewInlineClient(r, w, d.answer, d.note))
	return d
}

func (d *driver) answer(m *modproto.Message) {
	conn := d.c.Load().Conn()
	if res, e := d.b.Answer(m.Method, m.Params); e != nil {
		_ = conn.ReplyError(*m.ID, e.Code, e.Message)
	} else {
		_ = conn.Reply(*m.ID, res)
	}
}

func (d *driver) note(m *modproto.Message) {
	switch m.Method {
	case modproto.MethodLog:
		var l modproto.Log
		if json.Unmarshal(m.Params, &l) == nil {
			d.mu.Lock()
			d.logs = append(d.logs, l.Level+": "+l.Text)
			d.mu.Unlock()
		}
	case modproto.MethodHandlerFailed:
		var h modproto.HandlerFailed
		if json.Unmarshal(m.Params, &h) == nil {
			d.mu.Lock()
			d.failed = append(d.failed, h)
			d.mu.Unlock()
		}
	}
}

// call sends a request and waits for its answer, ctx's end or the host's.
func (d *driver) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := d.c.Load().Call(ctx, method, params)
	var e *modproto.Error
	switch {
	case errors.As(err, &e):
		return nil, errors.New(e.Message)
	case errors.Is(err, modproto.ErrClosed):
		return nil, errors.New("the host is gone")
	case err != nil:
		return nil, errors.New("no answer in time")
	}
	return raw, nil
}

func (d *driver) lines() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.logs)
}

func (d *driver) failures() []modproto.HandlerFailed {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.failed)
}

// withNow gives a clock.minute event's data the "now" it lacks.
func withNow(data json.RawMessage, now time.Time) json.RawMessage {
	m := map[string]json.RawMessage{}
	if len(data) > 0 && json.Unmarshal(data, &m) != nil {
		return data
	}
	if _, ok := m["now"]; ok {
		return data
	}
	m["now"], _ = json.Marshal(now.Format(time.RFC3339))
	out, _ := json.Marshal(m)
	return out
}
