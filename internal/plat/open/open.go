// Package open opens a web address in the user's browser, a vault in
// Obsidian, or a file in its viewer.
package open

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Web reports whether s is an absolute http or https address with a host,
// the only kind the opener is given: anything else (a file path, an app
// scheme, a leading dash) could launch a program instead of a page.
func Web(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || strings.HasPrefix(s, "-") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

// URL starts the platform's opener for addr and returns without waiting.
// It refuses anything but a web address.
func URL(addr string) error {
	if !Web(addr) {
		return errors.New("not a web address")
	}
	return start(addr)
}

// Obsidian starts the platform's opener for an obsidian://open address
// (Obsidian registers the scheme) and returns without waiting. It refuses
// any other address.
func Obsidian(addr string) error {
	if !ObsidianOpen(addr) {
		return errors.New("not an obsidian://open address")
	}
	return start(addr)
}

// File starts the platform's opener for path, a file the user opens in
// their viewer (an image in Preview, a PDF in its reader), and returns
// without waiting. It refuses anything but an absolute path to a
// regular file: the opener never takes it for an option or an address.
func File(path string) error {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n") {
		return errors.New("not an absolute path")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	return start(path)
}

// ObsidianOpen reports whether s is an obsidian://open address with a
// query and nothing that could read as an option or a second argument.
func ObsidianOpen(s string) bool {
	u, err := url.Parse(s)
	return err == nil && strings.HasPrefix(s, "obsidian://open?") && u.RawQuery != "" &&
		!strings.ContainsAny(s, " \t\r\n\"")
}

// start runs the opener for addr, checked by the caller.
func start(addr string) error {
	name, args := command(runtime.GOOS, addr, wsl(), func(n string) bool {
		_, err := exec.LookPath(n)
		return err == nil
	})
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// wsl reports whether this is Linux under Windows Subsystem for Linux.
func wsl() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// command picks the opener for goos; have says whether a program is on
// PATH. Under WSL it is wslview, else explorer.exe. On Windows it is
// rundll32: explorer exits 1 and cmd's start splits the address at &.
func command(goos, url string, inWSL bool, have func(string) bool) (string, []string) {
	switch {
	case goos == "darwin":
		return "open", []string{url}
	case goos == "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case inWSL && have("wslview"):
		return "wslview", []string{url}
	case inWSL:
		return "explorer.exe", []string{url}
	}
	return "xdg-open", []string{url}
}
