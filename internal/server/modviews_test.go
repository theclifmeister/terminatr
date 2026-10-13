package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const panelManifest = `id = "panel"
name = "Panel"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
views = [{ id = "queue", title = "Review queue", slot = "project.tab" }, { id = "cost", title = "Cost", slot = "dashboard.card" }]
[permissions]
project_read = true
views = true
`

// panelServer is a bare Server with mod panel installed and enabled
// with grants, running for project demo.
func panelServer(t *testing.T, grants string) *Server {
	return manifestServer(t, panelManifest, grants)
}

// manifestServer is panelServer with mod panel's manifest.
func manifestServer(t *testing.T, manifest, grants string) *Server {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "panel")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": manifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The built-in Memory tab off: these tests are about mod panel.
	cfg := "[mods.panel]\nenabled = true\ngrants = [" + grants + "]\nprojects = [\"demo\"]\n[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	return &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
}

// next is the sub's next event within a second.
func next(t *testing.T, sub *modbus.Sub) modbus.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ev, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("no event: %v", err)
	}
	return ev
}

// TestModViews: a mod publishes its views' trees, a console polls them
// and says what it shows (view.opened, view.closed), its keys reach the
// mod (ui.key), only after which the mod may open a view, and its
// cursor's moves (ui.cursor), which don't let it; a restarted
// host loses its trees and gets view.opened again.
func TestModViews(t *testing.T) {
	s := panelServer(t, `"project_read", "views"`)
	sub := s.SubscribeMod("panel", []string{"project_read", "views"}, nil)
	if ev := next(t, sub); ev.Name != modbus.ModReady {
		t.Fatalf("first event %s", ev.Name)
	}
	call := func(method string, params any) error {
		raw, _ := json.Marshal(params)
		_, err := s.CallMod(context.Background(), "panel", method, raw)
		return err
	}
	tree := map[string]any{"kind": "list", "id": "q", "rows": []any{map[string]any{"id": "T1", "spans": []any{map[string]any{"text": "T1 \x1b[31mfix"}}}}}
	if err := call("ui.view.set", map[string]any{"view": "queue", "widget": tree, "options": map[string]any{"project": "demo", "count": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := call("ui.view.set", map[string]any{"view": "nope", "widget": tree}); err == nil {
		t.Fatal("an undeclared view was set")
	}
	if err := call("ui.view.set", map[string]any{"view": "queue", "widget": tree, "options": map[string]any{"project": "other"}}); err == nil {
		t.Fatal("a tree for a project it doesn't run for was set")
	}
	if err := call("ui.view.set", map[string]any{"view": "queue", "widget": map[string]any{"kind": "bogus"}}); err == nil {
		t.Fatal("a bad tree was set")
	}
	if err := call("ui.view.open", map[string]any{"view": "queue"}); err == nil || err.(*modbroker.Error).Code != modbroker.CodeDenied {
		t.Fatalf("open without a human input: %v", err)
	}

	shown := []modview.Shown{{Mod: "panel", View: "queue", Slot: "project.tab", Project: "demo", Width: 70, Height: 20},
		{Mod: "panel", View: "queue", Slot: "project.tab", Project: "other"}} // dropped: not its project
	res, perr := s.modViews(proto.ModViewsParams{Shown: shown}, 42, human)
	if perr != nil {
		t.Fatal(perr)
	}
	r := res.(proto.ModViewsResult)
	if len(r.Views) != 2 || r.Views[0].View != "queue" || r.Views[0].Title != "Review queue" || r.Views[1].Slot != "dashboard.card" {
		t.Fatalf("views %+v", r.Views)
	}
	if tr := r.Views[0].Trees; len(tr) != 1 || tr[0].Project != "demo" || *tr[0].Count != 1 || tr[0].Tree.Rows[0].Spans[0].Text != "T1 [31mfix" {
		t.Fatalf("trees %+v", tr)
	}
	ev := next(t, sub)
	if v, ok := ev.Data.(modbus.View); ev.Name != modbus.ViewOpened || !ok || v.Project != "demo" || v.Width != 70 {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
	if sub.Len() != 0 {
		t.Fatal("the other project's tab was opened")
	}

	// Where the cursor went reaches the mod, but lets it open nothing.
	if _, perr := s.modViewInput(proto.ModViewInputParams{Mod: "panel", View: "queue", Project: "demo", Kind: "cursor", Widget: "q", Row: "T1"}, 42, human); perr != nil {
		t.Fatal(perr)
	}
	ev = next(t, sub)
	if in, ok := ev.Data.(modbus.Input); ev.Name != modbus.UICursor || !ok || in.Widget != "q" || in.Row != "T1" || in.Key != "" || in.Target.Project != "demo" {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
	if err := call("ui.view.open", map[string]any{"view": "cost"}); err == nil {
		t.Fatal("open after a cursor move")
	}
	if _, perr := s.modViewInput(proto.ModViewInputParams{Mod: "panel", View: "queue", Project: "demo", Kind: "scroll"}, 42, human); perr == nil {
		t.Fatal("kind scroll was taken")
	}
	if _, perr := s.modViewInput(proto.ModViewInputParams{Mod: "panel", View: "queue", Project: "demo", Kind: "key", Widget: "q", Row: "T1", Key: "o"}, 42,
		caller.Caller{Kind: caller.Coordinator}); perr == nil {
		t.Fatal("an agent's input was taken")
	}
	if _, perr := s.modViewInput(proto.ModViewInputParams{Mod: "panel", View: "queue", Project: "demo", Kind: "key", Widget: "q", Row: "T1", Key: "o"}, 42, human); perr != nil {
		t.Fatal(perr)
	}
	ev = next(t, sub)
	if in, ok := ev.Data.(modbus.Input); ev.Name != modbus.UIKey || !ok || in.Row != "T1" || in.Key != "o" || in.Target.Project != "demo" {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
	if err := call("ui.view.open", map[string]any{"view": "cost"}); err != nil {
		t.Fatalf("open after a human input: %v", err)
	}
	res, _ = s.modViews(proto.ModViewsParams{Shown: shown[:1]}, 42, human)
	if o := res.(proto.ModViewsResult).Open; len(o) != 1 || o[0].View != "cost" || o[0].Project != "demo" {
		t.Fatalf("opens %+v", o)
	}

	// A restart: no trees, and the tab on show opens again.
	sub = s.SubscribeMod("panel", []string{"project_read", "views"}, nil)
	next(t, sub) // mod.ready
	if ev := next(t, sub); ev.Name != modbus.ViewOpened {
		t.Fatalf("after a restart: %s", ev.Name)
	}
	res, _ = s.modViews(proto.ModViewsParams{Shown: shown[:1]}, 42, human)
	if tr := res.(proto.ModViewsResult).Views[0].Trees; len(tr) != 0 {
		t.Fatalf("stale trees after a restart: %+v", tr)
	}
	// The console shows nothing any more.
	s.modViews(proto.ModViewsParams{}, 42, human)
	if ev := next(t, sub); ev.Name != modbus.ViewClosed {
		t.Fatalf("closing: %s", ev.Name)
	}
	if _, perr := s.modViews(proto.ModViewsParams{}, 42, caller.Caller{Kind: caller.Thread}); perr == nil {
		t.Fatal("an agent polled the views")
	}
}

// TestModViewsNeedGrant: without the views grant a mod draws nothing.
func TestModViewsNeedGrant(t *testing.T) {
	s := panelServer(t, `"project_read"`)
	raw, _ := json.Marshal(map[string]any{"view": "queue", "widget": map[string]any{"kind": "text", "text": "x"}})
	if _, err := s.CallMod(context.Background(), "panel", "ui.view.set", raw); err == nil {
		t.Fatal("set without the grant")
	}
	res, _ := s.modViews(proto.ModViewsParams{}, 1, human)
	if v := res.(proto.ModViewsResult).Views; len(v) != 0 {
		t.Fatalf("views without the grant: %+v", v)
	}
}

const sectionManifest = `id = "panel"
name = "Panel"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
views = [{ id = "notes", title = "Notes", slot = "details.section" }, { id = "digest", title = "Digest", slot = "coordinator.section" }]
[permissions]
project_read = true
views = true
`

// TestModSections: a details.section is shown for the task or thread
// the details panel shows, view.opened names it and the mod publishes
// for it; a coordinator.section is for its project only, and neither is
// opened by the mod.
func TestModSections(t *testing.T) {
	s := manifestServer(t, sectionManifest, `"project_read", "views"`)
	sub := s.SubscribeMod("panel", []string{"project_read", "views"}, nil)
	next(t, sub) // mod.ready
	call := func(params any) error {
		raw, _ := json.Marshal(params)
		_, err := s.CallMod(context.Background(), "panel", "ui.view.set", raw)
		return err
	}
	tree := map[string]any{"kind": "text", "text": "2 notes"}
	for _, o := range []map[string]any{
		{"project": "demo", "task": "T12"},
		{"project": "demo", "thread": "t-0003"},
		{"project": "demo"},
	} {
		if err := call(map[string]any{"view": "notes", "widget": tree, "options": o}); err != nil {
			t.Fatalf("%v: %v", o, err)
		}
	}
	for _, bad := range []map[string]any{
		{"view": "notes", "options": map[string]any{"task": "T12"}},                                        // no project
		{"view": "notes", "options": map[string]any{"project": "demo", "task": "T12", "thread": "t-0003"}}, // both
		{"view": "notes", "options": map[string]any{"project": "demo", "task": "12"}},                      // not a ref
		{"view": "notes", "options": map[string]any{"project": "demo", "thread": "../x"}},
		{"view": "digest", "options": map[string]any{"project": "demo", "task": "T12"}}, // not a details.section
	} {
		bad["widget"] = tree
		if err := call(bad); err == nil {
			t.Fatalf("%v was set", bad)
		}
	}
	raw, _ := json.Marshal(map[string]any{"view": "notes"})
	if _, err := s.CallMod(context.Background(), "panel", "ui.view.open", raw); err == nil || err.(*modbroker.Error).Code != modbroker.CodeBadParams {
		t.Fatalf("a section opened: %v", err)
	}

	shown := []modview.Shown{
		{Mod: "panel", View: "notes", Slot: "details.section", Project: "demo", Task: "T12", Width: 40, Height: 20},
		{Mod: "panel", View: "digest", Slot: "coordinator.section", Project: "demo", Width: 30, Height: 20},
		{Mod: "panel", View: "digest", Slot: "coordinator.section", Project: "demo", Task: "T1"}, // dropped
	}
	res, perr := s.modViews(proto.ModViewsParams{Shown: shown}, 42, human)
	if perr != nil {
		t.Fatal(perr)
	}
	r := res.(proto.ModViewsResult)
	if len(r.Views) != 2 || len(r.Views[0].Trees) != 3 || r.Views[0].Trees[0].Task != "T12" || r.Views[0].Trees[1].Thread != "t-0003" {
		t.Fatalf("views %+v", r.Views)
	}
	got := map[string]modbus.View{}
	for range 2 {
		ev := next(t, sub)
		v := ev.Data.(modbus.View)
		if ev.Name != modbus.ViewOpened {
			t.Fatalf("event %s", ev.Name)
		}
		got[v.View] = v
	}
	if v := got["notes"]; v.Task != "T12" || v.Project != "demo" || v.Slot != "details.section" || v.Width != 40 {
		t.Fatalf("notes %+v", v)
	}
	if v := got["digest"]; v.Task != "" || v.Slot != "coordinator.section" {
		t.Fatalf("digest %+v", v)
	}
	if sub.Len() != 0 {
		t.Fatal("a section was opened for a task in the coordinator's panel")
	}
	// The details panel moves to a thread: the task's section closes,
	// the thread's opens.
	shown[0].Task, shown[0].Thread = "", "t-0003"
	s.modViews(proto.ModViewsParams{Shown: shown[:2]}, 42, human)
	ev := next(t, sub)
	if v := ev.Data.(modbus.View); ev.Name != modbus.ViewClosed || v.Task != "T12" {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
	ev = next(t, sub)
	if v := ev.Data.(modbus.View); ev.Name != modbus.ViewOpened || v.Thread != "t-0003" {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
}

const sidebarManifest = `id = "panel"
name = "Panel"
version = "0.1.0"
api = "1.7"
[provides]
main = "main.js"
views = [
  { id = "ticker", title = "Ticker", slot = "sidebar.section", description = "this project's PR polls" },
  { id = "ci", title = "ci status", slot = "sidebar.project.section", opens = "prs" },
  { id = "prs", title = "Open PRs", slot = "dashboard.card" },
]
[permissions]
project_read = true
views = true
`

// TestModSidebarSections: a sidebar section's height is capped by its
// slot, it takes a badge and no task, thread or count, it is never
// opened by the mod, mod.views names its description and companion,
// and a per-project section shows only for a project.
func TestModSidebarSections(t *testing.T) {
	s := manifestServer(t, sidebarManifest, `"project_read", "views"`)
	sub := s.SubscribeMod("panel", []string{"project_read", "views"}, nil)
	next(t, sub) // mod.ready
	call := func(params any) error {
		raw, _ := json.Marshal(params)
		_, err := s.CallMod(context.Background(), "panel", "ui.view.set", raw)
		return err
	}
	tree := map[string]any{"kind": "text", "text": "PRs 40s ago"}
	badge := []any{map[string]any{"text": "PR host ", "role": "bad"}, map[string]any{"icon": "blocked", "role": "bad"}}
	if err := call(map[string]any{"view": "ticker", "widget": tree, "options": map[string]any{"project": "demo", "height": 99, "badge": badge}}); err != nil {
		t.Fatal(err)
	}
	if err := call(map[string]any{"view": "ci", "widget": tree, "options": map[string]any{"project": "demo", "height": 99}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{
		{"view": "ticker", "options": map[string]any{"project": "demo", "count": 1}},
		{"view": "ci", "options": map[string]any{"project": "demo", "task": "T12"}},
		{"view": "ci", "options": map[string]any{"project": "demo", "thread": "t-0003"}},
		{"view": "prs", "options": map[string]any{"project": "demo", "badge": badge}},                           // not a sidebar section
		{"view": "ticker", "options": map[string]any{"badge": []any{map[string]any{"text": "thirteen cells"}}}}, // too wide
		{"view": "ticker", "options": map[string]any{"badge": []any{map[string]any{"text": "a"}, map[string]any{"text": "b"}, // too many
			map[string]any{"text": "c"}, map[string]any{"text": "d"}, map[string]any{"text": "e"}}}},
		{"view": "ticker", "options": map[string]any{"badge": []any{map[string]any{"icon": "bogus"}}}},
	} {
		bad["widget"] = tree
		if err := call(bad); err == nil {
			t.Errorf("%v was set", bad)
		}
	}
	raw, _ := json.Marshal(map[string]any{"view": "ticker"})
	if _, err := s.CallMod(context.Background(), "panel", "ui.view.open", raw); err == nil || err.(*modbroker.Error).Code != modbroker.CodeBadParams {
		t.Fatalf("a sidebar section opened: %v", err)
	}

	shown := []modview.Shown{
		{Mod: "panel", View: "ticker", Slot: "sidebar.section", Project: "demo", Width: 30, Height: 4},
		{Mod: "panel", View: "ci", Slot: "sidebar.project.section", Project: "demo", Width: 26, Height: 2},
		{Mod: "panel", View: "ci", Slot: "sidebar.project.section", Width: 26, Height: 2},                   // dropped: no project
		{Mod: "panel", View: "ci", Slot: "sidebar.project.section", Project: "other", Width: 26, Height: 2}, // dropped: not its project
		{Mod: "panel", View: "ci", Slot: "sidebar.project.section", Project: "../x", Width: 26, Height: 2},  // dropped: not a slug
	}
	res, perr := s.modViews(proto.ModViewsParams{Shown: shown}, 42, human)
	if perr != nil {
		t.Fatal(perr)
	}
	v := res.(proto.ModViewsResult).Views
	if len(v) != 3 || v[0].Description != "this project's PR polls" || v[1].Opens != "prs" || v[2].Opens != "" {
		t.Fatalf("views %+v", v)
	}
	tk, ci := v[0].Trees, v[1].Trees
	if len(tk) != 1 || tk[0].Height != modview.MaxSidebarHeight || len(tk[0].Badge) != 2 || tk[0].Badge[1].Icon != "blocked" {
		t.Fatalf("ticker %+v", tk)
	}
	if len(ci) != 1 || ci[0].Height != modview.MaxProjectSectionHeight || ci[0].Badge != nil {
		t.Fatalf("ci %+v", ci)
	}
	for range 2 {
		if ev := next(t, sub); ev.Name != modbus.ViewOpened || ev.Data.(modbus.View).Project != "demo" {
			t.Fatalf("event %s %+v", ev.Name, ev.Data)
		}
	}
	if sub.Len() != 0 {
		t.Fatal("a per-project section opened for no project, or another's")
	}
}
