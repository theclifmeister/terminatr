//go:build !darwin && !linux && !freebsd && !windows

package fsx

// Free can't be told here: -1.
func Free(string) int64 { return -1 }
