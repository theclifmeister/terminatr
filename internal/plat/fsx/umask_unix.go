//go:build unix

package fsx

import "syscall"

// SetUmask sets the process umask to mask (tests use it to match CI's).
func SetUmask(mask int) { syscall.Umask(mask) }
