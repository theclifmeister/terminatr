package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// fileDash is linkDash with a file offered to alpha with M-1, and one
// alpha sends with M-3 on its way.
func fileDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src, m := linkDash(t)
	src.data.Projects[0].Links[0].FilesWaiting = 1
	src.data.Projects[0].Links[0].FilesActive = 1
	src.data.Projects[0].Links[0].Held = 0
	src.data.Projects[0].countLinkWaits()
	m.setData(src.data)
	src.linkLog[0].File, src.linkLog[0].FileSize, src.linkLog[0].Transfer = "report.pdf", 4400000, "X-2"
	src.linkLog[1].State = link.StateSent
	src.linkLog[1].File, src.linkLog[1].FileSize, src.linkLog[1].Transfer = "trace.bin", 64<<20, "X-4"
	src.linkFiles = []link.Transfer{
		{ID: "X-2", Peer: "beta", Message: "M-1", Name: "report.pdf", Size: 4400000, State: link.FileOffered},
		{ID: "X-4", Out: true, Peer: "beta", Message: "M-3", Name: "trace.bin", Size: 64 << 20, Offset: 24 << 20, Rate: 1 << 20, State: link.FileSending},
	}
	return src, m
}

// TestMessagesTabFiles: the tab counts files that wait and move, NEEDS
// YOU says a file waits, and in the conversation the file is a line
// under its message: A accepts, X declines after y, C cancels one on
// its way after y.
func TestMessagesTabFiles(t *testing.T) {
	src, m := fileDash(t)
	if out := screen(m); !strings.Contains(out, "1 file waits") {
		t.Fatalf("no NEEDS YOU words:\n%s", out)
	}
	m.openTab("alpha", tabMessages, 0)
	if out := screen(m); !strings.Contains(out, "1 file waits for you · 1 on the way") {
		t.Fatalf("tab:\n%s", out)
	}
	press(m, "enter")
	keyPress(m, "up")
	out := screen(m)
	for _, want := range []string{"▸ report.pdf  4.2 MB  offered · waits for you", "▸ trace.bin  64.0 MB  sending 37% · 1.0 MB/s · 0:40 left", "A accept · X decline"} {
		if !strings.Contains(out, want) {
			t.Errorf("conversation lacks %q:\n%s", want, out)
		}
	}
	drain(t, m, press(m, "A"))
	if len(src.linkActs) != 1 || src.linkActs[0] != "alpha accept-file X-2" {
		t.Fatalf("accept %v", src.linkActs)
	}
	press(m, "X")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("decline didn't ask: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if len(src.linkActs) != 2 || src.linkActs[1] != "alpha decline-file X-2" {
		t.Fatalf("decline %v", src.linkActs)
	}
	keyPress(m, "down")
	if out := screen(m); !strings.Contains(out, "C cancel") || strings.Contains(out, "A accept") {
		t.Fatalf("keys on the sent file:\n%s", out)
	}
	press(m, "C")
	drain(t, m, press(m, "Y"))
	if len(src.linkActs) != 3 || src.linkActs[2] != "alpha cancel-file X-4" {
		t.Fatalf("cancel %v", src.linkActs)
	}
	// Received: enter reads it as the Library does.
	src.linkFiles[0].State = link.FileReceived
	src.libFiles = map[string]string{"M-1/report.pdf": "plain words"}
	m.top().(*convView).load(m)
	keyPress(m, "up")
	if out := screen(m); !strings.Contains(out, "received · sha256 ok") || !strings.Contains(out, "enter read") {
		t.Fatalf("received:\n%s", out)
	}
	keyPress(m, "enter")
	if out := screen(m); !strings.Contains(out, "report.pdf · 4.2 MB · from beta") || !strings.Contains(out, "plain words") {
		t.Fatalf("reader:\n%s", out)
	}
}

