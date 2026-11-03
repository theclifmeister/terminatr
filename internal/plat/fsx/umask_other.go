//go:build !unix

package fsx

// SetUmask does nothing where there is no umask.
func SetUmask(int) {}
