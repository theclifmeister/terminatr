package server

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/dop251/goja"

	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// The test binary is the supervisor's tm too: run as `mod host …` it is
// a mod host, as cmd/tm's is, with one test binding, $.call(method,
// params), a raw broker request (the $ API's own bindings come with
// internal/modapi).
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "mod" && os.Args[2] == "host" {
		os.Exit(testHostMain(os.Args[3:]))
	}
	os.Exit(m.Run())
}

func testHostMain(args []string) int {
	fs := flag.NewFlagSet("tm mod host", flag.ContinueOnError)
	budget := fs.Duration("budget", modhost.DefaultBudget, "")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		return modproto.ExitUsage
	}
	out := os.Stdout
	os.Stdout = os.Stderr
	script, err := modhost.Load(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return modproto.ExitLoad
	}
	bind := func(h *modhost.Host) error {
		return h.Dollar().Set("call", func(c goja.FunctionCall) goja.Value {
			return h.VM().ToValue(h.Call(c.Argument(0).String(), c.Argument(1)))
		})
	}
	return modhost.Run(modhost.Options{Name: fs.Arg(0), Script: script, Budget: *budget, In: os.Stdin, Out: out, Bind: bind})
}
