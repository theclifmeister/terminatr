package clienttest

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// shortDir is a folder for sockets, whose paths must stay short.
func shortDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "tmct-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestFakes: each fake fails a client the way it says.
func TestFakes(t *testing.T) {
	dir := shortDir(t)

	stale := filepath.Join(dir, "stale.sock")
	StaleSocket(t, stale)
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("stale socket file: %v", err)
	}
	if c, err := net.Dial("unix", stale); err == nil {
		c.Close()
		t.Fatal("a stale socket answered")
	}

	silent := filepath.Join(dir, "silent.sock")
	Silent(t, silent)
	c, err := net.Dial("unix", silent)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("silent said %d bytes, %v", n, err)
	}

	mute := filepath.Join(dir, "mute.sock")
	Mute(t, mute, "v9", "b9")
	m, err := net.Dial("unix", mute)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Write([]byte("{}\n"))
	br := bufio.NewReader(m)
	var hello proto.Hello
	if err := proto.ReadLine(br, &hello); err != nil || hello.Protocol != proto.Protocol || hello.Version != "v9" {
		t.Fatalf("hello %+v %v", hello, err)
	}
	m.Write([]byte(`{"id":1,"method":"ping"}` + "\n"))
	m.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := br.ReadByte(); err == nil {
		t.Fatal("mute answered")
	}

	lock, pid := filepath.Join(dir, "server.lock"), filepath.Join(dir, "server.pid")
	Running(t, lock, pid, 4242)
	if b, _ := os.ReadFile(pid); string(b) != "4242\n" {
		t.Fatalf("pid file %q", b)
	}
	if lk, err := flock.TryLock(lock); err == nil {
		lk.Unlock()
		t.Fatal("the lock is free")
	}
}
