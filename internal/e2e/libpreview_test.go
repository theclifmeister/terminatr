package e2e

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// writePreviewPNG writes a two-colour PNG, 600 by 300 pixels.
func writePreviewPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 600, 300))
	for y := range 300 {
		for x := range 600 {
			c := color.RGBA{R: 0xd0, G: 0x40, B: 0x20, A: 0xff}
			if y >= 150 {
				c = color.RGBA{R: 0x20, G: 0x60, B: 0xd0, A: 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLibraryImagePreview: a real server's thumbnail in half-blocks: beside
// the list at 120 columns, on enter at 80, with a terminal that takes
// 24-bit colour, which this terminal answers the probe for, as one over SSH
// does without COLORTERM.
func TestLibraryImagePreview(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	slug, _ := newProject(env, "demo")
	t.Setenv("TERMINATR_HOME", env.Home)
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	done, err := thread.Create(p, thread.Record{Title: "Screenshots", State: thread.Resolved})
	if err != nil {
		t.Fatal(err)
	}
	lib := thread.Path(p, done.ID, "library")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	writePreviewPNG(t, filepath.Join(lib, "shot.png"))
	if err := os.WriteFile(filepath.Join(lib, "notes.md"), []byte("# The notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// shot.png first, the newest.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(lib, "notes.md"), old, old); err != nil {
		t.Fatal(err)
	}

	w := env.Window(120, 40)
	w.WaitFor("SESSIONS", wait)
	w.Type("A")
	w.WaitFor("1 overview", wait)
	w.Type("8")
	w.WaitFor("notes.md", wait)
	w.WaitFor("▀", wait)
	if sc := w.Screen(); !strings.Contains(sc, "shot.png ·") || !strings.Contains(sc, "o open") {
		t.Errorf("the column or its key:\n%s", sc)
	}
	// The terminal got 24-bit colours on the cells.
	if raw := string(w.Raw()); !strings.Contains(raw, "48;2;") || !strings.Contains(raw, "▀") {
		t.Errorf("no 24-bit cells reached the terminal")
	}
	w.Quit()
	w.WaitExit(wait)

	w = env.Window(80, 30)
	w.WaitFor("SESSIONS", wait)
	w.Type("A")
	w.WaitFor("1 overview", wait)
	w.Type("8")
	w.WaitFor("notes.md", wait)
	if sc := w.Screen(); strings.Contains(sc, "▀") {
		t.Errorf("a column at 80:\n%s", sc)
	}
	w.Key(Enter)
	w.WaitFor("▀", wait)
	if sc := w.Screen(); strings.Contains(sc, "not text") {
		t.Errorf("still not text:\n%s", sc)
	}
	w.Quit()
	w.WaitExit(wait)
}
