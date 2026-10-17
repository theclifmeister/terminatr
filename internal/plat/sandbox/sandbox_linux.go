package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const mode = "landlock+seccomp"

// abi is the kernel's landlock ABI version, 0 without landlock.
var abi = sync.OnceValues(func() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("landlock: %w", errno)
	}
	return int(v), nil
})

// check fails only when neither half can apply: seccomp alone (a kernel
// without landlock) still keeps a host from opening files and sockets.
func check() error {
	_, lerr := abi()
	if lerr != nil && !seccompArch {
		return fmt.Errorf("%w: %v, and no seccomp filter for %s", ErrUnavailable, lerr, runtime.GOARCH)
	}
	return nil
}

// start starts cmd from a thread confined by landlock, which the child
// inherits; the thread then ends with its goroutine, never unlocked (the
// Go runtime makes new threads from a clean template thread, not from a
// locked one). no_new_privs, which landlock and seccomp need, is set the
// same way.
func start(cmd *exec.Cmd, pol Policy) (*Proc, error) {
	type result struct {
		mode string
		err  error
	}
	// The confined thread can't open /dev/null for an unset stdio file,
	// as cmd.Start would: open it here.
	if cmd.Stdin == nil || cmd.Stdout == nil || cmd.Stderr == nil {
		null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		defer null.Close()
		if cmd.Stdin == nil {
			cmd.Stdin = null
		}
		if cmd.Stdout == nil {
			cmd.Stdout = null
		}
		if cmd.Stderr == nil {
			cmd.Stderr = null
		}
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		// This thread is never unlocked: it exits with the goroutine.
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			done <- result{err: fmt.Errorf("%w: no_new_privs: %v", ErrUnavailable, err)}
			return
		}
		m := mode
		if err := restrictThread(pol); errors.Is(err, errNoLandlock) {
			m = "seccomp"
		} else if err != nil {
			done <- result{err: fmt.Errorf("%w: %v", ErrUnavailable, err)}
			return
		}
		done <- result{m, cmd.Start()}
	}()
	r := <-done
	if r.err != nil {
		return nil, r.err
	}
	if !seccompArch {
		r.mode = "landlock"
	}
	return &Proc{Mode: r.mode}, nil
}

var errNoLandlock = errors.New("no landlock")

// Access rights by landlock ABI version.
const (
	fsV1 = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM
	readExec = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_EXECUTE
)

// systemReads are what the dynamic loader and the Go runtime read to
// start a cgo build of tm: libraries (read and map, never written), the
// loader's cache and the time zone.
var systemReads = []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/libexec", "/etc/ld.so.cache",
	"/etc/localtime", "/usr/share/zoneinfo"}

