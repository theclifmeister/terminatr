package client

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
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
	go func() {
		time.Sleep(300 * time.Millisecond)
		ln, err := ipc.Listen(ipc.Addr(p.Socket))
		if err != nil {
			return
		}
		t.Cleanup(func() { ln.Close() })
		for {
			c, err := ln.Accept()
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
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	if err := waitUp(p, exited, p.Log); err != nil {
		t.Fatalf("waitUp: %v", err)
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
