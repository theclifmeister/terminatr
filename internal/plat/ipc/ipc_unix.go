//go:build unix

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
)

// umaskMu serialises the umask change around bind; the umask is the
// process's, not the goroutine's.
var umaskMu sync.Mutex

// Listen listens on a, readable and writable by this user only. Nothing
// may be at a: the caller removes a stale socket first, when it knows it
// is stale. Closing the listener leaves the socket file, which a successor
// may already have replaced; the owner removes it.
func Listen(a Addr) (net.Listener, error) {
	// The socket is created under a umask that leaves only the owner's
	// access, so it is never connectable by others, even briefly.
	umaskMu.Lock()
	old := syscall.Umask(0o177)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: string(a), Net: "unix"})
	syscall.Umask(old)
	umaskMu.Unlock()
	if err != nil {
		return nil, err
	}
	ln.SetUnlinkOnClose(false)
	if err := os.Chmod(string(a), 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Dial connects to a, ours or a peer's (an agent's own socket, as named
// in its status file). ctx bounds the connect only.
func Dial(ctx context.Context, a Addr) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", string(a))
}

// IsAbsent reports whether a Dial error means nothing listens at the
// address: no socket there, or one nobody accepts on (left by a process
// that died).
func IsAbsent(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// ownUID is this process's uid, which PeerOf compares the peer's with;
// a test stands in another.
var ownUID = os.Getuid

// PeerOf returns the process at the other end of c, a connection
// accepted from Listen.
func PeerOf(c net.Conn) (Peer, error) {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return Peer{}, fmt.Errorf("peer of %T: not a socket", c)
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var uid, pid int
	var serr error
	if err := raw.Control(func(fd uintptr) { uid, pid, serr = peerCred(int(fd)) }); err != nil {
		return Peer{}, err
	}
	if serr != nil {
		return Peer{}, serr
	}
	return Peer{PID: pid, SameUser: uid == ownUID()}, nil
}

// ShortDir is a short directory every user can make a socket directory
// in, for a run directory whose usual place is too long: /tmp (macOS's
// per-user temp dir is too long itself).
func ShortDir() string { return "/tmp" }
