package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// halfBlock is what a drawn preview has and nothing else in the tab does.
const halfBlock = "▀"

// twoTone is a w by h image, red above and blue below.
func twoTone(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if y >= h/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, twoTone(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, twoTone(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// settle runs cmd and what a thumbnail's answer asks for next (any other
// answer's next is a timer that doesn't end).
func settle(m *dash, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			settle(m, c)
		}
	default:
		_, next := m.Update(msg)
		if _, ok := msg.(libThumbMsg); ok {
			settle(m, next)
		}
	}
}

// colourTerm sets what the console's terminal says of 24-bit colour.
func colourTerm(t *testing.T, env string, probed bool) {
	t.Helper()
	t.Setenv("COLORTERM", env)
	t.Setenv("NO_COLOR", "")
	old := probedTruecolor.Load()
	probedTruecolor.Store(probed)
	t.Cleanup(func() { probedTruecolor.Store(old) })
}

// libraryDash is the Library tab, cols wide, on a window of 40 rows: an
// image, a note, and another project's image.
func libraryDash(t *testing.T, cols int) (*fakeSource, *dash, *projectView) {
	t.Helper()
	src, m := popupData(t)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	src.library = []thread.LibFile{
		{Thread: "t-0002", Task: "T2", Title: "Newer", State: "running", Name: "shot.png", Size: 884_000, Time: day(9)},
		{Thread: "t-0002", Task: "T2", Title: "Newer", State: "running", Name: "notes.md", Size: 1500, Time: day(8)},
		{Peer: "mini/api", Message: "M-4F2A-17", Title: "from mini/api", State: "received", Name: "peer.jpg", Size: 2000, Time: day(7)},
	}
	src.libFiles = map[string]string{"t-0002/shot.png": "\x89PNG\x00original", "t-0002/notes.md": "# Notes\n", "M-4F2A-17/peer.jpg": "\xff\xd8\xff\x00peer"}
	src.thumbs = map[string][]byte{"t-0002/shot.png": testJPEG(t, 300, 190), "M-4F2A-17/peer.jpg": testJPEG(t, 100, 100)}
	m.Update(tea.WindowSizeMsg{Width: cols, Height: 40})
	settle(m, keyPress(m, "A"))
	pv := m.top().(*projectView)
	settle(m, keyPress(m, "8"))
	return src, m, pv
}

// TestPreviewCells: the shape is kept, the size is within the cells and
// never enlarged, each cell's halves carry the image's colours, and
// what isn't a thumbnail draws nothing.
func TestPreviewCells(t *testing.T) {
	p := newImagePreview(testJPEG(t, 400, 200))
	if p == nil {
		t.Fatal("no preview")
	}
	lines := p.lines(38, 18)
	if len(lines) != 10 {
		t.Errorf("rows %d, want 10", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 38 {
			t.Errorf("row %d is %d cells", i, w)
		}
		if !strings.HasSuffix(l, reset) {
			t.Errorf("row %d leaves its colour on", i)
		}
	}
	// Red above blue: the first row's foreground, the last's background.
	if !strings.Contains(lines[0], "38;2;2") && !strings.Contains(lines[0], "38;2;25") {
		t.Errorf("first row isn't red: %q", lines[0][:30])
	}
	if !strings.Contains(lines[9], ";48;2;0;0;2") && !strings.Contains(lines[9], ";48;2;1;") {
		t.Errorf("last row isn't blue: %q", lines[9][:30])
	}
	if &p.lines(38, 18)[0] != &lines[0] {
		t.Error("a frame scaled it again")
	}
	if l := newImagePreview(testJPEG(t, 10, 10)).lines(38, 18); len(l) != 5 || ansi.StringWidth(l[0]) != 10 {
		t.Errorf("small image enlarged: %d rows of %d", len(l), ansi.StringWidth(l[0]))
	}
	if l := newImagePreview(testJPEG(t, 100, 400)).lines(38, 18); len(l) != 18 || ansi.StringWidth(l[0]) != 9 {
		t.Errorf("tall image: %d rows of %d", len(l), ansi.StringWidth(l[0]))
	}
	if l := newImagePreview(testJPEG(t, 400, 1)).lines(38, 18); len(l) != 1 || ansi.StringWidth(l[0]) != 38 {
		t.Errorf("thin image: %d rows", len(l))
	}
	for name, data := range map[string][]byte{"none": nil, "not a JPEG": []byte("hello"), "a PNG": testPNG(t, 10, 10), "too big": testJPEG(t, 401, 10)} {
		if newImagePreview(data) != nil {
			t.Errorf("%s made a preview", name)
		}
	}
}

// TestLibraryPreview120: at 120 columns the selected image draws beside
// the list, asked for once; a file without one shows its name and size
// there, and a click on the column selects nothing.
func TestLibraryPreview120(t *testing.T) {
	colourTerm(t, "truecolor", false)
	src, m, pv := libraryDash(t, 120)
	out := screen(m)
	if !strings.Contains(out, halfBlock) {
		t.Fatalf("no preview:\n%s", out)
	}
	var first string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "shot.png · 884.0 KB") {
			first = l
		}
	}
	if !strings.Contains(first, "shot.png · 884.0 KB") {
		t.Errorf("no caption:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "notes.md") && !strings.Contains(l, halfBlock) {
			t.Errorf("the column isn't beside the list: %q", l)
		}
	}
	if got := strings.Join(src.thumbAsked, ","); got != "t-0002/shot.png" {
		t.Errorf("asked %q", got)
	}
	// The same file again costs no call.
	settle(m, keyPress(m, "down"))
	settle(m, keyPress(m, "up"))
	if len(src.thumbAsked) != 1 {
		t.Errorf("asked again: %v", src.thumbAsked)
	}
	// A note: its name and size, no cells, no call.
	settle(m, keyPress(m, "down"))
	out = screen(m)
	if strings.Contains(out, halfBlock) || !strings.Contains(out, "notes.md · 1.5 KB") {
		t.Errorf("note's column:\n%s", out)
	}
	if len(src.thumbAsked) != 1 {
		t.Errorf("a note was asked for: %v", src.thumbAsked)
	}
	// Another project's image is asked for by its message.
	settle(m, keyPress(m, "down"))
	if got := src.thumbAsked[len(src.thumbAsked)-1]; got != "M-4F2A-17/peer.jpg" {
		t.Errorf("asked %q", got)
	}
	if !strings.Contains(screen(m), halfBlock) {
		t.Errorf("peer's image:\n%s", screen(m))
	}
	w := m.inner(viewWidth)
	before := pv.sel[tabLibrary]
	pv.click(m, 0, w-1, false)
	if pv.sel[tabLibrary] != before {
		t.Errorf("a click on the column selected row %d", pv.sel[tabLibrary])
	}
}

