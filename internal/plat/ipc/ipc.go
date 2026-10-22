// Package ipc is terminatr's local transport: the server's socket, the
// per-session mod sockets, the peer at the other end, and dialing an
// agent's own socket (Claude's messaging socket).
//
// AF_UNIX sockets on every OS, Windows included (10 1803 and later), so
// Claude's mod reaches the same paths there: its fetch only takes a unix
// socket path, never a named pipe. On Windows, Listen restricts the
// socket file with a DACL rather than a mode, and PeerOf gets the pid
// from SIO_AF_UNIX_GETPEERPID and compares the token's user SID.
//
// A socket path must fit sun_path (MaxPath, Fits): 104 bytes on macOS.
// The umask is the whole process's, shared by all goroutines: Listen
// changes it only under umaskMu and keeps the owner's bits, or a folder
// made meanwhile loses its x.
package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

// Addr is a local address: a socket path.
type Addr string

// MaxPath keeps a margin under the sun_path limit (104 bytes on macOS,
// 108 on Linux and Windows, all including the trailing NUL).
const MaxPath = 100

// ServerName is the server socket's name in its run directory.
const ServerName = "tm.sock"

// HumanName is the human socket's name, beside the server socket: the
// same server, for the human's console and CLI only. It is no longer
// than ServerName, so it fits wherever the server socket does. No agent's sandbox
// allows it (docs/SPEC.md §5.2), so the human-only methods answer there
// alone (§11.1).
const HumanName = "human"

// Fits reports whether a socket in dir named name fits the sun_path
// budget.
func Fits(dir, name string) bool {
	return len(filepath.Join(dir, name)) <= MaxPath
}

// maxTempBase is the longest temp folder ShortTemp takes: it leaves
// room under MaxPath for a test's own folder and a run directory's
// sockets below it.
const maxTempBase = 40

// ShortTemp is a folder for tests and their fixtures to make socket
// folders in: the temp folder ($TMPDIR, a thread's own temp folder in
// tm) when it is short enough, else ShortDir (macOS's per-user temp
// folder is too long). Never for a run directory the server and its
// clients must agree on: their temp folders may differ.
func ShortTemp() string {
	if d := filepath.Clean(os.TempDir()); len(d) <= maxTempBase {
		return d
	}
	return ShortDir()
}

// ServerAddr is the server's socket: override when set, else tm.sock in
// runDir. It fails when the path is over the sun_path budget.
func ServerAddr(runDir, override string) (Addr, error) {
	p := override
	if p == "" {
		p = filepath.Join(runDir, ServerName)
	}
	if len(p) > MaxPath {
		return "", fmt.Errorf("socket path %q is %d bytes; unix sockets allow at most %d here", p, len(p), MaxPath)
	}
	return Addr(p), nil
}

// HumanAddr is the human socket beside server socket a. It fails when
// the path is over the sun_path budget.
func HumanAddr(a Addr) (Addr, error) {
	p := filepath.Join(filepath.Dir(string(a)), HumanName)
	if len(p) > MaxPath {
		return "", fmt.Errorf("socket path %q is %d bytes; unix sockets allow at most %d here", p, len(p), MaxPath)
	}
	return Addr(p), nil
}

// SessionAddr is the address of a per-session socket named name in the
// session's runtime directory.
func SessionAddr(runtimeDir, name string) Addr {
	return Addr(filepath.Join(runtimeDir, name))
}

// Peer is the process at the other end of a connection.
type Peer struct {
	PID int
	// SameUser: the peer runs as this process's user.
	SameUser bool
}
