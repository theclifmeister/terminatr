//go:build !unix && !windows

package fsx

import "os"

// Other systems: the link count and the owner are unknown.

// FileLinks is the open file f's hard link count, unknown (1) here.
func FileLinks(f *os.File) (uint64, error) { return 1, nil }

// FileMine: ownership isn't checked here.
func FileMine(f *os.File) bool { return true }

func mineAt(string, os.FileInfo) bool { return true }

// OpenInNoBlock opens rel under r for reading.
func OpenInNoBlock(r *os.Root, rel string) (*os.File, error) { return r.Open(rel) }
