package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/link"
)

// openedFiles stubs the viewer for the test: each path it was given,
// and what the copy there held then.
func openedFiles(t *testing.T) *[][2]string {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	var got [][2]string
	old := openFile
	openFile = func(path string) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got = append(got, [2]string{path, string(b)})
		return nil
	}
	t.Cleanup(func() { openFile = old })
	return &got
}

// TestMessageOpenFile: o in a message's dialog opens its attachment, or
// with several a menu of them and the file it brought once received, in
// the viewer, from a copy of its own the user alone reads; the key bar
// offers it only with a file here. A type that could run doesn't open.
func TestMessageOpenFile(t *testing.T) {
	opened := openedFiles(t)
	src, m := linkDash(t)
	src.linkLog[0].Attachments = []string{"shot.png"}
	src.libFiles = map[string]string{"M-1/shot.png": "PNG bytes", "M-1/report.pdf": "PDF bytes", "M-1/run.sh": "rm -rf"}
	m.openTab("alpha", tabMessages, 0)
	// The held M-3 first: no file, no o.
	press(m, "enter")
	if out := screen(m); strings.Contains(out, "o open") {
		t.Fatalf("o offered without a file:\n%s", out)
	}
	press(m, "o")
	if !strings.Contains(m.msg, "no file here to open") || len(*opened) != 0 {
		t.Fatalf("o on M-3: %q %v", m.msg, *opened)
	}
	press(m, "esc")
	keyPress(m, "down")
	press(m, "enter")
	if out := screen(m); !strings.Contains(out, "o open it in your viewer") {
		t.Fatalf("no o in the key bar:\n%s", out)
	}
	drain(t, m, press(m, "o"))
	if len(*opened) != 1 || filepath.Base((*opened)[0][0]) != "shot.png" || (*opened)[0][1] != "PNG bytes" {
		t.Fatalf("opened %v", *opened)
	}
	path := (*opened)[0][0]
	if !strings.HasPrefix(filepath.Base(filepath.Dir(path)), openCopyPrefix) || !strings.HasPrefix(path, os.Getenv("TMPDIR")) {
		t.Errorf("the copy is at %s", path)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the copy's mode: %v %v", fi, err)
	}
	if !strings.Contains(m.msg, "opened shot.png in this computer's viewer") {
		t.Errorf("footer %q", m.msg)
	}

	// Its file received too: a menu of both, the file last.
	src.linkLog[0].File, src.linkLog[0].Transfer = "report.pdf", "X-2"
	src.linkFiles = []link.Transfer{{ID: "X-2", Peer: "beta", Message: "M-1", Name: "report.pdf", Size: 9, State: link.FileReceived}}
	m.top().(*msgView).load(m)
	if out := screen(m); !strings.Contains(out, "o open one in your viewer") {
		t.Fatalf("no o for several:\n%s", out)
	}
	press(m, "o")
	mv, ok := m.top().(*menuView)
	if !ok || len(mv.items) != 2 || mv.items[0].label != "shot.png" || mv.items[1].label != "report.pdf" {
		t.Fatalf("menu %T %+v", m.top(), m.top())
	}
	drain(t, m, mv.items[1].run(m))
	if len(*opened) != 2 || (*opened)[1][1] != "PDF bytes" {
		t.Fatalf("opened %v", *opened)
	}

	// A script never reaches the viewer.
	before := len(*opened)
	drain(t, m, m.openMsgFile("alpha", "beta", "M-1", "run.sh"))
	if len(*opened) != before || !strings.Contains(m.msg, "isn't a type tm opens") {
		t.Fatalf("run.sh: %q %v", m.msg, *opened)
	}
}

// TestMessageOpenFilePeer: in a paired server's popup a message is its
// header only: its files stay on that server, and o says so.
func TestMessageOpenFilePeer(t *testing.T) {
	opened := openedFiles(t)
	_, m := linkDash(t)
	v := &msgView{slug: "mini/api", other: "acme", alias: "mini", e: msgEntry{other: "acme", msg: &link.Message{ID: "M-9", Attachments: []string{"shot.png"}}}}
	m.push(v)
	press(m, "o")
	if !strings.Contains(m.msg, "stay on mini") || len(*opened) != 0 || strings.Contains(v.keys(m), "o open") {
		t.Fatalf("peer: %q %v %q", m.msg, *opened, v.keys(m))
	}
}

// TestWriteOpenCopy: a copy goes in a folder of its own under a plain
// name; a name with a path in it is refused, and copies a day old are
// swept.
func TestWriteOpenCopy(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	old := filepath.Join(tmp, openCopyPrefix+"old")
	if err := os.Mkdir(old, 0o700); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Add(-2 * openCopyKeep)
	os.Chtimes(old, day, day)
	a, err := writeOpenCopy("a.png", []byte("1"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := writeOpenCopy("a.png", []byte("2"))
	if err != nil || filepath.Dir(a) == filepath.Dir(b) {
		t.Fatalf("second copy %s %v", b, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the old copy stayed: %v", err)
	}
	for _, bad := range []string{"../x.png", "t-1/x.png", "..", ".", `a\b.png`} {
		if _, err := writeOpenCopy(bad, nil); err == nil {
			t.Errorf("wrote %q", bad)
		}
	}
}
