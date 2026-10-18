package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// Exit codes (docs/SPEC.md §6.3), shared by every command.
const (
	ExitOK      = 0 // done, or already true
	ExitRefused = 1 // refused: some items failed, valid ones were saved
	ExitUsage   = 2 // usage error; nothing saved
	ExitIO      = 3 // I/O or server error; check with list and retry
)

// Env is everything a command touches outside the files, so tests can run
// commands in-process.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	Cwd    string
	Caller caller.Caller
	// InServer: the command runs in the server for an agent (forward), so
	// opens files with the human's rights, not the agent's.
	InServer bool
	// Identify asks the server who the caller is (§11.1); ok is false
	// when no server answers. Nil skips the check.
	Identify func() (c caller.Caller, ok bool)
}

// OSEnv is the process's real environment.
func OSEnv() *Env {
	cwd, _ := os.Getwd()
	return &Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, Cwd: cwd, Caller: caller.FromEnv(),
		Identify: identifyByServer,
	}
}

// Run runs a tm subcommand from this package with the process's
// environment. handled is false if args name no command of this package,
// so cmd/tm can try others.
//
// An agent's project command runs inside the server (forward), which
// tells the caller from the process tree and writes the project folder
// that a sandboxed thread can't. Without a server it runs here.
func Run(args []string) (code int, handled bool) {
	e := OSEnv()
	if len(args) > 0 && forwarded[args[0]] && e.Getenv(caller.EnvSession) != "" {
		if code, ok := e.forward(args); ok {
			return code, true
		}
	}
	return e.Run(args)
}

type command func(e *Env, args []string) error

var commands = map[string]command{
	"project": runProject,
	"task":    runTask,
	"context": runContext,
	"inbox":   runInbox,
	"ask":     runAsk,
	"clear":   runClear,
	"library": runLibrary,
	"memory":  runMemory,
	"link":    runLink,
	"skill":   runSkill,
	"thread":  runThread,
	"report":  runReport,
	"status":  runStatus,
	"done":    runDone,
	"mcp":     runMCPCmd,
}

// Commands returns the names of this package's commands, sorted, for
// cmd/tm's usage line.
func Commands() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checked are the commands whose rights depend on the caller: for them
// the server's view of the caller is combined with the environment's.
// Every forwarded command is checked too, so one run here, its session's
// variables unset, is still narrowed to the agent the server sees.
var checked = map[string]bool{"mod": true, "skill": true}

// Run dispatches args (without the program name).
func (e *Env) Run(args []string) (code int, handled bool) {
	if len(args) == 0 || len(args) == 1 && args[0] == "--own" {
		return e.dashboardCmd(len(args) == 1), true
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return 0, false
	}
	if (checked[args[0]] || forwarded[args[0]]) && e.Identify != nil {
		if c, ok := e.Identify(); ok {
			e.Caller = caller.Narrower(e.Caller, c)
		}
	}
	err := cmd(e, args[1:])
	if isHelp(err) {
		fmt.Fprintln(e.Stdout, commandUsage(args[0]))
		return ExitOK, true
	}
	return e.report("tm "+args[0], err), true
}

// helpError is -h or --help on a command line: a request for usage, not
// a mistake. Run prints the command's usage on stdout and exits 0.
type helpError struct{}

func isHelp(err error) bool {
	var h *helpError
	return errors.As(err, &h)
}

func (*helpError) Error() string { return "usage requested" }

// commandUsage is the usage text of command name, the same a bare
// command prints.
func commandUsage(name string) string {
	switch name {
	case "access":
		return accessUsage
	case "agent":
		return agentUsage
	case "ask":
		return askUsage
	case "attach":
		return attachUsage
	case "clear":
		return clearUsage
	case "context":
		return contextUsage
	case "done":
		return `usage: tm done ["summary"] [--project <slug>] [--thread <id>]`
	case "doctor":
		return doctorUsage
	case "inbox":
		return inboxUsage
	case "library":
		return libraryUsage
	case "link":
		return linkUsage
	case "memory":
		return memoryUsage
	case "mcp":
		return "usage: tm mcp   (an MCP server on stdio: a thread's tools)"
	case "mod":
		return modUsage
	case "peer":
		return peerUsage
	case "project":
		return projectUsage
	case "relay":
		return relayUsage
	case "report":
		return reportUsage
	case "server":
		return serverUsage
	case "session":
		return sessionUsage
	case "skill":
		return skillUsage
	case "status":
		return statusUsage
	case "task":
		return taskUsage
	case "thread":
		return threadUsage
	case "update":
		return updateUsage
	case "watch":
		return watchUsage
	}
	return "usage: tm " + name + " [--help]"
}

