package moddev

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// tailMax is how much of a log's end Tail reads.
const tailMax = 1 << 20

// Tail returns the last n lines of the file at path (all of them for
// n <= 0, within its last MB) and the size it read to, where Follow
// starts.
func Tail(path string, n int) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := fi.Size()
	from := max(size-tailMax, 0)
	b := make([]byte, size-from)
	if _, err := f.ReadAt(b, from); err != nil && err != io.EOF {
		return nil, 0, err
	}
	if from > 0 {
		// The first line is cut.
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	text := strings.TrimSuffix(string(b), "\n")
	if text == "" {
		return nil, size, nil
	}
	lines := strings.Split(text, "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, size, nil
}

// Follow writes what is added to the file at path after offset to w, a
// line at a time, until ctx ends. A file that shrinks was rotated (the
// mod log moves to <id>.log.1 at 1 MB) and is read again from its start;
// a file not there yet is waited for.
func Follow(ctx context.Context, path string, offset int64, every time.Duration, w io.Writer) {
	var part []byte
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if fi, err := os.Stat(path); err == nil {
			if fi.Size() < offset {
				offset, part = 0, nil
			}
			if fi.Size() > offset {
				if f, err := os.Open(path); err == nil {
					b := make([]byte, fi.Size()-offset)
					n, _ := f.ReadAt(b, offset)
					f.Close()
					offset += int64(n)
					part = append(part, b[:n]...)
					if i := bytes.LastIndexByte(part, '\n'); i >= 0 {
						_, _ = w.Write(part[:i+1])
						part = append([]byte(nil), part[i+1:]...)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
