package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenNoLinks opens rel under the folder root for reading (without
// blocking on a FIFO) and refuses, with ErrLink, any link in the way: a
// component of rel that is a symlink, as seen before the open, and then
// by the open file itself (HandlePath: where the kernel says it is),
// so a link swapped in between the check and the open is caught. Where
// the kernel can't say, the components are looked at again after the open.
// root itself may be a link; it is resolved first.
func OpenNoLinks(root, rel string) (*os.File, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if err := noLinkParts(real, parts); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	f, err := OpenInNoBlock(r, filepath.Join(parts...))
	r.Close()
	if err != nil {
		return nil, err
	}
	if got, ok := HandlePath(f); ok {
		if got != filepath.Join(real, filepath.Join(parts...)) {
			f.Close()
			return nil, fmt.Errorf("%s: %w", filepath.Join(real, rel), ErrLink)
		}
		return f, nil
	}
	if err := noLinkParts(real, parts); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// noLinkParts fails with ErrLink when a prefix of parts under dir is a link.
func noLinkParts(dir string, parts []string) error {
	p := dir
	for _, part := range parts {
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if isLink(fi) {
			return fmt.Errorf("%s: %w", p, ErrLink)
		}
	}
	return nil
}
