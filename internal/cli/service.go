package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/pin"
	"github.com/theclifmeister/terminatr/internal/service"
)

const serviceUsage = `usage: tm server service install|uninstall [--print]`

// serviceConfig is the service for this tm and this user. serviceRun,
// when set (tests), replaces launchctl and systemctl.
var serviceRun func(name string, args ...string) error

func serviceConfig(e *Env) (service.Config, error) {
	p, err := client.ResolvePaths()
	if err != nil {
		return service.Config{}, err
	}
	c, err := service.Current(e.Getenv, os.Environ(), p.Home, p.RunDir, filepath.Dir(p.Log))
	c.Run = serviceRun
	return c, err
}

// serverService implements `tm server service install|uninstall`
// (docs/SPEC.md §3.1): optional start at login, off by default.
func serverService(e *Env, args []string) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall") {
		return srvUsage("server service", serviceUsage)
	}
	f := newFlags()
	printArg := f.Bool("print") // print the service file instead of installing it
	dryRun := f.Bool("dry-run") // same as --print
	if pos, err := f.Parse(args[1:]); err != nil || len(pos) > 0 {
		return srvUsage("server service", serviceUsage)
	}
	print := *printArg || *dryRun
	c, err := serviceConfig(e)
	if err != nil {
		return srvFail("server service", err)
	}
	path, err := c.File()
	if err != nil {
		return srvFail("server service", err)
	}
	if print {
		if args[0] == "uninstall" {
			fmt.Fprintf(e.Stdout, "would unload and remove %s\n", path)
			return nil
		}
		data, err := c.Render()
		if err != nil {
			return srvFail("server service", err)
		}
		fmt.Fprintf(e.Stdout, "# %s\n%s", path, data)
		return nil
	}
	if args[0] == "install" {
		if c.GOOS == "darwin" && c.RunDir != "" {
			// The job runs the pin.
			if err := pin.Ensure(c.RunDir, c.Bin); err != nil {
				return srvFail("server service install", err)
			}
		}
		if _, err := c.Install(); err != nil {
			return srvFail("server service install", err)
		}
		fmt.Fprintf(e.Stdout, "installed %s; the server now starts at login\n", path)
		return nil
	}
	if _, err := c.Uninstall(); err != nil {
		return srvFail("server service uninstall", err)
	}
	fmt.Fprintf(e.Stdout, "removed %s; the server starts on demand again\n", path)
	return nil
}