// TestLinkedProjectsFiles: F steps files ask › auto › off; auto asks
// first, the others save at once; the row shows it.
func TestLinkedProjectsFiles(t *testing.T) {
	src, m := linkDash(t)
	m.push(&linksView{slug: "alpha"})
	press(m, "F")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("auto didn't ask: %T", m.top())
	}
	if out := screen(m); !strings.Contains(out, "Take files from beta without asking? Up to 25 MB each") {
		t.Fatalf("confirm:\n%s", out)
	}
	drain(t, m, press(m, "Y"))
	if got := src.links[len(src.links)-1]; got != "alpha beta files auto" {
		t.Fatalf("links %v", src.links)
	}
	src.data.Projects[0].Links[0].Mine.Files = config.FilesAuto
	m.setData(src.data)
	if out := screen(m); !strings.Contains(out, "both ways, files auto") || !strings.Contains(out, "F files") {
		t.Fatalf("row:\n%s", out)
	}
	drain(t, m, press(m, "F"))
	if _, asked := m.top().(*confirmView); asked || src.links[len(src.links)-1] != "alpha beta files off" {
		t.Fatalf("off: %v (%T)", src.links, m.top())
	}
}

// TestLinkLimitsFiles: Link limits has the file size and pace; a size
// above the default asks first, a faster pace doesn't, and the pace
// stays within 64 to 2048 KiB/s.
func TestLinkLimitsFiles(t *testing.T) {
	src, m := popupData(t)
	feed(m, keyPress(m, "A"))
	pv := m.top().(*projectView)
	keyPress(m, "9")
	_ = pv
	sr := safetyRows{slug: "alpha"}
	rows := sr.linkLimitRows()
	var size, pace setting
	for _, r := range rows {
		switch r.label {
		case "File size":
			size = r
		case "File pace":
			pace = r
		}
	}
	if size.label == "" || pace.label == "" {
		t.Fatalf("rows %v", rows)
	}
	if size.value(m) != "25 MB" || pace.value(m) != "1024 KiB/s" {
		t.Fatalf("values %q %q", size.value(m), pace.value(m))
	}
	drain(t, m, size.change(m))
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("50 MB didn't ask: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	drain(t, m, pace.change(m))
	if _, ok := m.top().(*confirmView); ok {
		t.Fatal("a faster pace asked")
	}
	if got := strings.Join(src.settings, " "); !strings.Contains(got, "link_file_mb=50") || !strings.Contains(got, "link_file_kbps=2048") {
		t.Fatalf("settings %v", src.settings)
	}
}

// TestLibraryLinkGroup: files a linked project sent are a group of their
// own, "from beta · 2 files", read and deleted by their message.
func TestLibraryLinkGroup(t *testing.T) {
	src, m := popupData(t)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	src.library = []thread.LibFile{
		{Peer: "mini/api", Message: "M-4F2A-17", Title: "from mini/api", State: "received", Name: "report.pdf", Size: 4_200_000, Time: day(9)},
		{Thread: "t-0002", Task: "T2", Title: "Newer", State: "running", Name: "notes.md", Size: 1500, Time: day(8)},
		{Peer: "mini/api", Message: "M-4F2A-21", Title: "from mini/api", State: "received", Name: "trace.txt", Size: 12, Time: day(7)},
	}
	src.libFiles = map[string]string{"M-4F2A-17/report.pdf": "%PDF\x00", "M-4F2A-21/trace.txt": "line one\n"}
	feed(m, keyPress(m, "A"))
	keyPress(m, "8")
	out := screen(m)
	if !strings.Contains(out, "from mini/api · 2 files") {
		t.Fatalf("no group:\n%s", out)
	}
	if a, b, c := strings.Index(out, "report.pdf"), strings.Index(out, "trace.txt"), strings.Index(out, "notes.md"); !(a < b && b < c) {
		t.Fatalf("not grouped:\n%s", out)
	}
	keyPress(m, "enter")
	if out := screen(m); !strings.Contains(out, "report.pdf · 4.2 MB · from mini/api") || !strings.Contains(out, "not text") {
		t.Fatalf("binary:\n%s", out)
	}
	keyPress(m, "esc")
	keyPress(m, "down")
	keyPress(m, "enter")
	if out := screen(m); !strings.Contains(out, "line one") {
		t.Fatalf("text:\n%s", out)
	}
	keyPress(m, "esc")
	press(m, "X")
	if out := screen(m); !strings.Contains(out, "which mini/api sent with M-4F2A-21") {
		t.Fatalf("confirm:\n%s", out)
	}
	drain(t, m, press(m, "Y"))
	if len(src.libRemoved) != 1 || src.libRemoved[0] != "M-4F2A-21/trace.txt" {
		t.Fatalf("removed %v", src.libRemoved)
	}
}