// TestLibraryPreviewBusy: an arrow held down asks for one at a time and
// then for the file it stopped on.
func TestLibraryPreviewBusy(t *testing.T) {
	colourTerm(t, "truecolor", false)
	src, m := popupData(t)
	day := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, n := range []string{"a.png", "b.png", "c.png"} {
		src.library = append(src.library, thread.LibFile{Thread: "t-0002", Task: "T2", Title: "T", Name: n, Size: 9, Time: day})
	}
	src.thumbs = map[string][]byte{"t-0002/a.png": testJPEG(t, 8, 8), "t-0002/c.png": testJPEG(t, 8, 8)}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	settle(m, keyPress(m, "A"))
	cmd := keyPress(m, "8") // a.png asked for, its answer not here yet
	keyPress(m, "down")
	keyPress(m, "down")
	if len(src.thumbAsked) != 0 {
		t.Fatalf("asked before the first answer ran: %v", src.thumbAsked)
	}
	settle(m, cmd)
	if got := strings.Join(src.thumbAsked, ","); got != "t-0002/a.png,t-0002/c.png" {
		t.Errorf("asked %q", got)
	}
	if !strings.Contains(screen(m), halfBlock) {
		t.Errorf("c.png doesn't draw:\n%s", screen(m))
	}
}

