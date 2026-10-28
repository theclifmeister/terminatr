package server

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestProjectFiles: memory.list and memory.read read the project's
// memory files by their paths in it, no other; library.list lists a
// thread's report attachments and library.read reads one a chunk at a
// time; a path, a bad thread or name is refused.
func TestProjectFiles(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(pr.Path("CONTEXT.md"), []byte("# Context\nWhy.\n"), 0o644)
	os.MkdirAll(pr.Path("memory"), 0o755)
	os.WriteFile(pr.Path("memory", "build.md"), []byte("# Building\nmake.\n"), 0o644)
	th, err := thread.Create(pr, thread.Record{Title: "Report", Task: "T1", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(thread.Path(pr, th.ID, "library"), 0o755)
	big := make([]byte, proto.LibraryChunk+10)
	big[proto.LibraryChunk] = 'x'
	os.WriteFile(thread.Path(pr, th.ID, "library", "big.bin"), big, 0o644)

	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var mem proto.MemoryListResult
	if err := app.Call(proto.MethodMemoryList, proto.ProjectFilesParams{Project: "web"}, &mem); err != nil || len(mem.Files) < 2 ||
		mem.Files[0].Path != "CONTEXT.md" {
		t.Fatalf("memory.list: %+v %v", mem, err)
	}
	var text proto.MemoryReadResult
	if err := app.Call(proto.MethodMemoryRead, proto.MemoryReadParams{Project: "web", Path: "memory/build.md"}, &text); err != nil ||
		text.Text != "# Building\nmake.\n" {
		t.Fatalf("memory.read: %+v %v", text, err)
	}
	var lib proto.LibraryListResult
	if err := app.Call(proto.MethodLibraryList, proto.ProjectFilesParams{Project: "web"}, &lib); err != nil || len(lib.Files) != 1 ||
		lib.Files[0].Thread != th.ID || lib.Files[0].Task != "T1" || lib.Files[0].Name != "big.bin" || lib.Files[0].Size != int64(len(big)) {
		t.Fatalf("library.list: %+v %v", lib, err)
	}
	var chunk proto.LibraryReadResult
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: th.ID, Name: "big.bin"}, &chunk); err != nil ||
		len(chunk.Data) != proto.LibraryChunk || chunk.Size != int64(len(big)) || chunk.MTime.IsZero() || time.Since(chunk.MTime) > time.Hour {
		t.Fatalf("library.read: %d %d %v", len(chunk.Data), chunk.Size, err)
	}
	if err := app.Call(proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: th.ID, Name: "big.bin", Offset: proto.LibraryChunk}, &chunk); err != nil ||
		len(chunk.Data) != 10 || chunk.Data[0] != 'x' || chunk.Offset != proto.LibraryChunk {
		t.Fatalf("library.read's second chunk: %d %v", len(chunk.Data), err)
	}
	for _, c := range []struct {
		method string
		params any
		code   string
	}{
		{proto.MethodMemoryRead, proto.MemoryReadParams{Project: "web", Path: "TASKS.md"}, "not-memory"},
		{proto.MethodMemoryRead, proto.MemoryReadParams{Project: "web", Path: "../../config.toml"}, "not-memory"},
		{proto.MethodMemoryList, proto.ProjectFilesParams{Project: "nope"}, proto.ErrBadParams},
		{proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: th.ID, Name: "../thread.toml"}, "invalid-file"},
		{proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: "x", Name: "big.bin"}, "unknown-thread"},
		{proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: th.ID, Name: "gone.bin"}, "unknown-file"},
		{proto.MethodLibraryRead, proto.LibraryReadParams{Project: "web", ID: th.ID, Name: "big.bin", Offset: -1}, proto.ErrBadParams},
	} {
		var perr *proto.Error
		if err := app.Call(c.method, c.params, nil); !errors.As(err, &perr) || perr.Code != c.code {
			t.Errorf("%s %+v: %v, want %s", c.method, c.params, err, c.code)
		}
	}
}
