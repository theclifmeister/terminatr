//go:build unix

package hometest

import "syscall"

// CIUmask sets the process umask to 022, Linux CI's, so a test that hands
// a bare t.TempDir() (then 0755) to a private store fails on a developer's
// machine with a stricter umask (077 makes it 0700) as it would on CI.
func CIUmask() { syscall.Umask(0o022) }
