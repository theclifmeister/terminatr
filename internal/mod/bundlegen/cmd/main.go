// Command cmd writes the bundled code mods (package bundlegen); go
// generate runs it in internal/mod.
package main

import (
	"fmt"
	"os"

	"github.com/theclifmeister/terminatr/internal/mod/bundlegen"
)

func main() {
	if err := bundlegen.Write("../../examples/mods", "bundled"); err != nil {
		fmt.Fprintln(os.Stderr, "bundlegen:", err)
		os.Exit(1)
	}
}
