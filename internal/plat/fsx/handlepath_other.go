//go:build !linux && !darwin

package fsx

import "os"

// HandlePath: not known here.
func HandlePath(f *os.File) (string, bool) { return "", false }
