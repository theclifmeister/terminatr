// Package sandbox confines a program the OS way (mods design §7, the OS
// sandbox): seatbelt on macOS, landlock plus seccomp on Linux, a
// restricted token in a Job Object on Windows. It is the mod hosts' third
// wall, behind goja's language sandbox and the supervisor's process
// limits: a host needs no file or network access of its own, since the
// broker does its I/O over the protocol pipes.
package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Policy is what a confined program may do: run its own executable and
// read the files in Read. Everything else it opens, writes, connects to
// or runs is refused; the pipes it was started with keep working.
type Policy struct {
	// Exe is the program's executable; it may be read and run.
	Exe string
	// Read are files it may read: a mod host's bundle.
	Read []string
}

// ErrUnavailable wraps why this machine can't confine a program: the
// caller may start it without (and say so).
var ErrUnavailable = errors.New("OS sandbox unavailable")

// Mode names the OS sandbox this build applies ("seatbelt",
// "landlock+seccomp", "restricted token"), or "none".
func Mode() string { return mode }

// Check reports whether Start can confine a program here, and if not,
// why (wrapping ErrUnavailable).
func Check() error { return check() }

// A Proc is a program Start confined.
type Proc struct {
	// Mode is how it was confined (Mode, or less when only part of the
	// sandbox applies, as "seccomp" on a kernel without landlock).
	Mode    string
	release func()
}

// Release frees what holds the sandbox once the program has exited
// (cmd.Wait returned); it is safe on a nil Proc and more than once.
func (p *Proc) Release() {
	if p != nil && p.release != nil {
		p.release()
		p.release = nil
	}
}

// Start starts cmd confined by pol. When this machine can't confine it,
// it returns an error wrapping ErrUnavailable and cmd isn't started: the
// caller may then start it plainly. cmd.Path and pol's paths should be
// absolute.
func Start(cmd *exec.Cmd, pol Policy) (*Proc, error) {
	if err := check(); err != nil {
		return nil, err
	}
	return start(cmd, pol)
}

// Lockdown confines the calling program further, once it has read what
// it needs: on Linux a seccomp filter refuses opening files, sockets and
// running programs. It is a no-op elsewhere, where Start's sandbox
// already covers the whole program.
func Lockdown() error { return lockdown() }

// spellings are what a sandbox matching paths sees on the way to path:
// path made absolute, then each symlink along it (macOS's /var), and the
// resolved path when it differs.
func spellings(path string) []string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	out := []string{path}
	for p := path; ; {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			out = append(out, p)
		}
		up := filepath.Dir(p)
		if up == p {
			break
		}
		p = up
	}
	if real, err := filepath.EvalSymlinks(path); err == nil && real != path {
		out = append(out, real)
	}
	return out
}
