package cli

import (
	"encoding/json"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/client"
)

const appUsage = `usage: tm app bridge      the Mac app's bridge: NDJSON on stdin and stdout (docs/SPEC.md §3.8)
       tm app schema     the app API as JSON Schema, for the app's types`

func init() {
	commands["app"] = appCmd
}

// appCmd implements tm app (docs/SPEC.md §3.8). bridge is what the Mac
// app runs as its child: it reaches the server through the human
// socket, starting it through the launchd job when none answers, and
// answers the app API on its stdin and stdout until stdin ends.
func appCmd(e *Env, args []string) error {
	if len(args) != 1 {
		return srvUsage("app", appUsage)
	}
	switch args[0] {
	case "-h", "--help":
		return &helpError{}
	case "bridge":
		p, err := client.ResolvePaths()
		if err != nil {
			return srvFail("app bridge", err)
		}
		if err := appapi.Serve(e.Stdin, e.Stdout, appapi.Options{Paths: p, Autostart: true, Caller: e.Caller}); err != nil {
			return srvFail("app bridge", err)
		}
		return nil
	case "schema":
		enc := json.NewEncoder(e.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(appapi.MakeSchema())
	}
	return srvUsage("app", appUsage)
}
