package server

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// The PATH a write folder is checked against at launch (T574 case 2): a
// thread's files in a folder on the user's PATH run as the user. The
// server's own PATH may be launchd's minimal one, so the user's login
// shell's is added: resolved once, in the background from the server's
// start, as an interactive login shell (its .zshrc additions count).

// loginPathMarker heads the PATH line in the shell's output, which may
// carry a banner or prompt noise before it.
const loginPathMarker = "__tm_login_path__="

// loginPathWait bounds the shell's run.
const loginPathWait = 5 * time.Second

var (
	loginPathOnce sync.Once
	loginPathDirs []string
)

// loginPath is the user's login shell's PATH folders, none when it
// can't be had (no $SHELL, Windows, a shell that fails or hangs).
func loginPath() []string {
	loginPathOnce.Do(func() {
		shell := os.Getenv("SHELL")
		if caps.OS == "windows" || shell == "" || !filepath.IsAbs(shell) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), loginPathWait)
		defer cancel()
		cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '\n%s%s\n' "`+loginPathMarker+`" "$PATH"`)
		cmd.Stdin = nil
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			return
		}
		loginPathDirs = parseLoginPath(out)
	})
	return loginPathDirs
}

// parseLoginPath is the PATH folders of the shell's output: the last
// line that starts with loginPathMarker.
func parseLoginPath(out []byte) []string {
	var path string
	for _, l := range bytes.Split(out, []byte("\n")) {
		if v, ok := strings.CutPrefix(strings.TrimRight(string(l), "\r"), loginPathMarker); ok {
			path = v
		}
	}
	if path == "" {
		return nil
	}
	return filepath.SplitList(path)
}

// useLoginPath makes the write folders' PATH check take the user's login
// shell's PATH too, and starts resolving it.
func useLoginPath() {
	go loginPath()
	config.SetMorePath(loginPath)
}
