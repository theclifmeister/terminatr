package link

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

var human = caller.Caller{Kind: caller.Human}

// libFile writes a library file of thread id in p, n random bytes.
func libFile(t *testing.T, p *project.Project, id, name string, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	rand.Read(data)
	d := thread.Path(p, id, "library")
	os.MkdirAll(d, 0o755)
	if err := os.WriteFile(filepath.Join(d, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func transfer(t *testing.T, p *project.Project, id string) Transfer {
	t.Helper()
	for _, x := range Transfers(p, "") {
		if x.ID == id {
			return x
		}
	}
	t.Fatalf("%s has no transfer %s", p.Slug, id)
	return Transfer{}
}

func journal(p *project.Project) string {
	b, _ := os.ReadFile(p.Path("JOURNAL.md"))
	return string(b)
}

// TestFileLocal: a coordinator sends a library file with a message to a
// project on this server. The sender keeps a copy, the receiver's user
// is asked (not its coordinator), and accepting copies the copy, not
// the library file as it is now, beside the message there.
func TestFileLocal(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	data := libFile(t, a, "t-0001", "report.pdf", 100<<10)
	m, err := Send(ca, a, "b", Opts{Text: "the report", File: "t-0001/report.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if m.File != "report.pdf" || m.FileSize != 100<<10 || !ValidFileID(m.Transfer) || m.State != StateSent {
		t.Fatalf("message %+v", m)
	}
	// The library file changes after the send: the copy sent doesn't.
	libFile(t, a, "t-0001", "report.pdf", 10)
	out, in := transfer(t, a, m.Transfer), transfer(t, b, m.Transfer)
	if !out.Out || out.State != FileOffered || in.Out || in.State != FileOffered || in.Peer != "a" || in.Message != m.ID {
		t.Fatalf("out %+v in %+v", out, in)
	}
	if got, _ := os.ReadFile(filepath.Join(fileDir(a, "b", m.ID), "report.pdf")); !bytes.Equal(got, data) {
		t.Fatal("the sender's copy isn't what was sent")
	}
	asks := items(t, b, KindFile)
	if len(asks) != 1 || !asks[0].NeedsUser || asks[0].Message != m.Transfer || !strings.Contains(asks[0].Summary, "a offers a file with "+m.ID+" (report.pdf, 100.0 KB)") {
		t.Fatalf("items %+v", asks)
	}
	if !strings.Contains(Summary("a", m), "offers report.pdf, 100.0 KB") {
		t.Errorf("summary %q", Summary("a", m))
	}
	cb := coord
	cb.Project = "b"
	if _, err := AcceptFile(cb, b, m.Transfer); code(err) != "human-only" {
		t.Fatalf("coordinator accepted: %v", err)
	}
	if _, err := DeclineFile(cb, b, m.Transfer); code(err) != "human-only" {
		t.Fatalf("coordinator declined: %v", err)
	}
	got, err := AcceptFile(human, b, m.Transfer)
	if err != nil || got.State != FileReceived {
		t.Fatalf("accept %+v %v", got, err)
	}
	landed := filepath.Join(fileDir(b, "a", m.ID), "report.pdf")
	if got, _ := os.ReadFile(landed); !bytes.Equal(got, data) {
		t.Fatal("the landed file isn't what was sent")
	}
	if fi, _ := os.Stat(landed); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
	if transfer(t, a, m.Transfer).State != FileReceived {
		t.Error("the sender's copy isn't received")
	}
	if len(items(t, b, KindFile)) != 0 {
		t.Error("the ask is still open")
	}
	rm, _ := Read(b, m.ID)
	if p, err := FilePath(b, rm, "report.pdf"); err != nil || p != landed {
		t.Errorf("FilePath %q %v", p, err)
	}
	for _, w := range []string{"link.file-offered a " + m.Transfer, "link.file-accept a " + m.Transfer, "link.file-received a " + m.Transfer + " (100.0 KB"} {
		if !strings.Contains(journal(b), w) {
			t.Errorf("b's journal lacks %q:\n%s", w, journal(b))
		}
	}
	if strings.Contains(journal(b), "report.pdf") || strings.Contains(journal(a), "report.pdf") {
		t.Error("a journal names the file")
	}
	if _, err := AcceptFile(human, b, m.Transfer); code(err) != "not-offered" {
		t.Errorf("accepted twice: %v", err)
	}
}

// TestFileLocalSettings: files = auto takes it at once, with an item
// that needs no one; off refuses it at the send, on either side; a file
// over either side's size is refused.
func TestFileLocalSettings(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	libFile(t, a, "t-0001", "x.bin", 2<<20)
	if err := SetFiles(ca, "a", "b", config.FilesAuto); code(err) != "human-only" {
		t.Fatalf("coordinator set files: %v", err)
	}
	if err := SetFiles(human, "a", "b", config.FilesAuto); err != nil {
		t.Fatal(err)
	}
	cfg := mustConfig(t)
	if l, _ := cfg.Links("b"); l["a"].Files != config.FilesAuto {
		t.Fatalf("b's entry %+v: the pair writer sets both", l["a"])
	}
	m, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if in := transfer(t, b, m.Transfer); in.State != FileReceived || !in.Auto {
		t.Fatalf("in %+v", in)
	}
	got := items(t, b, KindFile)
	if len(got) != 1 || got[0].NeedsUser || !strings.Contains(got[0].Summary, "was received") {
		t.Fatalf("items %+v", got)
	}
	if !strings.Contains(journal(b), "link.file-accept a "+m.Transfer+" (auto)") {
		t.Errorf("journal:\n%s", journal(b))
	}
	SetFiles(human, "b", "a", config.FilesOff)
	if _, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.bin"}); code(err) != "files-off" {
		t.Fatalf("off: %v", err)
	}
	SetFiles(human, "a", "b", config.FilesAsk)
	os.WriteFile(filepath.Join(filepath.Dir(cfg.Path), "config.toml"), []byte("[projects.a.links]\nb = \"both\"\n\n[projects.b]\nlink_file_mb = 1\n\n[projects.b.links]\na = \"both\"\n"), 0o600)
	if _, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.bin"}); code(err) != "too-big" {
		t.Fatalf("too big: %v", err)
	}
}

// TestFileSources: a coordinator sends library files and its messages'
// files, never a path; the human may send any regular file. Links,
// hard links and names that aren't one are refused.
func TestFileSources(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	if _, err := Send(ca, a, "b", Opts{Text: "x", File: outside}); code(err) != "bad-attachment" {
		t.Errorf("coordinator path: %v", err)
	}
	m, err := Send(human, a, "b", Opts{Text: "x", File: outside})
	if err != nil || m.File != "secret.txt" {
		t.Fatalf("human path: %+v %v", m, err)
	}
	for _, bad := range []string{"t-0001/../../config.toml", "t-0001/.hidden", "nothing", "t-0009/none.txt", "M-999/x"} {
		if _, err := Send(ca, a, "b", Opts{Text: "x", File: bad}); code(err) != "bad-attachment" {
			t.Errorf("%s: %v", bad, err)
		}
	}
	d := thread.Path(a, "t-0001", "library")
	os.MkdirAll(d, 0o755)
	if os.Symlink(outside, filepath.Join(d, "link.txt")) == nil {
		if _, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/link.txt"}); code(err) != "bad-attachment" {
			t.Errorf("symlink: %v", err)
		}
	}
	if os.Link(outside, filepath.Join(d, "hard.txt")) == nil {
		if _, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/hard.txt"}); code(err) != "bad-attachment" {
			t.Errorf("hard link: %v", err)
		}
	}
	// b passes on the file it received, by its message.
	if _, err := AcceptFile(human, b, m.Transfer); err != nil {
		t.Fatal(err)
	}
	cb := coord
	cb.Project = "b"
	if _, err := Send(cb, b, "a", Opts{Text: "back", File: m.ID + "/secret.txt"}); err != nil {
		t.Fatalf("a message's file: %v", err)
	}
	// An offered file that isn't here yet can't be read or passed on.
	other := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(other, []byte("other"), 0o644)
	m2, err := Send(human, a, "b", Opts{Text: "x", File: other})
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := Read(b, m2.ID)
	if _, err := FilePath(b, r2, "secret.txt"); code(err) != "not-received" {
		t.Errorf("not received: %v", err)
	}
}

