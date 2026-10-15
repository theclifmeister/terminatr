package shell

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// CLICommand is exec.CommandContext, except that a program that is a batch
// file (az.cmd, gh.cmd, an npm shim) doesn't have cmd.exe parse its
// arguments as it would (BatchStart): an npm shim runs as node <script>,
// another batch file through cmd.exe with its arguments quoted for it, so
// a URL with & or % reaches it as it is. Set the environment from
// cmd.Environ(), not os.Environ(): it carries the variables the quoting
// needs.
func CLICommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	p, err := exec.LookPath(name)
	if err != nil || !IsBatch(p) {
		return exec.CommandContext(ctx, name, args...)
	}
	s, serr := BatchStart(p, args)
	if serr != nil {
		cmd := exec.CommandContext(ctx, p)
		cmd.Err = serr
		return cmd
	}
	cmd := exec.CommandContext(ctx, s.App, s.Argv[1:]...)
	if s.Line != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: s.Line}
		cmd.Env = append(os.Environ(), s.Env...)
	}
	return cmd
}

// Start is how to start a program: App with Argv, or, when Line is set,
// App with that command line and Env added to its environment.
type Start struct {
	App  string
	Argv []string
	Line string
	Env  []string
}

// BatchStart is how to start the batch file at path (as LookPath found
// it) with args without cmd.exe reading them as commands: an npm shim
// (NodeShim) as node <script> args, with no cmd.exe at all; another one
// through cmd.exe with batchLine's quoting, which refuses an argument it
// can't pass safely.
func BatchStart(path string, args []string) (Start, error) {
	if node, script, ok := NodeShim(path); ok {
		return Start{App: node, Argv: append([]string{node, script}, args...)}, nil
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	line, env, err := batchLine(comspec, path, args)
	if err != nil {
		return Start{}, err
	}
	return Start{App: comspec, Argv: []string{comspec}, Line: line, Env: env}, nil
}
