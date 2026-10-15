//go:build !darwin && !windows

package pin

import "os"

// cloneFile hard-links dst to src: nothing else knows a binary by its
// path, and a link costs nothing.
func cloneFile(src, dst string) error {
	return os.Link(src, dst)
}
