package server

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// boardWaits is what board b of project slug's rows say of the after
// links: each open task's unmet links and how many wait for it. A link
// to another project's task is unmet while its program's deps.json (the
// ticker's) says it is open, programs being on and slug in a program;
// otherwise it says nothing. cfg nil loads the config when a task has
// such a link.
func boardWaits(cfg *config.Config, slug string, b *tasks.Board) tasks.WaitsFor {
	if b == nil {
		return tasks.WaitsFor{}
	}
	var open func(string) bool
	for _, t := range b.Tasks {
		if len(t.AfterX) == 0 {
			continue
		}
		if cfg == nil {
			cfg, _ = config.Load()
		}
		if cfg != nil && features.Programs(cfg) {
			if prog := cfg.ProgramOf(slug); prog != "" {
				deps, _ := program.ReadDeps(prog)
				open = func(ref string) bool { return deps[ref].State == program.DepOpen }
			}
		}
		break
	}
	return b.Waits(open)
}
