package sandbox

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// run interprets prog, as the kernel would, on a struct seccomp_data for
// syscall nr with args, and returns the action.
func run(t *testing.T, prog []unix.SockFilter, arch uint32, nr uintptr, args ...uint64) uint32 {
	t.Helper()
	data := make([]byte, 64)
	binary.LittleEndian.PutUint32(data[offNr:], uint32(nr))
	binary.LittleEndian.PutUint32(data[offArch:], arch)
	for i, a := range args {
		binary.LittleEndian.PutUint64(data[offArg0+8*i:], a)
	}
	var acc uint32
	for pc := 0; pc < len(prog); pc++ {
		in := prog[pc]
		switch in.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			acc = binary.LittleEndian.Uint32(data[in.K:])
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K,
			unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K:
			var hit bool
			switch in.Code &^ (unix.BPF_JMP | unix.BPF_K) {
			case unix.BPF_JEQ:
				hit = acc == in.K
			case unix.BPF_JGE:
				hit = acc >= in.K
			default:
				hit = acc&in.K != 0
			}
			if hit {
				pc += int(in.Jt)
			} else {
				pc += int(in.Jf)
			}
		case unix.BPF_RET | unix.BPF_K:
			return in.K
		default:
			t.Fatalf("pc %d: unknown instruction %#x", pc, in.Code)
		}
	}
	t.Fatal("the program ends without a return")
	return 0
}

