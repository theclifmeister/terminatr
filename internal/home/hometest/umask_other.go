//go:build !unix

package hometest

// CIUmask does nothing where there is no umask.
func CIUmask() {}
