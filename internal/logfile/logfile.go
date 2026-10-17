// Package logfile reads a log file in chunks, never all of it: its end,
// the chunk before an offset, or what follows one (the , popup's Logs
// tab, through log.read).
package logfile

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
)

// Chunk is the bytes [Start, End) of a file Size long, whole lines only.
// Reset says the file is shorter than the offset asked to follow: it was
// rotated or cut, and the chunk is its end.
type Chunk struct {
	Text             []byte
	Start, End, Size int64
	Reset            bool
}

// Tail reads the chunk of at most max bytes that ends at before (0, or
// past the end: the file's end), from its first whole line. A missing
// file is an empty chunk.
func Tail(path string, before int64, max int) (Chunk, error) {
	f, size, err := open(path)
	if f == nil {
		return Chunk{}, err
	}
	defer f.Close()
	return tail(f, size, before, max)
}

// After reads what follows offset after, at most max bytes and up to the
// last whole line (a line longer than max comes in pieces). A file now
// shorter than after is read as Tail, Reset set.
func After(path string, after int64, max int) (Chunk, error) {
	f, size, err := open(path)
	if f == nil {
		return Chunk{}, err
	}
	defer f.Close()
	if after > size || after < 0 {
		c, err := tail(f, size, 0, max)
		c.Reset = true
		return c, err
	}
	n := min(int64(max), size-after)
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, after); err != nil && !errors.Is(err, io.EOF) {
		return Chunk{}, err
	}
	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
		buf = buf[:i+1]
	} else if n < int64(max) {
		buf = buf[:0] // a line still being written: the next read takes it whole
	}
	return Chunk{Text: buf, Start: after, End: after + int64(len(buf)), Size: size}, nil
}

func open(path string) (*os.File, int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, 0, errors.New("not a regular file")
	}
	return f, fi.Size(), nil
}

func tail(f *os.File, size, before int64, max int) (Chunk, error) {
	end := before
	if end <= 0 || end > size {
		end = size
	}
	start := max64(0, end-int64(max))
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return Chunk{}, err
	}
	if start > 0 {
		// From the first whole line, unless the chunk is one line's middle.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
			buf, start = buf[i+1:], start+int64(i+1)
		}
	}
	return Chunk{Text: buf, Start: start, End: end, Size: size}, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
