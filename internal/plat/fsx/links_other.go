//go:build !unix

package fsx

import "os"

// Links is fi's hard link count, unknown (1) here.
func Links(fi os.FileInfo) uint64 { return 1 }

// OpenInNoBlock opens rel under r for reading. Windows has no FIFOs a
// plain open blocks on.
func OpenInNoBlock(r *os.Root, rel string) (*os.File, error) { return r.Open(rel) }

// Mine: ownership isn't checked here.
func Mine(fi os.FileInfo) bool { return true }
