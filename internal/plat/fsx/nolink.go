package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// ErrLink means a path tm writes is a link, which it doesn't follow.
var ErrLink = errors.New("is a link")

// OpenNoLink opens path as os.OpenFile does, but refuses one whose last
// element is a link (ErrLink), dangling or not: for a file tm appends to
// or creates in a folder an agent can write, where a planted link would
// lead the write anywhere the server may write.
func OpenNoLink(path string, flag int, perm fs.FileMode) (*os.File, error) {
	f, err := openNoFollow(path, flag, perm)
	if err != nil && isLinkErr(path, err) {
		return nil, fmt.Errorf("%s: %w", path, ErrLink)
	}
	return f, err
}

// MkdirNoLink creates dir and its parents as os.MkdirAll does, and
// fails with ErrLink when dir is a link, so that what tm then writes in
// it stays where it is named.
func MkdirNoLink(dir string, perm fs.FileMode) error {
	if err := os.MkdirAll(dir, perm); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if isLink(fi) {
		return fmt.Errorf("%s: %w", dir, ErrLink)
	}
	return nil
}

// isLinkErr reports whether err, from opening path, came from path being
// a link.
func isLinkErr(path string, err error) bool {
	if errors.Is(err, ErrLink) {
		return true
	}
	fi, lerr := os.Lstat(path)
	return lerr == nil && isLink(fi)
}
