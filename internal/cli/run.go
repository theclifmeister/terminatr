package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const runUsage = `usage: tm run -- <command> [args…]
       tm run --list

A thread asks tm to run one of its project's approved commands outside
its sandbox, in its worktree (docs/SPEC.md §5.2): the command's words
match an approved command word for word (a last * takes any further
arguments), and tm runs them as they are, without a shell, with its own
environment. The output streams back, and tm run exits with the
command's exit code. --list lists the commands this thread may run.
Only the human approves commands (config.toml approved_commands, the
Access tab); a Codex thread may also run its project's excluded
commands this way.`

func init() {
	commands["run"] = runCmd
}

// runCmd implements `tm run`.
func runCmd(e *Env, args []string) error {
	var list bool
	var argv []string
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		return &helpError{}
	case len(args) == 1 && args[0] == "--list":
		list = true
	case len(args) > 1 && args[0] == "--":
		argv = args[1:]
	case len(args) > 0 && args[0] != "--" && args[0][0] != '-':
		argv = args
	default:
		return srvUsage("run", runUsage)
	}
	p, err := client.ResolvePaths()
	if err != nil {
		return srvFail("run", err)
	}
	var res proto.CommandRunResult
	c, err := client.OpenStream(p, proto.MethodCommandRun, proto.CommandRunParams{Argv: argv, List: list}, &res)
	var perr *proto.Error
	if errors.As(err, &perr) && perr.Code == proto.ErrUnknownMethod {
		err = errors.New("the running server can't run approved commands; restart it (tm server restart)")
	}
	if err != nil {
		return srvFail("run", err)
	}
	defer c.Close()
	if list {
		if len(res.Commands) == 0 {
			fmt.Fprintln(e.Stdout, "no approved commands for this thread")
		}
		for _, x := range res.Commands {
			line := x.Command
			if x.Ask {
				line += "   (asks the user first)"
			}
			fmt.Fprintln(e.Stdout, line)
		}
		return nil
	}
	for {
		var ev proto.CommandRunEvent
		if err := c.ReadLine(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("server hung up before the command ended")
			}
			return srvFail("run", err)
		}
		switch ev.Event {
		case proto.EventRunOutput:
			w := e.Stdout
			if ev.Stream == "stderr" {
				w = e.Stderr
			}
			w.Write(ev.Data)
		case proto.EventRunExit:
			if ev.Error != "" {
				fmt.Fprintf(e.Stderr, "tm run: %s\n", ev.Error)
			}
			if ev.Code < 0 {
				return &exitError{code: ExitIO}
			}
			if ev.Code != 0 {
				return &exitError{code: ev.Code}
			}
			return nil
		}
	}
}
