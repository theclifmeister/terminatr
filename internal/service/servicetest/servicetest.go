// Package servicetest keeps tests from leaving launchd jobs behind: a test
// that starts a server through launchd registers a dev.terminatr.server.*
// job whose plist lives in a temp dir, and nothing else boots it out.
package servicetest

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/service"
)

// jobs lists the loaded dev.terminatr.server.* labels ("" list off macOS).
func jobs() map[string]bool {
	out := map[string]bool{}
	if runtime.GOOS != "darwin" {
		return out
	}
	b, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && strings.HasPrefix(f[2], service.Label+".") {
			out[f[2]] = true
		}
	}
	return out
}

// Run is a TestMain body: it turns the launchd start off for the whole
// package (service.LaunchEnvVar), runs the tests, then fails the run, after
// booting them out, if they left a server job loaded that wasn't loaded
// before. Use as `os.Exit(servicetest.Run(m))`.
func Run(m *testing.M) int {
	if os.Getenv(service.LaunchEnvVar) == "" {
		os.Setenv(service.LaunchEnvVar, "off")
	}
	before := jobs()
	code := m.Run()
	return Leaked(before, code)
}

// Leaked boots out the server jobs loaded now that are not in before and
// returns 1 if there were any, else code. Run uses it; a TestMain that
// can't use Run takes a snapshot with Snapshot first.
func Leaked(before map[string]bool, code int) int {
	var left []string
	for l := range jobs() {
		if !before[l] {
			left = append(left, l)
			exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), l)).Run()
		}
	}
	if len(left) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests left launchd jobs loaded (booted out now): %s\n"+
			"a test started a server through launchd; set %s=off or boot the job out in t.Cleanup\n",
			strings.Join(left, " "), service.LaunchEnvVar)
		return 1
	}
	return code
}

// Snapshot is the server jobs loaded now, for Leaked.
func Snapshot() map[string]bool { return jobs() }
