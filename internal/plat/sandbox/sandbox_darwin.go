package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const mode = "seatbelt"

// sandboxExec applies a seatbelt profile and runs a program under it.
// It is deprecated as a tool but kept by Apple, and is what agents'
// sandboxes on macOS use too.
const sandboxExec = "/usr/bin/sandbox-exec"

// checked is check's answer: a process already in a sandbox (a tm server
// started from a sandboxed shell) can't apply another one.
var checked = sync.OnceValue(func() error {
	if _, err := os.Stat(sandboxExec); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	out, err := exec.Command(sandboxExec, "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s: %s", ErrUnavailable, sandboxExec, strings.TrimSpace(cleanOut(out, err)))
	}
	return nil
})

func check() error { return checked() }

func cleanOut(out []byte, err error) string {
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return err.Error()
}

// start runs cmd through sandbox-exec, which applies the profile and
// then execs cmd's program in place: the pid is the program's.
func start(cmd *exec.Cmd, pol Policy) (*Proc, error) {
	args := []string{"sandbox-exec", "-p", profile(pol, cmd.Path), cmd.Path}
	if len(cmd.Args) > 1 {
		args = append(args, cmd.Args[1:]...)
	}
	cmd.Path, cmd.Args = sandboxExec, args
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Proc{Mode: mode}, nil
}

// profile is pol as a seatbelt profile: deny everything, then allow
// running path, reading the executable, reading pol.Read, and what dyld
// and the Go runtime read to start (system libraries, "/", the time
// zone, sysctls). No file writes, no network, no other program, no
// mach services.
func profile(pol Policy, path string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process-exec")
	for _, s := range spellings(path) {
		fmt.Fprintf(&b, " (literal %s)", quote(s))
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-read*\n")
	for _, f := range append([]string{pol.Exe}, pol.Read...) {
		for _, s := range spellings(f) {
			fmt.Fprintf(&b, "  (literal %s)\n", quote(s))
		}
	}
	b.WriteString(`  (literal "/")
  (subpath "/usr/lib")
  (subpath "/System/Library")
  (subpath "/System/Cryptexes")
  (subpath "/System/Volumes/Preboot/Cryptexes")
  (literal "/private/etc/localtime")
  (subpath "/private/var/db/timezone")
  (subpath "/usr/share/zoneinfo"))
(allow file-map-executable)
(allow sysctl-read)
`)
	return b.String()
}

// quote is s as a seatbelt string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func lockdown() error { return nil }
