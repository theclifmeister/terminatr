//go:build !windows

package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// The fake codex's behaviours.
const (
	fakeOK   = `echo "Queued message x for thread y."`
	fakeFail = `echo "WARNING: proceeding" >&2; echo "Error: failed to queue session message: no rollout found" >&2; exit 1`
	fakeHang = `exec sleep 10`
)

// fakeCodex puts a codex on PATH that writes its arguments, NUL
// separated, to the returned file, then runs body.
func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + args + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args
}
