// Command tm is terminatr: an agent session host, a dashboard and the CLI
// that coordinator and thread agents call. The commands are specified in
// docs/SPEC.md §10; this file dispatches them.
package main

import (
	"fmt"
	"os"
	"strings"

	// The Go parts of built-in agents register themselves.
	_ "github.com/theclifmeister/terminatr/internal/agent/claude"
	"github.com/theclifmeister/terminatr/internal/agent/codex"
	"github.com/theclifmeister/terminatr/internal/cli"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/vault"
	"github.com/theclifmeister/terminatr/internal/version"
)

func main() {
	// Safe mode (mods design §9): no user mods in this tm, nor in a
	// server it starts, which inherits the variable.
	if len(os.Args) > 1 && os.Args[1] == "--no-mods" {
		os.Setenv(mod.EnvNoMods, "1")
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("tm", version.Version, "build", version.BuildID())
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "selftest" {
		if err := cli.Selftest(); err != nil {
			fmt.Fprintln(os.Stderr, "tm selftest:", err)
			os.Exit(1)
		}
		fmt.Println("libghostty-vt: ok")
		return
	}
	// Front matter is written as YAML once the projects' files have moved
	// (the server's first start with this version); before, an older
	// server that reads only TOML may still run.
	mdfile.WriteYAML(vault.Migrated())
	// Run by Codex's sandbox when tm checks a coordinator's profile
	// (internal/agent/codex/probe.go); not for people.
	if len(os.Args) > 1 && os.Args[1] == codex.ProbeCommand {
		os.Exit(codex.ProbeMain(os.Args[2:]))
	}
	// A code mod's host, started by the server (internal/modhost).
	if len(os.Args) > 2 && os.Args[1] == "mod" && os.Args[2] == "host" {
		os.Exit(modHost(os.Args[3:]))
	}
	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Println(usage())
		return
	}
	if code, ok := cli.Run(os.Args[1:]); ok {
		os.Exit(code)
	}
	fmt.Fprintf(os.Stderr, "tm: unknown command %q\n%s\n", os.Args[1], usage())
	os.Exit(2)
}

// own are the commands main runs itself, before the cli package.
var own = []string{"version", "selftest"}

// usage lists every command, from cli's table, so it can't drift.
func usage() string {
	return "usage: tm [--no-mods] [--own]   (the dashboard)\n" +
		"       tm [--no-mods] <command> ...\n" +
		"--no-mods is safe mode: a server this tm starts runs no user mods\n" +
		"commands: " + strings.Join(append(append([]string{}, own...), cli.Commands()...), " | ") + "\n" +
		"docs/SPEC.md §10 lists every command and flag"
}
