//go:build darwin || linux || freebsd

package fsx

import "syscall"

// Free is how many bytes the file system holding dir has free for this
// user; -1 when that can't be told.
func Free(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return -1
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize))
}
