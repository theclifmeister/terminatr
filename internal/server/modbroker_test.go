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
	"time"

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
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Fix the login", Status: "started", Steps: []string{"Repro", "Fix"}}, {Title: "Two", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddItem(project.KindDelegate, "T2", "delegate T2", false); err != nil {
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
	// Mod API 1.2: the steps, the date, and what asks the coordinator.
	if ts := got.([]ModTask); len(ts[0].Steps) != 2 || ts[0].Steps[1].Text != "Fix" || ts[0].Steps[1].Done ||
		ts[0].Updated == "" || ts[0].Asked != "" || ts[1].Asked != project.KindDelegate || ts[1].Steps == nil {
		t.Fatalf("tasks.list = %+v", ts)
	}
	if err := p.DoneItems(project.KindDelegate, "T2"); err != nil {
		t.Fatal(err)
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

	// Mod API 1.10: a task's design and after, and the designs, from
	// the board and the archive, as their vault notes show them.
	digest := designsDigest(p)
	view := "net-view"
	if _, err := p.Tasks().SetPlan(human, 2, &view, []string{"T1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Old", Status: "done"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().SetPlan(human, 3, &view, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().Archive(human, 3); err != nil {
		t.Fatal(err)
	}
	title, task, answers := "Network view", "T1", []string{"Yes"}
	if _, _, err := p.SetDesign(view, project.DesignEdit{Title: &title, Task: &task, Answers: &answers}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddQuestion(project.Question{Task: "T1", Header: "Colour", Question: "Red?", Options: []project.QuestionOption{{Label: "a"}, {Label: "b"}}}); err != nil {
		t.Fatal(err)
	}
	if designsDigest(p) == digest {
		t.Fatal("the designs' digest didn't change")
	}
	got, err = call("tasks.list", map[string]string{"slug": "demo"})
	if ts, _ := got.([]ModTask); err != nil || len(ts) != 2 || ts[1].Design != view || !slices.Equal(ts[1].After, []string{"T1"}) || ts[0].Design != "" || ts[0].After != nil {
		t.Fatalf("tasks.list = %+v, %v", got, err)
	}
	got, err = call("designs.list", map[string]string{"slug": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	ds := got.([]modapi.Design)
	if len(ds) != 1 {
		t.Fatalf("designs.list = %+v", ds)
	}
	if d := ds[0]; d.Name != view || d.Title != title || d.Status != "building" || d.Task != "T1" || d.TaskStatus != "started" ||
		!slices.Equal(d.Tasks, []string{"T2", "T3"}) || d.TasksDone != 1 || !slices.Equal(d.Answers, answers) ||
		len(d.Questions) != 1 || d.Questions[0].Header != "Colour" || d.Questions[0].Question != "Red?" {
		t.Fatalf("designs.list = %+v", d)
	}
	if _, err := call("designs.list", map[string]string{"slug": "other"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("designs of a project it doesn't run for: %v", err)
	}

	got, err = call("inbox.raise", map[string]any{"project": "demo", "item": map[string]string{"subject": "label\x1b[31m T1\nnow", "summary": "T1 looks like a bug"}})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := p.Inbox()
	if len(items) != 1 || items[0].Kind != project.KindMod || items[0].Subject != "mod:lab label[31m T1 now" ||
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
	// A path the mod chose is in the denial: escaped in the log, so it
	// can't act on the terminal that reads it (tm mod logs).
	before, _ = os.ReadFile(filepath.Join(tmHome, "logs", "mods", "lab.log"))
	if _, err := call("fs.read", map[string]string{"path": "/x\x1b]52;c;eA==\a\nforged line"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("escape path: %v", err)
	}
	if log3, _ := os.ReadFile(filepath.Join(tmHome, "logs", "mods", "lab.log")); strings.ContainsAny(string(log3[len(before):]), "\x1b\a") ||
		strings.Count(string(log3[len(before):]), "\n") != 1 || !strings.Contains(string(log3[len(before):]), `/x\x1b]52;c;eA==\a\nforged line`) {
		t.Fatalf("lab.log = %q", log3[len(before):])
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

// TestModBrokerAPI: the broker's methods are mod API v1's, the schema the
// $ bindings are built from (modapi.Methods), one list: every method the
// host sends has a handler, every handler is a method of the schema, and
// the broker checks the grant the schema says it needs. tm mod test's
// fake broker answers the schema's methods, so a mod that passes its
// tests calls nothing the server lacks (ui.toast did, T301).
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
	s.ModRequest(context.Background(), "ghost", conn, &modproto.Message{ID: &id, Method: "store.get", Params: json.RawMessage(`{"key":"k"}`)})
	m, err := read.Read()
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == nil || *m.ID != 7 || m.Error == nil || m.Error.Code != modproto.CodeDenied {
		t.Fatalf("reply %+v", m)
	}
	s.ModRequest(context.Background(), "ghost", conn, &modproto.Message{Method: "log"}) // a notification: no answer
}

// TestModRequestBounded: a mod gets modCallsAtOnce calls running and
// modCallsWaiting waiting; the next is refused at once, and the host's
// end ends the rest.
func TestModRequestBounded(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte("[mods.lab]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
	started := make(chan struct{}, modCallsAtOnce+modCallsWaiting)
	s.broker().Handle("test.block", "", func(ctx context.Context, _ *modbroker.Call) (any, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	inR, inW := io.Pipe()
	conn := modproto.NewConn(strings.NewReader(""), inW)
	read := modproto.NewConn(inR, io.Discard)
	replies := make(chan *modproto.Message, 2*(modCallsAtOnce+modCallsWaiting))
	go func() {
		for {
			m, err := read.Read()
			if err != nil {
				return
			}
			replies <- m
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := modCallsAtOnce + modCallsWaiting
	for i := range n {
		id := int64(i + 1)
		s.ModRequest(ctx, "lab", conn, &modproto.Message{ID: &id, Method: "test.block"})
	}
	for range modCallsAtOnce {
		<-started
	}
	id := int64(n + 1)
	s.ModRequest(ctx, "lab", conn, &modproto.Message{ID: &id, Method: "test.block"})
	m := <-replies
	if m.ID == nil || *m.ID != id || m.Error == nil || m.Error.Code != modproto.CodeFailed {
		t.Fatalf("call past the limit: %+v", m)
	}
	select {
	case <-started:
		t.Fatal("more than modCallsAtOnce calls ran at once")
	default:
	}
	cancel()
	for range n {
		if m := <-replies; m.Error == nil || m.Error.Code != modproto.CodeFailed {
			t.Fatalf("call after the host ended: %+v", m)
		}
	}
	// Each call is counted out just after its answer.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		s.modBr.mu.Lock()
		left := s.modBr.inFlight["lab"]
		s.modBr.mu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d calls still counted in flight", left)
		}
	}
}
