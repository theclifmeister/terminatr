package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// killUnderRuns is killUnder for every run dir this run made.
func killUnderRuns() []string {
	runDirsMu.Lock()
	defer runDirsMu.Unlock()
	var killed []string
	for _, d := range runDirs {
		killed = append(killed, killUnder(d)...)
	}
	return killed
}

// sweepStale kills the processes of dead runs and removes their
// binaries, test homes and run dirs. A bin dir without an owner file (older
// harness, or a run still building) counts as dead only after an hour. A
// run under a thread's approved command has a temp folder of its own
// (hometest.OutOfThreadTemps), new each run: a dead run's goes whole.
func sweepStale() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), binPrefix+"*"))
	others, _ := filepath.Glob(filepath.Join(ipc.ShortDir(), "tmtest-*", binPrefix+"*"))
	for _, d := range append(dirs, others...) {
		if !staleOwner(d) {
			continue
		}
		// The dead run's temp folder holds its run dirs too, and the
		// servers running from their pins.
		if parent := filepath.Dir(d); strings.HasPrefix(filepath.Base(parent), "tmtest-") && !sameDir(parent, os.TempDir()) {
			d = parent
		}
		killUnder(d)
		os.RemoveAll(d)
	}
	// Run dirs of dead runs. A server runs from its pin in there, not
	// from the bin dir. One without an owner file: no live server, and
	// old enough that no running test is between creating it and
	// starting its server.
	runs, _ := filepath.Glob(filepath.Join(runDirBase(), "tme2e*"))
	for _, d := range runs {
		if _, err := os.Stat(filepath.Join(d, ownerFile)); err == nil {
			if staleOwner(d) {
				killUnder(d)
				os.RemoveAll(d)
			}
			continue
		}
		fi, err := os.Stat(d)
		if err != nil || time.Since(fi.ModTime()) < 10*time.Minute {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(d, "server.pid"))
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); !Alive(pid) {
			os.RemoveAll(d)
		}
	}
}

// sameDir reports whether a and b name one folder.
func sameDir(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}

func staleOwner(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ownerFile))
	if err != nil {
		fi, err := os.Stat(dir)
		return err == nil && time.Since(fi.ModTime()) > time.Hour
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid != os.Getpid() && !Alive(pid)
}

// killUnder SIGKILLs every process whose executable lies in dir and
// returns what it killed.
func killUnder(dir string) []string {
	procs, err := proc.List()
	if err != nil {
		return nil
	}
	var killed []string
	prefix := dir + string(filepath.Separator)
	for _, p := range procs {
		if p.PID == os.Getpid() || len(p.Argv) == 0 || !strings.HasPrefix(p.Argv[0], prefix) {
			continue
		}
		if proc.Kill(p.PID) == nil {
			killed = append(killed, strconv.Itoa(p.PID)+" "+strings.Join(p.Argv, " "))
		}
	}
	return killed
}
