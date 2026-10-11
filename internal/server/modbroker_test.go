package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
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
	if err != nil || strings.Join(got.([]string), ",") != "demo" {
		t.Fatalf("projects.list = %v, %v", got, err)
	}
	got, err = call("tasks.list", map[string]string{"project": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if ts := got.([]modbus.Task); len(ts) != 2 || ts[0].ID != "T1" || ts[0].To != "started" {
		t.Fatalf("tasks.list = %+v", ts)
	}
	if _, err := call("tasks.list", map[string]string{"project": "other"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("a project it doesn't run for: %v", err)
	}
	if _, err := call("project.get", map[string]string{"project": "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("thread.get", map[string]string{"project": "demo", "thread": "t-0009"}); code(err) != modbroker.CodeNotFound {
		t.Fatalf("a missing thread: %v", err)
	}

	got, err = call("inbox.raise", map[string]string{"project": "demo", "subject": "label\x1b[31m T1\nnow", "summary": "T1 looks like a bug"})
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
	if _, err := call("inbox.raise", map[string]string{"project": "demo", "subject": "again"}); code(err) != modbroker.CodeDenied {
		t.Fatalf("raise without the grant: %v", err)
	}
}
