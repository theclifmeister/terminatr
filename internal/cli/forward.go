package cli

import (
	"bytes"
	"io"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// forwarded are the project commands an agent runs through the server
// (docs/SPEC.md §11.1).
var forwarded = map[string]bool{
	"project": true, "task": true, "context": true, "inbox": true, "library": true, "memory": true, "ask": true, "clear": true, "link": true,
	"thread": true, "report": true, "status": true, "done": true,
}

// accessWrites are the tm access subcommands that write config.toml.
var accessWrites = map[string]bool{"add": true, "rm": true, "edit": true, "drop": true, "undrop": true, "set": true, "reset": true}

// forwardedAccess reports whether args are a tm access write a
// coordinator runs through the server (coordinator_access): the server
// writes config.toml, which a coordinator's sandbox (Codex's) keeps
// read-only, with the caller it tells from the process tree; accessFor
// checks it there.
func forwardedAccess(args []string, getenv func(string) string) bool {
	return getenv(caller.EnvRole) == string(caller.Coordinator) && accessWrite(args)
}

// accessWrite reports whether args are a tm access write.
func accessWrite(args []string) bool {
	if len(args) < 2 || args[0] != "access" {
		return false
	}
	return accessWrites[args[1]] || args[1] == "preset" && len(args) > 2 && args[2] == "add"
}

// needsStdin reports whether a command reads stdin, so the client sends
// it along. Anything else gets none: an agent's tool shell may hold
// stdin open without writing to it.
func needsStdin(args []string) bool {
	for i, a := range args {
		if a == "-" && i > 0 && (args[i-1] == "--file" || args[i-1] == "--notes-file") {
			return true
		}
		if a == "--file=-" || a == "--notes-file=-" {
			return true
		}
	}
	switch args[0] {
	case "report":
		for _, a := range args[1:] {
			if a == "--show" || a == "--file" || strings.HasPrefix(a, "--file=") || a == "--help" || a == "-h" {
				return false
			}
		}
		return true
	case "task":
		if len(args) > 1 && args[1] == "add" {
			n := 0
			json := false
			for _, a := range args[2:] {
				if a == "--json" {
					json = true
				} else if !strings.HasPrefix(a, "-") {
					n++
				}
			}
			return json && n == 0
		}
	}
	return false
}

// forward runs args in the server. ok is false when no server answers;
// the command then runs here, with the caller from the environment.
func (e *Env) forward(args []string) (code int, ok bool) {
	p, err := client.ResolvePaths()
	if err != nil {
		return 0, false
	}
	c, err := client.Connect(p, false)
	if err != nil {
		return 0, false
	}
	defer c.Close()
	params := proto.CLIRunParams{Args: args, Cwd: e.Cwd}
	if needsStdin(args) {
		params.Stdin, _ = io.ReadAll(e.Stdin)
	}
	var res proto.CLIRunResult
	if err := c.Call(proto.MethodCLIRun, params, &res); err != nil {
		return e.report("tm "+args[0], srvFail(args[0], err)), true
	}
	io.WriteString(e.Stdout, res.Stdout)
	io.WriteString(e.Stderr, res.Stderr)
	return res.Code, true
}

// RunInServer runs a forwarded command for server.Options.RunCLI. The
// caller comes from the server; the TERMINATR_* variables a command
// reads are derived from it, never from the server's own environment.
func RunInServer(p proto.CLIRunParams, c caller.Caller) proto.CLIRunResult {
	var out, errb bytes.Buffer
	e := &Env{
		Stdin: bytes.NewReader(p.Stdin), Stdout: &out, Stderr: &errb, Cwd: p.Cwd, Caller: c, InServer: true,
		Getenv: func(k string) string {
			switch k {
			case caller.EnvProject:
				return c.Project
			case caller.EnvThread:
				return c.Thread
			}
			return ""
		},
	}
	res := proto.CLIRunResult{}
	if !forwarded[p.Args[0]] && !(c.Kind == caller.Coordinator && accessWrite(p.Args)) {
		res.Code = e.report("tm", usagef("%s is not a project command", p.Args[0]))
	} else {
		res.Code, _ = e.Run(p.Args)
	}
	res.Stdout, res.Stderr = out.String(), errb.String()
	return res
}
