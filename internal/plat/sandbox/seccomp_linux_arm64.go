package sandbox

import "golang.org/x/sys/unix"

const (
	seccompArch = true
	auditArch   = unix.AUDIT_ARCH_AARCH64
	x32Bit      = 0
)

// arm64 has only the *at syscalls; renameat2 is common.
var deniedArch = []uintptr{unix.SYS_RENAMEAT}
