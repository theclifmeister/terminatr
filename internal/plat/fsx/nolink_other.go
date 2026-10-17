//go:build !unix

package fsx

import (
	"io/fs"
	"os"
)

// openNoFollow refuses a path that is a link (any reparse point on
// Windows) before opening it: there is no O_NOFOLLOW here.
func openNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	if fi, err := os.Lstat(path); err == nil && isLink(fi) {
		return nil, ErrLink
	}
	return os.OpenFile(path, flag, perm)
}
