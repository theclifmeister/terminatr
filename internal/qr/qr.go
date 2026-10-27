// Package qr encodes bytes as a QR code (ISO/IEC 18004), in byte mode at
// error correction level M, the smallest version that holds them, for tm
// device pair to show in a terminal (docs/SPEC.md §16.15). It follows
// the standard's steps: data and Reed–Solomon codewords in blocks,
// interleaved; function patterns; the codewords placed in zigzag; the
// mask with the lowest penalty; format and version bits.
package qr

import (
	"errors"
	"strings"
)

// Code is a QR code: Size modules a side, dark ones true.
type Code struct {
	Size    int
	modules []bool
	// fn marks the function modules, which masks leave alone.
	fn []bool
}

// Dark reports whether the module at column x, row y is dark.
func (c *Code) Dark(x, y int) bool {
	return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.modules[y*c.Size+x]
}

// Error correction at level M: per version (index 1 to 40), the
// codewords each block adds and the number of blocks.
var (
	eccPerBlock = [41]int{0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26,
		26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28}
	eccBlocks = [41]int{0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16,
		17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49}
)

// eccFormatBits is level M's two bits in the format information.
const eccFormatBits = 0

// ErrTooLong says the data doesn't fit a version 40 code.
var ErrTooLong = errors.New("qr: too much data for a QR code")

// rawModules is how many modules of version v hold data and error
// correction: all but the function patterns and the format and version
// bits.
func rawModules(v int) int {
	n := (16*v+128)*v + 64
	if v >= 2 {
		a := v/7 + 2
		n -= (25*a-10)*a - 55
		if v >= 7 {
			n -= 36
		}
	}
	return n
}

// dataCodewords is how many data codewords version v holds at level M.
func dataCodewords(v int) int {
	return rawModules(v)/8 - eccPerBlock[v]*eccBlocks[v]
}

// Encode is data as a QR code.
func Encode(data []byte) (*Code, error) {
	v := 1
	for ; v <= 40; v++ {
		count := 8
		if v >= 10 {
			count = 16
		}
		if len(data) < 1<<count && 4+count+8*len(data) <= 8*dataCodewords(v) {
			break
		}
	}
	if v > 40 {
		return nil, ErrTooLong
	}
	var bits bitBuf
	bits.add(4, 4) // byte mode
	if v >= 10 {
		bits.add(len(data), 16)
	} else {
		bits.add(len(data), 8)
	}
	for _, b := range data {
		bits.add(int(b), 8)
	}
	capacity := 8 * dataCodewords(v)
	bits.add(0, min(4, capacity-len(bits)))
	bits.add(0, (8-len(bits)%8)%8)
	for pad := 0xEC; len(bits) < capacity; pad ^= 0xEC ^ 0x11 {
		bits.add(pad, 8)
	}
	words := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			words[i>>3] |= 1 << (7 - i&7)
		}
	}
	c := &Code{Size: 4*v + 17}
	c.modules = make([]bool, c.Size*c.Size)
	c.fn = make([]bool, c.Size*c.Size)
	c.functionPatterns(v)
	c.place(interleave(v, words))
	best, penalty := 0, -1
	for m := range 8 {
		c.mask(m)
		c.formatBits(m)
		if p := c.penalty(); penalty < 0 || p < penalty {
			best, penalty = m, p
		}
		c.mask(m) // undo: a mask is its own inverse
	}
	c.mask(best)
	c.formatBits(best)
	return c, nil
}

type bitBuf []bool

func (b *bitBuf) add(v, n int) {
	for i := n - 1; i >= 0; i-- {
		*b = append(*b, v>>i&1 != 0)
	}
}

