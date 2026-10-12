package sandbox

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// childEnv makes the test binary the confined program: it tries what a
// mod host must not do and prints one line per probe.
const childEnv = "TM_SANDBOX_TEST_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		os.Exit(child(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// child runs the probes: args are the allowed file, a secret file, a
// folder to write in and a TCP address to dial.
func child(args []string) int {
	allowed, secret, dir, addr := args[0], args[1], args[2], args[3]
	say := func(name string, err error) {
		if err != nil {
			fmt.Printf("%s err %v\n", name, err)
		} else {
			fmt.Printf("%s ok\n", name)
		}
	}
	_, err := os.ReadFile(allowed)
	say("read-allowed", err)
	_, _ = time.Now().Zone() // as the host does before Lockdown
	say("lockdown", Lockdown())
	_, err = os.ReadFile(secret)
	say("read-secret", err)
	err = os.WriteFile(filepath.Join(dir, "written"), []byte("x"), 0o600)
	say("write", err)
	if addr != "" {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			c.Close()
		}
		say("dial", err)
	}
	run := exec.Command(os.Args[0], "-test.run=^$")
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stderr, os.Stderr // no /dev/null to open
	say("exec", run.Run())
	return 0
}

func TestStartConfines(t *testing.T) {
	if err := Check(); err != nil {
		t.Skip(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	allowed := filepath.Join(root, "bundle.js")
	secret := filepath.Join(root, "secret")
	dir := filepath.Join(root, "data")
	for _, f := range []string{allowed, secret} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	addr := ""
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		defer ln.Close()
		addr = ln.Addr().String()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	} else {
		t.Logf("no listener (%v): the dial probe is skipped", err)
	}

	cmd := exec.Command(exe, allowed, secret, dir, addr)
	cmd.Dir = dir
	cmd.Env = append(bareEnv(), childEnv+"=1")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	p, err := Start(cmd, Policy{Exe: exe, Read: []string{allowed}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	err = cmd.Wait()
	p.Release()
	t.Logf("mode %s\n%s%s", p.Mode, out.String(), stderr.String())
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	got := map[string]string{}
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		name, res, _ := strings.Cut(sc.Text(), " ")
		got[name] = res
	}
	want := map[string]bool{"read-allowed": true, "lockdown": true, "read-secret": false, "write": false, "exec": false}
	if addr != "" {
		want["dial"] = false
	}
	for name, ok := range want {
		res, seen := got[name]
		switch {
		case !seen:
			t.Errorf("%s: no result", name)
		case ok && res != "ok":
			t.Errorf("%s: want ok, got %s", name, res)
		case !ok && res == "ok":
			t.Errorf("%s: allowed, want refused", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "written")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the confined child wrote a file (stat: %v)", err)
	}
}

// bareEnv is what a mod host gets (plat/proc.BareEnv).
func bareEnv() []string {
	if r := os.Getenv("SystemRoot"); r != "" {
		return []string{"SystemRoot=" + r}
	}
	return []string{}
}
