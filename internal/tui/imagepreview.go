package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// Half-block previews (docs/SPEC.md §4, Library tab): an image's thumbnail
// drawn in coloured ▀ cells, one cell two square pixels. You can't read
// the text in it but you can tell which screenshot it is. It takes 24-bit
// colour: any other terminal keeps the name and size, and no thumbnail is
// asked for. The Library's come from the server (library.thumbnail), so
// any refusal, an older server's too, is the same name and size.

// previewable says this console can draw a preview.
func previewable() bool { return trueColor() && os.Getenv("NO_COLOR") == "" }

// imagePreview is a decoded thumbnail and its cells at the last size
// asked for: a frame draws them without scaling again.
type imagePreview struct {
	img        image.Image
	cols, rows int
	cells      []string
}

// newImagePreview decodes a thumbnail, nil for bytes that aren't one. The
// bytes are the server's, made from a file a thread wrote: bounded, and a
// decoder's panic is one image without a preview.
func newImagePreview(data []byte) (p *imagePreview) {
	defer func() {
		if recover() != nil {
			p = nil
		}
	}()
	if len(data) == 0 || len(data) > proto.MaxThumbnail {
		return nil
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > proto.ThumbnailSide || cfg.Height > proto.ThumbnailSide {
		return nil
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return &imagePreview{img: img}
}

// lines are the image's cells in at most cols by rows, its shape kept and
// never enlarged. A cell is about twice as high as it is wide, so ▀'s two
// halves are about square.
func (p *imagePreview) lines(cols, rows int) []string {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	if p.cells != nil && p.cols == cols && p.rows == rows {
		return p.cells
	}
	b := p.img.Bounds()
	w, h := previewSize(b.Dx(), b.Dy(), cols, rows)
	px := make([]color.RGBA, w*h)
	for y := range h {
		y0, y1 := span(y, h, b.Dy())
		for x := range w {
			x0, x1 := span(x, w, b.Dx())
			px[y*w+x] = average(p.img, b.Min.X+x0, b.Min.X+x1, b.Min.Y+y0, b.Min.Y+y1)
		}
	}
	out := make([]string, 0, h/2)
	for y := 0; y < h; y += 2 {
		out = append(out, pixelRow(px[y*w:(y+1)*w], px[(y+1)*w:(y+2)*w]))
	}
	p.cols, p.rows, p.cells = cols, rows, out
	return out
}

// previewSize is w by h pixels fitted to cols by rows cells, shrunk only;
// the height is even, two pixels a cell.
func previewSize(w, h, cols, rows int) (int, int) {
	high := rows * 2
	if w > cols {
		w, h = cols, h*cols/w
	}
	if h > high {
		w, h = w*high/h, high
	}
	h = max(h, 2)
	return max(w, 1), min(h+h%2, high)
}

// span is the source pixels destination pixel i of n covers, of size
// source pixels: at least one.
func span(i, n, size int) (int, int) {
	from := i * size / n
	return from, max((i+1)*size/n, from+1)
}

// average is the mean colour of the rectangle.
func average(img image.Image, x0, x1, y0, y1 int) color.RGBA {
	var r, g, b, n uint32
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pr, pg, pb, _ := img.At(x, y).RGBA()
			r, g, b, n = r+pr>>8, g+pg>>8, b+pb>>8, n+1
		}
	}
	return color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 0xff}
}

// The Library tab's preview column: libPreviewWide is the narrowest list
// and column that share the popup, libPreviewRows the fewest rows worth
// drawing, libPreviewGap the space between them.
const (
	libPreviewWide = 96
	libPreviewRows = 6
	libPreviewGap  = 2
	// libThumbsKept is how many thumbnails a popup keeps in memory.
	libThumbsKept = 32
)

// libTabKeys are the Library tab's keys when it has files.
const libTabKeys = "enter read · o open · X delete · D delete the thread's · ← → tabs · esc close"

// libHeadMost is the most lines the popup keeps above the tab's body: the
// tab bar and a blank, the kinds, the filter and a blank.
const libHeadMost = 5

