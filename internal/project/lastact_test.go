package project

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeArchive(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := gzip.NewWriter(f)
	if _, err := z.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestLastHumanAct: what counts as the human's, and where it says they
// acted, read from journal lines as tm writes them.
func TestLastHumanAct(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		at    string // "" for none
		where string
	}{
		{name: "no lines"},
		{name: "only coordinator lines: none",
			lines: "2026-11-01T10:00:00Z coordinator task.add T1 x\n2026-11-01T10:01:00Z t-0001 thread.report t-0001 report 1\n"},
		{name: "making the project is not acting on it: none",
			lines: "2026-11-01T10:00:00Z human project.new web \n2026-11-01T10:01:00Z coordinator task.add T1 x\n"},
		{name: "the ticker, a mod and a paired server are not the human: none",
			lines: "2026-11-01T10:00:00Z ticker thread.resolve t-1 x\n2026-11-01T10:01:00Z mod:human x y z\n2026-11-01T10:02:00Z peer:human x y z\n"},
		{name: "a thread's words can't pass for the human's: none",
			lines: "2026-11-01T10:00:00Z t-0001 thread.report t-0001 2026-11-02T10:00:00Z human session.prompt x\n"},
		{name: "the terminal says no place",
			lines: "2026-11-01T10:00:00Z human thread.takeover t-1 x\n2026-11-01T10:01:00Z coordinator task.add T1 x\n",
			at:    "2026-11-01T10:00:00Z"},
		{name: "the app",
			lines: "2026-11-01T10:00:00Z human (app) session.prompt c-1 x\n",
			at:    "2026-11-01T10:00:00Z", where: "in the app"},
		{name: "the iPhone, a device name with spaces",
			lines: "2026-11-01T10:00:00Z human (iphone: iPhone (DUO) two) session.answer c-1 x\n",
			at:    "2026-11-01T10:00:00Z", where: "on the iPhone"},
		{name: "a paired server",
			lines: "2026-11-01T10:00:00Z human (peer: mini) questions.open c-1 x\n",
			at:    "2026-11-01T10:00:00Z", where: "through mini"},
		{name: "a paired server's iPhone",
			lines: "2026-11-01T10:00:00Z human (peer: mini › iphone: DUO) questions.open c-1 x\n",
			at:    "2026-11-01T10:00:00Z", where: "through mini"},
		{name: "the last one wins",
			lines: "2026-11-01T10:00:00Z human (app) session.prompt c-1 x\n2026-11-01T11:00:00Z human (iphone: DUO) session.prompt c-1 x\n",
			at:    "2026-11-01T11:00:00Z", where: "on the iPhone"},
		{name: "the heading and a broken line",
			lines: "# Journal\n\nnot a time human session.prompt\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Folder{Dir: t.TempDir()}
			writeFile(t, p.Path("JOURNAL.md"), tc.lines)
			at, where, ok := p.LastHumanAct()
			if ok != (tc.at != "") {
				t.Fatalf("ok %v at %v", ok, at)
			}
			if ok && (at.UTC().Format(time.RFC3339) != tc.at || where != tc.where) {
				t.Errorf("at %v where %q", at, where)
			}
		})
	}
}

// TestLastHumanActInTheArchive: a quiet project's last action may have left
// JOURNAL.md for a month's archive, and only the newest month that has one
// says it.
func TestLastHumanActInTheArchive(t *testing.T) {
	p := &Folder{Dir: t.TempDir()}
	if _, _, ok := p.LastHumanAct(); ok {
		t.Fatal("no journal at all has an action")
	}
	writeFile(t, p.Path("JOURNAL.md"), "# Journal\n\n2026-11-01T10:00:00Z coordinator task.add T1 x\n")
	writeArchive(t, p.Path("journal", "2026-07.md.gz"), "2026-07-02T10:00:00Z human (app) session.prompt c-1 x\n")
	writeArchive(t, p.Path("journal", "2026-08.md.gz"), "2026-08-02T10:00:00Z human session.prompt c-1 x\n2026-08-03T10:00:00Z human (iphone: DUO) session.prompt c-1 x\n")
	writeArchive(t, p.Path("journal", "2026-09.md.gz"), "2026-09-02T10:00:00Z ticker thread.resolve t-1 x\n")
	at, where, ok := p.LastHumanAct()
	if !ok || at.UTC().Format(time.DateOnly) != "2026-08-03" || where != "on the iPhone" {
		t.Fatalf("%v %q %v", at, where, ok)
	}
}

// TestLastHumanActFromJournal: Journal's own lines read back, for each way
// the human reaches it.
func TestLastHumanActFromJournal(t *testing.T) {
	for _, tc := range []struct {
		c     caller.Caller
		where string
	}{
		{caller.Caller{Kind: caller.Human}, ""},
		{caller.Caller{Kind: caller.Human, Via: caller.ViaApp}, "in the app"},
		{caller.ForDevice("iPhone DUO"), "on the iPhone"},
		{caller.ForPeerHuman("mini", ""), "through mini"},
		{caller.ForPeerHuman("mini", "DUO"), "through mini"},
	} {
		p := &Folder{Dir: t.TempDir()}
		if err := p.Journal(tc.c, "session.prompt", "c-1", "words"); err != nil {
			t.Fatal(err)
		}
		if _, where, ok := p.LastHumanAct(); !ok || where != tc.where {
			t.Errorf("%s: %q %v", tc.c, where, ok)
		}
	}
	p := &Folder{Dir: t.TempDir()}
	for _, c := range []caller.Caller{{Kind: caller.Coordinator}, {Kind: caller.Thread, Thread: "t-1"}, {Kind: caller.Ticker}, caller.ForMod("x"), caller.ForPeer("mini"), {Kind: caller.Director, Project: "p"}} {
		if err := p.Journal(c, "task.status", "T1", "human done"); err != nil {
			t.Fatal(err)
		}
	}
	if at, _, ok := p.LastHumanAct(); ok {
		t.Errorf("an agent's line is the human's, %v", at)
	}
}
