package cli

// Glue between the server/session commands and the dispatcher in cli.go.
// These commands talk to the server; their errors carry proto error codes
// that srvCode maps onto the shared exit codes.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/keychain"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/service"
)

func init() {
	commands["server"] = runServer
	commands["session"] = runSession
}

func runServer(e *Env, args []string) error  { return serverCmd(e, args) }
func runSession(e *Env, args []string) error { return sessionCmd(e, args) }

// cmdError is an error of the server command cmd ("session start"):
// Env.report prints it as "tm session start: …" and exits with srvCode.
type cmdError struct {
	cmd string
	err error
}

func (c *cmdError) Error() string { return "tm " + c.cmd + ": " + c.err.Error() }
func (c *cmdError) Unwrap() error { return c.err }

// srvFail is err as a failure of command cmd.
func srvFail(cmd string, err error) error { return &cmdError{cmd: cmd, err: err} }

// srvUsage is a usage error (exit 2) of command cmd.
func srvUsage(cmd, msg string) error { return &cmdError{cmd: cmd, err: &usageError{msg}} }

// srvCode is the exit code of a server command's error.
func srvCode(err error) int {
	var u *usageError
	var perr *proto.Error
	var verr *proto.MismatchError
	switch {
	case errors.As(err, &u):
		return ExitUsage
	case errors.Is(err, service.ErrNoConsole):
		return ExitRefused
	case errors.As(err, &verr):
		return ExitIO
	case errors.As(err, &perr):
		switch perr.Code {
		case proto.ErrBadParams:
			return ExitUsage
		case proto.ErrRefused, proto.ErrUnknownSession:
			return ExitRefused
		}
		return ExitIO
	}
	return ExitIO
}

// connect opens a control connection, starting the server if needed,
// with a warning when that start is over SSH and not through launchd
// (warnSSH).
func connect(autostart bool) (*server.Client, server.Paths, error) {
	p, err := server.ResolvePaths()
	if err != nil {
		return nil, p, err
	}
	c, err := server.Connect(p, autostart)
	if c != nil && c.Started && !service.Wanted(runtime.GOOS, os.Getenv) {
		warnSSH(os.Stderr, runtime.GOOS, os.Getenv)
	}
	return c, p, err
}

// warnSSH warns, on macOS, that a server started from an SSH login can't
// give its sessions the keychain (docs/SPEC.md §3.1). It never refuses.
func warnSSH(w io.Writer, goos string, getenv func(string) string) {
	if msg := keychain.StartWarning(goos, getenv); msg != "" {
		fmt.Fprintln(w, "tm: "+msg)
	}
}

// identifyByServer asks a running server who this process is, from its
// pid (docs/SPEC.md §11.1). It never starts a server: read-only task
// commands work without one, and then only the environment decides.
func identifyByServer() (caller.Caller, bool) {
	c, _, err := connect(false)
	if err != nil {
		return caller.Caller{}, false
	}
	defer c.Close()
	var info proto.CallerInfo
	if err := c.Call(proto.MethodCallerWho, nil, &info); err != nil {
		return caller.Caller{}, false
	}
	switch caller.Kind(info.Kind) {
	case caller.Coordinator:
		return caller.Caller{Kind: caller.Coordinator, Project: info.Project}, true
	case caller.Thread:
		return caller.Caller{Kind: caller.Thread, Project: info.Project, Thread: info.Thread}, true
	}
	return caller.Caller{Kind: caller.Human}, true
}
