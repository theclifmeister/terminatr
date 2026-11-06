package thumb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func flat(w, h int, c color.Color) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, c)
		}
	}
	return m
}

func pngOf(t *testing.T, m image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// hugePNG is a valid grey PNG of w by h pixels, written without holding
// them, so the test doesn't pay what the limit saves the server.
func hugePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, data []byte) {
		binary.Write(&out, binary.BigEndian, uint32(len(data)))
		c := crc32.NewIEEE()
		c.Write([]byte(kind))
		c.Write(data)
		out.WriteString(kind)
		out.Write(data)
		binary.Write(&out, binary.BigEndian, c.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = 8 // 8 bits, grey
	chunk("IHDR", ihdr)
	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, zlib.BestSpeed)
	row := make([]byte, w+1)
	for range h {
		zw.Write(row)
	}
	zw.Close()
	chunk("IDAT", z.Bytes())
	chunk("IEND", nil)
	return out.Bytes()
}

// centre is the colour of a thumbnail's middle pixel.
func centre(t *testing.T, th Thumb) (r, g, b uint32) {
	t.Helper()
	m, err := jpeg.Decode(bytes.NewReader(th.Data))
	if err != nil {
		t.Fatal(err)
	}
	if m.Bounds().Dx() != th.Width || m.Bounds().Dy() != th.Height {
		t.Fatalf("%v, says %dx%d", m.Bounds(), th.Width, th.Height)
	}
	r, g, b, _ = m.At(th.Width/2, th.Height/2).RGBA()
	return r >> 8, g >> 8, b >> 8
}

func near(a, b uint32) bool { return a+6 >= b && b+6 >= a }

func TestMakeFormats(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	var jp, gi bytes.Buffer
	jpeg.Encode(&jp, flat(800, 600, red), nil)
	frame := image.NewPaletted(image.Rect(0, 0, 800, 600), color.Palette{red, color.NRGBA{0, 0, 255, 255}})
	second := image.NewPaletted(image.Rect(0, 0, 800, 600), color.Palette{color.NRGBA{0, 0, 255, 255}, red})
	gif.EncodeAll(&gi, &gif.GIF{Image: []*image.Paletted{frame, second}, Delay: []int{0, 0}})
	for name, data := range map[string][]byte{"png": pngOf(t, flat(800, 600, red)), "jpeg": jp.Bytes(), "gif, first frame": gi.Bytes()} {
		th, err := Make(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if th.Width != 400 || th.Height != 300 {
			t.Errorf("%s: %dx%d", name, th.Width, th.Height)
		}
		if r, g, b := centre(t, th); !near(r, 255) || !near(g, 0) || !near(b, 0) {
			t.Errorf("%s: centre %d %d %d", name, r, g, b)
		}
	}
}

func TestMakeSizes(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{1200, 2600, 184, 400},
		{5000, 1, 400, 1},
		{1, 5000, 1, 400},
		{100, 50, 100, 50}, // never enlarged
		{1, 1, 1, 1},
		{400, 400, 400, 400},
	} {
		th, err := Make(pngOf(t, flat(c.w, c.h, color.Gray{128})))
		if err != nil || th.Width != c.wantW || th.Height != c.wantH {
			t.Errorf("%dx%d: %dx%d %v", c.w, c.h, th.Width, th.Height, err)
		}
	}
	// A bound that isn't at the origin (a GIF frame's can't be, but a
	// decoder's may): every source row is read from its own place.
	sub := flat(100, 100, color.Black)
	for y := 50; y < 100; y++ {
		for x := range 100 {
			sub.Set(x, y, color.White)
		}
	}
	got := shrink(sub.SubImage(image.Rect(0, 50, 100, 100)), 10, 10)
	if r, _, _, _ := got.At(5, 5).RGBA(); r>>8 != 255 {
		t.Errorf("subimage: %d", r>>8)
	}
}

func TestMakeTransparency(t *testing.T) {
	th, err := Make(pngOf(t, flat(10, 10, color.NRGBA{0, 0, 0, 0})))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b := centre(t, th); !near(r, 255) || !near(g, 255) || !near(b, 255) {
		t.Errorf("clear isn't white: %d %d %d", r, g, b)
	}
	half := pngOf(t, flat(10, 10, color.NRGBA{0, 0, 0, 128}))
	if th, err = Make(half); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := centre(t, th); !near(r, 127) {
		t.Errorf("half black isn't grey: %d", r)
	}
}