// libFileID is what the server names a library file by: its thread, or
// the message a linked project sent it with.
func libFileID(f thread.LibFile) string {
	if f.Peer != "" {
		return f.Message
	}
	return f.Thread
}

// thumbKey names a file's thumbnail in the popup.
func thumbKey(f thread.LibFile) string {
	return libFileID(f) + "/" + f.Name + "/" + strconv.FormatInt(f.Size, 10) + "/" + strconv.FormatInt(f.Time.UnixNano(), 10)
}

// libPreviewCols is the width of the preview column when the tab has one
// at w: the window has room, the terminal can draw it, and the library
// has an image to draw. A library with none keeps its rows.
func (pv *projectView) libPreviewCols(m *dash, w int) int {
	if w < libPreviewWide || !previewable() {
		return 0
	}
	if m.boxRows(box{title: pv.slug, keys: libTabKeys})-libHeadMost < libPreviewRows {
		return 0
	}
	for _, f := range pv.library {
		if thumb.Supported(f.Name) {
			return w * 3 / 8
		}
	}
	return 0
}

// drawLibPreview lays the selected file's name, size and cells over the
// rows of b's body that show, so the column stays put as the list scrolls.
func (pv *projectView) drawLibPreview(m *dash, b *box, w, cols int) {
	f, ok := pv.selLib()
	if !ok {
		return
	}
	right := []string{th().Faint.Render(fit(oneLine(f.Name)+" · "+sizeWords(f.Size), cols))}
	if img := pv.thumbs[thumbKey(f)]; img != nil {
		right = append(right, img.lines(cols, m.boxRows(*b)-len(b.head)-1)...)
	}
	list := w - cols - libPreviewGap
	for i, r := range right {
		at := b.scroll + i
		for len(b.body) <= at {
			b.body = append(b.body, "")
			b.hits = append(b.hits, noHit)
		}
		b.body[at] = fit(b.body[at], list) + reset + "  " + r
	}
}

// libThumbMsg is a thumbnail asked for by libThumbFetch: nil for a file
// that has none to show.
type libThumbMsg struct {
	pv  *projectView
	key string
	img *imagePreview
}

// libThumbFetch asks for the selected file's thumbnail when the open
// Library tab draws one and doesn't have it: one at a time, so holding
// an arrow asks for the file it stops on and not for all it passed.
func (m *dash) libThumbFetch() tea.Cmd {
	pv, ok := m.top().(*projectView)
	if !ok || pv.tab != tabLibrary || pv.thumbBusy || pv.libPreviewCols(m, m.inner(viewWidth)) == 0 {
		return nil
	}
	f, ok := pv.selLib()
	if !ok || !thumb.Supported(f.Name) {
		return nil
	}
	key := thumbKey(f)
	if _, done := pv.thumbs[key]; done {
		return nil
	}
	pv.thumbBusy = true
	src, slug, id, name := m.src, pv.slug, libFileID(f), f.Name
	return func() tea.Msg {
		return libThumbMsg{pv: pv, key: key, img: fetchThumb(src, slug, id, name)}
	}
}

// fetchThumb is a library file's thumbnail, nil for none.
func fetchThumb(src PollSource, slug, id, name string) *imagePreview {
	data, err := src.LibraryThumbnail(slug, id, name)
	if err != nil {
		return nil
	}
	return newImagePreview(data)
}

// keepThumb remembers a thumbnail, or that a file has none, until the
// popup closes; the oldest goes past libThumbsKept.
func (pv *projectView) keepThumb(key string, img *imagePreview) {
	if pv.thumbs == nil {
		pv.thumbs = map[string]*imagePreview{}
	}
	if _, have := pv.thumbs[key]; !have {
		pv.thumbOrder = append(pv.thumbOrder, key)
	}
	pv.thumbs[key] = img
	for len(pv.thumbOrder) > libThumbsKept {
		delete(pv.thumbs, pv.thumbOrder[0])
		pv.thumbOrder = pv.thumbOrder[1:]
	}
}
