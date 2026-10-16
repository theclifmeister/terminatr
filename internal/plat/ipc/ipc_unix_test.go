//go:build unix

package ipc

import (
	"os"
	"syscall"
	"testing"
)

// Dial's errors with no socket at the address and with one nobody
// accepts on.
var errNoSocket, errRefused error = syscall.ENOENT, syscall.ECONNREFUSED

// sockDir is a directory short enough for sockets (t.TempDir is too long
// on macOS).
func sockDir(t *testing.T) string {
	dir, err := os.MkdirTemp(ShortDir(), "tmipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// checkPrivate fails unless only this user can connect to a.
func checkPrivate(t *testing.T, a Addr) {
	if st, err := os.Stat(string(a)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode: %v, %v", st.Mode(), err)
	}
}

// Listen leaves the process umask as it found it.
func TestListenKeepsUmask(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	a := Addr(sockDir(t) + "/s")
	ln, err := Listen(a)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if got := syscall.Umask(0o022); got != 0o022 {
		t.Fatalf("umask after Listen = %o, want 022", got)
	}
	checkPrivate(t, a)
}

// TestPeerOfOtherUser: a peer whose uid isn't ours is not SameUser.
func TestPeerOfOtherUser(t *testing.T) {
	ownUID = func() int { return os.Getuid() + 1 }
	t.Cleanup(func() { ownUID = os.Getuid })
	a := SessionAddr(sockDir(t), "x.sock")
	ln, err := Listen(a)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := Dial(ctx(t), a)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := PeerOf(s)
	if err != nil {
		t.Fatalf("PeerOf: %v", err)
	}
	if p.SameUser || p.PID != os.Getpid() {
		t.Fatalf("peer %+v, want pid %d, another user", p, os.Getpid())
	}
}
