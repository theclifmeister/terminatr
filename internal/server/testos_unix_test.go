//go:build !windows

package server

import (
	"syscall"
	"testing"
)

// testExe is a program file's extension.
const testExe = ""

// chmodApplies: a directory without write permission refuses new files.
const chmodApplies = true

// testSh is the POSIX shell sessions run in tests.
func testSh(t testing.TB) string { return "/bin/sh" }

// testMkfifo makes a FIFO at path.
func testMkfifo(t testing.TB, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
}
