// Package systemd is what `tm relay install` needs of its host: whether
// it can run a systemd service, whether tm runs as root, and systemctl.
package systemd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Check says why this host can't run a systemd service, or nil: it must
// be Linux, booted with systemd (sd_booted's test).
func Check() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("the relay service is for Linux with systemd, and this is %s: run the relay with Docker or by hand instead (docs/OPERATIONS.md, Running a relay)", runtime.GOOS)
	}
	if fi, err := os.Stat("/run/systemd/system"); err != nil || !fi.IsDir() {
		return errors.New("the relay service is for Linux with systemd, and this Linux doesn't run systemd: run the relay with Docker or by hand instead (docs/OPERATIONS.md, Running a relay)")
	}
	return nil
}

// IsRoot reports whether tm runs as root.
func IsRoot() bool { return os.Geteuid() == 0 }

// Systemctl runs systemctl with args; a failure carries its output.
func Systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), msg)
		}
		return fmt.Errorf("systemctl %s: %v", strings.Join(args, " "), err)
	}
	return nil
}
