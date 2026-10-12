//go:build linux && !amd64 && !arm64

package sandbox

// No seccomp filter is written for this architecture: landlock alone.
const (
	seccompArch = false
	auditArch   = 0
	x32Bit      = 0
)

var deniedArch []uintptr
