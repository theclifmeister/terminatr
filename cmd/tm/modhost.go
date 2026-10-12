package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
	"github.com/theclifmeister/terminatr/internal/plat/sandbox"
)

// modHost runs `tm mod host [--budget D] <entry>`: one code mod, started
// by the server's supervisor (mods design §3), speaking modproto on stdin
// and stdout. Not for people; tm mod dev runs it for them.
func modHost(args []string) int {
	started := time.Now()
	fs := flag.NewFlagSet("tm mod host", flag.ContinueOnError)
	budget := fs.Duration("budget", modhost.DefaultBudget, "CPU budget of each loop slice")
	if err := fs.Parse(args); err != nil {
		return modproto.ExitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tm mod host [--budget D] <bundle.js | main.ts>")
		return modproto.ExitUsage
	}
	// stdout is the protocol's: anything else that prints goes to stderr.
	out := os.Stdout
	os.Stdout = os.Stderr
	script, err := modhost.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tm mod host:", err)
		return modproto.ExitLoad
	}
	// The script is read: from here the host opens no file, socket or
	// program (mods design §7). The time zone is read first, for Date.
	_, _ = time.Now().Zone()
	if err := sandbox.Lockdown(); err != nil {
		fmt.Fprintln(os.Stderr, "tm mod host: running without the seccomp filter:", err)
	}
	return modhost.Run(modhost.Options{
		Name: fs.Arg(0), Script: script, Budget: *budget,
		In: os.Stdin, Out: out, Bind: modapi.Bind, Started: started,
	})
}
