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

// Offsets into struct seccomp_data.
const (
	offNr   = 0
	offArch = 4
	offArg0 = 16 // low word of args[0]
)

// filter is the BPF program: a foreign architecture kills the process,
// a denied syscall fails with EPERM, a signal syscall fails with EPERM
// unless it names the process pid itself, the rest is allowed.
func filter(pid uint32) []unix.SockFilter {
	stmt := func(code uint16, k uint32) unix.SockFilter { return unix.SockFilter{Code: code, K: k} }
	jump := func(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
		return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
	}
	const (
		ld  = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		ret = unix.BPF_RET | unix.BPF_K
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
	for _, nr := range selfOnly {
		// Allowed only when the first argument is this process: the Go
		// runtime signals its own threads with tgkill(getpid(), tid).
		p = append(p,
			jump(jeq, uint32(nr), 0, 4),
			stmt(ld, offArg0),
			jump(jeq, pid, 1, 0),
			stmt(ret, deny),
			stmt(ld, offNr),
		)
	}
	return append(p, stmt(ret, unix.SECCOMP_RET_ALLOW))
}

// deniedCommon are the syscalls refused on every architecture: opening
// files, sockets, running programs, and kernel surfaces a host never
// needs. Architectures add their legacy calls (open, creat…).
var deniedCommon = []uintptr{
	unix.SYS_OPENAT, unix.SYS_OPENAT2, unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT,
	unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_BIND, unix.SYS_LISTEN,
	unix.SYS_EXECVE, unix.SYS_EXECVEAT,
	unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
	unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT, unix.SYS_UNSHARE, unix.SYS_SETNS,
	unix.SYS_KEXEC_LOAD, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE,
	unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_USERFAULTFD, unix.SYS_IO_URING_SETUP,
	unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY,
	unix.SYS_MKNODAT, unix.SYS_UNLINKAT, unix.SYS_RENAMEAT2, unix.SYS_MKDIRAT,
	unix.SYS_SYMLINKAT, unix.SYS_LINKAT, unix.SYS_TRUNCATE,
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
