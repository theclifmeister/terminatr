package qr

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// matrix is c's modules as rows of 0 and 1.
func matrix(c *Code) string {
	var b strings.Builder
	for y := range c.Size {
		for x := range c.Size {
			b.WriteByte("01"[btoi(c.Dark(x, y))])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// The codes below were checked by decoding them with macOS's QR detector
// (CoreImage's CIDetectorTypeQRCode) when the encoder was written; the
// hashes keep them as they were.
func TestEncodeGolden(t *testing.T) {
	src := strings.Repeat("terminatr://pair?relay=wss%3A%2F%2Frelay.example.net%23sha256%3Dabc", 20)
	for _, tc := range []struct {
		n, version int
		hash       string
	}{
		{5, 1, "7db40dda01346ae8"}, {40, 3, "dc246d3e35c99077"}, {213, 10, "02beb16faa51b2c9"},
		{400, 15, "914574e3df235552"}, {900, 24, "ada1df0b06189114"},
	} {
		c, err := Encode([]byte(src[:tc.n]))
		if err != nil {
			t.Fatal(err)
		}
		if v := (c.Size - 17) / 4; v != tc.version {
			t.Errorf("%d bytes: version %d, want %d", tc.n, v, tc.version)
		}
		h := sha256.Sum256([]byte(matrix(c)))
		if got := hex.EncodeToString(h[:8]); got != tc.hash {
			t.Errorf("%d bytes: matrix hash %s, want %s", tc.n, got, tc.hash)
		}
	}
}

// Every code has its three finders, timing and the dark module where a
// reader looks for them.
func TestFunctionPatterns(t *testing.T) {
	c, err := Encode([]byte("terminatr://pair?v=1"))
	if err != nil {
		t.Fatal(err)
	}
	n := c.Size
	for _, at := range [][2]int{{3, 3}, {n - 4, 3}, {3, n - 4}} {
		for d := -3; d <= 3; d++ {
			if !c.Dark(at[0]+d, at[1]-3) || !c.Dark(at[0]-3, at[1]+d) {
				t.Fatalf("finder at %v: its outer ring isn't dark", at)
			}
		}
		if !c.Dark(at[0], at[1]) || c.Dark(at[0]+2, at[1]) {
			t.Fatalf("finder at %v: wrong centre", at)
		}
	}
	for i := 8; i < n-8; i++ {
		if c.Dark(i, 6) != (i%2 == 0) || c.Dark(6, i) != (i%2 == 0) {
			t.Fatalf("timing at %d", i)
		}
	}
	if !c.Dark(8, n-8) {
		t.Fatal("no dark module")
	}
}

// Reed–Solomon over GF(2⁸): the example of ISO/IEC 18004 Annex I (1-M,
// "01234567"), whose 16 data codewords give these 10.
func TestReedSolomon(t *testing.T) {
	data := []byte{0x10, 0x20, 0x0C, 0x56, 0x61, 0x80, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11, 0xEC, 0x11}
	want := []byte{0xA5, 0x24, 0xD4, 0xC1, 0xED, 0x36, 0xC7, 0x87, 0x2C, 0x55}
	if got := rsRemainder(data, rsDivisor(10)); string(got) != string(want) {
		t.Fatalf("ecc % X, want % X", got, want)
	}
}

func TestTooLong(t *testing.T) {
	if _, err := Encode(make([]byte, 3000)); err != ErrTooLong {
		t.Fatalf("err %v", err)
	}
}

func TestTerminal(t *testing.T) {
	c, _ := Encode([]byte("x"))
	plain := c.Terminal(false)
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	if len(lines) != (c.Size+2*Quiet+1)/2 {
		t.Fatalf("%d lines", len(lines))
	}
	// the quiet border's top line is all light: blocks without colour
	if strings.Trim(lines[0], "█") != "" {
		t.Fatalf("first line %q", lines[0])
	}
	if col := c.Terminal(true); !strings.HasPrefix(col, "\x1b[30;107m") || strings.Count(col, "\x1b[0m") != len(lines) {
		t.Fatal("colour lines")
	}
}
