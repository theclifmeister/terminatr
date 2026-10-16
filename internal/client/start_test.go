package client

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/version"
)

// testPaths are the run paths of a server in a fresh, short dir (a unix
// socket's path is short).
func testPaths(t *testing.T) Paths {
	dir, err := os.MkdirTemp("", "tmc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return Paths{RunDir: dir, Socket: filepath.Join(dir, "tm.sock"), Lock: filepath.Join(dir, "server.lock"), PID: filepath.Join(dir, "server.pid"), Log: filepath.Join(dir, "server.log")}
}

// TestWaitUpLostRace: the server a client started exits having lost the
// lock to another client's, which is still starting: the client waits for
// that one to answer (T468: it took 4 s on a CI Mac, and the client gave
// up at once).
func TestWaitUpLostRace(t *testing.T) {
	p := testPaths(t)
	lk, err := flock.TryLock(p.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Unlock()
	serveHello(t, p.Socket, 300*time.Millisecond)
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	if err := waitUp(p, exited, p.Log); err != nil {
		t.Fatalf("waitUp: %v", err)
	}
}

// serveHello is a server starting up: after delay it listens on sock
// and answers each connection's hello.
func serveHello(t *testing.T, sock string, delay time.Duration) {
	ln := make(chan net.Listener, 1)
	t.Cleanup(func() {
		if l := <-ln; l != nil {
			l.Close()
		}
	})
	go func() {
		time.Sleep(delay)
		l, err := ipc.Listen(ipc.Addr(sock))
		ln <- l
		if err != nil {
			return
		}
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var hello proto.Hello
				if proto.ReadLine(bufio.NewReader(c), &hello) == nil {
					proto.WriteLine(c, proto.Hello{Protocol: proto.Protocol, Version: version.Version, Build: version.BuildID()})
				}
			}()
		}
	}()
}

// TestConnectWaitsForAStartingServer: a server that holds the lock but
// doesn't listen yet is starting: Connect waits for it, and removes
// none of its files, which a crashed server's would be.
func TestConnectWaitsForAStartingServer(t *testing.T) {
	p := testPaths(t)
	lk, err := flock.TryLock(p.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Unlock()
	if err := os.WriteFile(p.PID, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	serveHello(t, p.Socket, 300*time.Millisecond)
	c, err := Connect(p, false)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	c.Close()
	if c.Started {
		t.Error("Connect says it started the server")
	}
	if ReadPID(p.PID) != os.Getpid() {
		t.Error("Connect removed a starting server's pid file")
	}
}

// TestForceKill: with no server holding the lock, ForceKill removes
// what a crashed one left and says none runs; with the lock held and
// the pid file naming no live process it kills nothing and removes
// nothing.
func TestForceKill(t *testing.T) {
	p := testPaths(t)
	// A pid that was a process's and no longer is.
	child := exec.Command(os.Args[0], "-test.run=^$")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	dead := child.Process.Pid
	stale := func() {
		for _, f := range []string{p.Socket, p.HumanSocket()} {
			if err := os.WriteFile(f, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(p.PID, []byte(strconv.Itoa(dead)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stale()
	if _, err := ForceKill(p); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("no server: %v, want ErrNotRunning", err)
	}
	for _, f := range []string{p.Socket, p.HumanSocket(), p.PID} {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind: %v", f, err)
		}
	}

	stale()
	lk, err := flock.TryLock(p.Lock)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Unlock()
	if pid, err := ForceKill(p); err == nil || pid != dead {
		t.Fatalf("held lock, dead pid: %d, %v; want an error naming %d", pid, err, dead)
	}
	for _, f := range []string{p.Socket, p.HumanSocket(), p.PID} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s removed while the lock is held: %v", f, err)
		}
	}
}

// TestWaitUpExited: with no server holding the lock, a server that exited
// during start is an error at once.
func TestWaitUpExited(t *testing.T) {
	p := testPaths(t)
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	start := time.Now()
	err := waitUp(p, exited, p.Log)
	if err == nil || !strings.Contains(err.Error(), "server exited during start (exit status 1)") {
		t.Fatalf("waitUp: %v", err)
	}
	if d := time.Since(start); d > AutoStartTimeout/2 {
		t.Fatalf("waitUp took %v", d)
	}
}
