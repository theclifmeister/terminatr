package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

const labManifest = `id = "lab"
name = "Lab"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
[permissions]
project_read = true
inbox = true
`

// TestModBroker: an enabled mod reads the projects it runs for and
// raises an item, journaled as mod:lab; it is refused another project,
// a grant it lacks, and once the human takes the grant back.
func TestModBroker(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": labManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.lab]\nenabled = true\ngrants = [\"project_read\", \"inbox\"]\nprojects = [\"demo\"]\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Fix the login", Status: "started"}, {Title: "Two"}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
	call := func(method string, params any) (any, error) {
		raw, _ := json.Marshal(params)
		return s.CallMod(context.Background(), "lab", method, raw)
	}
	code := func(err error) int {
		if e, ok := err.(*modbroker.Error); ok {
			return e.Code
		}
		return 0
	}

	got, err := call("projects.list", nil)
	if ps, _ := got.([]ModProject); err != nil || len(ps) != 1 || ps[0].Slug != "demo" {
		t.Fatalf("projects.list = %v, %v", got, err)
	}
	got, err = call("tasks.list", map[string]string{"slug": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if ts := got.([]ModTask); len(ts) != 2 || ts[0].ID != "T1" || ts[0].Status != "started" {
		t.Fatalf("tasks.list = %+v", ts)
	}
	if _, err := call("tasks.list", map[string]string{"slug": "other"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("a project it doesn't run for: %v", err)
	}
	if _, err := call("project.get", map[string]string{"slug": "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("project.get", map[string]string{"project": "demo"}); err != nil {
		t.Fatal(err)
	}
	if got, err := call("thread.get", map[string]string{"project": "demo", "thread": "t-0009"}); err != nil || got != nil {
		t.Fatalf("a missing thread = %v, %v; want null", got, err)
	}
	if got, err := call("report.get", map[string]string{"project": "demo", "thread": "t-0009"}); err != nil || got != nil {
		t.Fatalf("a missing thread's report = %v, %v; want null", got, err)
	}

	got, err = call("inbox.raise", map[string]any{"project": "demo", "item": map[string]string{"subject": "label\x1b[31m T1\nnow", "summary": "T1 looks like a bug"}})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := p.Inbox()
	if len(items) != 1 || items[0].Kind != project.KindMod || items[0].Subject != "mod:lab label [31m T1 now" ||
		items[0].ID != got.(map[string]string)["id"] {
		t.Fatalf("items = %+v", items)
	}
	lines, _, _ := p.JournalTail(5)
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], " mod:lab inbox.raise "+items[0].ID) {
		t.Fatalf("journal = %q", lines)
	}
	before, _ := os.ReadFile(filepath.Join(tmHome, "logs", "mods", "lab.log"))
	if _, err := call("fs.read", map[string]string{"path": "~/.terminatr/config.toml"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("config.toml: %v", err)
	}
	if log2, _ := os.ReadFile(filepath.Join(tmHome, "logs", "mods", "lab.log")); !strings.Contains(string(log2[len(before):]), "broker: denied fs.read") {
		t.Fatalf("lab.log = %q", log2)
	}

	// The human takes the inbox grant back: it applies at the next read.
	cfg = strings.Replace(cfg, `, "inbox"`, "", 1)
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s.modBr.mu.Lock()
	s.modBr.reg = nil
	s.modBr.mu.Unlock()
	if _, err := call("inbox.raise", map[string]any{"project": "demo", "item": map[string]string{"subject": "again"}}); code(err) != modbroker.CodeDenied {
		t.Fatalf("raise without the grant: %v", err)
	}
}

// modPending are the schema's methods whose handlers come in their own
// tasks (Broker.Handle): drop each from here when its handler lands.
var modPending = map[string]bool{
	"ui.cell.set": true, "ui.status.set": true, "ui.toast": true,
	"ui.view.set": true, "ui.view.open": true,
}

// TestModBrokerAPI: the broker's methods are mod API v1's, the schema the
// $ bindings are built from (modapi.Methods), one list: every method the
// host sends has a handler, every handler is a method of the schema, and
// the broker checks the grant the schema says it needs.
func TestModBrokerAPI(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
	b := s.broker()
	schema := map[string]modapi.Method{}
	for _, m := range modapi.Methods {
		schema[m.Name] = m
		if m.Needs != "" && !slices.Contains(mod.Perms, m.Needs) {
			t.Errorf("%s: needs %q, which is no permission of mod.toml", m.Name, m.Needs)
		}
		needs, ok := b.Needs(m.Name)
		switch {
		case modPending[m.Name]:
			if ok {
				t.Errorf("%s has a handler now: drop it from modPending", m.Name)
			}
		case m.Local:
			// Answered in the host from hello; the broker may answer it too.
			if ok && needs != m.Needs {
				t.Errorf("%s: the broker checks %q, the schema says %q", m.Name, needs, m.Needs)
			}
		case !ok:
			t.Errorf("%s: a method of the schema with no handler", m.Name)
		case needs != m.Needs:
			t.Errorf("%s: the broker checks %q, the schema says %q", m.Name, needs, m.Needs)
		}
	}
	for name := range modPending {
		if _, ok := schema[name]; !ok {
			t.Errorf("modPending names %s, which the schema hasn't", name)
		}
	}
	for _, name := range b.Methods() {
		if _, ok := schema[name]; !ok {
			t.Errorf("%s: a handler for no method of the schema", name)
		}
	}
}

// TestModRequest: the supervisor's hook answers a host's request on its
// connection, a result or the broker's error code.
func TestModRequest(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
	inR, inW := io.Pipe()
	conn := modproto.NewConn(strings.NewReader(""), inW)
	read := modproto.NewConn(inR, io.Discard)
	id := int64(7)
	s.ModRequest("ghost", conn, &modproto.Message{ID: &id, Method: "store.get", Params: json.RawMessage(`{"key":"k"}`)})
	m, err := read.Read()
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == nil || *m.ID != 7 || m.Error == nil || m.Error.Code != modproto.CodeDenied {
		t.Fatalf("reply %+v", m)
	}
	s.ModRequest("ghost", conn, &modproto.Message{Method: "log"}) // a notification: no answer
}
