package modbroker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/proc"
)

// Run limits.
const (
	maxArgs       = 256
	maxArgBytes   = 64 << 10
	maxRunOutput  = 1 << 20 // each of stdout and stderr
	defaultRun    = 30 * time.Second
	maxRun        = 120 * time.Second
	runAfterClose = 2 * time.Second
)

// runEnv are the only variables a program a mod runs gets, from the
// server's own environment: where programs and the user's files are, the
// language, and Windows' system folders. No token, key or agent session
// variable is among them, so a gh or glab grant runs on that tool's own
// login, which the mod never sees.
var runEnv = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "TMPDIR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME",
	"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "PATHEXT", "COMSPEC", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "TEMP", "TMP",
}

// ScrubbedEnv is the environment a mod's program runs with: runEnv from
// getenv, then TERM=dumb and NO_COLOR=1.
func ScrubbedEnv(getenv func(string) string) []string {
	var env []string
	for _, k := range runEnv {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "TERM=dumb", "NO_COLOR=1")
}

// RunResult is what process.run answers.
type RunResult struct {
	Code      int    `json:"code"` // the exit code; -1 when killed
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated,omitempty"` // output over maxRunOutput was dropped
	TimedOut  bool   `json:"timed_out,omitempty"`
}

// capped keeps the first n bytes written to it.
type capped struct {
	buf  bytes.Buffer
	n    int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.n - c.buf.Len(); room < len(p) {
		c.over = true
		c.buf.Write(p[:max(room, 0)])
	} else {
		c.buf.Write(p)
	}
	return len(p), nil
}

// RunOptions are process.run's options.
type RunOptions struct {
	Cwd       string `json:"cwd"`
	Stdin     string `json:"stdin"`
	TimeoutMS int    `json:"timeout_ms"`
}

// process.run {argv, options: {cwd, stdin, timeout_ms}} (or the options
// flat): argv[0] is a program the
// mod's commands grant names, by name only; it runs with no shell, the
// scrubbed environment, in the mod's own folder or a folder its files
// grant may read, and is killed (with what it started) at the timeout.
// Its arguments may not name a file or folder the mod couldn't read
// (Paths.CheckArgs), nor point git elsewhere (CheckRun).
func run(ctx context.Context, c *Call) (any, error) {
	var a struct {
		Argv []string `json:"argv"`
		RunOptions
		Options *RunOptions `json:"options"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if a.Options != nil {
		a.RunOptions = *a.Options
	}
	if len(a.Stdin) > maxRunOutput {
		return nil, BadParams("process.run: the stdin is too long")
	}
	var progs []string
	if c.Mod.Manifest != nil {
		progs = c.Mod.Manifest.Permissions.Commands
	}
	if err := CheckRun(a.Argv, progs); err != nil {
		return nil, err
	}
	dir := c.Paths.Own
	if a.Cwd != "" {
		real, err := c.Paths.Check(a.Cwd, false)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			return nil, NotFound("process.run: %s isn't a folder", a.Cwd)
		}
		dir = real
	}
	if err := c.Paths.CheckArgs(a.Argv[1:], dir); err != nil {
		return nil, err
	}
	timeout := defaultRun
	if a.TimeoutMS > 0 {
		timeout = min(time.Duration(a.TimeoutMS)*time.Millisecond, maxRun)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// exec looks the name up in the server's PATH, the one the program
	// gets, and refuses a match in the working folder (exec.ErrDot).
	cmd := exec.CommandContext(ctx, a.Argv[0], a.Argv[1:]...)
	cmd.Dir = dir
	cmd.Env = ScrubbedEnv(os.Getenv)
	if root := c.Paths.Root(dir); root != "" {
		// git looks for its repository from dir up to the root it is
		// in, never in a folder above it.
		cmd.Env = append(cmd.Env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(root))
	}
	cmd.Stdin = strings.NewReader(a.Stdin)
	out, errOut := &capped{n: maxRunOutput}, &capped{n: maxRunOutput}
	cmd.Stdout, cmd.Stderr = out, errOut
	cmd.WaitDelay = runAfterClose
	proc.Group(cmd)
	c.Audit("process.run %s in %s", strings.Join(a.Argv, " "), dir)
	err := cmd.Run()
	res := RunResult{Stdout: out.buf.String(), Stderr: errOut.buf.String(), Truncated: out.over || errOut.over}
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		res.Code, res.TimedOut = -1, true
	case errors.As(err, &exit):
		res.Code = exit.ExitCode()
	case err != nil:
		return nil, Failed("process.run %s: %v", a.Argv[0], err)
	}
	return res, nil
}
