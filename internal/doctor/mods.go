package doctor

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// Mods checks the user mods in ~/.terminatr/mods (mods design §8,
// Doctor): a mod that is broken or needs another mod API, one asking for
// permissions the human hasn't granted, settings it refused, and
// [mods.<id>] tables with no mod installed. Warnings only: a mod never
// stops tm. Unknown [mods.<id>] keys are Settings' (config.Unknown).
func Mods(d Deps) []Check {
	const g = "mods"
	c, _ := config.Load() // Settings reports a broken file
	r, err := mod.Load(c)
	if err != nil {
		return []Check{{Group: g, Name: "mods", Status: Warn, Detail: firstLine(err.Error())}}
	}
	var out []Check
	for _, m := range r.Mods {
		switch m.State {
		case mod.Invalid:
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: "doesn't load: " + firstLine(m.Err.Error())})
			continue
		case mod.Incompatible:
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: firstLine(m.Err.Error())})
			continue
		}
		if m.State == mod.Enabled && len(m.WantsMore) > 0 {
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn,
				Detail: fmt.Sprintf("asks for %s, not granted: it runs without", strings.Join(m.WantsMore, ", "))})
		}
		for _, b := range m.BadSettings {
			out = append(out, Check{Group: g, Name: m.ID, Status: Warn, Detail: "setting " + b})
		}
	}
	if len(r.Orphans) > 0 {
		out = append(out, Check{Group: g, Name: "config.toml", Status: Warn,
			Detail: fmt.Sprintf("[mods.%s] names no installed mod, ignored", strings.Join(r.Orphans, "], [mods."))})
	}
	return out
}
