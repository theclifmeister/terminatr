package e2e

import "sync"

// A run's binaries live in a tm-e2e-bin* temp dir with an owner file
// naming the test process. Every tm server and session of the run
// executes from there, so a dir whose owner is gone marks what a dead run
// (a test timeout, a killed go test) left behind: Env cleanup never ran
// for it.
const (
	binPrefix = "tm-e2e-bin"
	ownerFile = "owner.pid"
)

var (
	failedMu sync.Mutex
	failed   []string // failed scenarios with their artifacts, for TestMain
)

// runDirs are the run dirs this run made. A server runs from its pin in
// there (run dir/bin/tm, docs/SPEC.md §3.6), not from the bin dir.
var (
	runDirsMu sync.Mutex
	runDirs   []string
)

func noteRunDir(dir string) {
	runDirsMu.Lock()
	defer runDirsMu.Unlock()
	runDirs = append(runDirs, dir)
}

func noteFailed(name, artifacts string) {
	failedMu.Lock()
	defer failedMu.Unlock()
	if artifacts != "" {
		name += " (artifacts: " + artifacts + ")"
	}
	failed = append(failed, name)
}
