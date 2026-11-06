package thumb

import (
	"bytes"
	"encoding/binary"
	"image"
)

// orientation is the EXIF orientation (1 to 8) of a JPEG's bytes: how the
// stored pixels are turned for display. 1 for anything it can't read.
// Only the first APP1 Exif segment's IFD0 is looked at.
func orientation(data []byte) int {
	i := 2 // after SOI
	for i+4 <= len(data) && data[i] == 0xff {
		m := data[i+1]
		if m == 0xff { // fill byte
			i++
			continue
		}
		if m == 0xda || m == 0xd9 { // scan or end: no more headers
			return 1
		}
		n := int(data[i+2])<<8 | int(data[i+3])
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+n]
		if m == 0xe1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(t[2:]) != 42 {
		return 1
	}
	off := uint64(bo.Uint32(t[4:]))
	if off+2 > uint64(len(t)) {
		return 1
	}
	count := uint64(bo.Uint16(t[off:]))
	for k := range count {
		e := off + 2 + k*12
		if e+12 > uint64(len(t)) {
			return 1
		}
		if bo.Uint16(t[e:]) != 0x0112 {
			continue
		}
		if bo.Uint16(t[e+2:]) != 3 || bo.Uint32(t[e+4:]) != 1 { // one SHORT
			return 1
		}
		if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orient is img turned as EXIF orientation o says to show it upright.
// It works on the small thumbnail, not on the original.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5: // transpose
				dx, dy = y, x
			case 6: // 90° clockwise
				dx, dy = h-1-y, x
			case 7: // transverse
				dx, dy = h-1-y, w-1-x
			case 8: // 90° counter-clockwise
				dx, dy = y, w-1-x
			}
			copy(out.Pix[out.PixOffset(dx, dy):out.PixOffset(dx, dy)+4], img.Pix[img.PixOffset(x, y):img.PixOffset(x, y)+4])
		}
	}
	return out
}