// TestLibraryPreview80: at 80 columns the list keeps the width and makes
// no call; enter draws the image, o opens the file, esc comes back.
func TestLibraryPreview80(t *testing.T) {
	colourTerm(t, "truecolor", false)
	opened := openedFiles(t)
	src, m, pv := libraryDash(t, 80)
	if out := screen(m); strings.Contains(out, halfBlock) || strings.Contains(out, "shot.png · ") {
		t.Fatalf("a column at 80:\n%s", out)
	}
	if len(src.thumbAsked) != 0 {
		t.Errorf("asked %v", src.thumbAsked)
	}
	settle(m, keyPress(m, "enter"))
	out := screen(m)
	if !strings.Contains(out, halfBlock) || !strings.Contains(out, "shot.png · 884.0 KB") || !strings.Contains(out, "o open") {
		t.Fatalf("viewer:\n%s", out)
	}
	if strings.Contains(out, "not text") {
		t.Errorf("still says not text:\n%s", out)
	}
	settle(m, keyPress(m, "o"))
	if len(*opened) != 1 || (*opened)[0][1] != "\x89PNG\x00original" {
		t.Errorf("opened %v", *opened)
	}
	settle(m, keyPress(m, "esc"))
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("esc: %T", m.top())
	}
	// o from the list; on a file that runs in a browser it says why not.
	settle(m, keyPress(m, "o"))
	if len(*opened) != 2 {
		t.Errorf("o in the list opened %d", len(*opened))
	}
	pv.library = append(pv.library, thread.LibFile{Thread: "t-0002", Name: "design.html", Size: 9, Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	pv.sel[tabLibrary] = 2
	if f, _ := pv.selLib(); f.Name != "design.html" {
		t.Fatalf("selected %s", f.Name)
	}
	settle(m, keyPress(m, "o"))
	if len(*opened) != 2 || !strings.Contains(m.msg, "isn't a type tm opens") {
		t.Errorf("a page: opened %d, %q", len(*opened), m.msg)
	}
}

// TestLibraryNoTruecolor: without 24-bit colour no column, no call: the
// name and size as before; o still opens.
func TestLibraryNoTruecolor(t *testing.T) {
	colourTerm(t, "", false)
	opened := openedFiles(t)
	src, m, _ := libraryDash(t, 120)
	if out := screen(m); strings.Contains(out, halfBlock) || strings.Contains(out, "shot.png · ") {
		t.Fatalf("a column:\n%s", out)
	}
	settle(m, keyPress(m, "enter"))
	out := screen(m)
	if strings.Contains(out, halfBlock) || !strings.Contains(out, "not text: only its name and size show") {
		t.Fatalf("viewer:\n%s", out)
	}
	if len(src.thumbAsked) != 0 {
		t.Errorf("asked %v", src.thumbAsked)
	}
	settle(m, keyPress(m, "o"))
	if len(*opened) != 1 {
		t.Errorf("opened %v", *opened)
	}
}

// TestLibraryNoColor: NO_COLOR says no colours, whatever the terminal takes.
func TestLibraryNoColor(t *testing.T) {
	colourTerm(t, "truecolor", false)
	t.Setenv("NO_COLOR", "1")
	src, m, _ := libraryDash(t, 120)
	if strings.Contains(screen(m), halfBlock) || len(src.thumbAsked) != 0 {
		t.Errorf("drew under NO_COLOR, asked %v:\n%s", src.thumbAsked, screen(m))
	}
}

// TestLibraryOverSSH: SSH drops COLORTERM; a terminal that answered the
// probe draws all the same, the bytes coming from the server.
func TestLibraryOverSSH(t *testing.T) {
	colourTerm(t, "", true)
	src, m, _ := libraryDash(t, 120)
	if !strings.Contains(screen(m), halfBlock) || len(src.thumbAsked) != 1 {
		t.Errorf("asked %v:\n%s", src.thumbAsked, screen(m))
	}
}

// TestLibraryOlderServer: a server that doesn't have the method, or a
// file that makes none, keeps the name and size; asked once, and enter
// reads what the file is: a text file called .png shows as text.
func TestLibraryOlderServer(t *testing.T) {
	colourTerm(t, "truecolor", false)
	src, m := popupData(t)
	src.library = []thread.LibFile{{Thread: "t-0002", Task: "T2", Title: "T", Name: "fake.png", Size: 6, Time: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}}
	src.libFiles = map[string]string{"t-0002/fake.png": "plain\n"}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	settle(m, keyPress(m, "A"))
	settle(m, keyPress(m, "8"))
	for range 3 {
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	}
	if len(src.thumbAsked) != 1 {
		t.Errorf("asked %v", src.thumbAsked)
	}
	if out := screen(m); strings.Contains(out, halfBlock) || !strings.Contains(out, "fake.png · 6 B") {
		t.Errorf("tab:\n%s", out)
	}
	settle(m, keyPress(m, "enter"))
	if out := screen(m); !strings.Contains(out, "plain") || strings.Contains(out, "not text") {
		t.Errorf("a text file called .png:\n%s", out)
	}
	if len(src.thumbAsked) != 1 {
		t.Errorf("enter asked again: %v", src.thumbAsked)
	}
}

// TestLibraryNoImages: no images, no column and no call, at any width.
func TestLibraryNoImages(t *testing.T) {
	colourTerm(t, "truecolor", false)
	src, m := popupData(t)
	src.library = []thread.LibFile{{Thread: "t-0002", Task: "T2", Title: "T", Name: "notes.md", Size: 1500, Time: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}}
	src.libFiles = map[string]string{"t-0002/notes.md": "# Notes\n"}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	settle(m, keyPress(m, "A"))
	settle(m, keyPress(m, "8"))
	out := screen(m)
	if strings.Contains(out, "notes.md · ") || len(src.thumbAsked) != 0 {
		t.Errorf("asked %v:\n%s", src.thumbAsked, out)
	}
	settle(m, keyPress(m, "enter"))
	if out := screen(m); !strings.Contains(out, "# Notes") {
		t.Errorf("a non-image's text:\n%s", out)
	}
}

// TestFilesViewerImage: an image of the session's folder draws in the
// viewer from its bytes, read with the view's repo and ignored; plain
// folder and several repos alike. Without 24-bit colour, or from a file
// that makes none, it says what it said.
func TestFilesViewerImage(t *testing.T) {
	colourTerm(t, "truecolor", false)
	f := &fakeFiles{texts: map[string]proto.FilesReadResult{}}
	f.install(t)
	pic := testPNG(t, 60, 30)
	f.texts["pic.png"] = proto.FilesReadResult{Name: "pic.png", Path: "pic.png", Size: int64(len(pic)), Binary: true, Data: pic}
	for _, repo := range []string{"", "web"} { // a plain folder or one repo; one of several
		f.read = nil
		e := proto.FileEntry{Name: "pic.png", Path: "pic.png"}
		tv := worktreeViewer(tmclient.Paths{}, "s-4", repo, e, false)
		if tv.image == nil || tv.note != "" || tv.body != "" || tv.open == nil {
			t.Fatalf("repo %q: viewer %+v", repo, tv)
		}
		last := f.read[len(f.read)-1]
		if !last.Binary || last.Repo != repo || last.Session != "s-4" {
			t.Errorf("read %+v", last)
		}
	}
	tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "pic.png", Path: "pic.png", Ignored: true}, true)
	if tv.image == nil || !strings.Contains(tv.title, "git-ignored: may hold secrets") || !f.read[len(f.read)-1].Ignored {
		t.Errorf("an ignored image: %+v, %+v", tv, f.read[len(f.read)-1])
	}
	// A corrupt image, and a server too old to send one, say not text.
	bad := []byte("\x89PNG\x00not an image")
	f.texts["bad.png"] = proto.FilesReadResult{Name: "bad.png", Path: "bad.png", Size: int64(len(bad)), Binary: true, Data: bad}
	f.texts["old.png"] = proto.FilesReadResult{Name: "old.png", Path: "old.png", Size: 9, Binary: true}
	for _, n := range []string{"bad.png", "old.png"} {
		tv := worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: n, Path: n}, false)
		if tv.image != nil || !strings.Contains(tv.note, "not text") {
			t.Errorf("%s: %+v", n, tv)
		}
	}
	if _, _, err := worktreeBytes(tmclient.Paths{}, "s-4", "", "old.png", false); err == nil {
		t.Error("an older server's nothing opened as an empty file")
	}
	// A binary that isn't an image: as today, and o isn't offered.
	f.texts["a.bin"] = proto.FilesReadResult{Name: "a.bin", Path: "a.bin", Size: 9, Binary: true}
	f.read = nil
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "a.bin", Path: "a.bin"}, false)
	if tv.image != nil || tv.open != nil || !strings.Contains(tv.note, "not text") || len(f.read) != 1 {
		t.Errorf("a.bin: %+v, read %d", tv, len(f.read))
	}
	// Text with an image's name reads as text.
	f.texts["t.png"] = proto.FilesReadResult{Name: "t.png", Path: "t.png", Size: 5, Data: []byte("words")}
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "t.png", Path: "t.png"}, false)
	if tv.body != "words" || tv.image != nil {
		t.Errorf("t.png: %+v", tv)
	}
	// No truecolor: no bytes read, the words of today.
	colourTerm(t, "", false)
	f.read = nil
	tv = worktreeViewer(tmclient.Paths{}, "s-4", "", proto.FileEntry{Name: "pic.png", Path: "pic.png"}, false)
	if tv.image != nil || !strings.Contains(tv.note, "not text") || tv.open == nil || len(f.read) != 1 || f.read[0].Binary {
		t.Errorf("no truecolor: %+v, read %+v", tv, f.read)
	}
}

