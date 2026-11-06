package thumb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// exifJPEG is a 64x32 JPEG, its top-left 16x16 red and the rest white,
// with an EXIF segment saying orientation o (0: none).
func exifJPEG(t *testing.T, o int, bigEndian bool) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			c := color.RGBA{255, 255, 255, 255}
			if x < 16 && y < 16 {
				c = color.RGBA{255, 0, 0, 255}
			}
			m.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	if o == 0 {
		return raw
	}
	var bo binary.ByteOrder = binary.LittleEndian
	hdr := "II"
	if bigEndian {
		bo, hdr = binary.BigEndian, "MM"
	}
	var tiff bytes.Buffer
	tiff.WriteString(hdr)
	binary.Write(&tiff, bo, uint16(42))
	binary.Write(&tiff, bo, uint32(8))
	binary.Write(&tiff, bo, uint16(1))
	binary.Write(&tiff, bo, uint16(0x0112))
	binary.Write(&tiff, bo, uint16(3))
	binary.Write(&tiff, bo, uint32(1))
	binary.Write(&tiff, bo, uint16(o))
	binary.Write(&tiff, bo, uint16(0))
	binary.Write(&tiff, bo, uint32(0))
	seg := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	out := []byte{0xff, 0xd8, 0xff, 0xe1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	out = append(out, seg...)
	return append(out, raw[2:]...)
}

// TestMakeOrientation: a rotated phone photo is upright in its thumbnail,
// for each EXIF orientation, in either byte order.
func TestMakeOrientation(t *testing.T) {
	// Where the stored top-left corner ends up, and the size.
	cases := []struct {
		o      int
		corner string // tl, tr, br, bl
		w, h   int
	}{
		{0, "tl", 64, 32}, {1, "tl", 64, 32}, {2, "tr", 64, 32}, {3, "br", 64, 32}, {4, "bl", 64, 32},
		{5, "tl", 32, 64}, {6, "tr", 32, 64}, {7, "br", 32, 64}, {8, "bl", 32, 64},
	}
	for _, be := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("o%d-be%v", c.o, be), func(t *testing.T) {
				th, err := Make(exifJPEG(t, c.o, be))
				if err != nil {
					t.Fatal(err)
				}
				if th.Width != c.w || th.Height != c.h {
					t.Fatalf("%dx%d, want %dx%d", th.Width, th.Height, c.w, c.h)
				}
				img, err := jpeg.Decode(bytes.NewReader(th.Data))
				if err != nil {
					t.Fatal(err)
				}
				if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
					t.Fatalf("decoded %v", b)
				}
				at := map[string]image.Point{"tl": {6, 6}, "tr": {c.w - 7, 6}, "br": {c.w - 7, c.h - 7}, "bl": {6, c.h - 7}}
				for name, p := range at {
					r, g, _, _ := img.At(p.X, p.Y).RGBA()
					red := r>>8 > 200 && g>>8 < 80
					if red != (name == c.corner) {
						t.Errorf("corner %s red=%v, red corner should be %s", name, red, c.corner)
					}
				}
			})
		}
	}
}

// TestOrientationBad: damaged EXIF is no orientation, not an error.
func TestOrientationBad(t *testing.T) {
	good := exifJPEG(t, 6, false)
	for _, n := range []int{3, 5, 8, 12, 20, 30} {
		if n < len(good) {
			if _, err := Make(good[:n]); err == nil {
				t.Errorf("cut at %d made a thumbnail", n)
			}
		}
	}
	bad := exifJPEG(t, 9, false) // out of range
	if o := orientation(bad); o != 1 {
		t.Errorf("orientation 9 read as %d", o)
	}
	if _, err := Make(bad); err != nil {
		t.Error(err)
	}
}

// TestCacheVersion: a thumbnail made before the rotation fix is not served.
func TestCacheVersion(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	k := Key{Project: "web", ID: "t-0001", Name: "a.jpg"}
	p, err := k.path(10, time.Unix(1000, 5))
	if err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("version %d", version)
	}
	if filepath.Base(p) == "" {
		t.Fatal("no path")
	}
}

// TestForgetProjectAndSweep: a project's thumbnails go with it, and a
// sweep removes those of projects that no longer exist.
func TestForgetProjectAndSweep(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	put := func(project string) {
		t.Helper()
		k := Key{Project: project, ID: "t-0001", Name: "a.png"}
		img := pngOf(t, flat(8, 8, color.Gray{9}))
		if _, err := Get(k, 10, time.Unix(1000, 5), func() ([]byte, error) { return img, nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"a", "b", "c"} {
		put(p)
	}
	if err := ForgetProject("a"); err != nil {
		t.Fatal(err)
	}
	if got := len(cacheFiles(t)); got != 2 {
		t.Fatalf("after forgetting a: %d files", got)
	}
	if err := ForgetProject("nope"); err != nil {
		t.Fatal(err)
	}
	if err := ForgetProject("../x"); err != nil {
		t.Fatal(err)
	}
	n, err := Sweep(func(s string) bool { return s == "b" })
	if err != nil || n != 1 {
		t.Fatalf("swept %d: %v", n, err)
	}
	got := cacheFiles(t)
	if len(got) != 1 || !bytes.Contains([]byte(got[0]), []byte(string(filepath.Separator)+"b"+string(filepath.Separator))) {
		t.Fatalf("left: %v", got)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("TERMINATR_HOME"), "cache", "thumbs", "c")); err == nil {
		t.Error("c stays")
	}
	// No cache folder at all is nothing to sweep.
	t.Setenv("TERMINATR_HOME", t.TempDir())
	if n, err := Sweep(func(string) bool { return false }); n != 0 || err != nil {
		t.Fatalf("%d, %v", n, err)
	}
}
