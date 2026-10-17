//go:build unix

package fsx

import (
	"io/fs"
	"os"
	"syscall"
)

// openNoFollow opens path with O_NOFOLLOW: the kernel refuses a link as
// the last element, with no gap between a check and the open.
func openNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}
