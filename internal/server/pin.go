package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// legacyPinDir is where servers before T87 pinned it, under the home.
const legacyPinDir = "server-bin"

// lockFDEnv hands the server lock across the exec into the pin: it
// names the inherited descriptor (takeLock, inheritedLock).
const lockFDEnv = "TERMINATR_SERVER_LOCK_FD"

// execPinned runs the pin in place of this process, keeping the pid
// (launchd's job, the caller's wait) and the lock, so no other server can
// start in between. run is proc.Exec (Options.Exec). It returns only
// on failure, with the lock still held and closed on exec again.
func execPinned(lock *lockFile, pin string, args []string, run func(string, []string, []string) error) error {
	fd, err := lock.Inheritable()
	if err != nil {
		return err
	}
	env := []string{lockFDEnv + "=" + strconv.Itoa(fd)}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, lockFDEnv+"=") {
			env = append(env, kv)
		}
	}
	err = run(pin, append([]string{pin}, args...), env)
	lock.Uninheritable()
	return err
}

// inheritedLock adopts the lock a server handed across its exec into
// the pin (execPinned), or returns nil. The descriptor must be the lock
// file at path; the flock it holds is this process's own.
func inheritedLock(path string) *lockFile {
	v, ok := os.LookupEnv(lockFDEnv)
	if !ok {
		return nil
	}
	os.Unsetenv(lockFDEnv)
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 3 {
		return nil
	}
	l := flock.Inherited(fd, path)
	if l == nil {
		return nil
	}
	return &lockFile{Lock: l, handedOver: true}
}

// removeLegacyPins removes the home's old server-bin directory, which an
// older server or dashboard may still run from during an upgrade: a file
// some live process runs is kept (and so is the directory), and the next
// start tries again. If the process list can't be read, nothing is
// removed. Call it only once the new pin is in place.
func removeLegacyPins(home string) {
	dir := filepath.Join(home, legacyPinDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	procs, err := proc.List()
	if err != nil {
		return
	}
	for _, e := range entries {
		f := filepath.Join(dir, e.Name())
		if !e.Type().IsRegular() || !runs(procs, f) {
			os.Remove(f)
		}
	}
	os.Remove(dir) // fails, harmlessly, while files remain
}

// runs reports whether some process runs the program at path.
func runs(procs []proc.Info, path string) bool {
	for _, p := range procs {
		if p.Exe == path || len(p.Argv) > 0 && p.Argv[0] == path {
			return true
		}
	}
	return false
}