// TestAttachedViewerImage: the Files view's Attached tab draws the
// server's thumbnail; a refusal reads the file as before.
func TestAttachedViewerImage(t *testing.T) {
	colourTerm(t, "truecolor", false)
	f := &fakeFiles{texts: map[string]proto.FilesReadResult{}}
	f.install(t)
	old := filesLibRead
	filesLibRead = func(string, string, string) ([]byte, bool, error) { return []byte("\x89PNG\x00"), false, nil }
	t.Cleanup(func() { filesLibRead = old })
	f.thumbs = map[string][]byte{"t-0002/a.png": testJPEG(t, 40, 20)}
	tv := attachedViewer(tmclient.Paths{}, "alpha", thread.LibFile{Thread: "t-0002", Name: "a.png", Size: 9})
	if tv.image == nil || tv.note != "" {
		t.Errorf("attached %+v", tv)
	}
	tv = attachedViewer(tmclient.Paths{}, "alpha", thread.LibFile{Thread: "t-0002", Name: "b.png", Size: 9})
	if tv.image != nil || !strings.Contains(tv.note, "not text") {
		t.Errorf("refused %+v", tv)
	}
}

// TestRemoteHasNoLibrary: a paired server's project has no Library tab,
// so nothing here draws one of its images: its files stay there.
func TestRemoteHasNoLibrary(t *testing.T) {
	for _, n := range rtabNames {
		if n == "library" {
			t.Error("the remote popup has a Library tab: previews have to ask peer.library-thumbnail")
		}
	}
}
