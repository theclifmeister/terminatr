package server

import (
	"fmt"
	"io"
	"os"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/logrot"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// Log rotation: server.log is rotated at logMaxBytes and logKeep old files
// are kept (server.log.1 is the newest).
const (
	logMaxBytes = 10 << 20
	logKeep     = 3
)

// OpenLog opens the rotating server log.
func OpenLog(p client.Paths) (io.WriteCloser, error) {
	l, err := logrot.Open(p.Log, logMaxBytes, logKeep)
	if err != nil {
		return nil, err
	}
	// The service's stderr is the one place left to say so.
	l.Failed = func(err error) { fmt.Fprintf(os.Stderr, "log rotation failed, logging to nowhere: %v\n", err) }
	return l, nil
}

// Respawn starts this binary again with the same arguments, detached
// (proc.StartDetached), and returns once it started. `tm server run
// --detached` uses it when it was launched by hand from a shell, which
// can't become a daemon in place (proc.Detached).
func Respawn() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	p, err := proc.StartDetached(proc.Spec{Argv: append([]string{bin}, os.Args[1:]...)})
	if err != nil {
		return err
	}
	return p.Release()
}
