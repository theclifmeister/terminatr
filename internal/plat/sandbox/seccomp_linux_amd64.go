package sandbox

import "golang.org/x/sys/unix"

const (
	seccompArch = true
	auditArch   = unix.AUDIT_ARCH_X86_64
	// x32Bit marks the x32 ABI's syscall numbers.
	x32Bit = 0x40000000
)

var deniedArch = []uintptr{unix.SYS_OPEN, unix.SYS_CREAT, unix.SYS_MKNOD, unix.SYS_UNLINK, unix.SYS_RENAME, unix.SYS_RENAMEAT,
	unix.SYS_MKDIR, unix.SYS_RMDIR, unix.SYS_SYMLINK, unix.SYS_LINK,
	unix.SYS_CHMOD, unix.SYS_CHOWN, unix.SYS_LCHOWN, unix.SYS_UTIME, unix.SYS_UTIMES, unix.SYS_FUTIMESAT}