// TestFileHeldAndCancel: on an ask link the file waits with its message
// and goes when it is approved; a dropped message cancels it. Either
// side cancels an open transfer; the coordinator only its own.
func TestFileHeldAndCancel(t *testing.T) {
	a, b, _ := setup(t)
	ca := coord
	ca.Project = "a"
	libFile(t, a, "t-0001", "x.txt", 10)
	SetAsk(human, "a", "b", true)
	m, err := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.txt"})
	if err != nil || m.State != StateHeld {
		t.Fatalf("held %+v %v", m, err)
	}
	if out := transfer(t, a, m.Transfer); out.State != FileHeld {
		t.Fatalf("out %+v", out)
	}
	if h := items(t, a, KindHeld); len(h) != 1 || !strings.Contains(h[0].Summary, "with x.txt (10 bytes)") {
		t.Fatalf("held item %+v", h)
	}
	if _, err := Approve(human, a, m.ID, false); err != nil {
		t.Fatal(err)
	}
	if transfer(t, a, m.Transfer).State != FileOffered || transfer(t, b, m.Transfer).State != FileOffered {
		t.Fatal("not offered once approved")
	}
	cb := coord
	cb.Project = "b"
	if _, err := CancelFile(cb, b, m.Transfer); code(err) != "human-only" {
		t.Errorf("receiving coordinator cancelled: %v", err)
	}
	if _, err := CancelFile(ca, a, m.Transfer); err != nil {
		t.Fatal(err)
	}
	if transfer(t, b, m.Transfer).State != FileCancelled || len(items(t, b, KindFile)) != 0 {
		t.Fatal("b's copy isn't cancelled, or its ask is open")
	}
	m2, _ := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.txt"})
	if _, err := Drop(human, a, m2.ID, false); err != nil {
		t.Fatal(err)
	}
	if transfer(t, a, m2.Transfer).State != FileCancelled {
		t.Fatal("a dropped message's file isn't cancelled")
	}
	SetAsk(human, "a", "b", false)
	m3, _ := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.txt"})
	if _, err := DeclineFile(human, b, m3.Transfer); err != nil {
		t.Fatal(err)
	}
	if transfer(t, a, m3.Transfer).State != FileDeclined {
		t.Fatal("not declined on the sender's copy")
	}
}

