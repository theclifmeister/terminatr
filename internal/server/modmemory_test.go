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

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// TestBuiltinMemoryTab: the built-in Memory tab (mods design §11, proof
// 2) runs from its precompiled bundle in a supervised host: shown for a
// project, it lists the memory files; enter on one reads it, headings in
// the head role and a wide glyph as "?"; n goes to the next file and
// enter back to the list. A link in memory/ is not listed.
func TestBuiltinMemoryTab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv(envModsPoll, "20ms")
	paths, err := client.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	p := newWatchProject(t)
	secret := filepath.Join(home, "secret.md")
	for name, text := range map[string]string{
		"CONTEXT.md":          "# Context\n\nThe plan, see [the doc](docs/plan.md).\n\n## Where things stand\n\nShipping 🚀 soon.\n",
		"MEMORY.md":           "# Memory\n\n- [Decisions](memory/decisions.md): host choices\n",
		"memory/decisions.md": "# Decisions\n\nGo for the server.\n",
		"memory/zeta.md":      "no heading\n",
	} {
		if err := os.WriteFile(p.Path(filepath.FromSlash(name)), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(secret, []byte("# Secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, p.Path("memory", "linked.md")); err != nil {
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

	shown := []modview.Shown{{Mod: mod.BundledMemory, View: mod.MemoryView, Slot: mod.SlotProjectTab, Project: "demo", Width: 70, Height: 20}}
	// tree polls as a console showing the tab until the tree has want.
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
				if v.Mod != mod.BundledMemory {
					continue
				}
				if v.Title != "memory" || v.Slot != mod.SlotProjectTab {
					t.Fatalf("view %+v", v)
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
				logText, _ := os.ReadFile(filepath.Join(home, "logs", "mods", mod.BundledMemory+".log"))
				t.Fatalf("no tree with %q; last %s; hosts %+v; log:\n%s", want, last, s.ModHosts(), logText)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	key := func(k, row string) {
		t.Helper()
		in := proto.ModViewInputParams{Mod: mod.BundledMemory, View: mod.MemoryView, Project: "demo", Kind: widget.EventKey, Key: k}
		if row != "" {
			in.Widget, in.Row = "files", row
		}
		if _, perr := s.modViewInput(in, 42, human); perr != nil {
			t.Fatal(perr)
		}
	}

	list := tree(`"id":"files"`)
	files := list.Children[0]
	var ids []string
	for _, r := range files.Rows {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, " "); got != "CONTEXT.md MEMORY.md memory/decisions.md memory/zeta.md" {
		t.Fatalf("files %s", got)
	}
	if r := files.Rows[2].Spans; r[0].Text != "Decisions" || r[1].Role != "faint" {
		t.Errorf("a note's row %+v", r)
	}

	key("enter", "CONTEXT.md")
	read := tree("Where things stand")
	b, _ := json.Marshal(read)
	for _, want := range []string{`"text":"Context"`, `"text":"Where things stand","role":"head"`, "The plan, see the doc.", "Shipping ? soon."} {
		if !strings.Contains(string(b), want) {
			t.Errorf("reader lacks %s: %s", want, b)
		}
	}
	for _, not := range []string{"docs/plan.md", "# Context", "🚀"} {
		if strings.Contains(string(b), not) {
			t.Errorf("reader shows %s: %s", not, b)
		}
	}

	for _, want := range []string{"  MEMORY.md", "memory/decisions.md", "memory/zeta.md", "  CONTEXT.md"} { // round to the first
		key("n", "")
		tree(want)
	}
	key("p", "")
	tree("memory/zeta.md")
	key("enter", "")
	tree(`"id":"files"`)
}