// restrictThread confines the calling thread with landlock: every file
// right the kernel knows is handled, and only pol's files and the system
// reads are allowed; TCP bind and connect (ABI 4) and signals and
// abstract unix sockets outside the domain (ABI 6) are refused too.
func restrictThread(pol Policy) error {
	v, err := abi()
	if err != nil {
		return errNoLandlock
	}
	fs := uint64(fsV1)
	if v >= 2 {
		fs |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if v >= 3 {
		fs |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if v >= 5 {
		fs |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	attr := unix.LandlockRulesetAttr{Handled_access_fs: fs}
	size := unsafe.Sizeof(attr.Handled_access_fs)
	if v >= 4 {
		attr.Handled_access_net = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
		size += unsafe.Sizeof(attr.Handled_access_net)
	}
	if v >= 6 {
		attr.Scoped = unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET | unix.LANDLOCK_SCOPE_SIGNAL
		size += unsafe.Sizeof(attr.Scoped)
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))
	allow := func(path string, access uint64) error {
		f, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("landlock rule %s: %w", path, err)
		}
		defer unix.Close(f)
		var st unix.Stat_t
		if err := unix.Fstat(f, &st); err != nil {
			return fmt.Errorf("landlock rule %s: %w", path, err)
		}
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			access |= unix.LANDLOCK_ACCESS_FS_READ_DIR
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access & fs, Parent_fd: int32(f)}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH,
			uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if errno != 0 {
			return fmt.Errorf("landlock rule %s: %w", path, errno)
		}
		return nil
	}
	if err := allow(pol.Exe, readExec); err != nil {
		return err
	}
	for _, p := range pol.Read {
		if err := allow(p, unix.LANDLOCK_ACCESS_FS_READ_FILE); err != nil {
			return err
		}
	}
	for _, p := range systemReads {
		if err := allow(p, readExec); err != nil {
			return err
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock restrict: %w", errno)
	}
	return nil
}

// lockdown installs the seccomp filter on every thread of this process
// (TSYNC): from here on it opens no file, makes no socket and runs no
// program, landlock or not.
func lockdown() error {
	if !seccompArch {
		return nil
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	prog := filter(uint32(os.Getpid()))
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC,
		uintptr(unsafe.Pointer(&fprog)))
	runtime.KeepAlive(prog)
	if errno != 0 {
		return fmt.Errorf("seccomp: %w", errno)
	}
	return nil
}

// Offsets into struct seccomp_data; arguments are read by their low word
// (both architectures with a filter are little-endian).
const (
	offNr   = 0
	offArch = 4
	offArg0 = 16
	offArg1 = 24
)

// filter is the BPF program: a foreign architecture kills the process,
// a denied syscall fails with EPERM, a call refused for its arguments
// (argRules) fails with EPERM, clone3 fails with ENOSYS, the rest is
// allowed.
func filter(pid uint32) []unix.SockFilter {
	stmt := func(code uint16, k uint32) unix.SockFilter { return unix.SockFilter{Code: code, K: k} }
	jump := func(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
		return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
	}
	const (
		ld   = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge  = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		jset = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
		ret  = unix.BPF_RET | unix.BPF_K
	)
	deny := uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	p := []unix.SockFilter{
		stmt(ld, offArch),
		jump(jeq, auditArch, 1, 0),
		stmt(ret, unix.SECCOMP_RET_KILL_PROCESS),
		stmt(ld, offNr),
	}
	if x32Bit != 0 {
		// x32 syscalls on x86_64 are another table: refuse them all.
		p = append(p, jump(jge, x32Bit, 0, 1), stmt(ret, deny))
	}
	for _, nr := range denied {
		p = append(p, jump(jeq, uint32(nr), 0, 1), stmt(ret, deny))
	}
	// clone3's flags are behind a pointer, out of the filter's reach:
	// ENOSYS makes the C library fall back to clone, whose flags are
	// checked below. The Go runtime only uses clone.
	p = append(p, jump(jeq, unix.SYS_CLONE3, 0, 1), stmt(ret, unix.SECCOMP_RET_ERRNO|uint32(unix.ENOSYS)))
	for _, r := range argRules(pid) {
		// Each rule is: not this syscall, skip it; load the argument;
		// test it; refuse; reload the syscall number.
		var test []unix.SockFilter
		n := len(r.vals)
		switch {
		case r.mask != 0:
			test = []unix.SockFilter{jump(jset, r.mask, 0, 1)}
		case r.allow:
			// A match jumps over the remaining tests and the refusal.
			for i, v := range r.vals {
				test = append(test, jump(jeq, v, uint8(n-i), 0))
			}
		default:
			// A match falls to the refusal; the last miss jumps over it.
			for i, v := range r.vals {
				miss := uint8(0)
				if i == n-1 {
					miss = 1
				}
				test = append(test, jump(jeq, v, uint8(n-1-i), miss))
			}
		}
		p = append(p, jump(jeq, uint32(r.nr), 0, uint8(len(test)+3)), stmt(ld, r.off))
		p = append(p, test...)
		p = append(p, stmt(ret, deny), stmt(ld, offNr))
	}
	return append(p, stmt(ret, unix.SECCOMP_RET_ALLOW))
}

// An argRule refuses syscall nr by the argument at offset off: when it
// has one of vals, or with allow when it has none of them, or with mask
// when it has any of those bits.
type argRule struct {
	nr    uintptr
	off   uint32
	vals  []uint32
	allow bool
	mask  uint32
}

// fioSetown is FIOSETOWN, which sets a descriptor's owner as F_SETOWN does.
const fioSetown = 0x8901

// nsFlags are clone's flags for new namespaces, which unshare would make.
const nsFlags = unix.CLONE_NEWNS | unix.CLONE_NEWCGROUP | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC |
	unix.CLONE_NEWUSER | unix.CLONE_NEWPID | unix.CLONE_NEWNET | unix.CLONE_NEWTIME

// argRules keep a host from acting on other processes through calls it
// otherwise needs: the signal calls and prlimit only name the host itself
// (pid, or 0 for prlimit; the Go runtime signals its own threads with
// tgkill(getpid(), tid)), a descriptor's owner (who gets its SIGIO or
// chosen signal) can't be set, and clone makes no namespace.
func argRules(pid uint32) []argRule {
	var rules []argRule
	for _, nr := range selfOnly {
		rules = append(rules, argRule{nr: nr, off: offArg0, vals: []uint32{pid}, allow: true})
	}
	return append(rules,
		argRule{nr: unix.SYS_PRLIMIT64, off: offArg0, vals: []uint32{0, pid}, allow: true},
		argRule{nr: unix.SYS_FCNTL, off: offArg1, vals: []uint32{unix.F_SETOWN, unix.F_SETOWN_EX, unix.F_SETSIG}},
		argRule{nr: unix.SYS_IOCTL, off: offArg1, vals: []uint32{fioSetown, unix.SIOCSPGRP}},
		argRule{nr: unix.SYS_CLONE, off: offArg0, mask: nsFlags},
	)
}

// deniedCommon are the syscalls refused on every architecture: opening
// files, sockets, running programs, changing any file's metadata,
// mounting or entering namespaces, changing another process's
// scheduling, and kernel surfaces a host never needs. Architectures add
// their legacy calls (open, creat, chmod…).
var deniedCommon = []uintptr{
	unix.SYS_OPENAT, unix.SYS_OPENAT2, unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT,
	unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_BIND, unix.SYS_LISTEN,
	unix.SYS_EXECVE, unix.SYS_EXECVEAT,
	unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
	unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT, unix.SYS_UNSHARE, unix.SYS_SETNS,
	unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_FSPICK, unix.SYS_MOVE_MOUNT,
	unix.SYS_OPEN_TREE, unix.SYS_OPEN_TREE_ATTR, unix.SYS_MOUNT_SETATTR,
	unix.SYS_KEXEC_LOAD, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE,
	unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_USERFAULTFD, unix.SYS_IO_URING_SETUP,
	unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
	unix.SYS_MKNODAT, unix.SYS_UNLINKAT, unix.SYS_RENAMEAT2, unix.SYS_MKDIRAT,
	unix.SYS_SYMLINKAT, unix.SYS_LINKAT, unix.SYS_TRUNCATE,
	// Metadata, by path and by descriptor: landlock has no right for it.
	unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2, unix.SYS_FCHMOD, unix.SYS_FCHOWNAT, unix.SYS_FCHOWN,
	unix.SYS_UTIMENSAT, unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR, unix.SYS_SETXATTRAT,
	unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR, unix.SYS_REMOVEXATTRAT,
	// Scheduling and memory placement, which take any process's id.
	unix.SYS_SETPRIORITY, unix.SYS_SCHED_SETAFFINITY, unix.SYS_SCHED_SETSCHEDULER, unix.SYS_SCHED_SETPARAM,
	unix.SYS_SCHED_SETATTR, unix.SYS_IOPRIO_SET, unix.SYS_MIGRATE_PAGES, unix.SYS_MOVE_PAGES,
	// tkill takes a thread id, which cannot be matched against the pid;
	// pidfd_send_signal takes a descriptor.
	unix.SYS_TKILL, unix.SYS_PIDFD_SEND_SIGNAL,
}

// selfOnly are the signal calls whose first argument is a process id:
// refused unless it is the host's own, so a host cannot signal the
// server or another session (landlock only scopes signals from ABI 6).
var selfOnly = []uintptr{
	unix.SYS_KILL, unix.SYS_TGKILL, unix.SYS_RT_SIGQUEUEINFO, unix.SYS_RT_TGSIGQUEUEINFO, unix.SYS_PIDFD_OPEN,
}

var denied = append(deniedCommon, deniedArch...)
