// Package thumb makes the small JPEG a console shows for a library's
// image, and keeps it (docs/SPEC.md §5.1 Library thumbnails). The server
// makes one on first view, from the file or a packed thread's tarball,
// and caches it outside the library, so nothing is added to what a
// thread attached and a console never fetches an original to draw a
// list. PNG, JPEG and GIF only, by their bytes, with limits on the
// file's size and its pixels: a huge image is refused before it is
// decoded, and a file that can't be read is refused the same way.
package thumb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

const (
	// Side is the most pixels a thumbnail has on its long side: a strip
	// of four on a Mac's Retina window, three across on an iPhone.
	Side = 400
	// MaxBytes is the most a thumbnail is, in one answer: under the 1 MiB
	// a device's link carries in a line once in base64.
	MaxBytes = 512 << 10
	// MaxSource is the largest file it reads, what a console may read of
	// a library file.
	MaxSource = 25 << 20
	// MaxPixels is the largest image it decodes. A decoded image takes 4
	// to 8 bytes a pixel whatever the file's size, and decoding takes the
	// time, so this is the limit that keeps a huge image from stalling
	// the server. An 8K screen is 33 MP.
	MaxPixels = 40_000_000

	quality = 70
	// version is in every cache key: a change to how thumbnails look, or
	// to which files make one, makes them again.
	version = 1
	// slots is how many are made at a time, and waitSlot how long a
	// request waits for one: a first scroll through a long library asks
	// for many, and the server has other work.
	slots = 2
)

// waitSlot is a var for the tests.
var waitSlot = 12 * time.Second

// ErrNoThumbnail says the file makes none, now or on another try: not
// PNG, JPEG or GIF, over a limit, or unreadable. A console shows a row.
var ErrNoThumbnail = errors.New("no thumbnail")

// ErrBusy says too many were being made: a later try may get it.
var ErrBusy = errors.New("thumbnails are being made")

// Thumb is a made thumbnail.
type Thumb struct {
	Width, Height int
	Data          []byte
}

// Supported says a file's name is that of an image thumbnails are made
// of. Clients ask only for these (a library with no images makes no
// call), and the server refuses others without reading them.
func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// Make is the thumbnail of data, an image's bytes: at most Side pixels on
// its long side, never enlarged, transparency over white, a GIF's first
// frame. ErrNoThumbnail for anything else or anything over the limits.
func Make(data []byte) (t Thumb, err error) {
	// The decoders take bytes a thread wrote. A panic in one is one
	// image without a thumbnail, not a server down.
	defer func() {
		if r := recover(); r != nil {
			t, err = Thumb{}, fmt.Errorf("%w: %v", ErrNoThumbnail, r)
		}
	}()
	if len(data) > MaxSource {
		return Thumb{}, fmt.Errorf("%w: %d bytes", ErrNoThumbnail, len(data))
	}
	decode, config := decoders(data)
	if decode == nil {
		return Thumb{}, fmt.Errorf("%w: not PNG, JPEG or GIF", ErrNoThumbnail)
	}
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return Thumb{}, fmt.Errorf("%w: %v", ErrNoThumbnail, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Thumb{}, fmt.Errorf("%w: %dx%d", ErrNoThumbnail, cfg.Width, cfg.Height)
	}
	src, err := decode(bytes.NewReader(data))
	if err != nil {
		return Thumb{}, fmt.Errorf("%w: %v", ErrNoThumbnail, err)
	}
	// What was decoded, not what the header said: they agree, or the
	// limit above would have checked the wrong image.
	if b := src.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return Thumb{}, fmt.Errorf("%w: %v is not %dx%d", ErrNoThumbnail, b, cfg.Width, cfg.Height)
	}
	w, h := fit(cfg.Width, cfg.Height)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, shrink(src, w, h), &jpeg.Options{Quality: quality}); err != nil {
		return Thumb{}, fmt.Errorf("%w: %v", ErrNoThumbnail, err)
	}
	if out.Len() > MaxBytes {
		return Thumb{}, fmt.Errorf("%w: %d bytes made", ErrNoThumbnail, out.Len())
	}
	return Thumb{Width: w, Height: h, Data: out.Bytes()}, nil
}

// decoders are the format's functions, by the file's first bytes and not
// its name; none for any other format. Never the image package's
// registry, which any package linked in may add formats to.
func decoders(data []byte) (func(io.Reader) (image.Image, error), func(io.Reader) (image.Config, error)) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return png.Decode, png.DecodeConfig
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return jpeg.Decode, jpeg.DecodeConfig
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return gif.Decode, gif.DecodeConfig // the first frame, within the screen it declares
	}
	return nil, nil
}

// fit is w by h within Side, its shape kept, at least a pixel each way.
func fit(w, h int) (int, int) {
	if w <= Side && h <= Side {
		return w, h
	}
	if w >= h {
		return Side, max(1, h*Side/w)
	}
	return max(1, w*Side/h), Side
}

