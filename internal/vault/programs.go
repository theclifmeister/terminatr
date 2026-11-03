package vault

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Programs in the vault (the programs design §3.4): _programs/<slug>/
// follows the vault rules of a project's folder. Its derived notes are
// rendered (RenderProgram), the human reaches it through _human/<slug>/,
// for: <slug> and #for-tm/<slug> (ScanHuman), and its journal is in the
// daily notes (RenderDaily). All of that only while programs are on
// ([features] programs): off, _programs/ is still tm's folder, which the
// scan for the human's notes never reads, and nothing routes to it.

// Programs are the programs the vault serves: none while programs are
// off, and none that clashes with a project (project.ProgramLeftOut) or
// whose folder can't be read. cfg is config.Load's (nil reads it).
func Programs(cfg *config.Config) []*program.Program {
	if cfg == nil {
		cfg, _ = config.Load() // a safety error still gives the tables
	}
	if !cfg.ProgramsOn() {
		return nil
	}
	var out []*program.Program
	for _, slug := range project.ProgramSlugs(cfg) {
		p, err := program.Open(slug, cfg)
		if err != nil || p.LeftOut {
			continue
		}
		out = append(out, p)
	}
	return out
}
