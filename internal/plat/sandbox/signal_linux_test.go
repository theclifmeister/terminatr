package sandbox

import (
	"os"
	"syscall"
)

// signalProbes: a signal to another process (the parent) is refused, to
// the process itself it works.
func signalProbes(say func(string, error)) {
	say("signal", syscall.Kill(os.Getppid(), 0))
	say("signal-self", syscall.Kill(os.Getpid(), 0))
}