// interleave splits the data codewords into version v's blocks, adds
// each block's error correction and interleaves them all.
func interleave(v int, data []byte) []byte {
	nb, ecc := eccBlocks[v], eccPerBlock[v]
	raw := rawModules(v) / 8
	short := nb - raw%nb
	shortLen := raw / nb
	div := rsDivisor(ecc)
	var blocks [][]byte
	k := 0
	for i := range nb {
		n := shortLen - ecc
		if i >= short {
			n++
		}
		d := data[k : k+n]
		k += n
		b := append(append([]byte{}, d...), rsRemainder(d, div)...)
		if i < short {
			// a short block has a gap where the long ones' last data
			// codeword goes, so the columns line up
			b = append(b[:n], append([]byte{0}, b[n:]...)...)
		}
		blocks = append(blocks, b)
	}
	var out []byte
	for i := range shortLen + 1 {
		for j, b := range blocks {
			if i != shortLen-ecc || j >= short {
				out = append(out, b[i])
			}
		}
	}
	return out
}

// rsDivisor is the Reed–Solomon generator polynomial of degree n over
// GF(2⁸) with the polynomial 0x11D, highest term first, its leading 1
// left out.
func rsDivisor(n int) []byte {
	r := make([]byte, n)
	r[n-1] = 1
	root := byte(1)
	for range n {
		for j := range r {
			r[j] = gfMul(r[j], root)
			if j+1 < n {
				r[j] ^= r[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return r
}

// rsRemainder is data's Reed–Solomon error correction for div.
func rsRemainder(data, div []byte) []byte {
	r := make([]byte, len(div))
	for _, b := range data {
		f := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i, d := range div {
			r[i] ^= gfMul(d, f)
		}
	}
	return r
}

func gfMul(x, y byte) byte {
	var z int
	for i := 7; i >= 0; i-- {
		z = z<<1 ^ (z>>7)*0x11D
		z ^= int(y>>i&1) * int(x)
	}
	return byte(z)
}

func (c *Code) set(x, y int, dark bool) {
	c.modules[y*c.Size+x] = dark
	c.fn[y*c.Size+x] = true
}

func (c *Code) functionPatterns(v int) {
	n := c.Size
	for i := range n {
		c.set(6, i, i%2 == 0)
		c.set(i, 6, i%2 == 0)
	}
	c.finder(3, 3)
	c.finder(n-4, 3)
	c.finder(3, n-4)
	pos := alignment(v)
	for i, a := range pos {
		for j, b := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == len(pos)-1) || (i == len(pos)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(a+dx, b+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	c.formatBits(0) // reserves them; Encode writes the real ones
	if v >= 7 {
		rem := v
		for range 12 {
			rem = rem<<1 ^ (rem>>11)*0x1F25
		}
		bits := v<<12 | rem
		for i := range 18 {
			dark := bits>>i&1 != 0
			a, b := n-11+i%3, i/3
			c.set(a, b, dark)
			c.set(b, a, dark)
		}
	}
}

// finder draws a finder pattern centred on x, y, with its separator.
func (c *Code) finder(x, y int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			xx, yy := x+dx, y+dy
			if xx < 0 || yy < 0 || xx >= c.Size || yy >= c.Size {
				continue
			}
			d := max(abs(dx), abs(dy))
			c.set(xx, yy, d != 2 && d != 4)
		}
	}
}

// alignment is version v's alignment pattern centres, on each axis.
func alignment(v int) []int {
	if v == 1 {
		return nil
	}
	n := v/7 + 2
	step := (v*8 + n*3 + 5) / (n*4 - 4) * 2
	if v == 32 {
		step = 26
	}
	out := make([]int, n)
	out[0] = 6
	for i := n - 1; i >= 1; i-- {
		out[i] = 4*v + 10 - (n-1-i)*step
	}
	return out
}

// formatBits writes the format information for mask m, both copies.
func (c *Code) formatBits(m int) {
	data := eccFormatBits<<3 | m
	rem := data
	for range 10 {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return bits>>i&1 != 0 }
	n := c.Size
	for i := range 6 {
		c.set(8, i, bit(i))
	}
	c.set(8, 7, bit(6))
	c.set(8, 8, bit(7))
	c.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		c.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		c.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		c.set(8, n-15+i, bit(i))
	}
	c.set(8, n-8, true) // the dark module
}

// place puts the codewords in the zigzag, right to left, two columns at
// a time, skipping the timing column.
func (c *Code) place(words []byte) {
	n := c.Size
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := range n {
			for j := range 2 {
				x := right - j
				up := (right+1)&2 == 0
				y := vert
				if up {
					y = n - 1 - vert
				}
				if c.fn[y*n+x] || i >= len(words)*8 {
					continue
				}
				c.modules[y*n+x] = words[i>>3]>>(7-i&7)&1 != 0
				i++
			}
		}
	}
}

// mask flips the data modules mask m selects.
func (c *Code) mask(m int) {
	n := c.Size
	for y := range n {
		for x := range n {
			var flip bool
			switch m {
			case 0:
				flip = (x+y)%2 == 0
			case 1:
				flip = y%2 == 0
			case 2:
				flip = x%3 == 0
			case 3:
				flip = (x+y)%3 == 0
			case 4:
				flip = (x/3+y/2)%2 == 0
			case 5:
				flip = x*y%2+x*y%3 == 0
			case 6:
				flip = (x*y%2+x*y%3)%2 == 0
			case 7:
				flip = ((x+y)%2+x*y%3)%2 == 0
			}
			if flip && !c.fn[y*n+x] {
				c.modules[y*n+x] = !c.modules[y*n+x]
			}
		}
	}
}

// penalty scores the code as the standard's mask evaluation does: runs,
// 2×2 boxes, finder-like patterns and the dark share.
func (c *Code) penalty() int {
	n := c.Size
	p := 0
	line := func(at func(i int) bool) {
		run := 0
		var prev bool
		var seq []bool
		for i := range n {
			d := at(i)
			if i > 0 && d == prev {
				run++
				if run == 5 {
					p += 3
				} else if run > 5 {
					p++
				}
			} else {
				run = 1
			}
			prev = d
			seq = append(seq, d)
		}
		// 1:1:3:1:1 with four light modules on one side (or the edge)
		pat := []bool{true, false, true, true, true, false, true}
		for i := 0; i+7 <= n; i++ {
			match := true
			for k, b := range pat {
				if seq[i+k] != b {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			light := func(from, to int) bool {
				for k := from; k < to; k++ {
					if k >= 0 && k < n && seq[k] {
						return false
					}
				}
				return true
			}
			if light(i-4, i) || light(i+7, i+11) {
				p += 40
			}
		}
	}
	for y := range n {
		line(func(x int) bool { return c.modules[y*n+x] })
	}
	for x := range n {
		line(func(y int) bool { return c.modules[y*n+x] })
	}
	dark := 0
	for y := range n {
		for x := range n {
			d := c.modules[y*n+x]
			if d {
				dark++
			}
			if x+1 < n && y+1 < n && d == c.modules[y*n+x+1] && d == c.modules[(y+1)*n+x] && d == c.modules[(y+1)*n+x+1] {
				p += 3
			}
		}
	}
	total := n * n
	k := (abs(dark*20-total*10)+total-1)/total - 1
	return p + max(k, 0)*10
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Quiet is the light border a code needs around it, in modules.
const Quiet = 4

// Terminal draws c for a terminal, two rows of modules to a line in
// half blocks, with a quiet border, dark on light as a camera wants it.
// With colour each line is black on white whatever the terminal's
// theme; without, light modules are blocks in the text's colour, for a
// terminal with light text on a dark background. Each line ends in a
// newline.
func (c *Code) Terminal(colour bool) string {
	var b strings.Builder
	lo, hi := -Quiet, c.Size+Quiet
	for y := lo; y < hi; y += 2 {
		if colour {
			b.WriteString("\x1b[30;107m")
		}
		for x := lo; x < hi; x++ {
			top, bottom := c.Dark(x, y), c.Dark(x, y+1)
			if !colour {
				top, bottom = !top, !bottom && y+1 < hi
			}
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteByte(' ')
			}
		}
		if colour {
			b.WriteString("\x1b[0m")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
