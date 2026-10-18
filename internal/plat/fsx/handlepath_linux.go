package fsx

import (
	"os"
	"strconv"
)

// HandlePath is the path the open file f is at now, as the kernel has
// it (links resolved); ok is false when it can't say.
func HandlePath(f *os.File) (string, bool) {
	p, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	return p, err == nil
}
