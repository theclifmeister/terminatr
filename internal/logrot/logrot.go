// Package logrot is a log file rotated by size, for the server's log
// and each mod's.
package logrot

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// File is an append-only log file rotated by size: once a write would
// take it over max bytes, path moves to path.1, path.1 to path.2 and so
// on, keep old files in all (path.1 is the newest). Another process may
// append whole lines to the same file (O_APPEND), so the size is read
// from the file before each write, not only counted. A file that can't
// be opened drops what is written to it: the program still runs.
type File struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	f    *os.File
	size int64

	// Failed, when set, is told why the file couldn't be opened again
	// after a rotation.
	Failed func(error)
}

// Open opens path for appending, its folder made if need be. On an error
// the File is still returned, and drops what is written to it.
func Open(path string, max int64, keep int) (*File, error) {
	l := &File{path: path, max: max, keep: keep}
	return l, l.open()
}

func (l *File) open() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	l.f, l.size = f, 0
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	return nil
}

// Write appends p, rotating first when p would take the file over max.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		if fi, err := l.f.Stat(); err == nil {
			l.size = fi.Size()
		}
		if l.size > 0 && l.size+int64(len(p)) > l.max {
			l.rotate()
		}
	}
	if l.f == nil {
		return len(p), nil
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *File) rotate() {
	l.f.Close()
	l.f, l.size = nil, 0
	for i := l.keep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	os.Rename(l.path, l.path+".1")
	if err := l.open(); err != nil && l.Failed != nil {
		l.Failed(err)
	}
}

// Close closes the file; later writes are dropped.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
