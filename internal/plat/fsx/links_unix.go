//go:build unix

package fsx

import (
	"os"
	"syscall"
)

// Links is fi's hard link count.
func Links(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}

// OpenInNoBlock opens rel under r for reading without blocking on a FIFO
// or device: it opens at once, and the caller checks what it is.
func OpenInNoBlock(r *os.Root, rel string) (*os.File, error) {
	return r.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

// Mine reports whether fi is owned by the user running this process.
func Mine(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
