package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// TestBuiltinTasksTab: the built-in Tasks tab (the action-binding pilot,
// mods design §12 Phase 2) runs only once turned on, from its
// precompiled bundle: shown for a project, it draws the board grouped
// under head rows, each task's row binding tm's task actions and the
// started task's steps as its more; b shows the done tasks.
func TestBuiltinTasksTab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv(envModsPoll, "20ms")
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := "[mods.tasks-tab]\nenabled = true\n[mods.memory-tab]\nenabled = false\n[mods.pr-ci-column]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newWatchProject(t)
	if _, err := p.Tasks().Add(human, []tasks.NewTask{
		{Title: "Fix the login", Status: "started", Steps: []string{"Repro", "Fix"}},
		{Title: "Ship it", Status: "ready"},
		{Title: "Old one", Status: "done"},
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{opts: Options{Paths: paths, Bin: os.Args[0]}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	sup := s.newModHosts()
	s.modSup.Store(sup)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runModHosts(ctx, sup)
	}()
	defer func() {
		cancel()
		<-done
	}()

	shown := []modview.Shown{{Mod: mod.BundledTasks, View: mod.TasksView, Slot: mod.SlotProjectTab, Project: "demo", Width: 70, Height: 20}}
	tree := func(want string) *widget.Widget {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		var last string
		for {
			res, perr := s.modViews(proto.ModViewsParams{Shown: shown}, 42, human)
			if perr != nil {
				t.Fatal(perr)
			}
			for _, v := range res.(proto.ModViewsResult).Views {
				if v.Mod != mod.BundledTasks {
					continue
				}
				for _, tr := range v.Trees {
					if tr.Project == "demo" && tr.Tree != nil {
						b, _ := json.Marshal(tr.Tree)
						if last = string(b); strings.Contains(last, want) {
							return tr.Tree
						}
					}
				}
			}
			if time.Now().After(deadline) {
				logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", mod.BundledTasks+".log"))
				t.Fatalf("no tree with %q; last %s; hosts %+v; log:\n%s", want, last, s.ModHosts(), logText)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	board := tree(`"id":"T1"`)
	rows := board.Children[0].Rows
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, " "); got != "g:in-motion T1 g:on-deck T2 g:done" {
		t.Fatalf("rows %s", got)
	}
	if !rows[0].Head || !rows[4].Head || len(rows[1].More) != 2 {
		t.Errorf("heads or steps: %+v", rows)
	}
	if a := rows[3].Actions; len(a) != 2 || a[0].ID != "task.show" || a[1].ID != "task.delegate" || a[1].Target["task"] != "T2" {
		t.Errorf("T2 binds %+v", a)
	}

	in := proto.ModViewInputParams{Mod: mod.BundledTasks, View: mod.TasksView, Project: "demo", Kind: widget.EventKey, Key: "b", Widget: "tasks", Row: "T1"}
	if _, perr := s.modViewInput(in, 42, human); perr != nil {
		t.Fatal(perr)
	}
	tree(`"id":"T3"`)
}
