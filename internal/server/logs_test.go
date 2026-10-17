package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestLogs: log.list names the server's, the service's, the consoles'
// and each mod's log; log.read reads one by its id, never a path, and
// both are the human's.
func TestLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	logs := filepath.Join(home, "logs")
	os.MkdirAll(filepath.Join(logs, "mods"), 0o700)
	os.WriteFile(filepath.Join(logs, "server.log"), []byte("a\nb\nc\n"), 0o600)
	os.WriteFile(filepath.Join(logs, "mods", "linear.log"), []byte("hi\n"), 0o600)
	os.WriteFile(filepath.Join(logs, "mods", "linear.log.1"), []byte("old\n"), 0o600)
	os.WriteFile(filepath.Join(logs, "mods", "Bad Id.log"), []byte("x\n"), 0o600)
	s := &Server{opts: Options{Paths: client.Paths{Log: filepath.Join(logs, "server.log")}}}
	human := caller.Caller{Kind: caller.Human}

	res, perr := s.logList(human)
	if perr != nil {
		t.Fatal(perr)
	}
	var ids []string
	for _, l := range res.(proto.LogListResult).Logs {
		ids = append(ids, l.ID)
		if l.ID == proto.LogConsole && !l.Missing {
			t.Error("console.log is there")
		}
	}
	if want := "server console mod:linear"; strings.Join(ids, " ") != want {
		t.Fatalf("logs %q, want %q", strings.Join(ids, " "), want)
	}
	os.WriteFile(filepath.Join(logs, "service.log"), []byte("launchd\n"), 0o600)
	res, _ = s.logList(human)
	if l := res.(proto.LogListResult).Logs; len(l) != 4 || l[1].ID != proto.LogService || l[1].Size != 8 {
		t.Fatalf("with service.log: %+v", l)
	}

	r, perr := s.logRead(proto.LogReadParams{ID: proto.LogServer, Max: 4}, human)
	if got := r.(proto.LogReadResult); perr != nil || got.Text != "c\n" || got.Start != 4 || got.Size != 6 {
		t.Fatalf("tail: %+v %v", r, perr)
	}
	after := int64(2)
	r, _ = s.logRead(proto.LogReadParams{ID: proto.LogServer, After: &after}, human)
	if got := r.(proto.LogReadResult); got.Text != "b\nc\n" || got.End != 6 {
		t.Fatalf("after: %+v", got)
	}
	r, _ = s.logRead(proto.LogReadParams{ID: "mod:linear"}, human)
	if got := r.(proto.LogReadResult); got.Text != "hi\n" {
		t.Fatalf("mod: %+v", got)
	}
	for _, id := range []string{"mod:../server", "../server.log", "/etc/passwd", "mod:linear.log.1", "mod:Bad Id"} {
		if _, perr := s.logRead(proto.LogReadParams{ID: id}, human); perr == nil || perr.Code != proto.ErrBadParams {
			t.Errorf("read %q: %v", id, perr)
		}
	}
	for _, c := range []caller.Caller{{Kind: caller.Coordinator, Project: "p"}, {Kind: caller.Thread, Project: "p", Thread: "t-0001"}} {
		if _, perr := s.logList(c); perr == nil || perr.Code != "human-only" {
			t.Errorf("%v listed the logs: %v", c.Kind, perr)
		}
		if _, perr := s.logRead(proto.LogReadParams{ID: proto.LogServer}, c); perr == nil || perr.Code != "human-only" {
			t.Errorf("%v read the log: %v", c.Kind, perr)
		}
	}
}
