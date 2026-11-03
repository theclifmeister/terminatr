// Package features is the one gate of the server-wide opt-ins in
// config.toml's [features] table (the programs design §3.5). Every path
// that acts on a program (the server's program.* methods and director
// sessions, the CLI, the ticker, the vault, the consoles' hello) asks
// Programs, and refuses with OffCode and OffMsg while it says no.
package features

import "github.com/theclifmeister/terminatr/internal/config"

// OffCode is the refusal code of every program path while programs are
// off.
const OffCode = config.ProgramsOff

// OffMsg is that refusal's text, the same everywhere.
const OffMsg = "programs are off on this server; the user turns them on in Settings → General → Programs"

// Key and Table name the switch in config.toml: [features] programs.
const (
	Table = "features"
	Key   = "programs"
)

// Programs reports whether [features] programs is on in cfg; a nil cfg
// (no file, or one that can't be read) is off.
func Programs(cfg *config.Config) bool { return cfg != nil && cfg.Features.Programs }
