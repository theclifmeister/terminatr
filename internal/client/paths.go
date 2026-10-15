package client

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
)

const homeEnv = home.Env

// Paths are the files the server and its clients agree on (docs/SPEC.md
// §3.2, §5.1).
type Paths struct {
	Home     string // TERMINATR_HOME, default ~/.terminatr
	RunDir   string // the socket's directory; also holds the lock and pid file
	Socket   string
	Lock     string // RunDir/server.lock
	PID      string // RunDir/server.pid
	Log      string // Home/logs/server.log
	Sessions string // Home/state/sessions.json
}

// HumanSocket is the human socket beside the server's (ipc.HumanName):
// the human's console and CLI dial it first, and the human-only methods
// answer only there (docs/SPEC.md §11.1).
func (p Paths) HumanSocket() string {
	return filepath.Join(filepath.Dir(p.Socket), ipc.HumanName)
}

// AgentsDir is the directory of the user's agent manifests.
func (p Paths) AgentsDir() string { return filepath.Join(p.Home, "agents") }

// ResolvePaths computes the paths from the process environment. The state
// directory comes from internal/home, the single source of truth for
// TERMINATR_HOME.
//
// The lock and pid file sit next to the socket, so a TERMINATR_SOCKET
// override (how tests isolate a server) also isolates its lock. A custom
// TERMINATR_HOME ignores XDG_RUNTIME_DIR for the same reason: a test home
// must never share a run directory with the user's real server.
func ResolvePaths() (Paths, error) {
	stateDir, err := home.Dir()
	if err != nil {
		return Paths{}, fmt.Errorf("server: %w", err)
	}
	custom := os.Getenv(homeEnv) != ""
	env := Env{TerminatrSocket: os.Getenv("TERMINATR_SOCKET"), TerminatrHome: stateDir, UID: os.Getuid()}
	if runtime.GOOS == "linux" && !custom {
		env.XDGRuntimeDir = os.Getenv("XDG_RUNTIME_DIR")
	}
	if env.TerminatrSocket != "" {
		if env.TerminatrSocket, err = filepath.Abs(env.TerminatrSocket); err != nil {
			return Paths{}, err
		}
	}
	sock, err := SocketPath(env)
	if err != nil {
		return Paths{}, err
	}
	if _, err := ipc.HumanAddr(ipc.Addr(sock)); err != nil {
		return Paths{}, err
	}
	run := filepath.Dir(sock)
	return Paths{
		Home:     stateDir,
		RunDir:   run,
		Socket:   sock,
		Lock:     filepath.Join(run, "server.lock"),
		PID:      filepath.Join(run, "server.pid"),
		Log:      filepath.Join(stateDir, "logs", "server.log"),
		Sessions: filepath.Join(stateDir, "state", "sessions.json"),
	}, nil
}
