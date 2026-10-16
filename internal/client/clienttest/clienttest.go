// Package clienttest fakes the broken servers a client of tm's control
// socket must cope with, for the tests of the clients (server.Connect,
// tm hook, tm doctor, the CLI): a socket file nobody serves, a server
// that accepts and never says hello, one that says hello and never
// answers, and the lock and pid file a running server holds.
package clienttest

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// StaleSocket leaves a socket file at path that nobody serves, as a
// server that crashed does.
func StaleSocket(t testing.TB, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
}

// Silent serves the socket at path until the test ends: it accepts
// every connection and never says a word, not even hello.
func Silent(t testing.TB, path string) {
	t.Helper()
	serve(t, path, func(net.Conn) {})
}

// Mute serves the socket at path until the test ends: it says hello, as
// a server of this protocol and version and build would, then reads
// what comes and never answers it.
func Mute(t testing.TB, path, version, build string) {
	t.Helper()
	serve(t, path, func(c net.Conn) {
		br := bufio.NewReader(c)
		br.ReadBytes('\n') // the client's hello
		b, _ := json.Marshal(proto.Hello{Protocol: proto.Protocol, Version: version, Build: build})
		c.Write(append(b, '\n'))
		io.Copy(io.Discard, br) // and never an answer
	})
}

// serve listens on path and hands each connection to conn on its own
// goroutine; the listener and the connections it accepted close when
// the test ends.
func serve(t testing.TB, path string, conn func(net.Conn)) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			go conn(c)
		}
	}()
}

// Running holds the server lock at lock, as a running server does, and
// writes pid to the pid file at pidFile, unless that is ""; the lock is
// let go when the test ends.
func Running(t testing.TB, lock, pidFile string, pid int) {
	t.Helper()
	lk, err := flock.TryLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lk.Unlock() })
	if pidFile == "" {
		return
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
