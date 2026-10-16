// Package ipctest holds test fixtures for code that listens on a socket.
package ipctest

import (
	"os"
	"testing"

	"github.com/theclifmeister/terminatr/internal/plat/ipc"
)

// ShortTempDir returns a new directory under ipc.ShortDir, removed when the
// test ends. t.TempDir is too long for a unix socket path on macOS
// (sun_path holds 104 bytes).
func ShortTempDir(t testing.TB, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp(ipc.ShortDir(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
