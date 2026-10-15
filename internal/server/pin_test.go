//go:build unix

package server

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/pin"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// TestPinRunsAndHandsOverLock: the server runs a copy of itself from the
// pin, and the lock goes with it: the descriptor is open across the exec
// and the exec'd server adopts it rather than taking it again.
func TestPinRunsAndHandsOverLock(t *testing.T) {
	t.Setenv(lockFDEnv, "")
	os.Unsetenv(lockFDEnv)
	run := t.TempDir()
	p := client.Paths{Lock: filepath.Join(run, "tm.lock"), PID: filepath.Join(run, "tm.pid")}
	lk, err := takeLock(p)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Unlock()
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	pin, err := pin.Binary(run, self)
	if err != nil {
		t.Fatal(err)
	}
	var argv, env []string
	err = execPinned(lk, pin, []string{"server", "run"}, func(bin string, a, e []string) error {
		if bin != pin {
			t.Errorf("exec %s", bin)
		}
		argv, env = a, e
		// The exec'd image would see the descriptor: it must stay open
		// across exec.
		flags, err := unix.FcntlInt(lk.Fd(), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC != 0 {
			t.Errorf("lock closes on exec: %d %v", flags, err)
		}
		// Run the exec'd server's half in a child holding the descriptor.
		cmd := exec.Command(self, "-test.run=^TestPinHelperAdopt$")
		cmd.Env = append(e, "TM_PIN_HELPER=adopt", "TM_PIN_LOCK="+p.Lock)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("adopting child: %v\n%s", err, out)
		}
		return syscall.ENOEXEC
	})
	if !errors.Is(err, syscall.ENOEXEC) {
		t.Fatalf("execPinned: %v", err)
	}
	if len(argv) != 3 || argv[0] != pin || argv[1] != "server" {
		t.Fatalf("argv %q", argv)
	}
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, lockFDEnv+"=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%s set %d times", lockFDEnv, n)
	}
	// A failed exec leaves the lock closed on exec again.
	if flags, _ := unix.FcntlInt(lk.Fd(), unix.F_GETFD, 0); flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("lock still open across exec after a failed one")
	}
	// Another process still can't take it.
	if _, err := tryLockElsewhere(t, self, p.Lock); err == nil {
		t.Fatal("lock free after the hand-over failed")
	}
}

// tryLockElsewhere tries the lock from another process.
func tryLockElsewhere(t *testing.T, self, lock string) ([]byte, error) {
	cmd := exec.Command(self, "-test.run=^TestPinHelperAdopt$")
	cmd.Env = append(os.Environ(), "TM_PIN_HELPER=try", "TM_PIN_LOCK="+lock)
	return cmd.CombinedOutput()
}

// TestPinHelperAdopt is the exec'd server of TestPinRunsAndHandsOverLock:
// it adopts the lock it inherited, which a fresh tryLock couldn't take.
func TestPinHelperAdopt(t *testing.T) {
	lock := os.Getenv("TM_PIN_LOCK")
	switch os.Getenv("TM_PIN_HELPER") {
	case "adopt":
		if _, err := tryLock(lock); !errors.Is(err, ErrLocked) {
			t.Fatalf("tryLock in the child: %v, want held", err)
		}
		lk := inheritedLock(lock)
		if lk == nil || !lk.handedOver {
			t.Fatal("inherited lock not adopted")
		}
		if os.Getenv(lockFDEnv) != "" {
			t.Fatal("hand-over variable left for sessions")
		}
		if flags, _ := unix.FcntlInt(lk.Fd(), unix.F_GETFD, 0); flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("adopted lock open across exec")
		}
	case "try":
		if _, err := tryLock(lock); err != nil {
			t.Fatal(err)
		}
	}
}

// TestInheritedLockRefusesOtherFiles: a descriptor that isn't the lock
// file isn't adopted.
func TestInheritedLockRefusesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	other, _ := os.Create(filepath.Join(dir, "other"))
	defer other.Close()
	t.Setenv(lockFDEnv, strconv.Itoa(int(other.Fd())))
	if lk := inheritedLock(filepath.Join(dir, "tm.lock")); lk != nil {
		t.Fatal("adopted a descriptor of another file")
	}
	t.Setenv(lockFDEnv, "x")
	if lk := inheritedLock(filepath.Join(dir, "tm.lock")); lk != nil {
		t.Fatal("adopted a malformed descriptor")
	}
}

// TestRemoveLegacyPins: the old server-bin directory goes, except a file a
// live process runs.
func TestRemoveLegacyPins(t *testing.T) {
	if _, err := proc.List(); err != nil {
		t.Skip("cannot list processes: ", err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, legacyPinDir)
	os.MkdirAll(dir, 0o700)
	idle := filepath.Join(dir, "tm-old")
	os.WriteFile(idle, []byte("x"), 0o700)
	removeLegacyPins(home)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("legacy dir kept: %v", err)
	}

	os.MkdirAll(dir, 0o700)
	os.WriteFile(idle, []byte("x"), 0o700)
	live := filepath.Join(dir, "tm-live")
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if b, err := os.ReadFile(self); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(live, b, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(live, "-test.run=^TestPinHelperSleep$")
	cmd.Env = append(os.Environ(), "TM_PIN_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("cannot run copied test binary: ", err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	// The helper prints once it runs; the pin is then a running binary.
	if line, err := bufio.NewReader(out).ReadString('\n'); line != "pin helper running\n" {
		t.Fatalf("helper said %q (%v)", line, err)
	}
	removeLegacyPins(home)
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("running binary removed: %v", err)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Fatalf("idle pin kept: %v", err)
	}
}

// TestPinHelperSleep is the process TestRemoveLegacyPins keeps running.
func TestPinHelperSleep(t *testing.T) {
	if os.Getenv("TM_PIN_HELPER") == "1" {
		fmt.Println("pin helper running")
		time.Sleep(time.Minute)
	}
}
