package fsx

import (
	"os"
	"syscall"
	"unsafe"
)

// fGetPath is fcntl's F_GETPATH.
const fGetPath = 50

// HandlePath is the path the open file f is at now, as the kernel has
// it (links resolved); ok is false when it can't say.
func HandlePath(f *os.File) (string, bool) {
	var buf [1024]byte
	if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), fGetPath, uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
		return "", false
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i]), true
		}
	}
	return "", false
}
