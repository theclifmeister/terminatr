package server

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// The test binary is the supervisor's tm too: run as `mod host …` it is
// a mod host, as cmd/tm's is, with the $ API's bindings (modapi.Bind).
// Run as `scope-probe SOCKET OTHER` inside a session it is a caller of
// session-addressed methods (TestScopeInSession).
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "mod" && os.Args[2] == "host" {
		os.Exit(testHostMain(os.Args[3:]))
	}
	if len(os.Args) == 4 && os.Args[1] == "scope-probe" {
		os.Exit(scopeProbe(os.Args[2], os.Args[3]))
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
	return modhost.Run(modhost.Options{Name: fs.Arg(0), Script: script, Budget: *budget, In: os.Stdin, Out: out, Bind: modapi.Bind})
}