// TestFileExpiry: an offer left unanswered expires on both sides after
// OfferExpiry, its item done.
func TestFileExpiry(t *testing.T) {
	a, b, clock := setup(t)
	ca := coord
	ca.Project = "a"
	libFile(t, a, "t-0001", "x.txt", 10)
	m, _ := Send(ca, a, "b", Opts{Text: "x", File: "t-0001/x.txt"})
	*clock = clock.Add(OfferExpiry - time.Hour)
	ExpireFiles()
	if transfer(t, b, m.Transfer).State != FileOffered {
		t.Fatal("expired early")
	}
	*clock = clock.Add(2 * time.Hour)
	ExpireFiles()
	if transfer(t, b, m.Transfer).State != FileExpired || transfer(t, a, m.Transfer).State != FileExpired || len(items(t, b, KindFile)) != 0 {
		t.Fatal("not expired")
	}
}

// TestFileStates: the words each state shows.
func TestFileStates(t *testing.T) {
	for _, tc := range []struct {
		t    Transfer
		want string
	}{
		{Transfer{State: FileReceiving, Size: 200, Offset: 76}, "receiving 38%"},
		{Transfer{State: FileFailed, Code: "stalled"}, "failed: stalled"},
		{Transfer{State: FileWaiting, Wait: "disk"}, "waiting: disk"},
		{Transfer{State: FileOffered}, "offered · waits for you"},
		{Transfer{State: FileOffered, Out: true}, "offered"},
		{Transfer{State: FileAccepted, Auto: true}, "accepted (auto)"},
	} {
		if got := FileState(tc.t); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.t, got, tc.want)
		}
	}
}