// usageError is exit 2: the command line was wrong and nothing was saved.
type usageError struct{ msg string }

func (u *usageError) Error() string { return u.msg }

// helpOr serves a command's help prologue: no arguments is a usage
// error, help, -h or --help prints usage. It reports whether args were
// the help request, and then the command ends with err.
func helpOr(e *Env, args []string, usage string) (bool, error) {
	if len(args) == 0 {
		return true, usagef("%s", usage)
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(e.Stdout, usage)
		return true, nil
	}
	return false, nil
}

// fromProto maps a server's *proto.Error to the project.Error the CLI
// prints and exits on; any other error passes through.
func fromProto(err error) error {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return &project.Error{Code: pe.Code, Msg: pe.Message}
	}
	return err
}

func usagef(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

// exitError carries a code for an outcome that was already printed, such
// as a bulk add with some failed items.
type exitError struct{ code int }

func (x *exitError) Error() string { return fmt.Sprintf("exit %d", x.code) }

// report prints err and maps it to an exit code.
func (e *Env) report(prefix string, err error) int {
	var u *usageError
	var r *tasks.Error
	var x *exitError
	var c *cmdError
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, &x):
		return x.code
	case errors.As(err, &c):
		fmt.Fprintf(e.Stderr, "tm %s: %v\n", c.cmd, c.err)
		return srvCode(c.err)
	case errors.As(err, &u):
		fmt.Fprintf(e.Stderr, "%s: %s\n", prefix, u.msg)
		return ExitUsage
	case errors.As(err, &r):
		fmt.Fprintf(e.Stderr, "%s: %s: %s\n", prefix, r.Code, r.Msg)
		return ExitRefused
	default:
		fmt.Fprintf(e.Stderr, "%s: %v\n", prefix, err)
		return ExitIO
	}
}

func (e *Env) printJSON(v any) error {
	enc := json.NewEncoder(e.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// flagSet is a small parser that, unlike package flag, accepts flags
// after positional arguments ("tm task status T12 blocked --note x").
type flagSet struct {
	bools map[string]*bool
	strs  map[string]*string
	lists map[string]*[]string
	set   map[string]bool
	// stopAtArg ends flag parsing at the first positional argument too,
	// for a command whose rest is another command's own command line
	// ("tm session start bash -l").
	stopAtArg bool
}

func newFlags() *flagSet {
	return &flagSet{bools: map[string]*bool{}, strs: map[string]*string{}, lists: map[string]*[]string{}, set: map[string]bool{}}
}

func (f *flagSet) Bool(name string) *bool     { v := new(bool); f.bools[name] = v; return v }
func (f *flagSet) String(name string) *string { v := new(string); f.strs[name] = v; return v }
func (f *flagSet) List(name string) *[]string { v := new([]string); f.lists[name] = v; return v }
func (f *flagSet) IsSet(name string) bool     { return f.set[name] }
func (f *flagSet) anySet(names ...string) bool {
	for _, n := range names {
		if f.set[n] {
			return true
		}
	}
	return false
}

// Parse returns the positional arguments. "--" ends flag parsing. A
// flag is --name or -name; a value follows as --name=v or --name v.
func (f *flagSet) Parse(args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(pos, args[i+1:]...), nil
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			if f.stopAtArg {
				return append(pos, args[i:]...), nil
			}
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if b, ok := f.bools[name]; ok {
			if hasVal {
				return nil, usagef("--%s takes no value", name)
			}
			*b, f.set[name] = true, true
			continue
		}
		s, isStr := f.strs[name]
		l, isList := f.lists[name]
		if !isStr && !isList {
			if name == "h" || name == "help" {
				return nil, &helpError{}
			}
			return nil, usagef("unknown flag %s", a)
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, usagef("--%s needs a value", name)
			}
			i++
			val = args[i]
		}
		f.set[name] = true
		if isStr {
			*s = val
		} else {
			*l = append(*l, val)
		}
	}
	return pos, nil
}

// readArg reads a file argument; "-" is stdin.
func (e *Env) readArg(path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(e.Stdin)
	} else {
		var f *os.File
		if f, err = e.openArg(path); err != nil {
			return "", err
		}
		data, err = io.ReadAll(f)
		f.Close()
	}
	if err != nil {
		return "", usagef("%v", err)
	}
	return string(data), nil
}

// abs resolves a path argument against the caller's cwd, which inside the
// server is not the process's own.
func (e *Env) abs(path string) string {
	if filepath.IsAbs(path) || e.Cwd == "" {
		return path
	}
	return filepath.Join(e.Cwd, path)
}