// shrink is src at w by h, each pixel the average of those it covers (w
// and h not over src's), on white. One source row at a time, through
// draw's fast paths, so it takes no second copy of the image.
func shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	row := image.NewRGBA(image.Rect(0, 0, sw, 1))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	sum := make([]uint64, w*4)
	for dy := range h {
		y0, y1 := dy*sh/h, (dy+1)*sh/h
		clear(sum)
		for y := y0; y < y1; y++ {
			draw.Draw(row, row.Rect, src, image.Pt(b.Min.X, b.Min.Y+y), draw.Src)
			for dx := range w {
				for x := dx * sw / w; x < (dx+1)*sw/w; x++ {
					p := row.Pix[x*4 : x*4+4 : x*4+4]
					sum[dx*4] += uint64(p[0])
					sum[dx*4+1] += uint64(p[1])
					sum[dx*4+2] += uint64(p[2])
					sum[dx*4+3] += uint64(p[3])
				}
			}
		}
		for dx := range w {
			n := uint64((y1 - y0) * ((dx+1)*sw/w - dx*sw/w))
			px := out.Pix[out.PixOffset(dx, dy) : out.PixOffset(dx, dy)+4]
			a := sum[dx*4+3] / n
			// Premultiplied, so white shows through by what alpha leaves.
			for c := range 3 {
				px[c] = uint8(min(255, sum[dx*4+c]/n+(255-a)))
			}
			px[3] = 255
		}
	}
	return out
}

// Key names a library file in the cache: its project, its thread or
// message, its name.
type Key struct {
	Project, ID, Name string
}

var slot = make(chan struct{}, slots)

// Get is k's thumbnail for the file of size and mod: the cached one, else
// made from read's bytes, once, and kept. A file that makes none is
// remembered too, so a corrupt image costs a read only once. A file
// changed by size or time makes a new one and the old goes.
func Get(k Key, size int64, mod time.Time, read func() ([]byte, error)) (Thumb, error) {
	if !Supported(k.Name) || size > MaxSource {
		return Thumb{}, fmt.Errorf("%w: %s", ErrNoThumbnail, k.Name)
	}
	path, err := k.path(size, mod)
	if err != nil {
		return Thumb{}, err
	}
	if t, ok, err := cached(path); ok {
		return t, err
	}
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-time.After(waitSlot):
		return Thumb{}, ErrBusy
	}
	// Another request for it may have made it while this one waited.
	if t, ok, err := cached(path); ok {
		return t, err
	}
	data, err := read()
	if err != nil {
		return Thumb{}, err
	}
	t, err := Make(data)
	if err != nil && !errors.Is(err, ErrNoThumbnail) {
		return Thumb{}, err
	}
	// A cache that can't be written costs only the next view's time: the
	// console gets its answer all the same.
	_ = store(path, t.Data, k)
	return t, err
}

// path is where k's thumbnail for that file is: under its project and
// thread, by hashes, since a name is the thread's to choose.
func (k Key) path(size int64, mod time.Time) (string, error) {
	d, err := home.ThumbCacheDir()
	if err != nil {
		return "", err
	}
	stamp := sha256.Sum256(fmt.Appendf(nil, "%d\x00%d\x00%d", version, size, mod.UnixNano()))
	return filepath.Join(d, k.Project, k.ID, nameHash(k.Name)+"-"+hex.EncodeToString(stamp[:8])+".jpg"), nil
}

func nameHash(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:8])
}

// cached is the thumbnail at path; ok says the cache knew. An empty file
// remembers that the file makes none.
func cached(path string) (t Thumb, ok bool, err error) {
	f, err := fsx.OpenNoLink(path, os.O_RDONLY, 0)
	if err != nil {
		return Thumb{}, false, nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		return Thumb{}, false, nil
	}
	if len(data) == 0 {
		return Thumb{}, true, fmt.Errorf("%w: remembered", ErrNoThumbnail)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Thumb{}, false, nil // not one of ours: made again
	}
	return Thumb{Width: cfg.Width, Height: cfg.Height, Data: data}, true, nil
}

// store keeps data (empty: makes none) at path, whole or not at all, in
// place of the file's older thumbnails.
func store(path string, data []byte, k Key) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".new-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	olds, _ := filepath.Glob(filepath.Join(dir, nameHash(k.Name)+"-*.jpg"))
	for _, o := range olds {
		if o != path {
			os.Remove(o)
		}
	}
	return nil
}

// Forget removes what the cache keeps of a file that was deleted (a
// thumbnail shows what the file showed): the named one, or all of id's
// when name is "". Forgetting nothing is no error.
func Forget(project, id, name string) error {
	d, err := home.ThumbCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(d, project, id)
	if name == "" {
		return os.RemoveAll(dir)
	}
	olds, _ := filepath.Glob(filepath.Join(dir, nameHash(name)+"-*.jpg"))
	for _, o := range olds {
		if err := os.Remove(o); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	os.Remove(dir) // when it is empty
	return nil
}
