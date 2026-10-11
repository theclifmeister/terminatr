// Package modtest is tm mod test (mods design §8, Dev workflow): it
// replays a code mod's event fixtures, tests/*.json in its folder,
// against the mod in a host run in-process, with a fake broker in place
// of the server's. The broker answers the $ API from the fixture and
// from a store and files of its own, refuses what the mod wasn't
// granted, and records every call, which the fixture's expectations are
// checked against. Nothing reaches the real server, projects or network.
package modtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// TestsDir is the folder of a mod's fixtures.
const TestsDir = "tests"

// Fixture is one tests/<name>.json: the mod's grants and settings, what
// the broker answers, the events to send in order, and what must come
// of them.
type Fixture struct {
	// Name says what the fixture shows; the file's name when empty.
	Name string `json:"name,omitempty"`
	// Grants are the permissions the mod runs with; nil is all it asks
	// for. Each must be one it asks for.
	Grants *[]string `json:"grants,omitempty"`
	// Settings overlay the manifest's defaults.
	Settings map[string]any `json:"settings,omitempty"`
	// Store and Files are the broker's store ($.store) and the files
	// $.fs reads, by path, at the start.
	Store map[string]json.RawMessage `json:"store,omitempty"`
	Files map[string]string          `json:"files,omitempty"`
	// Answers are the broker's answers, the first that matches a call
	// winning; a call none matches gets the broker's default (Broker).
	Answers []Answer `json:"answers,omitempty"`
	// Events are sent one at a time, each after the last one's handlers
	// are done. mod.ready goes first when the list doesn't start with it,
	// as after every start of a real host.
	Events []Step `json:"events"`
	Expect Expect `json:"expect"`
}

// Answer is the broker's answer to a call of Method whose params hold
// Params (Match's rule; none matches every call): Result, or Error.
type Answer struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Step is one event, in the event bus's shape: its name, the project it
// is about and its data. WaitMS waits after its handlers are done, for
// the mod's timers.
type Step struct {
	Name    string          `json:"name"`
	Project string          `json:"project,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	WaitMS  int             `json:"wait_ms,omitempty"`
}

// Expect is what must come of the events.
type Expect struct {
	// Calls must each be made, in this order, among any others; params
	// match by Match's rule.
	Calls []Call `json:"calls,omitempty"`
	// NoCalls are methods the mod must not call.
	NoCalls []string `json:"no_calls,omitempty"`
	// Logs must each be part of a $.log line.
	Logs []string `json:"logs,omitempty"`
	// Failures is how many handlers throw or reject; 0 unless set.
	Failures int `json:"failures,omitempty"`
	// Store must be part of the store at the end (Match's rule); a key
	// whose value is null must be missing.
	Store map[string]json.RawMessage `json:"store,omitempty"`
}

// Call is a $ API call: the method, as the wire names it
// ("ui.cell.set"), and its params object.
type Call struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (c Call) String() string {
	if len(c.Params) == 0 {
		return c.Method + "()"
	}
	return c.Method + " " + string(c.Params)
}

// Fixtures lists dir's tests/*.json, sorted.
func Fixtures(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, TestsDir, "*.json"))
	sort.Strings(paths)
	return paths, err
}

// ReadFixture reads and decodes one fixture; a key it doesn't know is an
// error, so a misspelt expectation can't pass unseen.
func ReadFixture(path string) (*Fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err := strictJSON(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if len(f.Events) == 0 {
		return nil, fmt.Errorf("%s: no events", filepath.Base(path))
	}
	for i, s := range f.Events {
		if s.Name == "" {
			return nil, fmt.Errorf("%s: events[%d] has no name", filepath.Base(path), i)
		}
	}
	for i, a := range f.Answers {
		if a.Method == "" {
			return nil, fmt.Errorf("%s: answers[%d] has no method", filepath.Base(path), i)
		}
	}
	for i, c := range f.Expect.Calls {
		if c.Method == "" {
			return nil, fmt.Errorf("%s: expect.calls[%d] has no method", filepath.Base(path), i)
		}
	}
	return &f, nil
}

func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Match reports whether got holds want: an object holds the keys of
// want's, each matching; an array has as many items, each matching; any
// other value is equal (numbers by value). JSON texts, decoded.
func Match(want, got json.RawMessage) bool {
	if len(want) == 0 {
		return true
	}
	var w, g any
	if json.Unmarshal(want, &w) != nil {
		return false
	}
	if len(got) == 0 {
		got = json.RawMessage("null")
	}
	if json.Unmarshal(got, &g) != nil {
		return false
	}
	return match(w, g)
}

func match(w, g any) bool {
	switch w := w.(type) {
	case map[string]any:
		gm, ok := g.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := gm[k]
			if !ok && wv != nil {
				return false
			}
			if !match(wv, gv) {
				return false
			}
		}
		return true
	case []any:
		ga, ok := g.([]any)
		if !ok || len(ga) != len(w) {
			return false
		}
		for i := range w {
			if !match(w[i], ga[i]) {
				return false
			}
		}
		return true
	default:
		return w == g
	}
}