func TestFilter(t *testing.T) {
	if !seccompArch {
		t.Skip("no seccomp filter for this architecture")
	}
	const pid, other = 4242, 4343
	prog := filter(pid)
	if len(prog) > unix.BPF_MAXINSNS {
		t.Fatalf("%d instructions, more than the kernel takes", len(prog))
	}
	var (
		allow = uint32(unix.SECCOMP_RET_ALLOW)
		eperm = uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
		nosys = uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS))
	)
	// The Go runtime's flags for a new thread (runtime.cloneFlags).
	const threadFlags = unix.CLONE_VM | unix.CLONE_FS | unix.CLONE_FILES | unix.CLONE_SIGHAND |
		unix.CLONE_SYSVSEM | unix.CLONE_THREAD
	cases := []struct {
		name string
		nr   uintptr
		args []uint64
		want uint32
	}{
		// What the host needs keeps working.
		{"read", unix.SYS_READ, nil, allow},
		{"write", unix.SYS_WRITE, nil, allow},
		{"clone-thread", unix.SYS_CLONE, []uint64{threadFlags}, allow},
		{"fcntl-getfl", unix.SYS_FCNTL, []uint64{0, unix.F_GETFL}, allow},
		{"fcntl-setfl", unix.SYS_FCNTL, []uint64{0, unix.F_SETFL}, allow},
		{"fcntl-getown", unix.SYS_FCNTL, []uint64{0, unix.F_GETOWN}, allow},
		{"ioctl-other", unix.SYS_IOCTL, []uint64{0, unix.TCGETS}, allow},
		{"tgkill-self", unix.SYS_TGKILL, []uint64{pid, 1}, allow},
		{"kill-self", unix.SYS_KILL, []uint64{pid}, allow},
		{"prlimit-zero", unix.SYS_PRLIMIT64, []uint64{0}, allow},
		{"prlimit-self", unix.SYS_PRLIMIT64, []uint64{pid}, allow},
		{"fstat", unix.SYS_FSTAT, nil, allow},

		// Files.
		{"openat", unix.SYS_OPENAT, nil, eperm},
		{"fchmodat", unix.SYS_FCHMODAT, nil, eperm},
		{"fchmodat2", unix.SYS_FCHMODAT2, nil, eperm},
		{"fchmod", unix.SYS_FCHMOD, nil, eperm},
		{"fchownat", unix.SYS_FCHOWNAT, nil, eperm},
		{"fchown", unix.SYS_FCHOWN, nil, eperm},
		{"utimensat", unix.SYS_UTIMENSAT, nil, eperm},
		{"setxattr", unix.SYS_SETXATTR, nil, eperm},
		{"lsetxattr", unix.SYS_LSETXATTR, nil, eperm},
		{"fsetxattr", unix.SYS_FSETXATTR, nil, eperm},
		{"setxattrat", unix.SYS_SETXATTRAT, nil, eperm},
		{"removexattr", unix.SYS_REMOVEXATTR, nil, eperm},
		{"lremovexattr", unix.SYS_LREMOVEXATTR, nil, eperm},
		{"fremovexattr", unix.SYS_FREMOVEXATTR, nil, eperm},
		{"removexattrat", unix.SYS_REMOVEXATTRAT, nil, eperm},

		// Other processes.
		{"kill-other", unix.SYS_KILL, []uint64{other}, eperm},
		{"tgkill-other", unix.SYS_TGKILL, []uint64{other, 1}, eperm},
		{"fcntl-setown", unix.SYS_FCNTL, []uint64{0, unix.F_SETOWN, other}, eperm},
		{"fcntl-setown-ex", unix.SYS_FCNTL, []uint64{0, unix.F_SETOWN_EX}, eperm},
		{"fcntl-setsig", unix.SYS_FCNTL, []uint64{0, unix.F_SETSIG}, eperm},
		{"ioctl-fiosetown", unix.SYS_IOCTL, []uint64{0, fioSetown}, eperm},
		{"ioctl-siocspgrp", unix.SYS_IOCTL, []uint64{0, unix.SIOCSPGRP}, eperm},
		{"prlimit-other", unix.SYS_PRLIMIT64, []uint64{other}, eperm},
		{"setpriority", unix.SYS_SETPRIORITY, nil, eperm},
		{"sched_setaffinity", unix.SYS_SCHED_SETAFFINITY, nil, eperm},
		{"sched_setscheduler", unix.SYS_SCHED_SETSCHEDULER, nil, eperm},
		{"sched_setparam", unix.SYS_SCHED_SETPARAM, nil, eperm},
		{"sched_setattr", unix.SYS_SCHED_SETATTR, nil, eperm},
		{"ioprio_set", unix.SYS_IOPRIO_SET, nil, eperm},
		{"migrate_pages", unix.SYS_MIGRATE_PAGES, nil, eperm},
		{"move_pages", unix.SYS_MOVE_PAGES, nil, eperm},

		// Namespaces and mounts.
		{"clone3", unix.SYS_CLONE3, nil, nosys},
		{"clone-newuser", unix.SYS_CLONE, []uint64{unix.CLONE_NEWUSER | uint64(unix.SIGCHLD)}, eperm},
		{"clone-newns", unix.SYS_CLONE, []uint64{threadFlags | unix.CLONE_NEWNS}, eperm},
		{"clone-newtime", unix.SYS_CLONE, []uint64{unix.CLONE_NEWTIME}, eperm},
		{"unshare", unix.SYS_UNSHARE, nil, eperm},
		{"fsopen", unix.SYS_FSOPEN, nil, eperm},
		{"fsconfig", unix.SYS_FSCONFIG, nil, eperm},
		{"fsmount", unix.SYS_FSMOUNT, nil, eperm},
		{"fspick", unix.SYS_FSPICK, nil, eperm},
		{"move_mount", unix.SYS_MOVE_MOUNT, nil, eperm},
		{"open_tree", unix.SYS_OPEN_TREE, nil, eperm},
		{"open_tree_attr", unix.SYS_OPEN_TREE_ATTR, nil, eperm},
		{"mount_setattr", unix.SYS_MOUNT_SETATTR, nil, eperm},
	}
	for _, c := range cases {
		if got := run(t, prog, auditArch, c.nr, c.args...); got != c.want {
			t.Errorf("%s: action %#x, want %#x", c.name, got, c.want)
		}
	}
	for _, nr := range denied {
		if got := run(t, prog, auditArch, nr); got != eperm {
			t.Errorf("denied syscall %d: action %#x, want EPERM", nr, got)
		}
	}
	if got := run(t, prog, auditArch^1, unix.SYS_READ); got != unix.SECCOMP_RET_KILL_PROCESS {
		t.Errorf("foreign architecture: action %#x, want kill", got)
	}
}
