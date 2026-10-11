package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const pingManifest = `id = "ping"
name = "Ping"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
[permissions]
notify = true
`

// TestModNotify: a mod with the notify grant puts a notification in the
// tray with the core's labels; it is refused a button that isn't a core
// action, a project it doesn't run for, and the grant once taken back.
func TestModNotify(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "ping")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": pingManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(tmHome, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[mods.ping]\nenabled = true\ngrants = [\"notify\"]\nprojects = [\"demo\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents(), notices: modnotify.New(nil)}
	call := func(params any) (any, error) {
		raw, _ := json.Marshal(params)
		return s.CallMod(context.Background(), "ping", "ui.notify", raw)
	}
	code := func(err error) int {
		if e, ok := err.(*modbroker.Error); ok {
			return e.Code
		}
		return 0
	}
	btn := func(id, proj, task string) map[string]any {
		return map[string]any{"id": id, "target": map[string]string{"project": proj, "task": task}}
	}

	if _, err := call(map[string]any{"notification": map[string]any{
		"title": "T12 \x1b[31mneeds you", "text": "in review\nfor 2 days", "level": "warn",
		"actions": []any{btn("task.accept", "demo", "T12"), btn("task.send-back", "demo", "T12")},
	}}); err != nil {
		t.Fatal(err)
	}
	res := s.list()
	if len(res.ModNotices) != 1 {
		t.Fatalf("tray = %+v", res.ModNotices)
	}
	n := res.ModNotices[0]
	if n.Mod != "ping" || n.Title != "T12 [31mneeds you" || n.Text != "in review for 2 days" || n.Level != "warn" ||
		len(n.Buttons) != 2 || n.Buttons[0].Label != "Accept" || n.Buttons[1].Label != "Send back" ||
		n.Buttons[0].Project != "demo" || n.Buttons[0].Task != 12 {
		t.Fatalf("notice = %+v", n)
	}

	for name, params := range map[string]any{
		"another project": map[string]any{"title": "x", "actions": []any{btn("task.accept", "other", "T1")}},
		"no such project": map[string]any{"title": "x", "actions": []any{btn("task.accept", "ghost", "T1")}},
		"not a core id":   map[string]any{"title": "x", "actions": []any{btn("settings.open", "demo", "T1")}},
		"no title":        map[string]any{"title": "  "},
	} {
		if _, err := call(params); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := call(map[string]any{"title": "x", "actions": []any{btn("task.accept", "other", "T1")}}); code(err) != modbroker.CodeDenied {
		t.Errorf("another project: %v", err)
	}

	// Only a human's console draws or dismisses.
	if _, perr := s.noticeCall(proto.MethodNotifyClaim, proto.NoticeParams{ID: n.ID}, caller.Caller{Kind: caller.Thread}); perr == nil {
		t.Error("an agent claimed a notification")
	}
	r, perr := s.noticeCall(proto.MethodNotifyClaim, proto.NoticeParams{ID: n.ID}, human)
	if perr != nil || !r.(proto.NoticeResult).OK {
		t.Fatalf("claim = %v, %v", r, perr)
	}
	if r, _ := s.noticeCall(proto.MethodNotifyClaim, proto.NoticeParams{ID: n.ID}, human); r.(proto.NoticeResult).OK {
		t.Error("two consoles drew it")
	}

	// The grant taken back: the tray empties and the call is refused.
	if err := os.WriteFile(cfgPath, []byte("[mods.ping]\nenabled = true\ngrants = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.modBr.mu.Lock()
	s.modBr.reg = nil
	s.modBr.mu.Unlock()
	if got := s.list().ModNotices; len(got) != 0 {
		t.Fatalf("tray after the grant is gone = %+v", got)
	}
	if _, err := call(map[string]any{"title": "x"}); code(err) != modbroker.CodeDenied || !strings.Contains(err.Error(), "notify") {
		t.Fatalf("without the grant: %v", err)
	}
}