// TestMakeRefuses: whatever isn't an image of the three, or is over a
// limit, is ErrNoThumbnail, and a 100 MP image costs no decoding.
func TestMakeRefuses(t *testing.T) {
	good := pngOf(t, flat(64, 64, color.Gray{9}))
	flipped := bytes.Clone(good)
	flipped[len(flipped)/2] ^= 0xff
	screen := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x00\x10\x00\x10\x00\x00\x02\x02D\x01\x00;")
	for name, data := range map[string][]byte{
		"empty":             nil,
		"text":              []byte("# notes\n"),
		"svg":               []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect/></svg>`),
		"header only":       good[:8],
		"truncated png":     good[:len(good)/2],
		"corrupt png":       flipped,
		"bad jpeg":          []byte("\xff\xd8\xff\xe0not a jpeg"),
		"frame over screen": screen,
		"over the bytes":    make([]byte, MaxSource+1),
	} {
		if _, err := Make(data); !errors.Is(err, ErrNoThumbnail) {
			t.Errorf("%s: %v", name, err)
		}
	}
	big := hugePNG(t, 10000, 10000)
	start := time.Now()
	_, err := Make(big)
	if !errors.Is(err, ErrNoThumbnail) {
		t.Fatalf("100 MP: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("100 MP took %v to refuse", d)
	}
	wide := hugePNG(t, 2_000_000, 21) // 42 MP in a strip
	if _, err := Make(wide); !errors.Is(err, ErrNoThumbnail) {
		t.Errorf("42 MP strip: %v", err)
	}
}

func TestSupported(t *testing.T) {
	for name, want := range map[string]bool{"a.png": true, "A.PNG": true, "b.jpg": true, "c.JPEG": true, "d.gif": true,
		"e.svg": false, "f.webp": false, "g": false, "png": false, "h.png.md": false} {
		if Supported(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}

func cacheFiles(t *testing.T) []string {
	t.Helper()
	d := os.Getenv("TERMINATR_HOME")
	var out []string
	filepath.WalkDir(filepath.Join(d, "cache", "thumbs"), func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// TestGetCaches: made once, kept, made anew when the file changes, the
// old one gone; a file that makes none is remembered; Forget removes.
func TestGetCaches(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	k := Key{Project: "web", ID: "t-0001", Name: "a.png"}
	img := pngOf(t, flat(20, 20, color.Gray{9}))
	reads := 0
	read := func() ([]byte, error) { reads++; return img, nil }
	mod := time.Unix(1000, 5)
	first, err := Get(k, 10, mod, read)
	if err != nil || reads != 1 {
		t.Fatalf("%v, %d reads", err, reads)
	}
	again, err := Get(k, 10, mod, read)
	if err != nil || reads != 1 || !bytes.Equal(first.Data, again.Data) || again.Width != 20 || again.Height != 20 {
		t.Fatalf("cached: %v, %d reads, %dx%d", err, reads, again.Width, again.Height)
	}
	if _, err := Get(k, 10, mod.Add(time.Nanosecond), read); err != nil || reads != 2 {
		t.Fatalf("changed: %v, %d reads", err, reads)
	}
	if got := cacheFiles(t); len(got) != 1 {
		t.Errorf("old one kept: %v", got)
	}
	// An unusable cache file is made again, not served.
	os.WriteFile(cacheFiles(t)[0], []byte("not a jpeg"), 0o600)
	if _, err := Get(k, 10, mod.Add(time.Nanosecond), read); err != nil || reads != 3 {
		t.Fatalf("spoilt: %v, %d reads", err, reads)
	}

	bad := Key{Project: "web", ID: "t-0001", Name: "bad.png"}
	badReads := 0
	readBad := func() ([]byte, error) { badReads++; return []byte("\x89PNG\r\n\x1a\nnope"), nil }
	for range 3 {
		if _, err := Get(bad, 5, mod, readBad); !errors.Is(err, ErrNoThumbnail) {
			t.Fatalf("bad: %v", err)
		}
	}
	if badReads != 1 {
		t.Errorf("a corrupt file read %d times", badReads)
	}
	// A failed read isn't the file's fault, and isn't remembered.
	gone := Key{Project: "web", ID: "t-0001", Name: "later.png"}
	boom := errors.New("boom")
	if _, err := Get(gone, 5, mod, func() ([]byte, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("read error: %v", err)
	}
	if _, err := Get(gone, 5, mod, read); err != nil {
		t.Errorf("after a failed read: %v", err)
	}

	// Not an image by name, or over the size: no read at all.
	never := func() ([]byte, error) { t.Error("read"); return nil, nil }
	if _, err := Get(Key{Project: "web", ID: "t-0001", Name: "a.svg"}, 5, mod, never); !errors.Is(err, ErrNoThumbnail) {
		t.Errorf("svg: %v", err)
	}
	if _, err := Get(k, MaxSource+1, mod, never); !errors.Is(err, ErrNoThumbnail) {
		t.Errorf("large: %v", err)
	}

	if err := Forget("web", "t-0001", "a.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(k, 10, mod.Add(time.Nanosecond), read); err != nil || reads != 5 {
		t.Errorf("forgotten: %v, %d reads", err, reads)
	}
	if err := Forget("web", "t-0001", ""); err != nil {
		t.Fatal(err)
	}
	if got := cacheFiles(t); len(got) != 0 {
		t.Errorf("forgot all, kept %v", got)
	}
	if err := Forget("web", "t-0009", "none.png"); err != nil {
		t.Errorf("forget nothing: %v", err)
	}
}

// TestGetBounded: however many ask at once, at most slots read and
// decode; one that waits too long hears busy, and the next try gets it.
func TestGetBounded(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	img := pngOf(t, flat(8, 8, color.Gray{9}))
	var now, most atomic.Int32
	read := func() ([]byte, error) {
		n := now.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		time.Sleep(5 * time.Millisecond)
		now.Add(-1)
		return img, nil
	}
	done := make(chan error)
	for i := range 12 {
		go func() {
			_, err := Get(Key{Project: "web", ID: "t-0001", Name: string(rune('a'+i)) + ".png"}, 5, time.Unix(1, 0), read)
			done <- err
		}()
	}
	for range 12 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if most.Load() > slots {
		t.Errorf("%d at once", most.Load())
	}

	old := waitSlot
	waitSlot = 10 * time.Millisecond
	defer func() { waitSlot = old }()
	for range slots {
		slot <- struct{}{}
	}
	k := Key{Project: "web", ID: "t-0002", Name: "b.png"}
	_, err := Get(k, 5, time.Unix(1, 0), read)
	for range slots {
		<-slot
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("full: %v", err)
	}
	if _, err := Get(k, 5, time.Unix(1, 0), read); err != nil {
		t.Errorf("after busy: %v", err)
	}
}
